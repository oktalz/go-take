package cast

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ClearScreen moves the cursor home and clears the screen.
const ClearScreen = "\x1b[H\x1b[2J"

// AppendOptions controls how Append joins two recordings.
type AppendOptions struct {
	// Pause is the time in seconds between the last event of the recording
	// and the first event of the appended one.
	Pause float64
	// Clear clears the screen where the appended recording starts.
	Clear bool
}

// Append adds the events of o to the end of c, so recordings made in stages
// play as one take. Both must have the same terminal size. A trailing exit
// event of c is dropped, and a caption still showing at its end is hidden.
func (c *Cast) Append(o *Cast, opts AppendOptions) error {
	if opts.Pause < 0 {
		return fmt.Errorf("pause must be >= 0, got %g", opts.Pause)
	}
	cols, rows := c.endSize()
	if cols != o.Header.Term.Cols || rows != o.Header.Term.Rows {
		return fmt.Errorf("terminal size differs: %dx%d, appended recording is %dx%d",
			cols, rows, o.Header.Term.Cols, o.Header.Term.Rows)
	}
	for len(c.Events) > 0 && c.Events[len(c.Events)-1].Code == Exit {
		c.Events = c.Events[:len(c.Events)-1]
	}
	var join []Event
	if c.captionShowing() {
		join = append(join, Event{Code: Caption})
	}
	if opts.Clear {
		join = append(join, Event{Code: Output, Data: ClearScreen})
	}
	start := len(c.Events)
	c.Events = append(c.Events, join...)
	c.Events = append(c.Events, o.Events...)
	if start < len(c.Events) {
		// the join and the first appended event happen together, after the pause
		c.Events[start].Delay = opts.Pause
		for i := start + 1; i < len(c.Events) && i <= start+len(join); i++ {
			c.Events[i].Delay = 0
		}
	}
	return nil
}

// endSize returns the terminal size at the end of the recording.
func (c *Cast) endSize() (cols, rows int) {
	cols, rows = c.Header.Term.Cols, c.Header.Term.Rows
	for _, e := range c.Events {
		if e.Code != Resize {
			continue
		}
		w, h, ok := strings.Cut(e.Data, "x")
		if !ok {
			continue
		}
		cw, err1 := strconv.Atoi(w)
		ch, err2 := strconv.Atoi(h)
		if err1 == nil && err2 == nil {
			cols, rows = cw, ch
		}
	}
	return cols, rows
}

// captionShowing reports whether a caption is still shown at the end.
func (c *Cast) captionShowing() bool {
	for _, e := range slices.Backward(c.Events) {
		if e.Code == Caption {
			return e.Data != ""
		}
	}
	return false
}
