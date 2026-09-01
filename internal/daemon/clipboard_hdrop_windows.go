//go:build windows

package daemon

import (
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"
)

const cfHDROP = 15

// DroppedImageFile returns the first image file path referenced by a CF_HDROP
// clipboard payload. Explorer-style "copy" of an image file (or its thumbnail)
// puts only a file-path list on the clipboard — no bitmap — so this lets such a
// copy be treated as an upload source. Returns "" when the clipboard holds no
// dropped image file.
func DroppedImageFile() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openClipboard(); err != nil {
		return ""
	}
	defer closeClipboard()

	if !clipboardFormatAvailable(cfHDROP) {
		return ""
	}
	h, _, _ := procGetClipboardData.Call(uintptr(cfHDROP))
	if h == 0 {
		return ""
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return ""
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	raw := make([]byte, int(size))
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&raw[0])), ptr, size)
	for _, p := range parseHDROPFilePaths(raw) {
		if isImageFilePath(p) {
			return p
		}
	}
	return ""
}

func isImageFilePath(p string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(p), ".")) {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "tif", "tiff", "heic":
		return true
	}
	return false
}
