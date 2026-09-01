package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shunmei/cc-clip/internal/shim"
)

// The pooled-upload path keeps one long-lived `ssh <host> <helper>` process per
// remote host alive inside this process (the hotkey loop is long-running, so
// the connection amortizes across pastes). Uploads become writes to that
// process's stdin pipe — no per-paste SSH handshake — which cuts a warm paste
// from roughly the full handshake cost (~0.6s measured) to the raw transfer.
//
// The helper is a POSIX sh loop (no remote install; it arrives as the ssh
// command string) speaking a line protocol on stdin:
//
//	home            -> ok <base64 $HOME>
//	dir <base64abs> -> ok <base64 absdir> (sets upload dir; prunes stale uploads)
//	put <file> <sz> -> consumes exactly sz bytes of payload, then
//	                   ok <base64 abspath> | err <base64 reason>
//	ping            -> pong
//
// Binary payloads stay byte-exact because dash/bash `read` never consumes past
// the newline and `head -c <sz>` consumes exactly the payload bytes that follow.
// Every base64 encode in the helper MUST pass -w0: GNU base64 wraps at 76
// columns by default, and a wrapped reply line poisons the stream — the reader
// parses the first half as a (truncated!) path and the second half surfaces as
// an unexpected line on the NEXT exchange.
//
// Failure handling is what makes this safe for remote reboots: any error on the
// pooled connection (write failure, bad reply, reply timeout) drops it, and the
// next upload dials a fresh connection and retries once. If the retry also
// fails, sshUploadAllInOne falls back to the proven one-shot path, so the paste
// itself never regresses.

const (
	pooledStaleAfter    = 30 * time.Second
	pooledProbeTimeout  = 10 * time.Second
	pooledReplyTimeout  = 60 * time.Second
	pooledPingTimeout   = 3 * time.Second
	pooledMaxSkippedNet = 16
)

var (
	sshPoolMu sync.Mutex
	sshPool   = map[string]*pooledSSHConn{}

	// pooledTiming gates phase-by-phase latency logging (dial vs reuse,
	// protocol exchanges, payload copy). Off by default; set CC_CLIP_TIMING=1
	// when diagnosing paste latency in hotkey.log.
	pooledTiming = os.Getenv("CC_CLIP_TIMING") != ""
)

func pooledLog(format string, args ...any) {
	if pooledTiming {
		log.Printf("pooled: "+format, args...)
	}
}

// sshUploadAllInOne uploads via the pooled connection when possible and falls
// back to a one-shot SSH exec otherwise. Callers are unchanged; one-shot exec
// keeps working for environments where the pooled path cannot be established.
func sshUploadAllInOne(host, remoteDir, localPath, filename string) (string, error) {
	remotePath, err := pooledUpload(host, remoteDir, localPath, filename)
	if err == nil {
		return remotePath, nil
	}
	// This line exists so hotkey.log can answer "why is every paste paying
	// the handshake" when the pooled path is silently broken somewhere.
	log.Printf("pooled upload unavailable, using one-shot ssh: %v", err)
	return sshUploadOneShot(host, remoteDir, localPath, filename)
}

func pooledUpload(host, remoteDir, localPath, filename string) (string, error) {
	c, err := getPooledSSHConn(host, remoteDir)
	if err != nil {
		return "", err
	}
	remotePath, err := c.upload(localPath, filename)
	if err == nil {
		return remotePath, nil
	}
	// The connection died underneath us (remote reboot, network blip, NAT
	// teardown). Drop it, redial, and retry the exact same upload once — the
	// helper overwrites the same filename, so the retry is idempotent.
	pooledLog("first upload attempt failed, reconnecting: %v", err)
	dropPooledSSHConn(host, c)
	c, err = getPooledSSHConn(host, remoteDir)
	if err != nil {
		return "", err
	}
	return c.upload(localPath, filename)
}

func getPooledSSHConn(host, remoteDir string) (*pooledSSHConn, error) {
	sshPoolMu.Lock()
	c := sshPool[host]
	sshPoolMu.Unlock()

	if c != nil && !c.isDead() {
		if err := c.ensureDir(remoteDir); err == nil && time.Since(c.lastUsed) <= pooledStaleAfter {
			pooledLog("reuse conn for %s", host)
			return c, nil
		} else if err == nil {
			// Idle long enough that the far end may have gone away silently.
			// Ping first so a rebooted remote costs a quick redial instead of
			// a full upload attempt into a dead socket.
			if err := c.ping(); err == nil {
				return c, nil
			}
		}
		dropPooledSSHConn(host, c)
	}

	c, err := dialPooledSSH(host, remoteDir)
	if err != nil {
		return nil, err
	}
	sshPoolMu.Lock()
	sshPool[host] = c
	sshPoolMu.Unlock()
	return c, nil
}

func dropPooledSSHConn(host string, c *pooledSSHConn) {
	sshPoolMu.Lock()
	if sshPool[host] == c {
		delete(sshPool, host)
	}
	sshPoolMu.Unlock()
	c.teardown()
}

// closePooledSSH tears down every pooled connection. The hotkey loop calls it
// on exit so the helper's ssh child does not outlive the process.
func closePooledSSH() {
	sshPoolMu.Lock()
	conns := make([]*pooledSSHConn, 0, len(sshPool))
	for h, c := range sshPool {
		conns = append(conns, c)
		delete(sshPool, h)
	}
	sshPoolMu.Unlock()
	for _, c := range conns {
		c.teardown()
	}
}

type pooledSSHConn struct {
	host   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	home   string
	dirRaw string
	dirAbs string

	mu       sync.Mutex
	dead     bool
	lastUsed time.Time
}

func pooledSSHArgs(host string) []string {
	return []string{
		"-o", "ClearAllForwardings=yes",
		"-o", "LogLevel=ERROR",
		// A hidden-window background process can never answer an auth prompt;
		// fail fast into the one-shot fallback instead of hanging invisibly.
		"-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"--", host, shim.WrapRemoteShell(remoteHelperScript()),
	}
}

func dialPooledSSH(host, remoteDir string) (*pooledSSHConn, error) {
	dialStart := time.Now()
	cmd := exec.Command("ssh", pooledSSHArgs(host)...)
	hideConsoleWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("ssh stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ssh stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start pooled ssh to %s: %w", host, err)
	}

	c := &pooledSSHConn{
		host:   host,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 64*1024),
	}
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		c.dead = true
		c.mu.Unlock()
	}()

	home, err := c.exchange(helperHomeLine(), pooledProbeTimeout)
	if err != nil {
		c.teardown()
		return nil, fmt.Errorf("pooled ssh home probe to %s: %w", host, err)
	}
	if !strings.HasPrefix(home, "/") {
		c.teardown()
		return nil, fmt.Errorf("pooled ssh home is not absolute: %q", home)
	}
	c.home = home
	if err := c.ensureDir(remoteDir); err != nil {
		c.teardown()
		return nil, fmt.Errorf("pooled ssh dir setup on %s: %w", host, err)
	}
	pooledLog("dialed %s in %v", host, time.Since(dialStart).Round(time.Millisecond))
	return c, nil
}

// ensureDir pushes the resolved absolute upload dir to the helper whenever it
// differs from what the helper currently holds.
func (c *pooledSSHConn) ensureDir(remoteDir string) error {
	if c.dirRaw == remoteDir {
		return nil
	}
	abs := resolveRemoteDir(c.home, remoteDir)
	if _, err := c.exchange(helperDirLine(abs), pooledProbeTimeout); err != nil {
		return err
	}
	c.dirRaw = remoteDir
	c.dirAbs = abs
	return nil
}

func (c *pooledSSHConn) upload(localPath, filename string) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat local file %s: %w", localPath, err)
	}

	if err := c.sendLine(helperPutLine(filename, info.Size())); err != nil {
		return "", err
	}
	putStart := time.Now()
	if _, err := io.CopyN(c.stdin, f, info.Size()); err != nil {
		return "", fmt.Errorf("payload write failed: %w", err)
	}
	ok, payload, err := c.readReply(pooledReplyTimeout)
	pooledLog("put %s: %d bytes in %v", filename, info.Size(), time.Since(putStart).Round(time.Millisecond))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("remote helper error: %s", payload)
	}
	c.mu.Lock()
	c.lastUsed = time.Now()
	c.mu.Unlock()
	return string(payload), nil
}

func (c *pooledSSHConn) ping() error {
	if err := c.sendLine(helperPingLine()); err != nil {
		return err
	}
	ok, _, err := c.readReply(pooledPingTimeout)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("unexpected ping reply")
	}
	return nil
}

// exchange sends one line and returns the decoded payload of the reply.
func (c *pooledSSHConn) exchange(line string, timeout time.Duration) (string, error) {
	if err := c.sendLine(line); err != nil {
		return "", err
	}
	ok, payload, err := c.readReply(timeout)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("helper rejected %q: %s", line, payload)
	}
	return string(payload), nil
}

func (c *pooledSSHConn) sendLine(line string) error {
	c.mu.Lock()
	dead := c.dead
	c.mu.Unlock()
	if dead {
		return errors.New("pooled ssh connection is dead")
	}
	_, err := io.WriteString(c.stdin, line+"\n")
	return err
}

// readReply reads helper output until a protocol line arrives. Unrecognized
// lines (banner noise that ignored LogLevel, blank lines) are skipped rather
// than treated as fatal, up to a small bound.
func (c *pooledSSHConn) readReply(timeout time.Duration) (bool, []byte, error) {
	type reply struct {
		ok bool
		p  []byte
		e  error
	}
	ch := make(chan reply, 1)
	go func() {
		for skipped := 0; skipped < pooledMaxSkippedNet; skipped++ {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				ch <- reply{e: err}
				return
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			ok, payload, perr := parseHelperLine(line)
			ch <- reply{ok: ok, p: payload, e: perr}
			return
		}
		ch <- reply{e: errors.New("helper protocol noise exceeded limit")}
	}()
	select {
	case r := <-ch:
		return r.ok, r.p, r.e
	case <-time.After(timeout):
		return false, nil, fmt.Errorf("timeout (%v) waiting for helper reply", timeout)
	}
}

func (c *pooledSSHConn) isDead() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dead
}

func (c *pooledSSHConn) teardown() {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// --- protocol line builders and parser (pure, unit-tested) ---

func helperHomeLine() string { return "home" }

func helperPingLine() string { return "ping" }

func helperDirLine(absDir string) string {
	return "dir " + base64.StdEncoding.EncodeToString([]byte(absDir))
}

func helperPutLine(filename string, size int64) string {
	return "put " + filename + " " + strconv.FormatInt(size, 10)
}

func parseHelperLine(line string) (bool, []byte, error) {
	line = strings.TrimRight(line, "\r\n")
	switch {
	case strings.HasPrefix(line, "ok "):
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("ok "):]))
		if err != nil {
			return false, nil, fmt.Errorf("bad ok payload: %w", err)
		}
		return true, b, nil
	case strings.HasPrefix(line, "err "):
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("err "):]))
		if err != nil {
			return false, nil, fmt.Errorf("bad err payload: %w", err)
		}
		return false, b, nil
	case strings.TrimSpace(line) == "pong":
		return true, []byte("pong"), nil
	default:
		return false, nil, fmt.Errorf("unexpected helper line %q", line)
	}
}

// remoteHelperScript is the POSIX sh program the pooled ssh process runs. The
// leading comment doubles as a pkill-friendly marker for diagnostics. It
// deliberately contains no single quotes so WrapRemoteShell's escaping stays a
// plain wrap.
func remoteHelperScript() string {
	return `# cc-clip upload helper
umask 077
while IFS= read -r line; do
  set -- $line
  case $1 in
    home)
      printf "ok %s\n" "$(printf %s "$HOME" | base64 -w0)"
      ;;
    dir)
      d=$(printf %s "$2" | base64 -d)
      find "$d" -type f -mmin +` + strconv.Itoa(remoteUploadMaxAgeMinutes) + ` -exec rm -f {} + 2>/dev/null || true
      printf "ok %s\n" "$(printf %s "$d" | base64 -w0)"
      ;;
    put)
      if mkdir -p "$d" && head -c "$3" > "$d/$2" && test -s "$d/$2"; then
        printf "ok %s\n" "$(printf %s "$d/$2" | base64 -w0)"
      else
        printf "err %s\n" "$(printf %s write_failed | base64 -w0)"
      fi
      ;;
    ping)
      printf "pong\n"
      ;;
  esac
done`
}
