package cast

import (
	"strings"
	"testing"
)

const bashPrompt = "\x1b[?2004h\x1b]0;me@box: ~\x07\x1b[01;32mme@box\x1b[00m:\x1b[01;34m~\x1b[00m$ "

func output(c *Cast) string {
	var b strings.Builder
	for _, e := range c.Events {
		if e.Code == Output {
			b.WriteString(e.Data)
		}
	}
	return b.String()
}

var trimCases = []struct {
	name   string
	events []Event
	want   string // remaining output
	n      int
}{
	{
		name: "typed key by key",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "ok\r\n" + bashPrompt},
			{Delay: 0.5, Code: Output, Data: "e"},
			{Delay: 0.1, Code: Output, Data: "x"},
			{Delay: 0.1, Code: Output, Data: "i"},
			{Delay: 0.1, Code: Output, Data: "t"},
			{Delay: 0.3, Code: Output, Data: "\r\n\x1b[?2004l\r"},
			{Delay: 0.01, Code: Output, Data: "exit\r\n"},
			{Delay: 0.01, Code: Exit, Data: "0"},
		},
		want: "ok\r\n" + bashPrompt, n: 6,
	},
	{
		name: "ctrl-d, exit printed in the prompt event",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "ok\r\n$ exit\r\n"},
			{Delay: 0.01, Code: Exit, Data: "0"},
		},
		want: "ok\r\n$ ", n: 1,
	},
	{
		name: "ctrl-d in bash: line break, then exit",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "ok\r\n" + bashPrompt},
			{Delay: 0.4, Code: Output, Data: "\x1b[?2004l\r\r\nexit\r\n"},
			{Delay: 0.01, Code: Exit, Data: "0"},
		},
		want: "ok\r\n" + bashPrompt, n: 1,
	},
	{
		name: "with input events and logout",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "% "},
			{Delay: 0.5, Code: Input, Data: "exit\r"},
			{Delay: 0.01, Code: Output, Data: "exit\r\nlogout\r\n"},
		},
		want: "% ", n: 2,
	},
	{
		name: "exit printed by a program is kept",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "$ ./run\r\n"},
			{Delay: 0.5, Code: Output, Data: "process exit\r\n"},
		},
		want: "$ ./run\r\nprocess exit\r\n",
	},
	{
		name: "output after exit is kept",
		events: []Event{
			{Delay: 0.1, Code: Output, Data: "$ exit\r\n"},
			{Delay: 0.5, Code: Output, Data: "still here\r\n"},
		},
		want: "$ exit\r\nstill here\r\n",
	},
	{
		name:   "a longer word is not exit",
		events: []Event{{Delay: 0.1, Code: Output, Data: "$ myexit\r\n"}},
		want:   "$ myexit\r\n",
	},
}

func TestTrimExit(t *testing.T) {
	for _, tc := range trimCases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Cast{Events: tc.events}
			n := c.TrimExit()
			if got := output(c); got != tc.want || n != tc.n {
				t.Fatalf("removed %d (want %d), output %q, want %q", n, tc.n, got, tc.want)
			}
			if tc.n > 0 && tc.events[len(tc.events)-1].Code == Exit && c.Events[len(c.Events)-1].Code != Exit {
				t.Fatal("exit status event must be kept")
			}
		})
	}
}
