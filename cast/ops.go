package cast

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// All edit operations work on a half-open event index range [from, to).
// Use IndexAt to translate absolute times into indices.

func (c *Cast) clampRange(from, to int) (start, end int, err error) {
	if from < 0 {
		from = 0
	}
	if to > len(c.Events) {
		to = len(c.Events)
	}
	if from > to {
		return 0, 0, fmt.Errorf("invalid range [%d, %d)", from, to)
	}
	return from, to, nil
}

func (c *Cast) clampIndex(at int) int {
	if at < 0 {
		return 0
	}
	if at > len(c.Events) {
		return len(c.Events)
	}
	return at
}

// Delete removes events [from, to) together with the time they occupied.
func (c *Cast) Delete(from, to int) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	c.Events = append(c.Events[:from], c.Events[to:]...)
	return nil
}

// Stretch multiplies the delays of events [from, to) by factor.
// A factor of 2 makes the section twice as slow, 0.5 twice as fast.
func (c *Cast) Stretch(from, to int, factor float64) error {
	if factor < 0 {
		return errors.New("factor must be >= 0")
	}
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	for i := from; i < to; i++ {
		c.Events[i].Delay *= factor
	}
	return nil
}

// StretchTo scales events [from, to) so the section lasts exactly d seconds.
func (c *Cast) StretchTo(from, to int, d float64) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	total := 0.0
	for i := from; i < to; i++ {
		total += c.Events[i].Delay
	}
	if total == 0 {
		return c.SetDelay(from, to, d/float64(max(1, to-from)))
	}
	return c.Stretch(from, to, d/total)
}

// SetDelay sets the delay of every event in [from, to) to d.
func (c *Cast) SetDelay(from, to int, d float64) error {
	if d < 0 {
		return errors.New("delay must be >= 0")
	}
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	for i := from; i < to; i++ {
		c.Events[i].Delay = d
	}
	return nil
}

// CapIdle limits every pause in [from, to) to at most limit seconds.
func (c *Cast) CapIdle(from, to int, limit float64) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	for i := from; i < to; i++ {
		if c.Events[i].Delay > limit {
			c.Events[i].Delay = limit
		}
	}
	return nil
}

// Pause adds d seconds of extra time before event at.
// Using at == len(Events) appends a marker carrying the pause at the end.
func (c *Cast) Pause(at int, d float64) {
	at = c.clampIndex(at)
	if at == len(c.Events) {
		c.Events = append(c.Events, Event{Delay: d, Code: Marker})
		return
	}
	c.Events[at].Delay += d
	if c.Events[at].Delay < 0 {
		c.Events[at].Delay = 0
	}
}

// Insert inserts events before index at.
func (c *Cast) Insert(at int, evs ...Event) {
	at = c.clampIndex(at)
	n := make([]Event, 0, len(c.Events)+len(evs))
	n = append(n, c.Events[:at]...)
	n = append(n, evs...)
	n = append(n, c.Events[at:]...)
	c.Events = n
}

// Duplicate copies events [from, to) and inserts the copy right after the range.
func (c *Cast) Duplicate(from, to int) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	cp := append([]Event(nil), c.Events[from:to]...)
	c.Insert(to, cp...)
	return nil
}

// Move moves events [from, to) so they start at index at
// (an index in the recording before the move).
func (c *Cast) Move(from, to, at int) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	at = c.clampIndex(at)
	if at >= from && at <= to {
		return nil
	}
	block := append([]Event(nil), c.Events[from:to]...)
	rest := append(append([]Event(nil), c.Events[:from]...), c.Events[to:]...)
	if at > to {
		at -= to - from
	}
	c.Events = rest
	c.Insert(at, block...)
	return nil
}

// Update replaces a single event.
func (c *Cast) Update(i int, e Event) error {
	if i < 0 || i >= len(c.Events) {
		return fmt.Errorf("event %d out of range", i)
	}
	if e.Delay < 0 {
		e.Delay = 0
	}
	c.Events[i] = e
	return nil
}

// Merge joins consecutive output events in [from, to) into one event,
// keeping the delay of the first one. Time of the merged events is dropped.
func (c *Cast) Merge(from, to int) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	out := append([]Event(nil), c.Events[:from]...)
	for i := from; i < to; i++ {
		e := c.Events[i]
		if len(out) > from && e.Code == Output && out[len(out)-1].Code == Output {
			out[len(out)-1].Data += e.Data
			continue
		}
		out = append(out, e)
	}
	out = append(out, c.Events[to:]...)
	c.Events = out
	return nil
}

// Split breaks output events in [from, to) into one event per line,
// distributing the original delay evenly between the parts.
func (c *Cast) Split(from, to int) error {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return err
	}
	out := append([]Event(nil), c.Events[:from]...)
	for i := from; i < to; i++ {
		e := c.Events[i]
		if e.Code != Output || !strings.Contains(strings.TrimSuffix(e.Data, "\n"), "\n") {
			out = append(out, e)
			continue
		}
		parts := strings.SplitAfter(e.Data, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		d := e.Delay / float64(len(parts))
		for _, p := range parts {
			out = append(out, Event{Delay: d, Code: Output, Data: p})
		}
	}
	out = append(out, c.Events[to:]...)
	c.Events = out
	return nil
}

// TypingOptions controls NormalizeTyping.
type TypingOptions struct {
	Delay  float64 `json:"delay"`  // delay between keystrokes in seconds
	Jitter float64 `json:"jitter"` // random +/- variation in seconds, makes typing look human
	Seed   uint64  `json:"seed"`   // random seed, for reproducible results
}

var ansiRe = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[PX^_][^\x1b]*\x1b\\|[@-Z\\-_])`)

// StripANSI removes terminal escape sequences.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// IsKeystroke reports whether an event looks like a single typed key:
// an input event, or output whose visible part is one or two characters
// (shell echo of a keystroke, possibly decorated with escape sequences).
func IsKeystroke(e Event) bool {
	switch e.Code {
	case Input:
		return true
	case Output:
		v := StripANSI(e.Data)
		if strings.ContainsAny(v, "\r\n") {
			return false
		}
		n := utf8.RuneCountInString(v)
		return n >= 1 && n <= 2 && len(e.Data) <= 64
	}
	return false
}

// NormalizeTyping evens out typing speed in [from, to).
// It finds runs of keystrokes and sets the delay between them to opts.Delay
// (+/- opts.Jitter). The first keystroke of each run keeps its delay so the
// pause before someone starts typing is preserved. Output that immediately
// follows an input event is treated as its echo and left attached to it.
// It returns the number of adjusted events.
func (c *Cast) NormalizeTyping(from, to int, opts TypingOptions) (int, error) {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return 0, err
	}
	if opts.Delay <= 0 {
		opts.Delay = 0.08
	}
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	hasInput := false
	for i := from; i < to; i++ {
		if c.Events[i].Code == Input {
			hasInput = true
			break
		}
	}
	changed := 0
	inRun := false
	for i := from; i < to; i++ {
		e := &c.Events[i]
		echo := i > 0 && e.Code == Output && c.Events[i-1].Code == Input && e.Delay < 0.05
		key := IsKeystroke(*e) && !echo
		if hasInput {
			// with captured input, input events are the ground truth
			key = e.Code == Input
		}
		switch {
		case echo:
			e.Delay = 0
		case key && inRun:
			d := opts.Delay
			if opts.Jitter > 0 {
				d += (rng.Float64()*2 - 1) * opts.Jitter
			}
			e.Delay = max(0.005, d)
			changed++
		case key:
			inRun = true
		case e.Code == Marker || e.Code == Resize:
		default:
			inRun = false
		}
	}
	return changed, nil
}

// InjectOptions describes a command to inject into a recording.
type InjectOptions struct {
	Prompt      string  `json:"prompt"`       // printed before typing starts (e.g. "$ "), may be empty
	Command     string  `json:"command"`      // typed character by character
	Output      string  `json:"output"`       // printed after Enter, line by line ("\n" becomes "\r\n")
	After       string  `json:"after"`        // printed after the output, e.g. the next prompt
	PreDelay    float64 `json:"pre_delay"`    // pause before the prompt / first keystroke
	TypingDelay float64 `json:"typing_delay"` // delay between keystrokes
	Jitter      float64 `json:"jitter"`       // random variation of typing delay
	EnterDelay  float64 `json:"enter_delay"`  // pause between the last keystroke and Enter
	OutputDelay float64 `json:"output_delay"` // pause between Enter and the first output line
	LineDelay   float64 `json:"line_delay"`   // delay between output lines
	RecordInput bool    `json:"record_input"` // also emit "i" events for keystrokes
	Seed        uint64  `json:"seed"`
}

// DefaultInjectOptions returns sensible timing defaults.
func DefaultInjectOptions() InjectOptions {
	return InjectOptions{
		PreDelay:    0.5,
		TypingDelay: 0.08,
		Jitter:      0.03,
		EnterDelay:  0.4,
		OutputDelay: 0.15,
		LineDelay:   0.02,
	}
}

// ToCRLF converts bare "\n" line endings to "\r\n" as a terminal expects.
func ToCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// CommandEvents generates the events for typing a command and showing its output.
func CommandEvents(o InjectOptions) []Event {
	rng := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	var evs []Event
	pending := o.PreDelay
	emit := func(code, data string) {
		evs = append(evs, Event{Delay: pending, Code: code, Data: data})
		pending = 0
	}
	if o.Prompt != "" {
		emit(Output, ToCRLF(o.Prompt))
		pending = o.TypingDelay * 3
	}
	first := true
	for _, r := range o.Command {
		if !first {
			pending = o.TypingDelay
			if o.Jitter > 0 {
				pending += (rng.Float64()*2 - 1) * o.Jitter
			}
			pending = max(0.005, pending)
		}
		first = false
		if o.RecordInput {
			emit(Input, string(r))
		}
		emit(Output, string(r))
	}
	if o.Command != "" || o.Prompt != "" {
		pending = o.EnterDelay
		if o.RecordInput {
			emit(Input, "\r")
		}
		emit(Output, "\r\n")
	}
	if o.Output != "" {
		pending = o.OutputDelay
		text := strings.ReplaceAll(o.Output, "\r\n", "\n")
		if !strings.HasSuffix(text, "\n") {
			text += "\n" // command output ends at the start of a new line
		}
		lines := strings.SplitAfter(text, "\n")
		for _, l := range lines {
			if l == "" {
				continue
			}
			emit(Output, ToCRLF(l))
			pending = o.LineDelay
		}
	}
	if o.After != "" {
		pending = max(pending, o.OutputDelay)
		emit(Output, ToCRLF(o.After))
	}
	return evs
}

// InjectCommand inserts a typed command and its output before index at.
// It returns the number of inserted events.
func (c *Cast) InjectCommand(at int, o InjectOptions) int {
	evs := CommandEvents(o)
	c.Insert(at, evs...)
	return len(evs)
}

// Markers returns the indices of all marker events.
func (c *Cast) Markers() []int {
	var m []int
	for i, e := range c.Events {
		if e.Code == Marker {
			m = append(m, i)
		}
	}
	return m
}

// MarkerTime returns the time of the first marker with the given label. An
// exact match wins over one that differs only in case.
func (c *Cast) MarkerTime(label string) (float64, error) {
	times := c.Times()
	found := -1
	for _, i := range c.Markers() {
		if c.Events[i].Data == label {
			return times[i], nil
		}
		if found < 0 && strings.EqualFold(c.Events[i].Data, label) {
			found = i
		}
	}
	if found >= 0 {
		return times[found], nil
	}
	var labels []string
	for _, i := range c.Markers() {
		if c.Events[i].Data != "" {
			labels = append(labels, strconv.Quote(c.Events[i].Data))
		}
	}
	if len(labels) == 0 {
		return 0, fmt.Errorf("no marker %q: the recording has no named markers", label)
	}
	return 0, fmt.Errorf("no marker %q (markers: %s)", label, strings.Join(labels, ", "))
}
