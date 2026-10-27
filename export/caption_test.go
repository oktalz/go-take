package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

func TestCaptions(t *testing.T) {
	c := cast.New(40, 6)
	c.Events = []cast.Event{
		{Delay: 0.1, Code: cast.Output, Data: "$ "},
		{Delay: 0.5, Code: cast.Caption, Data: "Hello & welcome"},
		{Delay: 1, Code: cast.Caption},
	}
	var caps []string
	for _, f := range Frames(c, Options{}) {
		caps = append(caps, f.Screen.Caption)
	}
	if got := strings.Join(caps, "|"); got != "||Hello & welcome|" {
		t.Fatalf("captions per frame = %q", got)
	}

	var buf bytes.Buffer
	if err := SVGFrame(&buf, c, Options{At: 0.7}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), ">Hello &amp; welcome</tspan>") {
		t.Fatal("caption missing from SVG")
	}
	if err := PNG(&buf, c, Options{At: 0.7}); err != nil {
		t.Fatal(err)
	}
}

func TestWrapCaption(t *testing.T) {
	fits := func(s string) bool { return len(s) <= 10 }
	got := wrapCaption("one two three four five six seven eight nine ten", fits)
	if len(got) != captionLines || !strings.HasSuffix(got[captionLines-1], "…") {
		t.Fatalf("lines = %q", got)
	}
	if got := wrapCaption("a\nb", fits); len(got) != 2 {
		t.Fatalf("line breaks: %q", got)
	}
}
