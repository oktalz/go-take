package export

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"os/exec"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

func TestVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	for container, magic := range map[string]string{"mp4": "ftyp", "webm": "\x1a\x45\xdf\xa3"} {
		var buf bytes.Buffer
		if err := Video(&buf, demo(), Options{FPS: 10}, container); err != nil {
			t.Fatalf("%s: %v", container, err)
		}
		if head := buf.String()[:min(buf.Len(), 12)]; !strings.Contains(head, magic) {
			t.Fatalf("%s: not a %s file: % x", container, container, head)
		}
	}
}

// TestPNGManyColors checks that a screen with more colors than a GIF palette
// holds keeps them exact in PNG.
func TestPNGManyColors(t *testing.T) {
	c := cast.New(64, 2)
	var b strings.Builder
	for i := range 64 {
		fmt.Fprintf(&b, "\x1b[48;2;%d;%d;100m ", i*4, 255-i*4)
	}
	c.Events = []cast.Event{{Delay: 0.1, Code: cast.Output, Data: b.String() + "\x1b[0m\r\n"}}
	var buf bytes.Buffer
	if err := PNG(&buf, c, Options{At: -1, HideCursor: true}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := newRasterizer(64, 2, Options{}.withDefaults(c))
	x, y := r.pad+37*r.cw+r.cw/2, r.pad+r.ch/2
	got, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if want := (color.NRGBA{37 * 4, 255 - 37*4, 100, 255}); !ok || got != want {
		t.Fatalf("cell 37 = %v, want %v", got, want)
	}
}
