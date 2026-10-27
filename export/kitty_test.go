package export

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

func apc(keys string, data []byte) string {
	return "\x1b_G" + keys + ";" + base64.StdEncoding.EncodeToString(data) + "\x1b\\"
}

// solid returns w x h RGBA pixels of one color.
func solid(w, h int, c color.RGBA) []byte {
	return bytes.Repeat([]byte{c.R, c.G, c.B, c.A}, w*h)
}

func kittyCast(events ...string) *cast.Cast {
	c := cast.New(10, 4)
	for _, e := range events {
		c.Events = append(c.Events, cast.Event{Delay: 0.1, Code: cast.Output, Data: e})
	}
	return c
}

var red = color.RGBA{255, 0, 0, 255}

func TestKittyDirectPlacement(t *testing.T) {
	c := kittyCast("ab", apc("a=T,f=32,s=2,v=2,c=2,r=2,i=7", solid(2, 2, red)), "X")
	s := ScreenAt(c, -1, Options{})
	for _, xy := range [][2]int{{2, 0}, {3, 0}, {2, 1}, {3, 1}} {
		cell := s.At(xy[0], xy[1])
		if cell.Image == nil || cell.Char != ' ' {
			t.Fatalf("cell %v = %+v, want image", xy, cell)
		}
		if int(cell.ImageCol) != xy[0]-2 || int(cell.ImageRow) != xy[1] {
			t.Fatalf("cell %v shows piece %d,%d", xy, cell.ImageCol, cell.ImageRow)
		}
	}
	if s.At(4, 1).Char != 'X' { // the cursor moved past the image
		t.Fatalf("text after image at wrong place: %q", s.At(4, 1).Char)
	}
}

func TestKittyNoCursorMove(t *testing.T) {
	c := kittyCast(apc("a=T,f=32,s=1,v=1,c=2,r=2,C=1", solid(1, 1, red)), "X")
	s := ScreenAt(c, -1, Options{})
	if s.At(0, 0).Char != 'X' || s.At(1, 0).Image == nil {
		t.Fatal("C=1 should leave the cursor on the image")
	}
}

func TestKittyDelete(t *testing.T) {
	c := kittyCast(apc("a=T,f=32,s=1,v=1,c=1,r=1,i=3", solid(1, 1, red)), "\x1b_Ga=d,d=i,i=3\x1b\\")
	s := ScreenAt(c, -1, Options{})
	if cell := s.At(0, 0); cell.Image != nil || cell.Char != ' ' {
		t.Fatalf("deleted image still shown: %+v", cell)
	}
}

// TestKittyPlaceholders follows what ntcharts sends: a chunked, zlib compressed
// transmission for a virtual placement, then placeholder cells colored with the id.
func TestKittyPlaceholders(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(bytes.Repeat([]byte{0, 0, 255}, 4*2))
	_ = zw.Close()
	payload := base64.StdEncoding.EncodeToString(z.Bytes())
	half := len(payload) / 2
	first := "\x1b_Ga=T,U=1,f=24,o=z,s=4,v=2,i=258,c=3,r=2,q=2,m=1;" + payload[:half] + "\x1b\\"
	second := "\x1b_Gm=0;" + payload[half:] + "\x1b\\"
	d := kittyDiacritics
	grid := "\x1b[38;2;0;1;2m" +
		// the last placeholder has no marks: it continues the row, as piece 0,2
		string([]rune{kittyPlaceholder, d[0], d[0], kittyPlaceholder, d[0], d[1], kittyPlaceholder}) +
		"\r\n" + string([]rune{kittyPlaceholder, d[1], d[0], kittyPlaceholder, d[1]}) + "\x1b[39m!"
	// split the command and the grid across events, as a pty read may
	c := kittyCast(first[:10], first[10:]+second[:3], second[3:], grid)
	s := ScreenAt(c, -1, Options{})
	want := [][4]int{{0, 0, 0, 0}, {1, 0, 0, 1}, {2, 0, 0, 2}, {0, 1, 1, 0}, {1, 1, 1, 1}}
	for _, w := range want {
		cell := s.At(w[0], w[1])
		if cell.Image == nil || int(cell.ImageRow) != w[2] || int(cell.ImageCol) != w[3] {
			t.Fatalf("cell %d,%d = %+v, want piece %d,%d", w[0], w[1], cell, w[2], w[3])
		}
	}
	if p := s.At(0, 0).Image; p.Cols != 3 || p.Rows != 2 || p.Image.Rect.Dx() != 4 {
		t.Fatalf("placement = %dx%d cells, image %v", p.Cols, p.Rows, p.Image.Rect)
	}
	if s.At(2, 1).Char != '!' {
		t.Fatalf("text after placeholders = %q", s.At(2, 1).Char)
	}
}

func near(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) bool { return max(x, y)-min(x, y) < 0x2000 }
	return d(ar, br) && d(ag, bg) && d(ab, bb)
}

func TestKittyRaster(t *testing.T) {
	c := kittyCast("\x1b[2;3H" + apc("a=T,f=32,s=4,v=4,c=2,r=1", solid(4, 4, red)))
	o := Options{FontSize: 16, Padding: 10, At: -1}

	var buf bytes.Buffer
	if err := PNG(&buf, c, o); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := newRasterizer(10, 4, o.withDefaults(c))
	// the image fits 2x1 cells keeping its square shape: its middle is red
	mid := image.Pt(r.pad+2*r.cw+r.ch/2, r.pad+r.ch+r.ch/2)
	if got := img.At(mid.X, mid.Y); !near(got, red) {
		t.Fatalf("PNG pixel = %v, want red", got)
	}

	buf.Reset()
	if err := GIF(&buf, c, o); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	last := g.Image[len(g.Image)-1]
	if got := last.At(mid.X, mid.Y); !near(got, red) {
		t.Fatalf("GIF pixel = %v, want red", got)
	}
}

func TestKittySVG(t *testing.T) {
	c := kittyCast(apc("a=T,f=32,s=1,v=1,c=3,r=2", solid(1, 1, red)))
	var buf bytes.Buffer
	if err := SVGFrame(&buf, c, Options{At: -1}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "<image ") != 1 || strings.Count(out, `<use href="#img0"/>`) != 2 {
		t.Fatalf("SVG without image runs:\n%s", out)
	}
}

func TestKittyTranscript(t *testing.T) {
	c := kittyCast("hi"+apc("a=T,f=32,s=1,v=1", solid(1, 1, red)),
		string([]rune{kittyPlaceholder, kittyDiacritics[0], kittyDiacritics[0]})+"!\r\n")
	var buf bytes.Buffer
	if err := Transcript(&buf, c); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "hi !\n" {
		t.Fatalf("transcript = %q", got)
	}
}
