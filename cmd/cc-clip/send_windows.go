//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"sync"
	"time"

	"github.com/shunmei/cc-clip/internal/win32"
)

// systemFocusProbe is the production focusProbe. It lives here rather than
// alongside the guard logic because Windows is the only platform that can
// answer the question, and the guard's decision logic stays build-tag-free so
// it remains testable everywhere.
func systemFocusProbe() focusResult {
	h, ok := win32.ForegroundWindow()
	return focusResult{Handle: h, Known: ok}
}

func defaultRemoteHost() (string, bool, error) {
	cfg, ok, err := loadHotkeyConfig()
	if err != nil {
		return "", false, err
	}
	if !ok || cfg.Host == "" {
		return "", false, nil
	}
	return cfg.Host, true, nil
}

// clipboardRestoreMu serializes the background clipboard-image restore
// against the next paste's clipboard writes: without it, a rapid second press
// could have its just-written remote-path text clobbered by the previous
// paste's image restore, and the terminal would paste the image bytes.
var clipboardRestoreMu sync.Mutex

const clipboardRestoreWait = 2500 * time.Millisecond

// waitForPendingRestore waits (bounded) for any in-flight background restore
// to finish. On timeout it proceeds anyway — the alternative, waiting
// unbounded, lets one hung powershell.exe stall every future paste.
func waitForPendingRestore() {
	deadline := time.Now().Add(clipboardRestoreWait)
	for {
		if clipboardRestoreMu.TryLock() {
			clipboardRestoreMu.Unlock()
			return
		}
		if time.Now().After(deadline) {
			log.Printf("clipboard restore still in flight after %v; proceeding anyway", clipboardRestoreWait)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pasteRemotePath writes the remote path to the clipboard and injects it with
// a synthesized Ctrl+Shift+V. It returns as soon as the keystroke has fired —
// the image-clipboard restore (a fresh PowerShell process, ~1s measured) runs
// in the background and deletes imagePath afterwards when tempFile is set.
// Callers must therefore not delete imagePath themselves on the success path.
func pasteRemotePath(remotePath, imagePath string, tempFile bool, delay time.Duration, restoreClipboard bool) error {
	// A previous paste's background restore may still be running; it must
	// finish (or be abandoned) before this paste's path text lands on the
	// clipboard.
	waitForPendingRestore()

	// Pin the window this paste is aimed at BEFORE touching the clipboard.
	// The keystroke below goes to whatever is focused when it fires. Without
	// this guard a window switch during `delay` delivers the remote path into
	// whatever the user moved to — a password manager, a chat box, a browser
	// URL bar.
	guard, err := newFocusGuard(systemFocusProbe)
	if err != nil {
		return err
	}

	if err := windowsSetClipboardText(remotePath); err != nil {
		return err
	}

	if delay > 0 {
		time.Sleep(delay)
	}

	if err := guard.verify(); err != nil {
		// Withhold the keystroke. cc-clip never snapshots the user's prior
		// clipboard, so the most we can undo is our own text write — put the
		// image back when the caller asked for restoration.
		if restoreClipboard {
			if restoreErr := windowsSetClipboardImage(imagePath); restoreErr != nil {
				return fmt.Errorf("%w (clipboard restore also failed: %v)", err, restoreErr)
			}
		}
		return err
	}

	if err := windowsSendCtrlShiftV(); err != nil {
		return err
	}

	if !restoreClipboard {
		if tempFile {
			os.Remove(imagePath)
		}
		return nil
	}

	go func() {
		// Give the keystroke's paste a moment to land before the clipboard
		// gets the image back. The restore goes through a fresh PowerShell
		// process (~1s measured), which is exactly why it runs in the
		// background — the caller reports success the moment the paste fires.
		time.Sleep(150 * time.Millisecond)
		clipboardRestoreMu.Lock()
		defer clipboardRestoreMu.Unlock()
		if err := windowsSetClipboardImage(imagePath); err != nil {
			log.Printf("paste succeeded but background clipboard restore failed: %v", err)
		}
		if tempFile {
			os.Remove(imagePath)
		}
	}()

	return nil
}

// windowsSetClipboardText installs the paste path natively. With
// SetClipboardData the SYSTEM owns the payload, so it survives this process
// by construction — the PowerShell SetDataObject($true)+OleFlushClipboard
// dance existed only because a managed DataObject's lifetime is tied to its
// process. Spawning powershell.exe cost ~370ms per paste, measured.
func windowsSetClipboardText(text string) error {
	if err := win32.SetClipboardText(text); err != nil {
		return fmt.Errorf("failed to set text clipboard: %w", err)
	}
	return nil
}

// windowsSetClipboardImage puts the image back on the clipboard natively.
// The temp file carries whatever the clipboard held — PNG most often, but
// browsers copying images hand over JPEG via CF_HTML — so decoding sniffs
// the real format instead of assuming PNG. A PNG payload additionally goes
// to the registered "PNG" format; the DIB rendering covers consumers that
// only read CF_DIB. Also ~1s of PowerShell process spawn per paste, now gone.
func windowsSetClipboardImage(imagePath string) error {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("failed to read clipboard restore image: %w", err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("clipboard restore image is not decodable: %w", err)
	}
	var pngBytes []byte
	if format == "png" {
		pngBytes = data
	}
	if err := win32.SetClipboardImage(pngBytes, win32.EncodeDIB(img)); err != nil {
		return fmt.Errorf("failed to restore image clipboard: %w", err)
	}
	return nil
}

// windowsSendCtrlShiftV delivers the paste keystroke via SendInput.
//
// It used to shell out to WinForms SendKeys, which Electron/Chromium terminals
// (Wave, Hyper, Tabby, VS Code's integrated terminal) ignore outright while
// SendWait still returned success — so a paste that never happened was
// reported as a success by the log, the tray balloon and the exit code
// (issue #140). win32.SendCtrlShiftV checks how many events the input stream
// actually accepted, so a refusal surfaces as an error.
func windowsSendCtrlShiftV() error {
	if err := win32.SendCtrlShiftV(); err != nil {
		return fmt.Errorf("failed to send Ctrl+Shift+V: %w", err)
	}
	return nil
}
