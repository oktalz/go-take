// Package play replays a recording in the current terminal.
package play

import (
	"context"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/oktalz/go-take/cast"
)

// Options configures playback.
type Options struct {
	Out           io.Writer
	Speed         float64 // playback speed multiplier (default 1)
	IdleTimeLimit float64 // cap pauses to this many seconds (0 = use header value)
	From          float64 // start here: output before it is printed at once (seconds)
	To            float64 // stop here (seconds, 0 = play to the end)
}

type action int

const (
	proceed action = iota
	quit
)

type player struct {
	keys   chan byte
	paused bool
}

// Play replays c. While playing, space pauses/resumes, '.' steps one event
// while paused and 'q' or Ctrl-C stops.
func Play(ctx context.Context, c *cast.Cast, opts Options) error {
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	idle := opts.IdleTimeLimit
	if idle == 0 {
		idle = c.Header.IdleTimeLimit
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := &player{keys: make(chan byte, 16)}
	if restore := p.readKeys(ctx); restore != nil {
		defer restore()
	}

	t := 0.0
	for _, e := range c.Events {
		t += e.Delay
		if opts.To > 0 && t > opts.To+1e-9 {
			return nil
		}
		d := e.Delay
		if idle > 0 && d > idle {
			d = idle
		}
		if t < opts.From+1e-9 {
			d = 0 // fast forward to the start, and begin right away
		}
		act, err := p.wait(ctx, time.Duration(d/opts.Speed*float64(time.Second)))
		if err != nil || act == quit {
			return err
		}
		if e.Code != cast.Output {
			continue
		}
		if _, err := io.WriteString(opts.Out, e.Data); err != nil {
			return err
		}
	}
	return nil
}

// readKeys puts the terminal in raw mode and forwards keystrokes to p.keys.
// It returns a function restoring the terminal, or nil if stdin is not a terminal.
func (p *player) readKeys(ctx context.Context) func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	go func() {
		b := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(b); err != nil {
				return
			}
			select {
			case p.keys <- b[0]:
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { _ = term.Restore(fd, old) }
}

// wait sleeps for d while handling pause, step and quit keys.
func (p *player) wait(ctx context.Context, d time.Duration) (action, error) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		var tc <-chan time.Time
		if !p.paused {
			tc = timer.C
		}
		select {
		case <-ctx.Done():
			return quit, ctx.Err()
		case <-tc:
			return proceed, nil
		case k := <-p.keys:
			switch k {
			case 'q', 3: // q, Ctrl-C
				return quit, nil
			case ' ':
				p.paused = !p.paused
			case '.':
				if p.paused {
					return proceed, nil
				}
			default:
			}
		}
	}
}
