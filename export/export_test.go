package export

import (
	"bytes"
	"image/gif"
	"image/png"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

func demo() *cast.Cast {
	c := cast.New(20, 4)
	c.Events = []cast.Event{
		{Delay: 0.5, Code: cast.Output, Data: "\x1b[1;32m$\x1b[0m "},
		{Delay: 0.1, Code: cast.Output, Data: "l"},
		{Delay: 0.1, Code: cast.Output, Data: "s"},
		{Delay: 0.3, Code: cast.Output, Data: "\r\nfile.txt </script>\r\n"},
		{Delay: 0.5, Code: cast.Output, Data: "50%\r100%\r\n"},
	}
	return c
}

func TestGIF(t *testing.T) {
	var buf bytes.Buffer
	if err := GIF(&buf, demo(), Options{Window: true}); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 6 { // blank start + 5 events
		t.Fatalf("frames = %d", len(g.Image))
	}
	total := 0
	for _, d := range g.Delay {
		total += d
	}
	if total != 350 { // 1.5s of events + 2s end delay
		t.Fatalf("total delay = %d", total)
	}
}

func TestGIFSpeedAndIdle(t *testing.T) {
	var buf bytes.Buffer
	if err := GIF(&buf, demo(), Options{Speed: 2, IdleTimeLimit: 0.2}); err != nil {
		t.Fatal(err)
	}
	g, _ := gif.DecodeAll(&buf)
	total := 0
	for _, d := range g.Delay {
		total += d
	}
	// delays capped to 0.2: 0.2+0.1+0.1+0.2+0.2 = 0.8s, at 2x = 0.4s, + 2s end
	if total != 240 {
		t.Fatalf("total delay = %d", total)
	}
}

func TestStillFrames(t *testing.T) {
	var buf bytes.Buffer
	if err := PNG(&buf, demo(), Options{At: 0.65}); err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(&buf); err != nil {
		t.Fatal(err)
	}
	var txt strings.Builder
	if err := ScreenText(&txt, demo(), Options{At: 0.65}); err != nil {
		t.Fatal(err)
	}
	if txt.String() != "$ l\n" {
		t.Fatalf("screen at 0.65s = %q", txt.String())
	}
}

func TestSVG(t *testing.T) {
	var buf bytes.Buffer
	if err := SVG(&buf, demo(), Options{}); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"@keyframes k{", `class="b"`, "file.txt &lt;/script&gt;", "100%"} {
		if !strings.Contains(s, want) {
			t.Errorf("svg is missing %q", want)
		}
	}
}

func TestTranscript(t *testing.T) {
	var b strings.Builder
	if err := Transcript(&b, demo()); err != nil {
		t.Fatal(err)
	}
	want := "$ ls\nfile.txt </script>\n100%\n"
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
}

func TestHTMLEscapesScript(t *testing.T) {
	var b strings.Builder
	if err := HTML(&b, demo(), HTMLOptions{PlayerJS: []byte("var x = '</script>';")}); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	// only the page's own three script elements may close
	if n := strings.Count(s, "</script>"); n != 3 {
		t.Fatalf("found %d </script> tags", n)
	}
	if !strings.Contains(s, `file.txt <\/script>`) {
		t.Fatal("cast data not escaped")
	}
}

func TestResetClearsScreen(t *testing.T) {
	c := cast.New(20, 4)
	c.Events = []cast.Event{
		{Code: cast.Output, Data: strings.Repeat("x", 80)}, // fill the whole screen
		{Delay: 0.1, Code: cast.Output, Data: "\x1bc$ "},
		{Delay: 0.5, Code: cast.Output, Data: strings.Repeat("y", 20) + "\x1b"}, // a reset split across events
		{Code: cast.Output, Data: "c# "},
	}
	for at, want := range map[float64]string{0.1: "$", 1: "#"} {
		var buf bytes.Buffer
		if err := ScreenText(&buf, c, Options{At: at}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != want {
			t.Fatalf("screen at %v after reset:\n%q", at, got)
		}
	}
}

func TestTransparentBackground(t *testing.T) {
	c := demo()
	c.Header.Term.Theme = &cast.Theme{FG: "#ffffff", BG: TransparentBG}
	if th := ThemeFor(c.Header); !th.Transparent || th.FG != hex("#ffffff") {
		t.Fatalf("theme: %+v", th)
	}

	var buf bytes.Buffer
	if err := PNG(&buf, c, Options{}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(1, 1).RGBA(); a != 0 {
		t.Fatalf("PNG corner alpha = %d, want transparent", a)
	}

	buf.Reset()
	if err := GIF(&buf, c, Options{}); err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range g.Image {
		if f.Bounds() != g.Image[0].Bounds() || g.Disposal[i] != gif.DisposalBackground {
			t.Fatalf("frame %d: bounds %v, disposal %d; want whole frames cleared after showing",
				i, f.Bounds(), g.Disposal[i])
		}
	}

	buf.Reset()
	if err := SVG(&buf, c, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `<rect width="100%"`) {
		t.Fatal("SVG has a background rectangle")
	}
}

func TestDrawnGlyphs(t *testing.T) {
	// box drawing fills the cell edge to edge, so neighbors join
	a := drawnGlyph('─', 10, 20, 1)
	if a == nil {
		t.Fatal("─ is not drawn")
	}
	for _, x := range []int{0, 9} {
		col := 0
		for y := range 20 {
			col += int(a.Pix[y*a.Stride+x])
		}
		if col == 0 {
			t.Fatalf("─ does not reach column %d", x)
		}
	}
	for ch, want := range map[rune]bool{'╭': true, '█': true, '░': true, '⣿': true, '': true, 'a': false, '❯': false} {
		if got := drawnGlyph(ch, 10, 20, 1) != nil; got != want {
			t.Errorf("drawnGlyph(%q) drawn = %v, want %v", ch, got, want)
		}
	}
	if len(boxSegments) != 4+16*4+14+15+12 {
		t.Fatalf("box drawing table has %d entries", len(boxSegments))
	}
	if boxSegments['┍'] != [4]uint8{0, 2, 1, 0} || boxSegments['╊'] != [4]uint8{2, 2, 2, 1} {
		t.Fatal("box drawing table is out of order")
	}
}

func TestFontOptions(t *testing.T) {
	c := demo()
	var buf bytes.Buffer
	if err := PNG(&buf, c, Options{Fonts: []string{"/does/not/exist.ttf"}}); err == nil {
		t.Fatal("expected an error for a missing font file")
	}
	buf.Reset()
	if err := PNG(&buf, c, Options{NoSystemFonts: true}); err != nil {
		t.Fatal(err)
	}
}

func TestWindowStyles(t *testing.T) {
	styles := map[string]string{
		"linux": WindowLinux, "Windows": WindowWindows, "macos": WindowMac, "": DefaultWindowStyle(),
	}
	for in, want := range styles {
		if got, err := windowStyle(in); err != nil || got != want {
			t.Errorf("windowStyle(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := windowStyle("beos"); err == nil {
		t.Error("expected an error for an unknown style")
	}
	if got := windowTitle("", "/bin/bash -l"); got != "bash" {
		t.Errorf("title from command = %q", got)
	}

	c := demo()
	c.Header.Title = "my demo"
	marks := map[string]string{WindowLinux: `font-weight="bold"`, WindowWindows: `<line`, WindowMac: `#ff5f57`}
	for _, st := range WindowStyles {
		var buf bytes.Buffer
		if err := SVG(&buf, c, Options{Window: true, WindowStyle: st}); err != nil {
			t.Fatal(err)
		}
		svg := buf.String()
		if !strings.Contains(svg, ">my demo</text>") || !strings.Contains(svg, marks[st]) {
			t.Errorf("%s: SVG title bar is missing the title or %s", st, marks[st])
		}
		buf.Reset()
		if err := PNG(&buf, c, Options{Window: true, WindowStyle: st}); err != nil {
			t.Fatal(err)
		}
		if _, err := png.Decode(&buf); err != nil {
			t.Fatal(err)
		}
	}
}
