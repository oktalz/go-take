// Package record records a terminal session into a .take or asciicast file.
package record

import (
	"cmp"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/oktalz/go-take/cast"
)

type session interface {
	io.ReadWriteCloser
	resize(cols, rows int) error
	wait() int
}

// Options configures a recording.
type Options struct {
	Command       string // command to run, defaults to $SHELL
	Title         string // recording title
	Env           []string
	IdleTimeLimit float64 // pauses longer than this are shortened while recording (0 = off)
	Cols, Rows    int     // override terminal size (0 = use current terminal)
	CaptureInput  bool    // also record keystrokes as "i" events
	Format        cast.Format
}

// Record runs the command in a pseudo terminal and streams events to out.
// Events are written as they happen, so an interrupted recording is still usable.
func Record(out io.Writer, opts Options) error {
	restoreVT := EnableVT()
	defer restoreVT()

	shell := defaultShell()
	command := cmp.Or(opts.Command, shell)
	stdinFd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(stdinFd)
	cols, rows := termSize(opts.Cols, opts.Rows)
	termType := cmp.Or(os.Getenv("TERM"), "xterm-256color")

	enc := cast.NewEncoder(out, opts.Format)
	err := enc.WriteHeader(cast.Header{
		Version:       3,
		Term:          cast.Term{Cols: cols, Rows: rows, Type: termType},
		Timestamp:     time.Now().Unix(),
		IdleTimeLimit: opts.IdleTimeLimit,
		Command:       command,
		Title:         opts.Title,
		Env:           map[string]string{"SHELL": os.Getenv("SHELL")},
	})
	if err == nil {
		err = enc.Flush()
	}
	if err != nil {
		return err
	}

	env := append(os.Environ(), "GOTAKE_RECORDING=1", "TERM="+termType)
	env = append(env, opts.Env...)

	ptmx, err := startPTY(opts.Command, shell, env, cols, rows)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	w := &eventWriter{enc: enc, last: time.Now(), idle: opts.IdleTimeLimit}
	if isTTY {
		old, err := term.MakeRaw(stdinFd)
		if err != nil {
			return err
		}
		defer func() { _ = term.Restore(stdinFd, old) }()
		stop := forwardResize(ptmx, cols, rows, w)
		defer stop()
	}
	var onInput func(string)
	if opts.CaptureInput {
		onInput = func(s string) { w.emit(cast.Input, s) }
	}
	go copyInput(ptmx, onInput)
	if err := copyOutput(ptmx, w); err != nil {
		return errors.Join(err, w.close())
	}

	status := ptmx.wait()
	w.emit(cast.Exit, strconv.Itoa(status))
	return w.close()
}

// termSize picks the recording size: explicit values, else the current terminal, else 80x24.
func termSize(cols, rows int) (c, r int) {
	if term.IsTerminal(int(os.Stdin.Fd())) && (cols == 0 || rows == 0) {
		if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			cols, rows = orDefault(cols, w), orDefault(rows, h)
		}
	}
	return orDefault(cols, 80), orDefault(rows, 24)
}

// copyInput forwards stdin to the pty, passing keystrokes to onInput when set.
func copyInput(ptmx io.Writer, onInput func(string)) {
	buf := make([]byte, 4096)
	var dec utf8Buffer
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if s := dec.decode(buf[:n]); onInput != nil && s != "" {
				onInput(s)
			}
			if _, werr := ptmx.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// copyOutput forwards pty output to stdout and records it until the child exits.
func copyOutput(ptmx io.Reader, w *eventWriter) error {
	buf := make([]byte, 32*1024)
	var dec utf8Buffer
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if _, werr := os.Stdout.Write(buf[:n]); werr != nil {
				return werr
			}
			if s := dec.decode(buf[:n]); s != "" {
				w.emit(cast.Output, s)
			}
		}
		if err != nil {
			if s := dec.flush(); s != "" {
				w.emit(cast.Output, s)
			}
			if childExited(err) {
				return nil
			}
			return err
		}
	}
}

func orDefault(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}

type eventWriter struct {
	last time.Time
	err  error
	enc  cast.Encoder
	idle float64
	mu   sync.Mutex
}

func (ew *eventWriter) emit(code, data string) {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	now := time.Now()
	d := now.Sub(ew.last).Seconds()
	ew.last = now
	if ew.idle > 0 && d > ew.idle {
		d = ew.idle
	}
	if ew.err == nil {
		ew.err = ew.enc.WriteEvent(cast.Event{Delay: d, Code: code, Data: data})
	}
	if ew.err == nil {
		ew.err = ew.enc.Flush()
	}
}

func (ew *eventWriter) close() error {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if ew.err != nil {
		return ew.err
	}
	return ew.enc.Close()
}

// utf8Buffer turns a byte stream into valid UTF-8 strings, holding back
// incomplete multi-byte sequences split across reads.
type utf8Buffer struct{ pending []byte }

func (u *utf8Buffer) decode(b []byte) string {
	data := append(u.pending, b...)
	u.pending = nil
	// find how many trailing bytes form an incomplete rune
	cut := len(data)
	for i := 1; i <= 3 && i <= len(data); i++ {
		c := data[len(data)-i]
		if c < 0x80 {
			break
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(data[len(data)-i:]) {
				cut = len(data) - i
			}
			break
		}
	}
	u.pending = append([]byte(nil), data[cut:]...)
	return strings.ToValidUTF8(string(data[:cut]), "�")
}

func (u *utf8Buffer) flush() string {
	s := strings.ToValidUTF8(string(u.pending), "�")
	u.pending = nil
	return s
}
