//go:build windows

package record

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"

	"github.com/oktalz/go-take/cast"
)

type winPTY struct {
	inW       *os.File
	outR      *os.File
	done      <-chan int
	hpc       windows.Handle
	closeOnce sync.Once
}

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if os.Getenv("PSModulePath") != "" {
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			return p
		}
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			return p
		}
	}
	return cmp.Or(os.Getenv("COMSPEC"), "cmd.exe")
}

// EnableVT enables virtual terminal sequence processing on stdout and stderr
// so ANSI escape codes render in Command Prompt and Windows PowerShell.
func EnableVT() func() {
	var restores []func()
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := windows.Handle(f.Fd())
		var mode uint32
		if windows.GetConsoleMode(h, &mode) == nil {
			_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
			old := mode
			restores = append(restores, func() { _ = windows.SetConsoleMode(h, old) })
		}
	}
	return func() {
		for _, r := range restores {
			r()
		}
	}
}

// cellPixels reports no cell size: the Windows console does not tell it.
func cellPixels() cellSize { return cellSize{} }

func startPTY(command, shell string, env []string, cols, rows int, _ cellSize) (session, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, err
	}

	var hpc windows.Handle
	coord := windows.Coord{X: int16(cols), Y: int16(rows)}
	err = windows.CreatePseudoConsole(coord, windows.Handle(inR.Fd()), windows.Handle(outW.Fd()), 0, &hpc)
	_ = inR.Close()
	_ = outW.Close()
	if err != nil {
		_ = inW.Close()
		_ = outR.Close()
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = inW.Close()
		_ = outR.Close()
		return nil, err
	}
	defer attrList.Delete()

	hpcPtr := *(**byte)(unsafe.Pointer(&hpc))
	err = attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(hpcPtr), unsafe.Sizeof(hpc))
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = inW.Close()
		_ = outR.Close()
		return nil, err
	}

	var cmdLine string
	if command == "" {
		if p, lookErr := exec.LookPath(shell); lookErr == nil {
			shell = p
		}
		cmdLine = windows.ComposeCommandLine([]string{shell})
	} else {
		comspec := cmp.Or(os.Getenv("COMSPEC"), "cmd.exe")
		if p, lookErr := exec.LookPath(comspec); lookErr == nil {
			comspec = p
		}
		cmdLine = windows.ComposeCommandLine([]string{comspec, "/c", command})
	}
	cmdLine16, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = inW.Close()
		_ = outR.Close()
		return nil, err
	}

	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrList.List(),
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	err = windows.CreateProcess(nil, cmdLine16, nil, nil, false, flags, envBlock(env), nil, &si.StartupInfo, &pi)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = inW.Close()
		_ = outR.Close()
		return nil, fmt.Errorf("start %q: %w", cmp.Or(command, shell), err)
	}

	done := make(chan int, 1)
	p := &winPTY{inW: inW, outR: outR, hpc: hpc, done: done}
	go func() {
		_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(pi.Process, &code)
		_ = windows.CloseHandle(pi.Thread)
		_ = windows.CloseHandle(pi.Process)
		p.closeConsole()
		done <- int(code)
	}()
	return p, nil
}

func envBlock(env []string) *uint16 {
	var b []uint16
	for _, e := range env {
		b = append(b, utf16.Encode([]rune(e))...)
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0]
}

func (p *winPTY) closeConsole() {
	p.closeOnce.Do(func() { windows.ClosePseudoConsole(p.hpc) })
}

func (p *winPTY) Read(b []byte) (int, error)  { return p.outR.Read(b) }
func (p *winPTY) Write(b []byte) (int, error) { return p.inW.Write(b) }

func (p *winPTY) Close() error {
	p.closeConsole()
	return errors.Join(p.inW.Close(), p.outR.Close())
}

// echoOff is always false: the pseudo console does not tell.
func (*winPTY) echoOff() bool { return false }

func (p *winPTY) resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *winPTY) wait() int { return <-p.done }

// forwardResize polls the console size on Windows and forwards changes to the ConPTY.
func forwardResize(s session, cols, rows int, w *eventWriter) (stop func()) {
	quit := make(chan struct{})
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				c, r, err := term.GetSize(int(os.Stdout.Fd()))
				if err != nil || (c == cols && r == rows) {
					continue
				}
				cols, rows = c, r
				_ = s.resize(c, r)
				w.emit(cast.Resize, fmt.Sprintf("%dx%d", c, r))
			}
		}
	}()
	return func() { close(quit) }
}

func childExited(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, syscall.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)
}
