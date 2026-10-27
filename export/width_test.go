package export

import (
	"bytes"
	"strings"
	"testing"
)

func TestWideCharacters(t *testing.T) {
	// a combining accent takes no cell, wide characters take two
	c := kittyCast("日本|\r\né|\r\n🙂x|\x1b[1;20H")
	s := ScreenAt(c, -1, Options{})
	for _, want := range [][2]int{{4, 0}, {1, 1}, {3, 2}} {
		if got := s.At(want[0], want[1]).Char; got != '|' {
			t.Fatalf("row %d: | not at column %d (%q there)", want[1], want[0], got)
		}
	}
	if !s.At(0, 0).Wide || !s.At(1, 0).WideTail || s.At(1, 0).Char != ' ' {
		t.Fatalf("wide cells = %+v %+v", s.At(0, 0), s.At(1, 0))
	}

	var buf bytes.Buffer
	if err := ScreenText(&buf, c, Options{At: -1}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := strings.Cut(buf.String(), "\n"); got != "日本|" {
		t.Fatalf("screen text = %q", got)
	}
	buf.Reset()
	if err := SVGFrame(&buf, c, Options{At: -1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `text-anchor="middle">日</text>`) {
		t.Fatal("wide character not drawn on its own in SVG")
	}
	if err := PNG(&buf, c, Options{At: -1}); err != nil {
		t.Fatal(err)
	}
}

func TestWidenASCII(t *testing.T) {
	if s := "plain \x1b[1mtext\x1b[0m"; widen(s) != s {
		t.Fatal("ASCII changed")
	}
}
