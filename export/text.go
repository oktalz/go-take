package export

import (
	"bufio"
	"io"
	"strings"

	"github.com/oktalz/go-take/cast"
)

// Transcript writes all output as plain text: escape sequences are removed and
// carriage returns / backspaces are applied, so progress bars and line editing
// collapse to their final state.
func Transcript(w io.Writer, c *cast.Cast) error {
	bw := bufio.NewWriter(w)
	var line []rune
	col := 0
	flush := func() {
		_, _ = bw.WriteString(strings.TrimRight(string(line), " ") + "\n")
		line, col = line[:0], 0
	}
	for _, e := range c.Events {
		if e.Code != cast.Output {
			continue
		}
		placeholder := false
		for _, r := range cast.StripANSI(e.Data) {
			if _, mark := diacriticIndex[r]; mark && placeholder {
				continue // row and column of a Kitty image placeholder
			}
			placeholder = r == kittyPlaceholder
			if placeholder {
				r = ' '
			}
			switch r {
			case '\n':
				flush()
			case '\r':
				col = 0
			case '\b':
				col = max(col-1, 0)
			case '\t':
				for {
					put(&line, col, ' ')
					col++
					if col%8 == 0 {
						break
					}
				}
			default:
				if r < ' ' || r == 0x7f {
					continue
				}
				put(&line, col, r)
				col++
			}
		}
	}
	if len(line) > 0 {
		flush()
	}
	return bw.Flush()
}

func put(line *[]rune, col int, r rune) {
	for len(*line) <= col {
		*line = append(*line, ' ')
	}
	(*line)[col] = r
}

// ScreenText writes the text visible on screen at recording time o.At (<0 = end).
func ScreenText(w io.Writer, c *cast.Cast, o Options) error {
	s := ScreenAt(c, o.At, o)
	bw := bufio.NewWriter(w)
	lines := make([]string, s.Rows)
	for y := range s.Rows {
		var b strings.Builder
		for x := range s.Cols {
			if c := s.At(x, y); !c.WideTail {
				b.WriteString(string(c.Char))
			}
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	_, _ = bw.WriteString(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
	return bw.Flush()
}
