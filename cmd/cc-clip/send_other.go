//go:build !windows

package main

import (
	"fmt"
	"time"
)

func defaultRemoteHost() (string, bool, error) {
	return "", false, nil
}

func pasteRemotePath(remotePath, imagePath string, tempFile bool, delay time.Duration, restoreClipboard bool) error {
	_ = tempFile
	return fmt.Errorf("--paste is only supported on Windows")
}
