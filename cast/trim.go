package cast

import (
	"regexp"
	"slices"
	"strings"
)

var (
	// one or more "exit"/"logout" words at the very end of the output:
	// the typed command plus what the shell prints while exiting
	exitTail = regexp.MustCompile(`(?:exit|logout)(?:\s*(?:exit|logout))*\s*$`)
	// the line before it must end in a prompt, so program output is never cut
	promptEnd = regexp.MustCompile(`[$#%>❯»] ?$`)
)

// TrimExit removes the "exit" (or Ctrl-D, or "logout") that ended the
// recording, so it finishes on the last prompt. It only acts when the word
// was typed at a prompt and nothing but whitespace follows it. It returns the
// number of removed events; partially cut events count too.
func (c *Cast) TrimExit() int {
	// visible text of the tail of the recording, and where each output event starts in it
	start, size := len(c.Events), 0
	for start > 0 && size < 2048 {
		start--
		if c.Events[start].Code == Output {
			size += len(c.Events[start].Data)
		}
	}
	type segment struct{ index, begin int }
	var segs []segment
	var vis strings.Builder
	for i := start; i < len(c.Events); i++ {
		if c.Events[i].Code == Output {
			segs = append(segs, segment{i, vis.Len()})
			vis.WriteString(visibleText(c.Events[i].Data))
		}
	}
	text := vis.String()
	loc := exitTail.FindStringIndex(text)
	if loc == nil {
		return 0
	}
	// Ctrl-D makes bash print a line break before "exit", so the prompt may be
	// on the line above; those line breaks are cut as well
	before := strings.TrimRight(text[:loc[0]], "\n")
	if !promptEnd.MatchString(before[strings.LastIndex(before, "\n")+1:]) {
		return 0
	}
	at := len(before)

	// the output event in which the exit starts
	cut := segs[len(segs)-1]
	for _, seg := range slices.Backward(segs) {
		if seg.begin <= at {
			cut = seg
			break
		}
	}
	removed := 0
	// keystrokes of the exit command recorded before the output that echoes them
	first := cut.index
	for first > 0 && c.Events[first-1].Code != Output {
		first--
	}
	var out []Event
	for i, e := range c.Events[:cut.index] {
		if i >= first && e.Code == Input {
			removed++
			continue
		}
		out = append(out, e)
	}
	e := c.Events[cut.index]
	switch keep := rawPrefix(e.Data, at-cut.begin); {
	case keep == "":
		removed++
	case keep != e.Data:
		e.Data = keep
		out = append(out, e)
		removed++
	default:
		out = append(out, e)
	}
	for _, e := range c.Events[cut.index+1:] {
		if e.Code == Output || e.Code == Input {
			removed++
			continue
		}
		out = append(out, e) // keep markers, resizes and the exit status
	}
	c.Events = out
	return removed
}

// visibleText strips escape sequences and carriage returns.
func visibleText(s string) string {
	return strings.ReplaceAll(StripANSI(s), "\r", "")
}

// rawPrefix returns the beginning of s holding its first n visible bytes
// (as counted by visibleText), escape sequences in between included.
func rawPrefix(s string, n int) string {
	spans := ansiRe.FindAllStringIndex(s, -1)
	seen, i := 0, 0
	for i < len(s) && seen < n {
		if len(spans) > 0 && spans[0][0] == i {
			i = spans[0][1]
			spans = spans[1:]
			continue
		}
		if s[i] != '\r' {
			seen++
		}
		i++
	}
	return s[:i]
}
