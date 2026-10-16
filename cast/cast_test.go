package cast

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

const v3 = `{"version": 3, "term": {"cols": 80, "rows": 24}}
# a comment
[0.5, "o", "$ "]
[1.0, "o", "l"]
[0.2, "o", "s"]
[0.3, "o", "\r\n"]
[0.1, "o", "file.txt\r\n"]
`

const v2 = `{"version": 2, "width": 100, "height": 30, "env": {"TERM": "xterm"}}
[0.5, "o", "a"]
[1.5, "o", "b"]
[1.75, "i", "c"]
`

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func mustRead(t *testing.T, s string) *Cast {
	t.Helper()
	c, err := Read(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReadV3RoundTrip(t *testing.T) {
	c := mustRead(t, v3)
	if len(c.Events) != 5 || c.Header.Term.Cols != 80 {
		t.Fatalf("unexpected cast: %+v", c)
	}
	if !near(c.Duration(), 2.1) {
		t.Fatalf("duration = %v", c.Duration())
	}
	var buf bytes.Buffer
	if err := c.Write(&buf, FormatAsciicast); err != nil {
		t.Fatal(err)
	}
	c2 := mustRead(t, buf.String())
	if len(c2.Events) != 5 || c2.Events[4].Data != "file.txt\r\n" {
		t.Fatalf("round trip mismatch: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `[0.1, "o", "file.txt\r\n"]`) {
		t.Fatalf("unexpected encoding: %s", buf.String())
	}
}

func TestReadV2(t *testing.T) {
	c := mustRead(t, v2)
	if c.Header.Version != 3 || c.Header.Term.Cols != 100 || c.Header.Term.Type != "xterm" {
		t.Fatalf("header: %+v", c.Header)
	}
	want := []float64{0.5, 1.0, 0.25}
	for i, w := range want {
		if !near(c.Events[i].Delay, w) {
			t.Fatalf("event %d delay = %v, want %v", i, c.Events[i].Delay, w)
		}
	}
}

func TestNoHTMLEscaping(t *testing.T) {
	b, _ := Event{Delay: 1, Code: "o", Data: "<a & b>\x1b[0m"}.MarshalJSON()
	if string(b) != `[1.0, "o", "<a & b>\u001b[0m"]` {
		t.Fatalf("got %s", b)
	}
}

func TestDeleteStretch(t *testing.T) {
	c := mustRead(t, v3)
	if err := c.Stretch(0, 1, 2); err != nil {
		t.Fatal(err)
	}
	if !near(c.Events[0].Delay, 1.0) {
		t.Fatal("stretch failed")
	}
	if err := c.Delete(1, 3); err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != 3 || c.Events[1].Data != "\r\n" {
		t.Fatalf("delete failed: %+v", c.Events)
	}
	if err := c.StretchTo(0, 3, 0.7); err != nil {
		t.Fatal(err)
	}
	if !near(c.Duration(), 0.7) {
		t.Fatalf("stretch_to duration = %v", c.Duration())
	}
}

func TestIndexAt(t *testing.T) {
	c := mustRead(t, v3)
	for _, tc := range []struct {
		t    float64
		want int
	}{{0, 0}, {0.5, 0}, {0.6, 1}, {1.5, 1}, {1.6, 2}, {100, 5}} {
		if got := c.IndexAt(tc.t); got != tc.want {
			t.Errorf("IndexAt(%v) = %d, want %d", tc.t, got, tc.want)
		}
	}
}

func TestNormalizeTyping(t *testing.T) {
	c := mustRead(t, v3)
	c.Insert(3, Event{Delay: 0.9, Code: "o", Data: "\x1b[1m-\x1b[0m"})
	n, err := c.NormalizeTyping(0, len(c.Events), TypingOptions{Delay: 0.05})
	if err != nil {
		t.Fatal(err)
	}
	// "$ " starts the run (2 visible chars), then l, s, and the styled "-"
	if n != 3 {
		t.Fatalf("adjusted %d, want 3", n)
	}
	if !near(c.Events[0].Delay, 0.5) {
		t.Fatal("first keystroke of a run must keep its delay")
	}
	for _, i := range []int{1, 2, 3} {
		if !near(c.Events[i].Delay, 0.05) {
			t.Fatalf("event %d delay = %v", i, c.Events[i].Delay)
		}
	}
	if !near(c.Events[5].Delay, 0.1) {
		t.Fatal("non-keystroke output must not change")
	}
}

func TestNormalizeTypingWithInput(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{
		{Delay: 1, Code: "i", Data: "a"},
		{Delay: 0.01, Code: "o", Data: "a"},
		{Delay: 0.5, Code: "i", Data: "b"},
		{Delay: 0.01, Code: "o", Data: "b"},
		{Delay: 0.3, Code: "i", Data: "c"},
		{Delay: 0.01, Code: "o", Data: "c"},
	}
	n, _ := c.NormalizeTyping(0, len(c.Events), TypingOptions{Delay: 0.1})
	if n != 2 {
		t.Fatalf("adjusted %d", n)
	}
	if !near(c.Events[2].Delay, 0.1) || !near(c.Events[3].Delay, 0) || !near(c.Events[0].Delay, 1) {
		t.Fatalf("%+v", c.Events)
	}
}

func TestInjectCommand(t *testing.T) {
	c := mustRead(t, v3)
	o := DefaultInjectOptions()
	o.Jitter = 0
	o.Prompt, o.Command, o.Output = "$ ", "ls", "a\nb\n"
	n := c.InjectCommand(len(c.Events), o)
	evs := c.Events[len(c.Events)-n:]
	var out strings.Builder
	for _, e := range evs {
		out.WriteString(e.Data)
	}
	if out.String() != "$ ls\r\na\r\nb\r\n" {
		t.Fatalf("output %q", out.String())
	}
	if n != 6 {
		t.Fatalf("inserted %d events", n)
	}
	if !near(evs[2].Delay, o.TypingDelay) {
		t.Fatalf("typing delay %v", evs[2].Delay)
	}
}

func TestMoveMergeSplit(t *testing.T) {
	c := mustRead(t, v3)
	if err := c.Move(4, 5, 0); err != nil {
		t.Fatal(err)
	}
	if c.Events[0].Data != "file.txt\r\n" {
		t.Fatalf("move: %+v", c.Events)
	}
	if err := c.Move(0, 1, 5); err != nil {
		t.Fatal(err)
	}
	if c.Events[4].Data != "file.txt\r\n" {
		t.Fatalf("move back: %+v", c.Events)
	}
	if err := c.Merge(0, 5); err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != 1 || c.Events[0].Data != "$ ls\r\nfile.txt\r\n" {
		t.Fatalf("merge: %+v", c.Events)
	}
	if err := c.Split(0, 1); err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != 2 || c.Events[1].Data != "file.txt\r\n" {
		t.Fatalf("split: %+v", c.Events)
	}
}

func TestSection(t *testing.T) {
	c := mustRead(t, v3)
	s, err := c.Section(3, 5)
	if err != nil {
		t.Fatal(err)
	}
	// prior output folded into one event, then the section starting right away
	if len(s.Events) != 3 || s.Events[0].Data != "$ ls" ||
		s.Events[1].Delay != 0 || s.Events[2].Data != "file.txt\r\n" {
		t.Fatalf("%+v", s.Events)
	}
	if len(c.Events) != 5 {
		t.Fatal("source modified")
	}
}

func TestWriteV2(t *testing.T) {
	c := mustRead(t, v3)
	var buf bytes.Buffer
	if err := c.WriteV2(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `{"version":2,"width":80,"height":24}`) {
		t.Fatalf("header: %s", strings.SplitN(buf.String(), "\n", 2)[0])
	}
	back := mustRead(t, buf.String())
	if !near(back.Duration(), c.Duration()) || len(back.Events) != len(c.Events) {
		t.Fatalf("round trip: %v vs %v", back.Duration(), c.Duration())
	}
}

func TestMarkerTime(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{
		{Delay: 1, Code: Output, Data: "a"},
		{Delay: 0.5, Code: Marker, Data: "Intro"},
		{Delay: 2, Code: Marker, Data: "intro"},
		{Delay: 1, Code: Marker},
	}
	for label, want := range map[string]float64{"intro": 3.5, "Intro": 1.5, "INTRO": 1.5} {
		if got, err := c.MarkerTime(label); err != nil || !near(got, want) {
			t.Errorf("MarkerTime(%q) = %v, %v; want %v", label, got, err, want)
		}
	}
	if _, err := c.MarkerTime("nope"); err == nil || !strings.Contains(err.Error(), `"Intro", "intro"`) {
		t.Errorf("missing marker error: %v", err)
	}
}
