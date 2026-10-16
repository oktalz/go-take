package cast

import (
	"bufio"
	"encoding/json"
	"io"
	"maps"
	"strings"
)

// Section returns events [from, to) as a new recording. Output and resizes
// before from are folded into one leading event, so the section starts on
// the same screen it had in the full recording.
func (c *Cast) Section(from, to int) (*Cast, error) {
	from, to, err := c.clampRange(from, to)
	if err != nil {
		return nil, err
	}
	n := &Cast{Header: c.Clone().Header}
	var prior strings.Builder
	for _, e := range c.Events[:from] {
		switch e.Code {
		case Output:
			prior.WriteString(e.Data)
		case Resize:
			if prior.Len() > 0 {
				n.Events = append(n.Events, Event{Code: Output, Data: prior.String()})
				prior.Reset()
			}
			n.Events = append(n.Events, Event{Code: Resize, Data: e.Data})
		default:
		}
	}
	if prior.Len() > 0 {
		n.Events = append(n.Events, Event{Code: Output, Data: prior.String()})
	}
	n.Events = append(n.Events, c.Events[from:to]...)
	if from > 0 && len(n.Events) > 0 && from < to {
		// the first real event starts right away instead of after its original pause
		n.Events[len(n.Events)-(to-from)].Delay = 0
	}
	return n, nil
}

// WriteV2 encodes the recording as asciicast v2 (absolute timestamps),
// for tools that do not read v3 yet.
func (c *Cast) WriteV2(w io.Writer) error {
	bw := bufio.NewWriter(w)
	h := c.Header
	env := map[string]string{}
	maps.Copy(env, h.Env)
	if h.Term.Type != "" {
		env["TERM"] = h.Term.Type
	}
	if len(env) == 0 {
		env = nil
	}
	// a struct keeps "version" first, like files written by asciinema
	v2 := struct { // betteralign:ignore
		Version       int               `json:"version"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		Timestamp     int64             `json:"timestamp,omitempty"`
		IdleTimeLimit float64           `json:"idle_time_limit,omitempty"`
		Command       string            `json:"command,omitempty"`
		Title         string            `json:"title,omitempty"`
		Env           map[string]string `json:"env,omitempty"`
		Theme         *Theme            `json:"theme,omitempty"`
	}{
		Version: 2, Width: h.Term.Cols, Height: h.Term.Rows, Timestamp: h.Timestamp,
		IdleTimeLimit: h.IdleTimeLimit, Command: h.Command, Title: h.Title, Env: env, Theme: h.Term.Theme,
	}
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v2); err != nil {
		return err
	}
	t := 0.0
	for _, e := range c.Events {
		t += e.Delay
		if e.Code == Exit {
			continue // not part of v2
		}
		if err := WriteEvent(bw, Event{Delay: t, Code: e.Code, Data: e.Data}); err != nil {
			return err
		}
	}
	return bw.Flush()
}
