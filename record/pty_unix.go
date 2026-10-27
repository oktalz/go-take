//go:build !windows

package record

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/oktalz/go-take/cast"
)

type unixPTY struct {
	ptmx *os.File
	cmd  *exec.Cmd
	cell cellSize
}

func defaultShell() string {
	return cmp.Or(os.Getenv("SHELL"), "/bin/sh")
}

// EnableVT enables virtual terminal processing on Windows (no-op on Unix).
func EnableVT() func() { return func() {} }

// cellPixels returns the cell size of the terminal on stdout, so programs in
// the recording (and images in exports) see the same pixels.
func cellPixels() cellSize {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return cellSize{}
	}
	return cellSize{int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)}
}

func (c cellSize) winsize(cols, rows int) *pty.Winsize {
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows), X: uint16(cols * c.w), Y: uint16(rows * c.h)}
}

func startPTY(command, shell string, env []string, cols, rows int, cell cellSize) (session, error) {
	cmd := exec.Command(shell)
	if command != "" {
		cmd = exec.Command("/bin/sh", "-c", command)
	}
	cmd.Env = env
	ptmx, err := pty.StartWithSize(cmd, cell.winsize(cols, rows))
	if err != nil {
		return nil, fmt.Errorf("start %q: %w", cmp.Or(command, shell), err)
	}
	return &unixPTY{ptmx: ptmx, cmd: cmd, cell: cell}, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.ptmx.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.ptmx.Write(b) }
func (p *unixPTY) Close() error                { return p.ptmx.Close() }

func (p *unixPTY) resize(cols, rows int) error {
	return pty.Setsize(p.ptmx, p.cell.winsize(cols, rows))
}

func (p *unixPTY) wait() int {
	if err := p.cmd.Wait(); err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			return ee.ExitCode()
		}
	}
	return 0
}

// forwardResize propagates terminal resizes to the pty and records them.
func forwardResize(s session, _, _ int, w *eventWriter) (stop func()) {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			c, r, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil {
				continue
			}
			_ = s.resize(c, r)
			w.emit(cast.Resize, fmt.Sprintf("%dx%d", c, r))
		}
	}()
	return func() { signal.Stop(winch) }
}

// childExited reports whether a pty read error just means the child closed it
// (Linux reports EIO for that).
func childExited(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)
}
