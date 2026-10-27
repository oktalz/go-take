// Command gotake records, plays and edits terminal sessions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/oktalz/go-take/cast"
	"github.com/oktalz/go-take/play"
	"github.com/oktalz/go-take/record"
	"github.com/oktalz/go-take/version"
	"github.com/oktalz/go-take/web"
)

const usage = `gotake — terminal recorder

Usage:
  gotake rec    [flags] [FILE]          record a terminal session (the final exit is left out)
  gotake run    [flags] SCRIPT          record a session driven by a script (see "gotake run -h")
  gotake play   [flags] [FILE]          replay in this terminal (space: pause, q: quit)
  gotake edit   [flags] [FILE]          open the web editor
  gotake serve  [flags] [DIR]           serve recordings + embeddable player
  gotake export [flags] [FILE]          export to gif, svg, html, png, txt, asciicast
  gotake player-js                      print gotake-player.js (for self-hosting)

  gotake info    [FILE]                 show recording details
  gotake cat     [FILE]                 print the raw output, without timing
  gotake convert IN OUT                 convert between .take and asciicast (.cast, v2 or v3)
  gotake version                        print version information
  gotake tag                            print version tag

Edit commands (modify FILE in place, or write to -o OUT):
  gotake cut     -from T -to T [FILE]   remove a section
  gotake stretch -from T -to T -factor F [FILE]
  gotake speed   -factor F [FILE]       speed up (F>1) or slow down the whole recording
  gotake idle    -max S [FILE]          cap pauses to S seconds
  gotake typing  [-from T -to T] [-delay S] [-jitter S] [FILE]
                                        even out typing speed
  gotake inject  -at T -cmd "ls" [-output TEXT | -output-file F] [-prompt "$ "] [FILE]
                                        inject a typed command and its output
  gotake pause   -at T -seconds S [FILE] insert a pause
  gotake marker  -at T [-label L] [FILE] add a marker / chapter
  gotake trim-exit [FILE]               remove the exit / Ctrl-D at the end of a recording
  gotake replace -find TEXT -replace TEXT [-regex] [-ignore-case] [-raw] [FILE]
                                        replace text in all output (e.g. a shell prompt)
  gotake redact  [-pattern REGEX] [FILE] mask secrets in output with * (default: common
                                        tokens, keys and password=... values)

FILE defaults to out.take, and .take is added to a name without an extension ("demo" is demo.take).
Recordings are saved as .take; FILE.cast reads and writes asciicast instead.
Times (T) are seconds ("12.5"), minutes:seconds ("1:02.5") or the label of a marker ("intro").
Run "gotake COMMAND -h" for command flags.
`

func main() {
	_ = record.EnableVT()
	_ = version.Set()
	if len(os.Args) < 2 {
		eprintf("%s%s", version.Logo, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd != "play" {
		eprintf("%s", version.Logo)
	}
	var err error
	switch cmd {
	case "rec", "record":
		err = cmdRec(args)
	case "run":
		err = cmdRun(args)
	case "play":
		err = cmdPlay(args)
	case "edit", "editor":
		err = cmdEdit(args)
	case "serve":
		err = cmdServe(args)
	case "export":
		err = cmdExport(args)
	case "player-js":
		_, err = os.Stdout.Write(web.PlayerJS())
	case "info":
		err = cmdInfo(args)
	case "cat":
		err = cmdCat(args)
	case "convert":
		err = cmdConvert(args)
	case "cut", "stretch", "speed", "idle", "typing", "inject", "pause", "marker", "replace", "redact", "trim-exit":
		err = cmdEditOp(cmd, args)
	case "version", "-v", "--version":
		fmt.Println("gotake", version.Version)
		fmt.Println("built from:", version.Repo)
	case "tag":
		fmt.Println(version.Tag)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		eprintf("unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		eprintf("gotake: %v\n", err)
		os.Exit(1)
	}
}

// parseArgs parses flags that may appear before or after positional arguments
// (the flag package alone stops at the first positional one).
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// defaultFile is the recording used when a command is given no FILE.
const defaultFile = "out" + cast.TakeExt

// parseFile parses flags and returns the FILE argument, defaulting to out.take.
// A name without an extension that does not exist gets .take added, so "demo" finds demo.take.
func parseFile(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parseArgs(fs, args)
	if err != nil {
		return "", err
	}
	switch len(pos) {
	case 0:
		return defaultFile, nil
	case 1:
		return withTakeExt(pos[0]), nil
	default:
		return "", fmt.Errorf("%s: expected at most one FILE argument", fs.Name())
	}
}

// withTakeExt adds .take to a name that has no extension and does not exist as given.
func withTakeExt(path string) string {
	if filepath.Ext(path) != "" {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return path + cast.TakeExt
}

func cmdRec(args []string) error {
	fs := flag.NewFlagSet("rec", flag.ContinueOnError)
	command := fs.String("c", "", "command to record (default $SHELL)")
	title := fs.String("t", "", "title")
	input := fs.Bool("i", false, "also record keystrokes (input events)")
	idle := fs.Float64("idle", 0, "cap pauses to this many seconds while recording")
	overwrite := fs.Bool("y", false, "overwrite FILE if it exists")
	cols := fs.Int("cols", 0, "override terminal width")
	rows := fs.Int("rows", 0, "override terminal height")
	keepExit := fs.Bool("keep-exit", false, "keep the exit / Ctrl-D that ended the recording")
	path, err := parseFile(fs, args)
	if err != nil {
		return err
	}
	if filepath.Ext(path) == "" {
		path += cast.TakeExt // even when a file without extension exists
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if !*overwrite {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s exists, use -y to overwrite", path)
		}
		return err
	}
	eprintf("\x1b[33m● recording to %s — exit the shell (Ctrl-D) to finish\x1b[0m\r\n", path)
	err = record.Record(f, record.Options{
		Command: *command, Title: *title, CaptureInput: *input, IdleTimeLimit: *idle, Cols: *cols, Rows: *rows,
		Format: cast.FormatFor(path),
	})
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if !*keepExit {
		if err := trimExitFile(path); err != nil {
			return err
		}
	}
	eprintf("\x1b[33m■ recording saved to %s\x1b[0m\n", path)
	return nil
}

const runHelp = `Records a session from a script instead of typing, so a demo can be made
again whenever what it shows changes. One setting or step per line; # starts
a comment; text with spaces is quoted ("ls -l"), with \e \r \n \t escapes.

Settings, before the first step:
  size 80x24              terminal size (default 80x24)
  shell "bash --norc"     command to run (default $SHELL)
  title TEXT              recording title
  env NAME VALUE          environment variable
  typing 80ms [40ms]      typing speed and its random variation
  timeout 10s             how long wait waits by default

Steps:
  type "ls -l" [DELAY]    type text, a character at a time
  send "text"             send text at once (no typing)
  enter [N]               press a key N times; also tab space backspace esc up
                          down left right home end pgup pgdown delete ctrl+c...
  sleep 1.5s              pause
  wait "\$ $" [TIMEOUT]   wait for output matching a regular expression
                          (output since the last key, escape sequences removed)
  hide / show             leave out what happens in between (e.g. setup); the
                          prompt, or everything since a clear, stays on screen
  marker [LABEL]          add a marker

Example:
  size 80x20
  shell "bash --norc --noprofile"
  hide
  send "PS1='$ '\r"
  wait "^\$ $"
  show
  type "echo hello"
  enter
  wait "^\$ $"
  sleep 2

Flags:
`

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.Usage = func() {
		eprintf("Usage: gotake run [flags] SCRIPT\n\n%s", runHelp)
		fs.PrintDefaults()
	}
	out := fs.String("o", "", "recording to write (default: SCRIPT with .take instead of its extension)")
	overwrite := fs.Bool("y", false, "overwrite the recording if it exists")
	quiet := fs.Bool("q", false, "do not show the session while recording")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errors.New("run: expected one SCRIPT argument")
	}
	f, err := os.Open(pos[0])
	if err != nil {
		return err
	}
	script, err := record.ParseScript(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", pos[0], err)
	}
	path := *out
	if path == "" {
		path = strings.TrimSuffix(pos[0], filepath.Ext(pos[0])) + cast.TakeExt
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if !*overwrite {
		flags |= os.O_EXCL
	}
	w, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s exists, use -y to overwrite", path)
		}
		return err
	}
	var live io.Writer = os.Stdout
	if *quiet {
		live = io.Discard
	}
	err = record.RunScript(w, live, script, cast.FormatFor(path))
	if err := errors.Join(err, w.Close()); err != nil {
		return err
	}
	eprintf("\r\n\x1b[33m■ recording saved to %s\x1b[0m\n", path)
	return nil
}

// trimExitFile removes the exit command that ended a recording, if there is one.
func trimExitFile(path string) error {
	c, err := cast.Load(path)
	if err != nil {
		return err
	}
	if c.TrimExit() == 0 {
		return nil
	}
	return c.Save(path)
}

func cmdPlay(args []string) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	speed := fs.Float64("s", 1, "speed multiplier")
	idle := fs.Float64("idle", 0, "cap pauses to this many seconds")
	var from, to timeFlag
	fs.Var(&from, "from", "start at this time or marker")
	fs.Var(&to, "to", "stop at this time or marker")
	path, err := parseFile(fs, args)
	if err != nil {
		return err
	}
	c, err := cast.Load(path)
	if err == nil {
		err = resolveTimes(c, &from, &to)
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = play.Play(ctx, c, play.Options{Speed: *speed, IdleTimeLimit: *idle, From: from.v, To: to.v})
	fmt.Print("\x1b[0m\r\n")
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func cmdEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	path, err := parseFile(fs, args)
	if err != nil {
		return err
	}
	ed, err := web.NewEditor(path)
	if err != nil {
		return err
	}
	ln, url, err := listen(*addr, "editing "+path)
	if err != nil {
		return err
	}
	if !*noOpen {
		openBrowser(url)
	}
	return http.Serve(ln, ed.Handler())
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "0.0.0.0:8080", "listen address")
	open := fs.Bool("open", false, "open a browser")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	dir := "."
	if len(pos) > 0 {
		dir = pos[0]
	}
	ln, url, err := listen(*addr, "serving recordings from "+dir)
	if err != nil {
		return err
	}
	if *open {
		openBrowser(url)
	}
	return http.Serve(ln, web.Gallery(dir))
}

// listen opens the listener and returns the URL it is reachable at.
func listen(addr, what string) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	url := "http://" + ln.Addr().String() + "/"
	eprintf("gotake: %s at %s (Ctrl-C to stop)\n", what, url)
	return ln, url, nil
}

func openBrowser(url string) {
	var cmds [][]string
	switch runtime.GOOS {
	case "darwin":
		cmds = [][]string{{"open", url}}
	case "windows":
		cmds = [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		cmds = [][]string{{"xdg-open", url}, {"wslview", url}}
	}
	for _, c := range cmds {
		if p, err := exec.LookPath(c[0]); err == nil {
			if exec.Command(p, c[1:]...).Start() == nil {
				return
			}
		}
	}
}

func cmdInfo(args []string) error {
	path, err := parseFile(flag.NewFlagSet("info", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	c, err := cast.Load(path)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	keys, longest := 0, 0.0
	for _, e := range c.Events {
		counts[e.Code]++
		if cast.IsKeystroke(e) {
			keys++
		}
		longest = max(longest, e.Delay)
	}
	h := c.Header
	printf("file:       %s\n", path)
	if h.Title != "" {
		printf("title:      %s\n", h.Title)
	}
	printf("terminal:   %dx%d %s\n", h.Term.Cols, h.Term.Rows, h.Term.Type)
	if h.Term.CellWidth > 0 {
		printf("cell size:  %dx%d px\n", h.Term.CellWidth, h.Term.CellHeight)
	}
	printf("duration:   %s\n", fmtTime(c.Duration()))
	printf("events:     %d (output %d, input %d, markers %d, resize %d)\n",
		len(c.Events), counts[cast.Output], counts[cast.Input], counts[cast.Marker], counts[cast.Resize])
	printf("keystrokes: %d\n", keys)
	printf("max pause:  %.3fs\n", longest)
	if h.IdleTimeLimit > 0 {
		printf("idle limit: %.2fs\n", h.IdleTimeLimit)
	}
	for _, i := range c.Markers() {
		printf("marker:     %s  %s\n", fmtTime(c.Times()[i]), c.Events[i].Data)
	}
	return nil
}

func cmdCat(args []string) error {
	path, err := parseFile(flag.NewFlagSet("cat", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	c, err := cast.Load(path)
	if err != nil {
		return err
	}
	for _, e := range c.Events {
		if e.Code == cast.Output {
			if _, err := os.Stdout.WriteString(e.Data); err != nil {
				return err
			}
		}
	}
	return nil
}

func cmdConvert(args []string) error {
	if len(args) != 2 {
		return errors.New("convert: expected IN OUT")
	}
	c, err := cast.Load(args[0])
	if err != nil {
		return err
	}
	return c.Save(args[1])
}

// printf writes to stdout; a failed write to the terminal is not actionable.
func printf(format string, a ...any) { _, _ = fmt.Printf(format, a...) }

// eprintf writes to stderr; a failed write to the terminal is not actionable.
func eprintf(format string, a ...any) { _, _ = fmt.Fprintf(os.Stderr, format, a...) }

func fmtTime(t float64) string {
	m := int(t) / 60
	return fmt.Sprintf("%d:%06.3f", m, t-float64(m*60))
}

func strconvFloat(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// parseTime parses "12.5", "1:02.5" or "1:02:03".
func parseTime(s string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	t := 0.0
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid time %q", s)
		}
		t = t*60 + v
	}
	return t, nil
}
