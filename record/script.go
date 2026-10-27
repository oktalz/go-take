package record

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oktalz/go-take/cast"
)

// A Script records a terminal session from steps instead of a person typing,
// so a demo can be recorded again whenever what it shows changes:
//
//	size 80x24
//	shell "bash --norc"
//	hide                  # set up without recording it
//	send "PS1='$ '\r"
//	wait "^\$ $"
//	show
//	type "ls --color"
//	enter
//	wait "^\$ $"          # the prompt is back
//	sleep 2
//
// Settings (before the first step): size COLSxROWS, shell COMMAND,
// title TEXT, env NAME VALUE, typing DELAY [JITTER], timeout DELAY.
//
// Steps: type TEXT [DELAY], send TEXT (all at once), a key (enter, tab,
// space, backspace, esc, up, down, left, right, home, end, pgup, pgdown,
// delete, ctrl+X) with an optional count, sleep DELAY, wait REGEXP [TIMEOUT]
// (for output since the last keystroke, escape sequences removed),
// hide and show (leave a part out of the recording, such as setup; what is
// still on screen from it, the prompt or everything since a clear, is kept),
// marker [LABEL], and caption [TEXT] (shown over the terminal until the next
// caption; without TEXT it hides the caption).
type Script struct {
	Title      string
	Shell      string // command to run, default $SHELL
	Env        []string
	Steps      []Step
	Cols, Rows int
	TypeDelay  float64 // seconds between typed characters
	TypeJitter float64 // random variation of TypeDelay, in seconds
	Timeout    float64 // default for wait, in seconds
}

// Step is one line of a script.
type Step struct {
	Re    *regexp.Regexp // wait
	Kind  string         // type, send, key, sleep, wait, hide, show, marker, caption
	Text  string         // what type, send and key send; the marker label
	Line  int
	Count int     // key repetitions
	Delay float64 // seconds: sleep, wait timeout, type delay (0: script default)
}

var keys = map[string]string{
	"enter": "\r", "tab": "\t", "space": " ", "backspace": "\x7f", "esc": "\x1b",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pgup": "\x1b[5~", "pgdown": "\x1b[6~", "delete": "\x1b[3~",
}

// keyText returns what a key sends: a name from keys, or ctrl+ a letter.
func keyText(name string) (string, bool) {
	if s, ok := keys[name]; ok {
		return s, true
	}
	if l, ok := strings.CutPrefix(name, "ctrl+"); ok && len(l) == 1 && l[0] >= 'a' && l[0] <= 'z' {
		return string(rune(l[0] - 'a' + 1)), true
	}
	return "", false
}

// ParseScript reads a script.
func ParseScript(r io.Reader) (*Script, error) {
	s := &Script{Cols: 80, Rows: 24, TypeDelay: 0.08, TypeJitter: 0.04, Timeout: 10}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for n := 1; sc.Scan(); n++ {
		words, err := cast.SplitLine(sc.Text())
		if err == nil && len(words) > 0 {
			err = s.line(n, words[0], words[1:])
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
	}
	return s, sc.Err()
}

func (s *Script) line(n int, word string, args []string) error {
	if ok, err := s.setting(word, args); ok {
		if err == nil && len(s.Steps) > 0 {
			err = fmt.Errorf("%s must come before the first step", word)
		}
		return err
	}
	st := Step{Kind: word, Line: n, Count: 1}
	switch word {
	case "type", "send":
		if err := argCount(args, 1, 2); err != nil {
			return err
		}
		st.Text = args[0]
		if len(args) > 1 {
			if err := delayArg(&st.Delay, args[1]); err != nil {
				return err
			}
		}
	case "sleep":
		if err := argCount(args, 1, 1); err != nil {
			return err
		}
		if err := delayArg(&st.Delay, args[0]); err != nil {
			return err
		}
	case "wait":
		if err := argCount(args, 1, 2); err != nil {
			return err
		}
		re, err := regexp.Compile("(?m)" + args[0])
		if err != nil {
			return err
		}
		st.Re = re
		if len(args) > 1 {
			if err := delayArg(&st.Delay, args[1]); err != nil {
				return err
			}
		}
	case "hide", "show":
		if err := argCount(args, 0, 0); err != nil {
			return err
		}
	case "marker", "caption":
		if err := argCount(args, 0, 1); err != nil {
			return err
		}
		if len(args) > 0 {
			st.Text = args[0]
		}
	default:
		text, ok := keyText(word)
		if !ok {
			return fmt.Errorf("unknown step %q", word)
		}
		if err := argCount(args, 0, 1); err != nil {
			return err
		}
		if len(args) > 0 {
			c, err := strconv.Atoi(args[0])
			if err != nil || c < 1 {
				return fmt.Errorf("bad count %q", args[0])
			}
			st.Count = c
		}
		st.Kind, st.Text = "key", text
	}
	s.Steps = append(s.Steps, st)
	return nil
}

// setting applies a setting line; it reports false for steps.
func (s *Script) setting(word string, args []string) (bool, error) {
	switch word {
	case "size":
		if err := argCount(args, 1, 1); err != nil {
			return true, err
		}
		if _, err := fmt.Sscanf(args[0], "%dx%d", &s.Cols, &s.Rows); err != nil || s.Cols < 1 || s.Rows < 1 {
			return true, fmt.Errorf("size %q, want COLSxROWS", args[0])
		}
	case "shell", "title":
		if err := argCount(args, 1, 1); err != nil {
			return true, err
		}
		if word == "shell" {
			s.Shell = args[0]
		} else {
			s.Title = args[0]
		}
	case "env":
		if err := argCount(args, 2, 2); err != nil {
			return true, err
		}
		s.Env = append(s.Env, args[0]+"="+args[1])
	case "typing":
		if err := argCount(args, 1, 2); err != nil {
			return true, err
		}
		if err := delayArg(&s.TypeDelay, args[0]); err != nil {
			return true, err
		}
		if len(args) > 1 {
			return true, delayArg(&s.TypeJitter, args[1])
		}
	case "timeout":
		if err := argCount(args, 1, 1); err != nil {
			return true, err
		}
		return true, delayArg(&s.Timeout, args[0])
	default:
		return false, nil
	}
	return true, nil
}

func argCount(args []string, lo, hi int) error {
	if len(args) < lo || len(args) > hi {
		if lo == hi {
			return fmt.Errorf("want %d arguments, got %d", lo, len(args))
		}
		return fmt.Errorf("want %d to %d arguments, got %d", lo, hi, len(args))
	}
	return nil
}

func delayArg(dst *float64, s string) error {
	d, err := cast.ParseDelay(s)
	if err == nil {
		*dst = d
	}
	return err
}

// scriptRun is a script being recorded.
type scriptRun struct {
	w       *eventWriter
	pty     session
	changed chan struct{} // output came
	exited  chan struct{} // the shell exited
	s       *Script
	rng     *rand.Rand
	seen    strings.Builder // output since the last keystroke, for wait
	hid     strings.Builder // output while hidden
	mu      sync.Mutex
	hidden  bool
}

// RunScript records s to out, showing the session on live (may be io.Discard).
// The recording ends after the last step, once output has settled.
func RunScript(out, live io.Writer, s *Script, format cast.Format) error {
	restoreVT := EnableVT()
	defer restoreVT()
	shell := defaultShell()
	termType := cmp.Or(os.Getenv("TERM"), "xterm-256color")
	enc := cast.NewEncoder(out, format)
	err := enc.WriteHeader(cast.Header{
		Version:   3,
		Term:      cast.Term{Cols: s.Cols, Rows: s.Rows, Type: termType},
		Timestamp: time.Now().Unix(),
		Command:   s.Shell,
		Title:     s.Title,
	})
	if err == nil {
		err = enc.Flush()
	}
	if err != nil {
		return err
	}
	env := append(os.Environ(), "GOTAKE_RECORDING=1", "TERM="+termType)
	ptmx, err := startPTY(s.Shell, shell, append(env, s.Env...), s.Cols, s.Rows, cellSize{})
	if err != nil {
		return err
	}
	r := &scriptRun{
		w: &eventWriter{enc: enc, last: time.Now()}, pty: ptmx, s: s,
		changed: make(chan struct{}, 1), exited: make(chan struct{}),
		rng: rand.New(rand.NewPCG(1, 2)), //nolint:gosec // typing rhythm, not security
	}
	outErr := make(chan error, 1)
	go func() {
		outErr <- copyOutput(ptmx, live, r.output)
		close(r.exited)
	}()

	err = r.steps()
	if err == nil {
		r.settle()
	}
	_ = ptmx.Close() // hangs up the shell
	select {
	case <-r.exited:
		err = errors.Join(err, <-outErr)
	case <-time.After(2 * time.Second):
	}
	return errors.Join(err, r.w.close())
}

func (r *scriptRun) output(s string) {
	r.mu.Lock()
	hidden := r.hidden
	// as lines read: no escape sequences, and no carriage returns (shells put one before the prompt)
	_, _ = r.seen.WriteString(strings.ReplaceAll(cast.StripANSI(s), "\r", ""))
	if hidden {
		_, _ = r.hid.WriteString(s)
	}
	r.mu.Unlock()
	if !hidden {
		r.w.emit(cast.Output, s)
	}
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *scriptRun) steps() error {
	for _, st := range r.s.Steps {
		if err := r.step(st); err != nil {
			return fmt.Errorf("line %d: %s: %w", st.Line, st.Kind, err)
		}
	}
	return nil
}

func (r *scriptRun) step(st Step) error {
	switch st.Kind {
	case "type":
		delay := cmp.Or(st.Delay, r.s.TypeDelay)
		for _, ch := range st.Text {
			if err := r.send(string(ch)); err != nil {
				return err
			}
			jitter := (r.rng.Float64()*2 - 1) * r.s.TypeJitter
			sleep(max(delay+jitter, 0))
		}
	case "send":
		return r.send(st.Text)
	case "key":
		for range st.Count {
			if err := r.send(st.Text); err != nil {
				return err
			}
			sleep(r.s.TypeDelay)
		}
	case "sleep":
		sleep(st.Delay)
	case "wait":
		return r.wait(st.Re, cmp.Or(st.Delay, r.s.Timeout))
	case "hide", "show":
		r.mu.Lock()
		r.hidden = st.Kind == "hide"
		hid := r.hid.String()
		r.hid.Reset()
		r.mu.Unlock()
		if !r.hidden {
			r.w.mu.Lock()
			r.w.last = time.Now() // the hidden part takes no time in the recording
			r.w.mu.Unlock()
			if s := onScreen(hid); s != "" {
				r.w.emit(cast.Output, s)
			}
		}
	case "marker":
		r.w.emit(cast.Marker, st.Text)
	case "caption":
		r.w.emit(cast.Caption, st.Text)
	default:
	}
	return nil
}

// onScreen returns the end of hidden output that is still on screen when it
// is shown again: everything since the screen was last cleared, or else the
// line the cursor is on (usually the prompt).
func onScreen(hidden string) string {
	at := max(strings.LastIndex(hidden, "\x1b[2J"), strings.LastIndex(hidden, "\x1bc"))
	if at >= 0 {
		return hidden[at:]
	}
	return hidden[strings.LastIndexByte(hidden, '\n')+1:]
}

func (r *scriptRun) send(s string) error {
	r.mu.Lock()
	r.seen.Reset()
	r.mu.Unlock()
	_, err := r.pty.Write([]byte(s))
	return err
}

// wait waits until the output since the last keystroke matches re.
func (r *scriptRun) wait(re *regexp.Regexp, timeout float64) error {
	deadline := time.After(time.Duration(timeout * float64(time.Second)))
	for {
		r.mu.Lock()
		ok := re.MatchString(r.seen.String())
		r.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-r.changed:
		case <-r.exited:
			return errors.New("the shell exited")
		case <-deadline:
			r.mu.Lock()
			seen := r.seen.String()
			r.mu.Unlock()
			if len(seen) > 200 {
				seen = "…" + seen[len(seen)-200:]
			}
			return fmt.Errorf("no output matching %q within %gs; the output was %q", re.String()[4:], timeout, seen)
		}
	}
}

// settle waits until output stops for a moment (at most a few seconds), so
// the output of the last step is in the recording.
func (r *scriptRun) settle() {
	const quiet, most = 500 * time.Millisecond, 5 * time.Second
	limit := time.After(most)
	for {
		select {
		case <-r.changed:
		case <-time.After(quiet):
			return
		case <-r.exited:
			return
		case <-limit:
			return
		}
	}
}

func sleep(seconds float64) { time.Sleep(time.Duration(seconds * float64(time.Second))) }
