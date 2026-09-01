package main

import (
	"strings"
	"testing"
)

func TestSSHUploadAllInOneCmdIncludesExpiry(t *testing.T) {
	cmd := sshUploadAllInOneCmd("~/.cache/cc-clip/uploads", "clip-x.png")
	// The pruned-uploads step must run before the file is written, so a
	// just-uploaded image is never deleted.
	expiry := remoteUploadExpiryCmd()
	wi := strings.Index(cmd, expiry)
	wc := strings.Index(cmd, `cat > "$d/"`)
	if wi < 0 {
		t.Fatalf("expected expiry command in upload script:\n%s", cmd)
	}
	if wc < 0 || wi > wc {
		t.Fatalf("expiry (index %d) must precede file write (index %d); script:\n%s", wi, wc, cmd)
	}
	if !strings.Contains(expiry, "rm -f") {
		t.Fatalf("expiry command should delete files, got %q", expiry)
	}
}

func TestRemoteUploadExpiryCmdAgeConstant(t *testing.T) {
	cmd := remoteUploadExpiryCmd()
	want := "-mmin +" + "1440"
	if !strings.Contains(cmd, want) {
		t.Fatalf("expiry command %q missing %q", cmd, want)
	}
}
