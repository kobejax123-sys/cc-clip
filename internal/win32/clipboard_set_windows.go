//go:build windows

package win32

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procSetOpenClipboard        = user32DLL.NewProc("OpenClipboard")
	procSetCloseClipboard       = user32DLL.NewProc("CloseClipboard")
	procSetEmptyClipboard       = user32DLL.NewProc("EmptyClipboard")
	procSetClipboardData        = user32DLL.NewProc("SetClipboardData")
	procSetRegisterClipboardFmt = user32DLL.NewProc("RegisterClipboardFormatW")
	procSetGlobalAlloc          = kernel32DLL.NewProc("GlobalAlloc")
	procSetGlobalLock           = kernel32DLL.NewProc("GlobalLock")
	procSetGlobalUnlock         = kernel32DLL.NewProc("GlobalUnlock")
	procSetGlobalFree           = kernel32DLL.NewProc("GlobalFree")
)

const (
	setCfUnicodeText = 13
	setCfDIB         = 8
	setGmemMoveable  = 0x0002
	setPngFormatName = "PNG"
)

var procSetRtlMoveMemory = kernel32DLL.NewProc("RtlMoveMemory")

// SetClipboardText atomically replaces the clipboard with a CF_UNICODETEXT
// payload. The system owns the allocated memory, so the text survives this
// process exiting — natively, with no PowerShell round-trip.
func SetClipboardText(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	u16, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("text contains NUL: %w", err)
	}
	blob := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(blob[i*2:], v)
	}

	if err := openSetClipboard(); err != nil {
		return err
	}
	defer closeSetClipboard()
	emptySetClipboard()
	return setSetClipboardData(setCfUnicodeText, blob)
}

// SetClipboardImage installs a PNG (registered "PNG" format, when png is
// non-nil) and a 32-bit CF_DIB rendering of the same picture in one clipboard
// transaction. Terminals and image tools differ on which format they read;
// publishing both covers the common consumers.
func SetClipboardImage(png, dib []byte) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openSetClipboard(); err != nil {
		return err
	}
	defer closeSetClipboard()
	emptySetClipboard()
	if len(png) > 0 {
		if err := setSetClipboardData(registeredSetFormat(setPngFormatName), png); err != nil {
			return err
		}
	}
	return setSetClipboardData(setCfDIB, dib)
}

func openSetClipboard() error {
	r1, _, err := procSetOpenClipboard.Call(0)
	if r1 == 0 {
		return fmt.Errorf("OpenClipboard failed: %v", err)
	}
	return nil
}

func closeSetClipboard() { procSetCloseClipboard.Call() }

func emptySetClipboard() { procSetEmptyClipboard.Call() }

// setSetClipboardData hands one format to the clipboard. On success the
// system owns the HGLOBAL; on failure we must free it ourselves.
func setSetClipboardData(format uint32, data []byte) error {
	h, _, err := procSetGlobalAlloc.Call(uintptr(setGmemMoveable), uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc failed: %v", err)
	}
	p, _, err := procSetGlobalLock.Call(h)
	if p == 0 {
		procSetGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed: %v", err)
	}
	procSetRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procSetGlobalUnlock.Call(h)

	r1, _, err := procSetClipboardData.Call(uintptr(format), h)
	if r1 == 0 {
		procSetGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData(%d) failed: %v", format, err)
	}
	return nil
}

func registeredSetFormat(name string) uint32 {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	r1, _, _ := procSetRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r1)
}
