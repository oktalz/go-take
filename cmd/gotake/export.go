package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oktalz/go-take/cast"
	"github.com/oktalz/go-take/export"
	"github.com/oktalz/go-take/web"
)

// exportArgs holds the flags of "gotake export".
type exportArgs struct {
	format, out  string
	html         export.HTMLOptions
	opts         export.Options
	from, to, at timeFlag
}

func (e *exportArgs) register(fs *flag.FlagSet) {
	fs.StringVar(&e.format, "f", "",
		"format: gif, svg, html, png, svg-frame, txt, screen, cast, v2 (default: from -o extension)")
	fs.StringVar(&e.out, "o", "", "output file (default: FILE with the format's extension, - for stdout)")
	fs.Var(&e.from, "from", "export only from this time or marker")
	fs.Var(&e.to, "to", "export only up to this time or marker")
	fs.Var(&e.at, "at", "time or marker of the frame for png, svg-frame and screen (default: end)")
	fs.Float64Var(&e.opts.Speed, "speed", 1, "playback speed")
	fs.Float64Var(&e.opts.IdleTimeLimit, "idle", 0, "cap pauses to this many seconds (default: header value, -1: none)")
	fs.Float64Var(&e.opts.FPS, "fps", 30, "maximum frames per second (gif, svg)")
	fs.Float64Var(&e.opts.FontSize, "font-size", 16, "font size in pixels")
	fs.IntVar(&e.opts.Padding, "padding", 12, "padding around the terminal in pixels")
	fs.Var((*windowFlag)(&e.opts), "window",
		"draw a window title bar: -window for this system's style, or -window=linux, windows or mac")
	fs.BoolVar(&e.opts.HideCursor, "no-cursor", false, "do not draw the cursor")
	fs.Var((*listFlag)(&e.opts.Fonts), "font",
		"font file (TTF, OTF, TTC) for gif and png, tried before Go Mono; repeat for more (default $"+export.FontEnv+")")
	fs.BoolVar(&e.opts.NoSystemFonts, "no-system-fonts", false,
		"do not look up installed fonts for characters the fonts lack (gif, png)")
	fs.StringVar(&e.html.Title, "title", "", "page title (html)")
	fs.BoolVar(&e.html.Autoplay, "autoplay", false, "start playing on load (html)")
	fs.BoolVar(&e.html.Loop, "loop", false, "loop playback (html)")
}

// load reads the recording and cuts the -from/-to section.
func (e *exportArgs) load(path string) (*cast.Cast, error) {
	c, err := cast.Load(path)
	if err == nil {
		err = resolveTimes(c, &e.from, &e.to, &e.at)
	}
	if err != nil || (!e.from.set && !e.to.set) {
		return c, err
	}
	start, end := 0, len(c.Events)
	if e.from.set {
		start = c.IndexAt(e.from.v)
	}
	if e.to.set {
		end = c.IndexAt(e.to.v)
	}
	return c.Section(start, end)
}

// destination picks the output path: -o, or FILE with the format's extension.
func (e *exportArgs) destination(path string, f export.Format) string {
	if e.out != "" {
		return e.out
	}
	dst := strings.TrimSuffix(path, filepath.Ext(path)) + f.Ext
	if dst == path {
		dst = strings.TrimSuffix(path, f.Ext) + "-" + f.Name + f.Ext
	}
	return dst
}

func cmdExport(args []string) error {
	var e exportArgs
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	e.register(fs)
	path, err := parseFile(fs, args)
	if err != nil {
		return err
	}
	f, err := pickFormat(e.format, e.out)
	if err != nil {
		return err
	}
	c, err := e.load(path)
	if err != nil {
		return err
	}
	e.opts.At = -1
	if e.at.set {
		e.opts.At = e.at.v
		if e.from.set {
			e.opts.At -= e.from.v // -at is a time in the original recording
		}
	}
	e.html.PlayerJS = web.PlayerJS()

	dst := e.destination(path, f)
	if dst == "-" {
		bw := bufio.NewWriter(os.Stdout)
		return errors.Join(export.Write(bw, f, c, e.opts, e.html), bw.Flush())
	}
	if err := writeFile(dst, func(w io.Writer) error { return export.Write(w, f, c, e.opts, e.html) }); err != nil {
		return err
	}
	if st, err := os.Stat(dst); err == nil {
		eprintf("%s: %s, %s\n", dst, f.Label, humanSize(st.Size()))
	}
	return nil
}

// windowFlag is -window (the style of this system) or -window=STYLE.
type windowFlag export.Options

func (w *windowFlag) IsBoolFlag() bool { return true }

func (w *windowFlag) String() string {
	if w == nil || !w.Window {
		return "false"
	}
	return w.WindowStyle
}

func (w *windowFlag) Set(s string) error {
	switch strings.ToLower(s) {
	case "false", "0", "no", "off":
		w.Window = false
		return nil
	case "true", "1", "yes", "on", "auto":
		w.Window, w.WindowStyle = true, ""
		return nil
	}
	for _, st := range export.WindowStyles {
		if strings.EqualFold(s, st) || (st == export.WindowMac && strings.EqualFold(s, "macos")) {
			w.Window, w.WindowStyle = true, st
			return nil
		}
	}
	return fmt.Errorf("unknown window style %q (use %s)", s, strings.Join(export.WindowStyles, ", "))
}

// listFlag collects the values of a flag given several times.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }

// writeFile creates path and writes it through a buffer.
func writeFile(path string, write func(io.Writer) error) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(file)
	if err := write(bw); err != nil {
		return errors.Join(err, file.Close())
	}
	return errors.Join(bw.Flush(), file.Close())
}

func pickFormat(name, out string) (export.Format, error) {
	if name != "" {
		return export.Lookup(name)
	}
	if f, ok := export.ForPath(out); ok {
		return f, nil
	}
	if out == "" || out == "-" {
		return export.Format{}, errors.New("export: choose a format with -f, or an -o file with a known extension")
	}
	return export.Format{}, errors.New("export: unknown extension of " + out + ", use -f")
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return strconvFloat(float64(n)/(1<<20)) + " MB"
	case n >= 1<<10:
		return strconvFloat(float64(n)/(1<<10)) + " KB"
	default:
		return strconvFloat(float64(n)) + " B"
	}
}
