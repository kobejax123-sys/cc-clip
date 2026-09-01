package win32

import (
	"image"
	"image/color"
	"testing"
)

type constImg struct {
	w, h int
	px   map[image.Point]color.NRGBA
}

func (c constImg) ColorModel() color.Model { return color.NRGBAModel }
func (c constImg) Bounds() image.Rectangle { return image.Rect(0, 0, c.w, c.h) }
func (c constImg) At(x, y int) color.Color { return c.px[image.Pt(x, y)] }

func TestEncodeDIBHeader(t *testing.T) {
	img := constImg{w: 2, h: 3, px: map[image.Point]color.NRGBA{}}
	dib := EncodeDIB(img)
	if len(dib) != 40+2*3*4 {
		t.Fatalf("dib len = %d, want %d", len(dib), 40+2*3*4)
	}
	want := map[int]uint32{
		0:  40,        // biSize
		4:  2,         // biWidth
		8:  3,         // biHeight
		16: 0,         // BI_RGB
		20: 2 * 3 * 4, // biSizeImage
	}
	for off, v := range want {
		got := uint32(dib[off]) | uint32(dib[off+1])<<8 | uint32(dib[off+2])<<16 | uint32(dib[off+3])<<24
		if got != v {
			t.Fatalf("header[%d] = %d, want %d", off, got, v)
		}
	}
	if dib[12] != 1 || dib[14] != 32 {
		t.Fatalf("planes/bpp = %d/%d, want 1/32", dib[12], dib[14])
	}
}

func TestEncodeDIBBottomUpBGRA(t *testing.T) {
	img := constImg{w: 2, h: 2, px: map[image.Point]color.NRGBA{
		{0, 0}: {R: 1, G: 2, B: 3, A: 4},     // top-left
		{1, 0}: {R: 5, G: 6, B: 7, A: 8},     // top-right
		{0, 1}: {R: 9, G: 10, B: 11, A: 12},  // bottom-left
		{1, 1}: {R: 13, G: 14, B: 15, A: 16}, // bottom-right
	}}
	dib := EncodeDIB(img)
	px := dib[40:]
	// First stored row must be the image's BOTTOM row, pixels as BGRA.
	want := []byte{
		11, 10, 9, 12, // bottom-left
		15, 14, 13, 16, // bottom-right
		3, 2, 1, 4, // top-left
		7, 6, 5, 8, // top-right
	}
	for i, w := range want {
		if px[i] != w {
			t.Fatalf("px[%d] = %d, want %d", i, px[i], w)
		}
	}
}
