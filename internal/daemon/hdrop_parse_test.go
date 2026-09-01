package daemon

import (
	"bytes"
	"reflect"
	"testing"
	"unicode/utf16"
)

// buildHDROP constructs a CF_HDROP payload: a DROPFILES header whose pFiles
// points just past the header, followed by the UTF-16LE double-NUL path list.
func buildHDROP(t *testing.T, paths ...string) []byte {
	t.Helper()
	const pFiles = 20 // sizeof(DROPFILES)
	var buf bytes.Buffer
	hdr := make([]byte, pFiles)
	hdr[0] = byte(pFiles)
	hdr[1] = byte(pFiles >> 8)
	hdr[2] = byte(pFiles >> 16)
	hdr[3] = byte(pFiles >> 24)
	hdr[16] = 1 // fWide: paths are wide (UTF-16)
	buf.Write(hdr)
	for _, p := range paths {
		for _, u := range utf16.Encode([]rune(p)) {
			buf.WriteByte(byte(u))
			buf.WriteByte(byte(u >> 8))
		}
		buf.WriteByte(0)
		buf.WriteByte(0) // path NUL terminator
	}
	buf.WriteByte(0)
	buf.WriteByte(0) // array terminator
	return buf.Bytes()
}

func TestParseHDROPFilePathsSingle(t *testing.T) {
	got := parseHDROPFilePaths(buildHDROP(t, `C:\one.png`))
	if !reflect.DeepEqual(got, []string{`C:\one.png`}) {
		t.Fatalf("got %#v, want [C:\\one.png]", got)
	}
}

func TestParseHDROPFilePathsMultiple(t *testing.T) {
	got := parseHDROPFilePaths(buildHDROP(t, `C:\a.png`, `D:\b\c.jpg`))
	want := []string{`C:\a.png`, `D:\b\c.jpg`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseHDROPFilePathsSupportsUnicode(t *testing.T) {
	got := parseHDROPFilePaths(buildHDROP(t, `C:\图 片.png`))
	if !reflect.DeepEqual(got, []string{`C:\图 片.png`}) {
		t.Fatalf("got %#v, want unicode path", got)
	}
}

func TestParseHDROPFilePathsEmptyPayload(t *testing.T) {
	if got := parseHDROPFilePaths(nil); got != nil {
		t.Fatalf("nil payload = %#v, want nil", got)
	}
	if got := parseHDROPFilePaths([]byte{1, 2}); got != nil {
		t.Fatalf("short payload = %#v, want nil", got)
	}
}

func TestParseHDROPFilePathsBadOffset(t *testing.T) {
	raw := buildHDROP(t, `C:\a.png`)
	// Corrupt pFiles to an out-of-range offset -> no panic, no paths.
	raw[0] = 0xfe
	raw[1] = 0xff
	raw[2] = 0xff
	raw[3] = 0x7f
	if got := parseHDROPFilePaths(raw); got != nil {
		t.Fatalf("out-of-range pFiles = %#v, want nil", got)
	}
}
