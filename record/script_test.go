package record

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/oktalz/go-take/cast"
)

func TestParseScript(t *testing.T) {
	s, err := ParseScript(strings.NewReader(`
# a comment
size 100x30
shell "sh"
typing 50ms 10ms
env FOO bar
type "ls -l" 20ms
enter 2
ctrl+c
wait '^\$ $' 3s
sleep 1.5
hide
show
marker intro
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Cols != 100 || s.Rows != 30 || s.Shell != "sh" || s.TypeDelay != 0.05 || s.TypeJitter != 0.01 {
		t.Fatalf("settings = %+v", s)
	}
	var kinds []string
	for _, st := range s.Steps {
		kinds = append(kinds, st.Kind)
	}
	if got := strings.Join(kinds, " "); got != "type key key wait sleep hide show marker" {
		t.Fatalf("steps = %s", got)
	}
	if st := s.Steps[1]; st.Text != "\r" || st.Count != 2 {
		t.Fatalf("enter = %+v", st)
	}
	if s.Steps[2].Text != "\x03" {
		t.Fatalf("ctrl+c = %q", s.Steps[2].Text)
	}
	if st := s.Steps[3]; st.Delay != 3 || !st.Re.MatchString("out\n$ ") {
		t.Fatalf("wait = %+v", st)
	}
}

func TestParseScriptErrors(t *testing.T) {
	for _, src := range []string{
		"jump 3",            // unknown step
		"type",              // missing text
		"enter x",           // bad count
		"wait '('",          // bad regexp
		"sleep soon",        // bad delay
		"type a\nsize 10x2", // setting after a step
		"size big",
	} {
		if _, err := ParseScript(strings.NewReader(src)); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}

func TestOnScreen(t *testing.T) {
	for in, want := range map[string]string{
		"setup\r\nmore\r\n$ ":       "$ ",
		"junk\x1b[H\x1b[2J$ ":       "\x1b[2J$ ",
		"a\nb\x1bc$ \r\nline\r\n$ ": "\x1bc$ \r\nline\r\n$ ",
		"no newline":                "no newline",
	} {
		if got := onScreen(in); got != want {
			t.Errorf("onScreen(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a Unix shell")
	}
	s, err := ParseScript(strings.NewReader(`
shell "sh"
typing 1ms 0
env PS1 "$ "
hide
send "echo hidden\r"
wait 'hidden$'
show
type "echo shown"
enter
wait '^shown$'
marker done
`))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := RunScript(&buf, io.Discard, s, cast.FormatTake); err != nil {
		t.Fatal(err)
	}
	c, err := cast.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	markers := 0
	for _, e := range c.Events {
		switch e.Code {
		case cast.Output:
			out.WriteString(e.Data)
		case cast.Marker:
			markers++
		default:
		}
	}
	text := strings.ReplaceAll(cast.StripANSI(out.String()), "\r", "")
	if strings.Contains(text, "echo hidden") || !strings.Contains(text, "shown\n") || markers != 1 {
		t.Fatalf("recorded %q with %d markers", text, markers)
	}
}
