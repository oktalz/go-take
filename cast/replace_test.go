package cast

import (
	"strings"
	"testing"
)

const prompt = "\x1b]0;zlatko@WSL: ~/src/x\x07\x1b[01;32mzlatko@WSL\x1b[00m:\x1b[01;34m~/src/x\x1b[00m$ "

func TestReplaceIgnoreANSI(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{
		{Delay: 0.1, Code: Output, Data: prompt},
		{Delay: 0.1, Code: Output, Data: "l"},
		{Delay: 0.1, Code: Input, Data: "zlatko@WSL:~/src/x"},
		{Delay: 0.1, Code: Output, Data: "\r\n" + prompt},
	}
	o := ReplaceOptions{Find: "zlatko@WSL:~/src/x", Replace: "demo:~", IgnoreANSI: true}
	events, n, err := c.Find(0, len(c.Events), o)
	if err != nil || n != 2 || len(events) != 2 || events[0] != 0 || events[1] != 3 {
		t.Fatalf("find: %v %d %v", events, n, err)
	}
	m, ev, err := c.Replace(0, len(c.Events), o)
	if err != nil || m != 2 || ev != 2 {
		t.Fatalf("replace: %d %d %v", m, ev, err)
	}
	// colors are kept, the window title (an escape sequence) is untouched
	want := "\x1b]0;zlatko@WSL: ~/src/x\x07\x1b[01;32mdemo:~\x1b[00m\x1b[01;34m\x1b[00m$ "
	if c.Events[0].Data != want {
		t.Fatalf("got  %q\nwant %q", c.Events[0].Data, want)
	}
	if c.Events[2].Data != "zlatko@WSL:~/src/x" {
		t.Fatal("input events must not change")
	}
}

func TestReplaceRaw(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{{Delay: 0.1, Code: Output, Data: prompt}}
	// raw mode also reaches inside escape sequences, e.g. the window title
	o := ReplaceOptions{Find: "zlatko@WSL", Replace: "me@box"}
	if m, _, _ := c.Replace(0, 1, o); m != 2 {
		t.Fatalf("replaced %d, want 2", m)
	}
	want := "\x1b]0;me@box: ~/src/x\x07\x1b[01;32mme@box\x1b[00m:\x1b[01;34m~/src/x\x1b[00m$ "
	if c.Events[0].Data != want {
		t.Fatalf("got %q", c.Events[0].Data)
	}
}

func TestReplaceRegex(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{{Delay: 0.1, Code: Output, Data: "\x1b[32mUser@Host\x1b[0m:~$ "}}
	o := ReplaceOptions{Find: `(\w+)@host`, Replace: "${1}@demo", Regex: true, IgnoreCase: true, IgnoreANSI: true}
	if m, _, err := c.Replace(0, 1, o); err != nil || m != 1 {
		t.Fatalf("replace: %d %v", m, err)
	}
	if got := c.Events[0].Data; got != "\x1b[32mUser@demo\x1b[0m:~$ " {
		t.Fatalf("got %q", got)
	}
	if _, _, err := c.Find(0, 1, ReplaceOptions{Find: "(", Regex: true}); err == nil {
		t.Fatal("expected regex error")
	}
}

func TestReplaceMask(t *testing.T) {
	c := New(80, 24)
	c.Events = []Event{
		{Code: Output, Data: "token: \x1b[1mghp_" + strings.Repeat("a", 36) + "\x1b[0m ok\r\n"},
		{Code: Output, Data: "export API_KEY=hunter2secret; key=AKIAABCDEFGHIJKLMNOP\r\n"},
		{Code: Output, Data: "nothing to see here\r\n"},
	}
	o := ReplaceOptions{Find: SecretPattern, Regex: true, Mask: true, IgnoreANSI: true}
	m, ev, err := c.Replace(0, len(c.Events), o)
	if err != nil {
		t.Fatal(err)
	}
	if m != 3 || ev != 2 {
		t.Fatalf("masked %d matches in %d events", m, ev)
	}
	want := []string{
		"token: \x1b[1m" + strings.Repeat("*", 40) + "\x1b[0m ok\r\n",
		"export API_KEY=*************; key=********************\r\n",
		"nothing to see here\r\n",
	}
	for i, w := range want {
		if c.Events[i].Data != w {
			t.Errorf("event %d = %q, want %q", i, c.Events[i].Data, w)
		}
	}
}
