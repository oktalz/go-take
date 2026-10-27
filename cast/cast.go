// Package cast implements the recordings used by gotake.
//
// A recording is a header and a list of events, each with a delay relative to
// the previous one. It is saved in gotake's own .take format (see take.go), or
// in asciicast v3 (https://docs.asciinema.org/manual/asciicast/v3/) for import
// and export: newline-delimited JSON where the first line is a header object
// and every following line is an event `[delay, code, data]`. Lines starting
// with '#' are comments and are ignored.
//
// asciicast v2 files (absolute timestamps) can be read and are converted on load.
package cast

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"strconv"
	"strings"
)

// Event codes.
const (
	Output = "o" // data written to the terminal
	Input  = "i" // data typed by the user (keystrokes)
	Resize = "r" // terminal resize, data is "COLSxROWS"
	Marker = "m" // marker / chapter, data is an optional label
	Exit   = "x" // process exit, data is the exit status
)

// Theme is the terminal color theme.
type Theme struct {
	FG      string `json:"fg"`
	BG      string `json:"bg"`
	Palette string `json:"palette,omitempty"`
}

// Term describes the recorded terminal.
type Term struct { // betteralign:ignore
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
	Theme   *Theme `json:"theme,omitempty"`
	// CellWidth and CellHeight are the size of a cell in pixels, when the
	// terminal told (gotake extension). Images are sized by them.
	CellWidth  int `json:"cell_width,omitempty"`
	CellHeight int `json:"cell_height,omitempty"`
}

// Header is the first line of a recording.
// Field order is the JSON key order in files, so it is kept readable on purpose.
type Header struct { // betteralign:ignore
	Version       int               `json:"version"`
	Term          Term              `json:"term"`
	Timestamp     int64             `json:"timestamp,omitempty"`
	IdleTimeLimit float64           `json:"idle_time_limit,omitempty"`
	Command       string            `json:"command,omitempty"`
	Title         string            `json:"title,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
}

// Event is a single recorded event.
type Event struct {
	Code  string
	Data  string
	Delay float64 // seconds since the previous event
}

// MarshalJSON encodes the event as `[delay, code, data]`.
func (e Event) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("[")
	b.WriteString(FormatDelay(e.Delay))
	b.WriteString(", ")
	writeJSONString(&b, e.Code)
	b.WriteString(", ")
	writeJSONString(&b, e.Data)
	b.WriteString("]")
	return b.Bytes(), nil
}

// UnmarshalJSON decodes an event from `[delay, code, data]`.
func (e *Event) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) != 3 {
		return fmt.Errorf("event must have 3 elements, got %d", len(raw))
	}
	if err := json.Unmarshal(raw[0], &e.Delay); err != nil {
		return fmt.Errorf("event delay: %w", err)
	}
	if err := json.Unmarshal(raw[1], &e.Code); err != nil {
		return fmt.Errorf("event code: %w", err)
	}
	if err := json.Unmarshal(raw[2], &e.Data); err != nil {
		return fmt.Errorf("event data: %w", err)
	}
	return nil
}

// Cast is a full recording.
type Cast struct {
	Events []Event
	Header Header
}

// New returns an empty recording for a terminal of the given size.
func New(cols, rows int) *Cast {
	return &Cast{Header: Header{Version: 3, Term: Term{Cols: cols, Rows: rows}}}
}

// Clone returns a deep copy of the recording.
func (c *Cast) Clone() *Cast {
	n := &Cast{Header: c.Header, Events: make([]Event, len(c.Events))}
	copy(n.Events, c.Events)
	if c.Header.Env != nil {
		n.Header.Env = maps.Clone(c.Header.Env)
	}
	n.Header.Tags = append([]string(nil), c.Header.Tags...)
	if c.Header.Term.Theme != nil {
		t := *c.Header.Term.Theme
		n.Header.Term.Theme = &t
	}
	return n
}

// Times returns the absolute time of every event.
func (c *Cast) Times() []float64 {
	ts := make([]float64, len(c.Events))
	t := 0.0
	for i, e := range c.Events {
		t += e.Delay
		ts[i] = t
	}
	return ts
}

// Duration is the absolute time of the last event.
func (c *Cast) Duration() float64 {
	t := 0.0
	for _, e := range c.Events {
		t += e.Delay
	}
	return t
}

// IndexAt returns the index of the first event at or after absolute time t.
func (c *Cast) IndexAt(t float64) int {
	acc := 0.0
	for i, e := range c.Events {
		acc += e.Delay
		if acc >= t-1e-9 {
			return i
		}
	}
	return len(c.Events)
}

// Load reads a recording from a file.
func Load(path string) (*Cast, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Save writes a recording to a file atomically, as asciicast v3 for a .cast
// file and as .take for anything else.
func (c *Cast) Save(path string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := c.Write(f, FormatFor(path)); err != nil {
		return errors.Join(err, f.Close(), os.Remove(tmp))
	}
	if err := f.Close(); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return os.Rename(tmp, path)
}

type headerV2 struct {
	Env           map[string]string `json:"env"`
	Theme         *Theme            `json:"theme"`
	Command       string            `json:"command"`
	Title         string            `json:"title"`
	Version       int               `json:"version"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	Timestamp     int64             `json:"timestamp"`
	IdleTimeLimit float64           `json:"idle_time_limit"`
}

// Read parses a .take, asciicast v2 or asciicast v3 recording.
func Read(r io.Reader) (*Cast, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if isTake(data) {
		return readTake(data)
	}
	return readAsciicast(bytes.NewReader(data))
}

// readAsciicast parses an asciicast v2 or v3 recording.
func readAsciicast(r io.Reader) (*Cast, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	c := &Cast{}
	version := 0
	prev := 0.0
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 || b[0] == '#' {
			continue
		}
		if version == 0 {
			var probe struct {
				Version int `json:"version"`
			}
			if err := json.Unmarshal(b, &probe); err != nil {
				return nil, fmt.Errorf("line %d: header: %w", line, err)
			}
			switch probe.Version {
			case 2:
				var h headerV2
				if err := json.Unmarshal(b, &h); err != nil {
					return nil, fmt.Errorf("line %d: header: %w", line, err)
				}
				c.Header = Header{
					Version:       3,
					Term:          Term{Cols: h.Width, Rows: h.Height, Theme: h.Theme},
					Timestamp:     h.Timestamp,
					IdleTimeLimit: h.IdleTimeLimit,
					Command:       h.Command,
					Title:         h.Title,
					Env:           h.Env,
				}
				if t, ok := h.Env["TERM"]; ok {
					c.Header.Term.Type = t
				}
			case 3:
				if err := json.Unmarshal(b, &c.Header); err != nil {
					return nil, fmt.Errorf("line %d: header: %w", line, err)
				}
			default:
				return nil, fmt.Errorf("unsupported asciicast version %d", probe.Version)
			}
			version = probe.Version
			continue
		}
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if version == 2 {
			abs := e.Delay
			e.Delay = math.Max(0, abs-prev)
			prev = abs
		}
		if e.Delay < 0 {
			e.Delay = 0
		}
		c.Events = append(c.Events, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, errors.New("empty recording: missing header")
	}
	return c, nil
}

// Write encodes the recording in format f.
func (c *Cast) Write(w io.Writer, f Format) error {
	enc := NewEncoder(w, f)
	if err := enc.WriteHeader(c.Header); err != nil {
		return err
	}
	for _, e := range c.Events {
		if err := enc.WriteEvent(e); err != nil {
			return err
		}
	}
	return enc.Close()
}

// WriteHeader writes the header line.
func WriteHeader(w io.Writer, h Header) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(h); err != nil {
		return err
	}
	_, err := w.Write(b.Bytes())
	return err
}

// WriteEvent writes a single event line.
func WriteEvent(w io.Writer, e Event) error {
	b, _ := e.MarshalJSON()
	b = append(b, '\n')
	_, err := w.Write(b)
	return err
}

// FormatDelay formats a delay with microsecond precision and no trailing zeros.
func FormatDelay(d float64) string {
	s := strconv.FormatFloat(d, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

func writeJSONString(b *bytes.Buffer, s string) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Truncate(b.Len() - 1) // drop newline added by Encode
}
