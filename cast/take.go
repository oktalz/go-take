package cast

// The .take format is the native gotake file format. It is line based text,
// one header field or event per line, meant to be read and edited by hand:
//
//	gotake 1
//	term 80x24 xterm-256color
//	title "demo"
//
//	out    0.5 "\e[32m$\e[0m "
//	type   0.3 "ls -la" 0.08 0.07 0.11 0.09 0.1
//	out    0.4 "\r\n"
//	marker 0.0 "listing"
//
// Every event line starts with a keyword and the delay in seconds since the
// previous event ("80ms" and "1.5s" are accepted too). "type" holds a run of
// single-character output events, which is how a shell echoes typed text: the
// delay before the first character, the text, and then the delay before each
// following character, or every=D when they are all the same (without either,
// characters are typed every 0.08s). Text can be changed without fixing the
// delays: missing ones are filled in with their average and extra ones are
// ignored. "wait D" adds D to the next event's delay.
//
// Strings are double quoted, with \e, \r, \n, \t, \\, \", \xHH, \uXXXX and
// \UXXXXXXXX escapes. A word starting with '#' starts a comment that runs to the
// end of the line. Reading and writing a file keeps every event and delay, but
// not comments or "wait" lines.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format is an on-disk recording format.
type Format int

// Recording formats.
const (
	FormatTake      Format = iota // gotake's own text format, see above
	FormatAsciicast               // asciicast v3, for import and export
)

// TakeExt is the file extension of the .take format.
const TakeExt = ".take"

// FormatFor picks the format from a file name: .cast is asciicast, anything else is .take.
func FormatFor(path string) Format {
	if strings.EqualFold(filepath.Ext(path), ".cast") {
		return FormatAsciicast
	}
	return FormatTake
}

// defaultTypeDelay is used between characters of a "type" line that gives no delays.
const defaultTypeDelay = 0.08

// takeWords maps event codes to keywords; other codes are written as "event".
var takeWords = map[string]string{Output: "out", Input: "in", Resize: "resize", Marker: "marker", Exit: "exit"}

// Encoder writes a recording event by event.
type Encoder interface {
	WriteHeader(h Header) error
	WriteEvent(e Event) error
	// Flush writes buffered data out. A .take encoder keeps a run of typed
	// characters until it ends, so they become a single "type" line.
	Flush() error
	// Close writes everything still pending. It does not close the writer.
	Close() error
}

// NewEncoder returns an encoder writing format f to w.
func NewEncoder(w io.Writer, f Format) Encoder {
	if f == FormatAsciicast {
		return &castEncoder{w: bufio.NewWriter(w)}
	}
	return &takeEncoder{w: bufio.NewWriter(w)}
}

type castEncoder struct{ w *bufio.Writer }

func (e *castEncoder) WriteHeader(h Header) error {
	h.Version = 3
	return WriteHeader(e.w, h)
}

func (e *castEncoder) WriteEvent(ev Event) error { return WriteEvent(e.w, ev) }
func (e *castEncoder) Flush() error              { return e.w.Flush() }
func (e *castEncoder) Close() error              { return e.w.Flush() }

// takeEncoder relies on bufio.Writer keeping the first write error, which Flush returns.
type takeEncoder struct {
	w   *bufio.Writer
	run []Event // typed characters not written yet
}

func (e *takeEncoder) WriteHeader(h Header) error {
	e.line("gotake", "1")
	size := strconv.Itoa(h.Term.Cols) + "x" + strconv.Itoa(h.Term.Rows)
	if h.Term.Type != "" {
		e.line("term", size, quoteTake(h.Term.Type))
	} else {
		e.line("term", size)
	}
	if h.Term.Version != "" {
		e.line("term-version", quoteTake(h.Term.Version))
	}
	if t := h.Term.Theme; t != nil {
		words := []string{"theme", quoteTake(t.FG), quoteTake(t.BG)}
		if t.Palette != "" {
			words = append(words, quoteTake(t.Palette))
		}
		e.line(words...)
	}
	if h.Title != "" {
		e.line("title", quoteString(h.Title))
	}
	if h.Command != "" {
		e.line("command", quoteTake(h.Command))
	}
	if h.Timestamp != 0 {
		e.line("timestamp", strconv.FormatInt(h.Timestamp, 10))
	}
	if h.IdleTimeLimit > 0 {
		e.line("idle", FormatDelay(h.IdleTimeLimit))
	}
	for _, k := range slices.Sorted(maps.Keys(h.Env)) {
		e.line("env", quoteTake(k), quoteTake(h.Env[k]))
	}
	for _, t := range h.Tags {
		e.line("tag", quoteTake(t))
	}
	e.line()
	return nil
}

func (e *takeEncoder) WriteEvent(ev Event) error {
	if isTypedChar(ev) {
		e.run = append(e.run, ev)
		return nil
	}
	e.writeRun()
	e.writeSingle(ev)
	return nil
}

func (e *takeEncoder) Flush() error { return e.w.Flush() }

func (e *takeEncoder) Close() error {
	e.writeRun()
	return e.w.Flush()
}

// line writes words separated by spaces as one line.
func (e *takeEncoder) line(words ...string) {
	for i, w := range words {
		if i > 0 {
			_ = e.w.WriteByte(' ')
		}
		_, _ = e.w.WriteString(w)
	}
	_ = e.w.WriteByte('\n')
}

// isTypedChar reports whether e can be part of a "type" line: output of one printable character.
func isTypedChar(e Event) bool {
	if e.Code != Output {
		return false
	}
	r, size := utf8.DecodeRuneInString(e.Data)
	return size == len(e.Data) && r != utf8.RuneError && unicode.IsPrint(r)
}

// keyword pads an event keyword so the delays line up.
func keyword(word string) string { return fmt.Sprintf("%-6s", word) }

func (e *takeEncoder) writeRun() {
	run := e.run
	e.run = e.run[:0]
	if len(run) == 1 {
		e.writeSingle(run[0])
	}
	if len(run) < 2 {
		return
	}
	var text strings.Builder
	delays := make([]string, len(run)-1)
	for i, ev := range run {
		text.WriteString(ev.Data)
		if i > 0 {
			delays[i-1] = FormatDelay(ev.Delay)
		}
	}
	words := []string{keyword("type"), FormatDelay(run[0].Delay), quoteString(text.String())}
	if len(delays) > 1 && slices.Min(delays) == slices.Max(delays) {
		words = append(words, "every="+delays[0])
	} else {
		words = append(words, delays...)
	}
	e.line(words...)
}

// writeSingle writes one event as its own line.
func (e *takeEncoder) writeSingle(ev Event) {
	word, ok := takeWords[ev.Code]
	if !ok {
		e.line(keyword("event"), FormatDelay(ev.Delay), quoteTake(ev.Code), quoteString(ev.Data))
		return
	}
	switch {
	case ev.Code == Resize || ev.Code == Exit:
		e.line(keyword(word), FormatDelay(ev.Delay), quoteTake(ev.Data))
	case ev.Data == "" && ev.Code == Marker:
		e.line(keyword(word), FormatDelay(ev.Delay))
	default:
		e.line(keyword(word), FormatDelay(ev.Delay), quoteString(ev.Data))
	}
}

// quoteTake returns s as a .take word: simple names and numbers as they are, anything else quoted.
func quoteTake(s string) string {
	if isBareWord(s) {
		return s
	}
	return quoteString(s)
}

// quoteString returns s as a quoted .take string.
func quoteString(s string) string {
	var b strings.Builder
	_ = b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == 0x1b:
			b.WriteString(`\e`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case !unicode.IsPrint(r) && r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		case !unicode.IsPrint(r):
			fmt.Fprintf(&b, `\U%08x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	_ = b.WriteByte('"')
	return b.String()
}

// isBareWord reports whether s can be written without quotes: numbers, sizes such as
// 80x24 and names such as xterm-256color or /bin/bash.
func isBareWord(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '#' {
		return false
	}
	for _, c := range []byte(s) {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && strings.IndexByte("_-.:/+", c) < 0 {
			return false
		}
	}
	return true
}

// isTake reports whether data is a .take file: its first line that is not
// blank or a comment is "gotake ...".
func isTake(data []byte) bool {
	for line := range bytes.Lines(data) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		rest, ok := bytes.CutPrefix(line, []byte("gotake"))
		return ok && (len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t')
	}
	return false
}

// readTake parses a .take file.
func readTake(data []byte) (*Cast, error) {
	c := &Cast{Header: Header{Version: 3}}
	started := false
	wait := 0.0
	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		toks, err := splitTake(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if len(toks) == 0 {
			continue
		}
		if !started {
			if len(toks) != 2 || toks[1] != "1" {
				return nil, fmt.Errorf("line %d: unsupported .take version, want \"gotake 1\"", n)
			}
			started = true
			continue
		}
		evs, err := c.takeLine(toks[0], toks[1:])
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", n, toks[0], err)
		}
		if toks[0] == "wait" {
			wait += evs[0].Delay
			continue
		}
		if len(evs) > 0 {
			evs[0].Delay += wait
			wait = 0
		}
		c.Events = append(c.Events, evs...)
	}
	if !started {
		return nil, errors.New("empty recording: missing \"gotake 1\" line")
	}
	return c, nil
}

// takeLine applies a header line to c, or returns the events of an event line.
// For "wait" it returns one event holding just the delay.
func (c *Cast) takeLine(word string, args []string) ([]Event, error) {
	if ok, err := c.headerLine(word, args); ok {
		return nil, err
	}
	switch word {
	case "wait":
		if err := wantArgs(args, 1, 1); err != nil {
			return nil, err
		}
		d, err := parseDelay(args[0])
		return []Event{{Delay: d}}, err
	case "type":
		return typeEvents(args)
	case "event":
		if err := wantArgs(args, 3, 3); err != nil {
			return nil, err
		}
		d, err := parseDelay(args[0])
		return []Event{{Delay: d, Code: args[1], Data: args[2]}}, err
	default:
		return keywordEvent(word, args)
	}
}

// headerLine applies a header line to c. It reports false for other lines.
func (c *Cast) headerLine(word string, args []string) (bool, error) {
	h := &c.Header
	switch word {
	case "term":
		if err := wantArgs(args, 1, 2); err != nil {
			return true, err
		}
		if _, err := fmt.Sscanf(args[0], "%dx%d", &h.Term.Cols, &h.Term.Rows); err != nil {
			return true, fmt.Errorf("size %q, want COLSxROWS", args[0])
		}
		if len(args) > 1 {
			h.Term.Type = args[1]
		}
	case "term-version":
		return true, setString(&h.Term.Version, args)
	case "theme":
		if err := wantArgs(args, 2, 3); err != nil {
			return true, err
		}
		h.Term.Theme = &Theme{FG: args[0], BG: args[1]}
		if len(args) > 2 {
			h.Term.Theme.Palette = args[2]
		}
	case "title":
		return true, setString(&h.Title, args)
	case "command":
		return true, setString(&h.Command, args)
	case "timestamp":
		if err := wantArgs(args, 1, 1); err != nil {
			return true, err
		}
		ts, err := strconv.ParseInt(args[0], 10, 64)
		h.Timestamp = ts
		return true, err
	case "idle":
		if err := wantArgs(args, 1, 1); err != nil {
			return true, err
		}
		d, err := parseDelay(args[0])
		h.IdleTimeLimit = d
		return true, err
	case "env":
		if err := wantArgs(args, 2, 2); err != nil {
			return true, err
		}
		if h.Env == nil {
			h.Env = map[string]string{}
		}
		h.Env[args[0]] = args[1]
	case "tag":
		if err := wantArgs(args, 1, 1); err != nil {
			return true, err
		}
		h.Tags = append(h.Tags, args[0])
	default:
		return false, nil
	}
	return true, nil
}

// keywordEvent returns the event of an out, in, resize, marker or exit line.
func keywordEvent(word string, args []string) ([]Event, error) {
	code := ""
	for k, v := range takeWords {
		if v == word {
			code = k
		}
	}
	if code == "" {
		return nil, errors.New("unknown keyword")
	}
	minArgs := 2
	if code == Marker {
		minArgs = 1
	}
	if err := wantArgs(args, minArgs, 2); err != nil {
		return nil, err
	}
	d, err := parseDelay(args[0])
	e := Event{Delay: d, Code: code}
	if len(args) > 1 {
		e.Data = args[1]
	}
	return []Event{e}, err
}

// typeEvents expands `type DELAY "text" [D2 ... DN | every=D]` into one output
// event per character. After the text is edited by hand the delays may not match
// it any more: missing ones are their average, extra ones are ignored.
func typeEvents(args []string) ([]Event, error) {
	if len(args) < 2 {
		return nil, errors.New("want DELAY \"text\" [delays...]")
	}
	first, err := parseDelay(args[0])
	if err != nil {
		return nil, err
	}
	chars := strings.Split(args[1], "")
	if len(chars) == 0 {
		return nil, errors.New("empty text")
	}
	every := defaultTypeDelay
	var delays []float64
	if rest := args[2:]; len(rest) == 1 && strings.HasPrefix(rest[0], "every=") {
		if every, err = parseDelay(strings.TrimPrefix(rest[0], "every=")); err != nil {
			return nil, err
		}
	} else if len(rest) > 0 {
		sum := 0.0
		for _, s := range rest {
			d, err := parseDelay(s)
			if err != nil {
				return nil, err
			}
			delays = append(delays, d)
			sum += d
		}
		every = sum / float64(len(delays))
	}
	evs := make([]Event, len(chars))
	for i, ch := range chars {
		d := first
		switch {
		case i == 0:
		case i <= len(delays):
			d = delays[i-1]
		default:
			d = every
		}
		evs[i] = Event{Delay: d, Code: Output, Data: ch}
	}
	return evs, nil
}

func wantArgs(args []string, lo, hi int) error {
	if len(args) < lo || len(args) > hi {
		if lo == hi {
			return fmt.Errorf("want %d arguments, got %d", lo, len(args))
		}
		return fmt.Errorf("want %d to %d arguments, got %d", lo, hi, len(args))
	}
	return nil
}

func setString(dst *string, args []string) error {
	if err := wantArgs(args, 1, 1); err != nil {
		return err
	}
	*dst = args[0]
	return nil
}

// parseDelay reads seconds: "0.5", "0.5s" or "500ms".
func parseDelay(s string) (float64, error) {
	scale := 1.0
	if v, ok := strings.CutSuffix(s, "ms"); ok {
		s, scale = v, 0.001
	} else if v, ok := strings.CutSuffix(s, "s"); ok {
		s = v
	}
	d, err := strconv.ParseFloat(s, 64)
	if err != nil || d < 0 || math.IsInf(d, 0) || math.IsNaN(d) {
		return 0, fmt.Errorf("bad delay %q", s)
	}
	return d * scale, nil
}

// splitTake splits a line into words: bare words and quoted strings, up to a comment.
func splitTake(line string) ([]string, error) {
	var toks []string
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '#':
			return toks, nil
		case c == '"':
			s, n, err := unquoteTake(line[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, s)
			i += n
		default:
			j := i
			for j < len(line) && !strings.ContainsRune(" \t\r\n", rune(line[j])) {
				j++
			}
			toks = append(toks, line[i:j])
			i = j
		}
	}
	return toks, nil
}

var simpleEscapes = map[byte]byte{'e': 0x1b, 'r': '\r', 'n': '\n', 't': '\t', '\\': '\\', '"': '"'}

// unquoteTake decodes the quoted string at the start of s and returns how many bytes it used.
func unquoteTake(s string) (string, int, error) {
	var b []byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			return string(b), i + 1, nil
		}
		if c != '\\' {
			b = append(b, c)
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if e, ok := simpleEscapes[s[i]]; ok {
			b = append(b, e)
			continue
		}
		digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[s[i]]
		if digits == 0 || i+digits >= len(s) {
			return "", 0, fmt.Errorf("bad escape \\%c", s[i])
		}
		v, err := strconv.ParseUint(s[i+1:i+1+digits], 16, 32)
		if err != nil {
			return "", 0, fmt.Errorf("bad escape \\%s", s[i:i+1+digits])
		}
		if s[i] == 'x' {
			b = append(b, byte(v))
		} else {
			b = utf8.AppendRune(b, rune(v))
		}
		i += digits
	}
	return "", 0, errors.New("unterminated string")
}
