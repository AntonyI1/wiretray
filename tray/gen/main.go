// Command gen writes the tray's ICO assets and the README logo. Run
// from the repo root:
//
//	go run ./tray/gen
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// The mark is the split tunnel drawn as a dot constellation: one path
// of dots forking into two. States light the dots differently, the way
// the tunnel actually comes up: gray when down, stem lit while
// connecting, everything lit when connected, red on error.

type spot struct {
	x, y, r float64
	stem    bool // lights up first while connecting
}

// The dots sit at the density Tailscale's tray icon uses (radius 0.10
// of the canvas, centers 0.30 or more apart), which is what reads
// calmly in a Windows tray. The arrangement is ours: a path forking.
var lattice = []spot{
	{0.20, 0.50, 0.10, true},
	{0.50, 0.50, 0.10, true},
	{0.80, 0.20, 0.10, false},
	{0.80, 0.80, 0.10, false},
}

type palette struct {
	stem, branch color.NRGBA
}

var (
	lit   = color.NRGBA{R: 0xf2, G: 0xf2, B: 0xf2, A: 0xff}
	dim   = color.NRGBA{R: 0x8a, G: 0x8a, B: 0x8a, A: 0xb4}
	red   = color.NRGBA{R: 0xd9, G: 0x50, B: 0x44, A: 0xff}
	green = color.NRGBA{R: 0x2f, G: 0xa0, B: 0x5a, A: 0xff}
)

var states = map[string]palette{
	"disconnected": {stem: dim, branch: dim},
	"connecting":   {stem: lit, branch: dim},
	"connected":    {stem: lit, branch: lit},
	"error":        {stem: red, branch: red},
}

func main() {
	outDir := filepath.Join("tray", "icons")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}

	// 16 through 48 covers Windows tray rendering at common DPI scales,
	// so the shell always finds an exact size instead of resampling.
	sizes := []int{16, 20, 24, 32, 48}
	for name, pal := range states {
		var imgs []*image.NRGBA
		for _, sz := range sizes {
			imgs = append(imgs, render(sz, pal))
		}
		path := filepath.Join(outDir, name+".ico")
		if err := os.WriteFile(path, ico(imgs), 0o644); err != nil {
			fail(err)
		}
	}

	// The executable's own icon (Explorer, file properties) ships as
	// PNGs that go-winres packs into the resource section: full mark at
	// 256, the reduced layout at 16.
	for _, v := range []struct {
		name string
		size int
	}{{"app-256.png", 256}, {"app-16.png", 16}} {
		f, err := os.Create(filepath.Join(outDir, v.name))
		if err != nil {
			fail(err)
		}
		if err := png.Encode(f, render(v.size, palette{stem: green, branch: green})); err != nil {
			fail(err)
		}
		if err := f.Close(); err != nil {
			fail(err)
		}
	}

	// The README logo is the same constellation in brand green, which
	// reads on both light and dark backgrounds.
	f, err := os.Create(filepath.Join("docs", "logo.png"))
	if err != nil {
		fail(err)
	}
	if err := png.Encode(f, render(256, palette{stem: green, branch: green})); err != nil {
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}

	fmt.Println("icons and logo written")
}

func render(size int, pal palette) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			var best float64
			var c color.NRGBA
			for _, sp := range lattice {
				d := math.Hypot(fx-sp.x*s, fy-sp.y*s)
				a := (sp.r*s + 0.5 - d) * 255
				if a > best {
					best = a
					if sp.stem {
						c = pal.stem
					} else {
						c = pal.branch
					}
				}
			}
			if best <= 0 {
				continue
			}
			if best > 255 {
				best = 255
			}
			alpha := uint8(best * float64(c.A) / 255)
			img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: alpha})
		}
	}
	return img
}

// ico packs images as one multi-size ICO: directory, one entry per
// image, then each image as a classic 32bpp DIB with its AND mask.
func ico(imgs []*image.NRGBA) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	put16 := func(v uint16) { _ = binary.Write(&b, le, v) }
	put32 := func(v uint32) { _ = binary.Write(&b, le, v) }

	put16(0)
	put16(1)
	put16(uint16(len(imgs)))

	var dibs [][]byte
	for _, img := range imgs {
		dibs = append(dibs, dib(img))
	}

	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		w := img.Bounds().Dx()
		b.WriteByte(byte(w % 256)) // 256 encodes as 0
		b.WriteByte(byte(w % 256))
		b.WriteByte(0)
		b.WriteByte(0)
		put16(1)
		put16(32)
		put32(uint32(len(dibs[i])))
		put32(uint32(offset))
		offset += len(dibs[i])
	}
	for _, d := range dibs {
		b.Write(d)
	}
	return b.Bytes()
}

func dib(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	andStride := ((w + 31) / 32) * 4 // AND mask rows pad to 4 bytes

	var b bytes.Buffer
	le := binary.LittleEndian
	put16 := func(v uint16) { _ = binary.Write(&b, le, v) }
	put32 := func(v uint32) { _ = binary.Write(&b, le, v) }

	put32(40)
	put32(uint32(w))
	put32(uint32(h * 2))
	put16(1)
	put16(32)
	put32(0)
	put32(0)
	put32(0)
	put32(0)
	put32(0)
	put32(0)

	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, h*andStride))
	return b.Bytes()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
