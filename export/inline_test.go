package export

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeSixel(t *testing.T) {
	// raster 4x12, red (RGB percent) then blue (HLS hue 0 is blue), repeat, next band,
	// back to its start ($) and red over the second column
	img := decodeSixel(`"1;1;4;12#1;2;100;0;0#1!4~-#2;1;0;50;100~~$#1?~`)
	if img == nil || img.Rect.Dx() != 4 || img.Rect.Dy() != 12 {
		t.Fatalf("image = %v", img)
	}
	red, blue := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}
	for _, c := range []struct {
		x, y int
		want color.NRGBA
	}{{0, 0, red}, {3, 5, red}, {0, 6, blue}, {1, 11, red}, {2, 6, color.NRGBA{}}} {
		if got := img.NRGBAAt(c.x, c.y); got != c.want {
			t.Errorf("pixel %d,%d = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestSixelPlacement(t *testing.T) {
	// six bands are 16x36 pixels: 2x3 cells of 8x16; the cursor goes below the image
	c := kittyCast("ab\x1bPq#1;2;100;0;0#1!16~-!16~-!16~-!16~-!16~-!16~\x1b\\X")
	s := ScreenAt(c, -1, Options{})
	for _, xy := range [][2]int{{2, 0}, {3, 0}, {2, 2}, {3, 2}} {
		if s.At(xy[0], xy[1]).Image == nil {
			t.Fatalf("no image at %v", xy)
		}
	}
	if s.At(2, 3).Char != 'X' {
		t.Fatalf("cursor not below the image: %q", s.At(2, 3).Char)
	}
}

func TestOtherDCSPassesThrough(t *testing.T) {
	// DECRQSS is a DCS too, but not Sixel; text after it must still show
	s := ScreenAt(kittyCast("\x1bP$qm\x1b\\ok"), -1, Options{})
	if s.At(0, 0).Char != 'o' || s.At(1, 0).Char != 'k' {
		t.Fatalf("screen starts %q%q", s.At(0, 0).Char, s.At(1, 0).Char)
	}
}

func TestITermImage(t *testing.T) {
	var b bytes.Buffer
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	_ = png.Encode(&b, src)
	data := base64.StdEncoding.EncodeToString(b.Bytes())
	// width 4 cells, aspect kept: 32x32 pixels in 8x16 cells is 4x2; split across events
	osc := "\x1b]1337;File=name=eC5wbmc=;width=4;inline=1:" + data + "\x07!"
	s := ScreenAt(kittyCast(osc[:7], osc[7:20], osc[20:]), -1, Options{})
	p := s.At(0, 0).Image
	if p == nil || p.Cols != 4 || p.Rows != 2 {
		t.Fatalf("placement = %+v", p)
	}
	if s.At(4, 1).Char != '!' {
		t.Fatalf("cursor not after the image: %q", s.At(4, 1).Char)
	}
	// without inline=1 it is a download, not shown
	s = ScreenAt(kittyCast("\x1b]1337;File=size=3:"+data+"\x07"), -1, Options{})
	if s.At(0, 0).Image != nil {
		t.Fatal("download shown as an image")
	}
}
