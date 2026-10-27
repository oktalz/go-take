package cast

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTake(t *testing.T, c *Cast) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Write(&buf, FormatTake); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// sameEvents compares events by their written delays, which round to microseconds.
func sameEvents(t *testing.T, a, b []Event) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("got %d events, want %d", len(b), len(a))
	}
	for i := range a {
		if a[i].Code != b[i].Code || a[i].Data != b[i].Data || FormatDelay(a[i].Delay) != FormatDelay(b[i].Delay) {
			t.Fatalf("event %d: got %+v, want %+v", i, b[i], a[i])
		}
	}
}

func TestTakeRoundTripFiles(t *testing.T) {
	files, _ := filepath.Glob("../*.cast")
	more, _ := filepath.Glob("../examples/*.cast")
	files = append(files, more...)
	if len(files) == 0 {
		t.Skip("no .cast files")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			c, err := Load(f)
			if err != nil {
				t.Fatal(err)
			}
			take := writeTake(t, c)
			c2 := mustRead(t, take)
			if !reflect.DeepEqual(c.Header, c2.Header) {
				t.Fatalf("header: got %+v, want %+v", c2.Header, c.Header)
			}
			sameEvents(t, c.Events, c2.Events)
			if again := writeTake(t, c2); again != take {
				t.Fatalf("second write differs:\n%s\n---\n%s", take, again)
			}
		})
	}
}

func TestTakeGroupsTyping(t *testing.T) {
	c := mustRead(t, v3)
	got := writeTake(t, c)
	want := `gotake 1
term 80x24

out    0.5 "$ "
type   1.0 "ls" 0.2
out    0.3 "\r\n"
out    0.1 "file.txt\r\n"
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTakeReadHandWritten(t *testing.T) {
	c := mustRead(t, `# my demo
gotake 1
term 100x30 xterm-256color
title "hello \"world\""
env SHELL /bin/bash
theme "#d6d6d6" "#121314"

out    0 "\e[32m$\e[0m "   # the prompt
type   500ms "abc" every=80ms
wait   2
type   0.1 "ok"
out    0.2 "tab\there\x07 \u00e9 \U0001F600"
marker 0
marker 1s "chapter"
resize 0 120x40
exit   0 0
event  0.5 x-custom "data"
`)
	h := c.Header
	if h.Term.Cols != 100 || h.Term.Rows != 30 || h.Term.Type != "xterm-256color" ||
		h.Title != `hello "world"` || h.Env["SHELL"] != "/bin/bash" || h.Term.Theme.BG != "#121314" {
		t.Fatalf("header: %+v", h)
	}
	want := []Event{
		{Code: Output, Data: "\x1b[32m$\x1b[0m "},
		{Delay: 0.5, Code: Output, Data: "a"},
		{Delay: 0.08, Code: Output, Data: "b"},
		{Delay: 0.08, Code: Output, Data: "c"},
		{Delay: 2.1, Code: Output, Data: "o"},
		{Delay: 0.08, Code: Output, Data: "k"},
		{Delay: 0.2, Code: Output, Data: "tab\there\a é 😀"},
		{Code: Marker},
		{Delay: 1, Code: Marker, Data: "chapter"},
		{Code: Resize, Data: "120x40"},
		{Code: Exit, Data: "0"},
		{Delay: 0.5, Code: "x-custom", Data: "data"},
	}
	sameEvents(t, want, c.Events)
	// and it survives a write and read
	sameEvents(t, want, mustRead(t, writeTake(t, c)).Events)
}

func TestTakeTypeEditedText(t *testing.T) {
	// "ab" became "abcd": the two new characters get the average delay
	c := mustRead(t, "gotake 1\ntype 0.5 \"abcd\" 0.1 0.3\ntype 0 \"xy\" 0.1 0.2 0.3\n")
	want := []Event{
		{Delay: 0.5, Code: Output, Data: "a"},
		{Delay: 0.1, Code: Output, Data: "b"},
		{Delay: 0.3, Code: Output, Data: "c"},
		{Delay: 0.2, Code: Output, Data: "d"},
		{Code: Output, Data: "x"},
		{Delay: 0.1, Code: Output, Data: "y"},
	}
	sameEvents(t, want, c.Events)
}

func TestTakeEscapesRoundTrip(t *testing.T) {
	c := New(80, 24)
	data := "\x1b[1m\"q\" \\ \r\n\t\x00\x7f \u0085 \u200b é 😀 \xff"
	c.Events = []Event{{Delay: 0.1, Code: Output, Data: data}, {Code: Input, Data: "\x03"}}
	c2 := mustRead(t, writeTake(t, c))
	sameEvents(t, c.Events, c2.Events)
}

func TestTakeErrors(t *testing.T) {
	for name, src := range map[string]string{
		"version":    "gotake 2\n",
		"keyword":    "gotake 1\nbogus 1 \"x\"\n",
		"unquoted":   "gotake 1\nout 0.1 \"abc\n",
		"delay":      "gotake 1\nout -1 \"x\"\n",
		"type delay": "gotake 1\ntype 0.1 \"abc\" 0.1 x\n",
		"escape":     "gotake 1\nout 0 \"\\q\"\n",
		"term":       "gotake 1\nterm big\n",
	} {
		if _, err := Read(strings.NewReader(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSaveFormatFromExtension(t *testing.T) {
	dir := t.TempDir()
	c := mustRead(t, v3)
	for name, prefix := range map[string]string{"a.take": "gotake 1", "a.cast": `{"version":3`, "a": "gotake 1"} {
		p := filepath.Join(dir, name)
		if err := c.Save(p); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if !strings.HasPrefix(string(b), prefix) {
			t.Fatalf("%s starts with %q", name, b[:20])
		}
		c2, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		sameEvents(t, c.Events, c2.Events)
	}
}

func TestTakeCellSize(t *testing.T) {
	c := New(80, 24)
	c.Header.Term.CellWidth, c.Header.Term.CellHeight = 9, 18
	var buf bytes.Buffer
	if err := c.Write(&buf, FormatTake); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\ncell 9x18\n") {
		t.Fatalf("no cell line:\n%s", buf.String())
	}
	back, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if back.Header.Term.CellWidth != 9 || back.Header.Term.CellHeight != 18 {
		t.Fatalf("cell size = %dx%d", back.Header.Term.CellWidth, back.Header.Term.CellHeight)
	}
}

func TestTakeCaption(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{{Delay: 1, Code: Caption, Data: "Now we deploy"}, {Delay: 2, Code: Caption}}
	var buf bytes.Buffer
	if err := c.Write(&buf, FormatTake); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `caption 1.0 "Now we deploy"`) {
		t.Fatalf("no caption line:\n%s", buf.String())
	}
	back, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sameEvents(t, c.Events, back.Events)
}
