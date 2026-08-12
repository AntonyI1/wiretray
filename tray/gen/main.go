// Command gen writes the tray's ICO assets. Run from the repo root:
//
//	go run ./tray/gen
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
)

const size = 32

var states = map[string]color.NRGBA{
	"disconnected": {R: 0x8a, G: 0x8a, B: 0x8a, A: 0xff},
	"connecting":   {R: 0xd9, G: 0x9a, B: 0x1b, A: 0xff},
	"connected":    {R: 0x2f, G: 0xa0, B: 0x5a, A: 0xff},
	"error":        {R: 0xc2, G: 0x3b, B: 0x2e, A: 0xff},
}

func main() {
	outDir := filepath.Join("tray", "icons")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}
	for name, c := range states {
		path := filepath.Join(outDir, name+".ico")
		if err := os.WriteFile(path, ico(dot(c)), 0o644); err != nil {
			fail(err)
		}
	}
	fmt.Println("icons written to", outDir)
}

// dot draws a filled circle with soft edges on a transparent square.
func dot(c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	center, radius := float64(size-1)/2, 11.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-center, float64(y)-center)
			a := (radius + 0.5 - d) * 255
			if a <= 0 {
				continue
			}
			if a > 255 {
				a = 255
			}
			img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(a)})
		}
	}
	return img
}

// ico wraps one 32bpp BGRA bitmap in the classic ICO container: icon
// directory, one entry, BITMAPINFOHEADER with doubled height, bottom-up
// pixel rows, then the legacy AND mask.
func ico(img *image.NRGBA) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	put16 := func(v uint16) { _ = binary.Write(&b, le, v) }
	put32 := func(v uint32) { _ = binary.Write(&b, le, v) }

	xorSize := size * size * 4
	andSize := size * (size / 8)

	put16(0) // reserved
	put16(1) // type: icon
	put16(1) // image count

	b.WriteByte(size)
	b.WriteByte(size)
	b.WriteByte(0) // palette colors: none
	b.WriteByte(0) // reserved
	put16(1)       // color planes
	put16(32)      // bits per pixel
	put32(uint32(40 + xorSize + andSize))
	put32(22) // data offset: 6 byte dir + 16 byte entry

	put32(40) // BITMAPINFOHEADER size
	put32(uint32(size))
	put32(uint32(size * 2)) // height counts XOR and AND blocks together
	put16(1)                // planes
	put16(32)               // bpp
	put32(0)                // compression: none
	put32(0)                // image size: valid as 0 when uncompressed
	put32(0)                // x pixels per meter
	put32(0)                // y pixels per meter
	put32(0)                // colors used
	put32(0)                // important colors

	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, andSize))
	return b.Bytes()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
