package main

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

func TestHelperPutLine(t *testing.T) {
	got := helperPutLine("clip-20260101-000000-abcd.png", 1234567)
	want := "put clip-20260101-000000-abcd.png 1234567"
	if got != want {
		t.Fatalf("helperPutLine = %q, want %q", got, want)
	}
}

func TestHelperDirLineRoundTrip(t *testing.T) {
	dir := `/workspaces1/yuehui.xu/.cache/cc-clip/uploads`
	line := helperDirLine(dir)
	if !strings.HasPrefix(line, "dir ") {
		t.Fatalf("dir line %q missing prefix", line)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "dir "))
	if err != nil {
		t.Fatalf("dir payload is not base64: %v", err)
	}
	if string(decoded) != dir {
		t.Fatalf("decoded dir = %q, want %q", decoded, dir)
	}
}

func TestHelperDirLineHandlesSpaces(t *testing.T) {
	dir := `/home/some user/uploads with space`
	line := helperDirLine(dir)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "dir "))
	if err != nil {
		t.Fatalf("dir payload is not base64: %v", err)
	}
	if string(decoded) != dir {
		t.Fatalf("decoded dir = %q, want %q", decoded, dir)
	}
	if strings.Contains(line, " ") && strings.Count(strings.TrimPrefix(line, "dir "), " ") != 0 {
		t.Fatalf("base64 payload must not contain spaces (protocol uses space as field separator): %q", line)
	}
}

func TestParseHelperLineOK(t *testing.T) {
	payload := `/tmp/clip-x.png`
	ok, got, err := parseHelperLine("ok " + base64.StdEncoding.EncodeToString([]byte(payload)))
	if err != nil || !ok {
		t.Fatalf("parseHelperLine = (%t, %v)", ok, err)
	}
	if string(got) != payload {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestParseHelperLineErr(t *testing.T) {
	ok, got, err := parseHelperLine("err " + base64.StdEncoding.EncodeToString([]byte("write_failed")))
	if err != nil {
		t.Fatalf("parseHelperLine error: %v", err)
	}
	if ok {
		t.Fatal("err line must not parse as ok")
	}
	if string(got) != "write_failed" {
		t.Fatalf("payload = %q, want write_failed", got)
	}
}

func TestParseHelperLinePong(t *testing.T) {
	ok, payload, err := parseHelperLine("pong\n")
	if err != nil || !ok {
		t.Fatalf("parseHelperLine = (%t, %v)", ok, err)
	}
	if string(payload) != "pong" {
		t.Fatalf("payload = %q, want pong", payload)
	}
}

func TestParseHelperLineRejectsGarbage(t *testing.T) {
	for _, line := range []string{"", "hello", "ok !!!", "err !!!", "ok"} {
		if _, _, err := parseHelperLine(line); err == nil {
			t.Fatalf("parseHelperLine(%q) unexpectedly succeeded", line)
		}
	}
}

func TestRemoteHelperScriptProtocol(t *testing.T) {
	s := remoteHelperScript()
	for _, want := range []string{
		"umask 077",
		"IFS= read -r line",
		"head -c \"$3\"",
		"base64 -d",
		// GNU base64 wraps at 76 columns unless -w0; a wrapped reply line
		// corrupts the stream (half lands in the next read).
		"base64 -w0",
		"test -s",
		"-mmin +" + strconv.Itoa(remoteUploadMaxAgeMinutes),
		"cc-clip upload helper",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("helper script missing %q", want)
		}
	}
	if strings.Contains(s, "'") {
		t.Fatalf("helper script must stay single-quote-free for WrapRemoteShell:\n%s", s)
	}
}
