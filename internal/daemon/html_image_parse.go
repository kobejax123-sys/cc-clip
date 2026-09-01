package daemon

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// htmlDataImagePattern matches the first image embedded as a base64 data: URI
// inside CF_HTML clipboard content (for example
// `<img src="data:image/png;base64,...">`). Apps that "copy an image"
// (browsers, rich-text editors) often publish only HTML and never a DIB, so
// this branch is what lets such copies be treated as an image. It lives in a
// build-tag-free file so the parse function (and its test) compile and run on
// every platform, not just Windows.
var htmlDataImagePattern = regexp.MustCompile(`(?i)src\s*=\s*["']data:image/(png|jpe?g|gif|webp|bmp);base64,([A-Za-z0-9+/=]+)["']`)

// parseHTMLDataImage extracts the first image embedded as a base64 data: URI
// from CF_HTML clipboard bytes, returning its format (png/jpg/gif/webp/bmp) and
// decoded bytes. It is a pure function so it can be unit-tested without
// touching a real clipboard; the Windows clipboard reader calls it after
// reading the "HTML Format" payload.
func parseHTMLDataImage(html []byte) (format string, data []byte, ok bool) {
	m := htmlDataImagePattern.FindSubmatch(html)
	if m == nil {
		return "", nil, false
	}
	sub := strings.ToLower(string(m[1]))
	if sub == "jpeg" {
		sub = "jpg"
	}
	decoded, err := base64.StdEncoding.DecodeString(string(m[2]))
	if err != nil || len(decoded) == 0 {
		return "", nil, false
	}
	return sub, decoded, true
}
