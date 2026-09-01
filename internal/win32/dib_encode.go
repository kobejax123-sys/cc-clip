package win32

import (
	"image"
	"image/color"

	"encoding/binary"
)

// EncodeDIB renders img as a 32-bit BI_RGB DIB: a 40-byte BITMAPINFOHEADER
// followed by bottom-up BGRA rows. This is the payload of the CF_DIB
// clipboard format. Rows at 32bpp are always dword-aligned, so no row padding
// is needed.
func EncodeDIB(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	header := 40
	out := make([]byte, header+w*h*4)
	binary.LittleEndian.PutUint32(out[0:4], 40)
	binary.LittleEndian.PutUint32(out[4:8], uint32(w))
	binary.LittleEndian.PutUint32(out[8:12], uint32(h))
	binary.LittleEndian.PutUint16(out[12:14], 1)
	binary.LittleEndian.PutUint16(out[14:16], 32)
	binary.LittleEndian.PutUint32(out[16:20], 0) // BI_RGB
	binary.LittleEndian.PutUint32(out[20:24], uint32(w*h*4))

	px := out[header:]
	for y := 0; y < h; y++ {
		row := h - 1 - y // bottom-up: the first stored row is the bottom one
		for x := 0; x < w; x++ {
			// NRGBA conversion yields straight (non-premultiplied) 8-bit
			// channels, which is what 32bpp DIBs conventionally store.
			n := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			o := (row*w + x) * 4
			px[o] = n.B
			px[o+1] = n.G
			px[o+2] = n.R
			px[o+3] = n.A
		}
	}
	return out
}
