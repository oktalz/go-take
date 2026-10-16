package cast

import (
	"regexp"
	"strings"
)

// ReplaceOptions configures Find and Replace.
type ReplaceOptions struct {
	Find       string `json:"find"`
	Replace    string `json:"replace"`
	Regex      bool   `json:"regex"`       // Find is a regular expression, Replace may use $1
	IgnoreCase bool   `json:"ignore_case"` // case-insensitive matching
	// IgnoreANSI matches against the visible text only, so "user@host:~/dir"
	// is found even when the shell colors parts of it. Escape sequences inside
	// a match are kept, the replacement is inserted where the match starts.
	IgnoreANSI bool `json:"ignore_ansi"`
}

func (o ReplaceOptions) compile() (*regexp.Regexp, error) {
	expr := o.Find
	if !o.Regex {
		expr = regexp.QuoteMeta(expr)
	}
	if o.IgnoreCase {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// Find returns the indices of output events in [from, to) that contain a match,
// and the total number of matches.
func (c *Cast) Find(from, to int, o ReplaceOptions) (events []int, matches int, err error) {
	from, to, err = c.clampRange(from, to)
	if err != nil || o.Find == "" {
		return nil, 0, err
	}
	re, err := o.compile()
	if err != nil {
		return nil, 0, err
	}
	for i := from; i < to; i++ {
		if c.Events[i].Code != Output {
			continue
		}
		text, _ := o.searchText(c.Events[i].Data)
		if n := len(findMatches(text, re)); n > 0 {
			events = append(events, i)
			matches += n
		}
	}
	return events, matches, nil
}

// Replace replaces every match in output events [from, to).
// It returns the number of replaced matches and of changed events.
func (c *Cast) Replace(from, to int, o ReplaceOptions) (matches, events int, err error) {
	from, to, err = c.clampRange(from, to)
	if err != nil || o.Find == "" {
		return 0, 0, err
	}
	re, err := o.compile()
	if err != nil {
		return 0, 0, err
	}
	for i := from; i < to; i++ {
		e := &c.Events[i]
		if e.Code != Output {
			continue
		}
		data, n := replaceData(e.Data, re, o)
		if n > 0 {
			e.Data = data
			matches += n
			events++
		}
	}
	return matches, events, nil
}

type match struct {
	sub        []int // submatch indices, for $1 expansion
	start, end int   // byte offsets into the searched text
}

// visible returns s without escape sequences, and for escape sequences
// a map from their start offset to their end offset.
func visible(s string) (text string, escapes map[int]int) {
	spans := ansiRe.FindAllStringIndex(s, -1)
	if len(spans) == 0 {
		return s, nil
	}
	escapes = make(map[int]int, len(spans))
	var b strings.Builder
	prev := 0
	for _, sp := range spans {
		b.WriteString(s[prev:sp[0]])
		escapes[sp[0]] = sp[1]
		prev = sp[1]
	}
	b.WriteString(s[prev:])
	return b.String(), escapes
}

// searchText is the text matched for data: all of it, or only the visible part.
func (o ReplaceOptions) searchText(data string) (text string, escapes map[int]int) {
	if o.IgnoreANSI {
		return visible(data)
	}
	return data, nil
}

// findMatches returns the non-empty matches in text.
func findMatches(text string, re *regexp.Regexp) []match {
	var ms []match
	for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
		if m[1] > m[0] {
			ms = append(ms, match{start: m[0], end: m[1], sub: m})
		}
	}
	return ms
}

func replaceData(data string, re *regexp.Regexp, o ReplaceOptions) (string, int) {
	text, escapes := o.searchText(data)
	ms := findMatches(text, re)
	if len(ms) == 0 {
		return data, 0
	}
	repl := make([]string, len(ms))
	for i, m := range ms {
		repl[i] = o.Replace
		if o.Regex {
			repl[i] = string(re.ExpandString(nil, o.Replace, text, m.sub))
		}
	}

	// Walk the original data. Visible bytes are counted with vi (an offset
	// into text); escape sequences are copied as they are, even inside matches.
	var b strings.Builder
	b.Grow(len(data))
	mi, vi := 0, 0
	for i := 0; i < len(data); {
		if end, ok := escapes[i]; ok {
			b.WriteString(data[i:end])
			i = end
			continue
		}
		for mi < len(ms) && vi >= ms[mi].end {
			mi++
		}
		inMatch := mi < len(ms) && vi >= ms[mi].start
		if mi < len(ms) && vi == ms[mi].start {
			b.WriteString(repl[mi])
		}
		if !inMatch {
			b.WriteString(data[i : i+1])
		}
		vi++
		i++
	}
	return b.String(), len(ms)
}
