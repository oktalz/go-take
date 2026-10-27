package export

import (
	"math"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

// syncCast redraws "old" as "new" in a synchronized update spread over three events.
func syncCast() *cast.Cast {
	c := cast.New(10, 2)
	c.Events = []cast.Event{
		{Delay: 0.1, Code: cast.Output, Data: "old"},
		{Delay: 0.5, Code: cast.Output, Data: "\x1b[?2026h\x1b[2J\x1b[H"},
		{Delay: 0.2, Code: cast.Output, Data: "ne"},
		{Delay: 0.2, Code: cast.Output, Data: "w\x1b[?2026l"},
	}
	return c
}

func screenLine(s *Screen) string {
	var b strings.Builder
	for x := range s.Cols {
		_, _ = b.WriteRune(s.At(x, 0).Char)
	}
	return strings.TrimSpace(b.String())
}

func TestSynchronizedUpdate(t *testing.T) {
	var lines []string
	for _, f := range Frames(syncCast(), Options{}) {
		lines = append(lines, screenLine(f.Screen))
	}
	if got := strings.Join(lines, ","); got != ",old,new" {
		t.Fatalf("frames = %s, want no half drawn ones", got)
	}
	if got := screenLine(ScreenAt(syncCast(), 0.7, Options{})); got != "new" {
		t.Fatalf("screen inside the update = %q, want the finished one", got)
	}
}

func TestSynchronizedUpdateTimeout(t *testing.T) {
	c := syncCast()
	c.Events[3].Delay = 3 // the end comes too late: the half drawn screen is shown
	var lines []string
	frames := Frames(c, Options{})
	for _, f := range frames {
		lines = append(lines, screenLine(f.Screen))
	}
	if got := strings.Join(lines, ","); got != ",old,ne,new" {
		t.Fatalf("frames = %s", got)
	}
	if at := frames[2].Time; math.Abs(at-1.6) > 1e-9 { // the update began at 0.6
		t.Fatalf("half drawn screen shown at %g, want when the terminal gives up waiting", at)
	}
}
