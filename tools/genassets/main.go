// genassets renders the TV launcher banner and app icons as PNGs so the
// repo does not need image-editing tools. Run: go run . ../../tv/app/src/main/res
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	ink  = color.RGBA{0x0F, 0x12, 0x16, 0xFF}
	lime = color.RGBA{0xB5, 0xD9, 0x6A, 0xFF}
	fog  = color.RGBA{0xD9, 0xDE, 0xE5, 0xFF}
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genassets <res dir>")
		os.Exit(2)
	}
	res := os.Args[1]
	must(write(filepath.Join(res, "mipmap-xhdpi", "banner.png"), banner(320, 180, 2)))
	must(write(filepath.Join(res, "mipmap-xxhdpi", "banner.png"), banner(480, 270, 3)))
	must(write(filepath.Join(res, "mipmap-xxxhdpi", "banner.png"), banner(640, 360, 4)))
	must(write(filepath.Join(res, "mipmap-xhdpi", "ic_launcher.png"), icon(96)))
	must(write(filepath.Join(res, "mipmap-xxhdpi", "ic_launcher.png"), icon(144)))
	must(write(filepath.Join(res, "mipmap-xxxhdpi", "ic_launcher.png"), icon(192)))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func write(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// glyph draws the mark: a down-arrow landing on a tray, the "load onto TV" idea.
func glyph(img *image.RGBA, cx, cy, s float64, c color.RGBA) {
	// arrow shaft
	fillRect(img, cx-s*0.12, cy-s*0.55, cx+s*0.12, cy+s*0.05, c)
	// arrow head (triangle)
	for y := cy - s*0.05; y <= cy+s*0.3; y++ {
		t := (y - (cy - s*0.05)) / (s * 0.35)
		hw := s * 0.38 * (1 - t)
		fillRect(img, cx-hw, y, cx+hw, y+1, c)
	}
	// tray
	fillRect(img, cx-s*0.5, cy+s*0.38, cx-s*0.42, cy+s*0.6, c)
	fillRect(img, cx+s*0.42, cy+s*0.38, cx+s*0.5, cy+s*0.6, c)
	fillRect(img, cx-s*0.5, cy+s*0.52, cx+s*0.5, cy+s*0.6, c)
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	for y := int(math.Floor(y0)); y < int(math.Ceil(y1)); y++ {
		for x := int(math.Floor(x0)); x < int(math.Ceil(x1)); x++ {
			if (image.Point{x, y}).In(img.Rect) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func banner(w, h int, scale float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, ink)
		}
	}
	// subtle lighter band on the left third
	for y := 0; y < h; y++ {
		for x := 0; x < w/4; x++ {
			img.SetRGBA(x, y, color.RGBA{0x1A, 0x1F, 0x26, 0xFF})
		}
	}
	glyph(img, float64(w)/8, float64(h)/2-2*scale, 38*scale, lime)
	must(label(img, "ATV Loader", float64(w)/4+16*scale, float64(h)/2+8*scale, 17*scale, fog))
	return img
}

func icon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	r := s * 0.2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inRoundRect(float64(x)+0.5, float64(y)+0.5, 0, 0, s, s, r) {
				img.SetRGBA(x, y, ink)
			}
		}
	}
	glyph(img, s/2, s/2-s*0.04, s*0.62, lime)
	return img
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return math.Hypot(x-cx, y-cy) <= r
}

func label(img *image.RGBA, text string, x, y, size float64, c color.RGBA) error {
	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(int(x), int(y))}
	d.DrawString(text)
	return nil
}
