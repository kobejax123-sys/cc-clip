package daemon

import (
	"encoding/base64"
	"testing"
)

func TestParseHTMLDataImagePNG(t *testing.T) {
	repl := base64.StdEncoding.EncodeToString([]byte("AAA"))
	data := []byte(`<html><body><img src="data:image/png;base64,` + repl + `">
<p>hi</p></body></html>`)
	format, img, ok := parseHTMLDataImage(data)
	if !ok {
		t.Fatal("expected to find an embedded image")
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if string(img) != "AAA" {
		t.Fatalf("img = %q, want AAA", img)
	}
}

func TestParseHTMLDataImageJPEGNormalizesToJPG(t *testing.T) {
	data := []byte(`<img src="data:image/jpeg;base64,QUJD">`)
	format, img, ok := parseHTMLDataImage(data)
	if !ok {
		t.Fatal("expected to find an embedded image")
	}
	if format != "jpg" {
		t.Fatalf("format = %q, want jpg", format)
	}
	if string(img) != "ABC" {
		t.Fatalf("img = %q, want ABC (decoded)", img)
	}
}

func TestParseHTMLDataImageHandlesOtherFormats(t *testing.T) {
	for _, tc := range []struct {
		sub  string
		want string
	}{
		{"gif", "gif"},
		{"webp", "webp"},
		{"bmp", "bmp"},
	} {
		data := []byte(`<img src="data:image/` + tc.sub + `;base64,QUJD">`)
		format, _, ok := parseHTMLDataImage(data)
		if !ok {
			t.Fatalf("expected embedded image for %s", tc.sub)
		}
		if format != tc.want {
			t.Fatalf("format = %q, want %q", format, tc.want)
		}
	}
}

func TestParseHTMLDataImageNoEmbeddedImage(t *testing.T) {
	// src with a plain URL, or no img at all, must not be treated as an image.
	for _, html := range []string{
		`<html><body>text only</body></html>`,
		`<img src="https://example.com/x.png">`,
		`<img src="data:text/plain;base64,QUJD">`,
		`<img alt="x">`,
	} {
		if _, _, ok := parseHTMLDataImage([]byte(html)); ok {
			t.Fatalf("expected no embedded image for %q", html)
		}
	}
}

func TestParseHTMLDataImageRejectsBrokenBase64(t *testing.T) {
	if _, _, ok := parseHTMLDataImage([]byte(`<img src="data:image/png;base64,###">`)); ok {
		t.Fatal("expected invalid base64 to be rejected")
	}
}

func TestParseHTMLDataImageFallsBackToStdEncoding(t *testing.T) {
	// base64.StdEncoding requires proper padding; ensure a standard encoding
	// round-trips (this also guards against switching to Raw correctly).
	decoded := []byte("hello")
	enc := base64.StdEncoding.EncodeToString(decoded)
	html := `<img src="data:image/png;base64,` + enc + `">`
	format, img, ok := parseHTMLDataImage([]byte(html))
	if !ok {
		t.Fatal("expected a valid std-encoding to decode")
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if string(img) != "hello" {
		t.Fatalf("img = %q, want hello", img)
	}
}
