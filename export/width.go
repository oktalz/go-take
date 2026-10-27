package export

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// vt10x gives every character one cell, but terminals give East Asian wide
// characters and most emoji two, and combining marks none. Before output
// reaches vt10x, each wide character is followed by wideTail, which takes its
// second cell, and zero-width characters are left out. Text after them then
// lands in the columns the program meant.
const wideTail = '\U0010FFFD'

// cellWidth returns how many cells a terminal gives r.
func cellWidth(r rune) int {
	switch {
	case r < 0x300:
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

// widen prepares output for vt10x: wide characters get a second cell and
// zero-width characters are dropped.
func widen(s string) string {
	i := 0
	for i < len(s) && s[i] < utf8.RuneSelf {
		i++
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	_, _ = b.WriteString(s[:i])
	for _, r := range s[i:] {
		switch cellWidth(r) {
		case 0:
		case 2:
			_, _ = b.WriteRune(r)
			_, _ = b.WriteRune(wideTail)
		default:
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}
