package export

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/hinshun/vt10x"

	"github.com/oktalz/go-take/cast"
)

// vt10x Glyph.Mode bits (unexported in the library).
const (
	modeUnderline = 1 << 1
	modeBold      = 1 << 2
	modeItalic    = 1 << 4
)

// Options configures rendering. The zero value gives sensible defaults.
type Options struct {
	Theme         *Theme  // colors (default: from the recording header)
	Speed         float64 // playback speed (default 1)
	IdleTimeLimit float64 // cap pauses (0 = header value, <0 = no cap)
	FPS           float64 // maximum frames per second for animations (default 30, max 50)
	FontSize      float64 // font size in pixels (default 16)
	EndDelay      float64 // how long the last frame stays before looping (default 2s)
	At            float64 // time of the frame for still images (<0 = end)
	Padding       int     // space around the terminal in pixels (default 12)
	Window        bool    // draw a window title bar
	// WindowStyle is the title bar's look: WindowLinux, WindowWindows or WindowMac
	// (default: the system gotake runs on).
	WindowStyle string
	// WindowTitle is the text in the title bar (default: the recording's title or command).
	WindowTitle string
	HideCursor  bool // do not draw the cursor
	// Fonts are font files (TTF, OTF, TTC) for GIF and PNG, tried in order before
	// the built-in Go Mono (default: the GOTAKE_FONT environment variable).
	Fonts []string
	// NoSystemFonts stops looking up installed fonts for characters the fonts lack,
	// so the output does not depend on the machine.
	NoSystemFonts bool
}

func (o Options) withDefaults(c *cast.Cast) Options {
	if o.Speed <= 0 {
		o.Speed = 1
	}
	if o.IdleTimeLimit == 0 {
		o.IdleTimeLimit = c.Header.IdleTimeLimit
	}
	if o.FPS <= 0 {
		o.FPS = 30
	}
	o.FPS = math.Min(o.FPS, 50) // GIF delays below 2/100s are not honored by browsers
	if o.FontSize <= 0 {
		o.FontSize = 16
	}
	if o.EndDelay <= 0 {
		o.EndDelay = 2
	}
	if o.Padding <= 0 {
		o.Padding = 12
	}
	if s, err := windowStyle(o.WindowStyle); err == nil {
		o.WindowStyle = s
	} else {
		o.WindowStyle = DefaultWindowStyle()
	}
	if o.WindowTitle == "" {
		o.WindowTitle = windowTitle(c.Header.Title, c.Header.Command)
	}
	if len(o.Fonts) == 0 {
		o.Fonts = filepath.SplitList(os.Getenv(FontEnv))
	}
	if o.Theme == nil {
		t := ThemeFor(c.Header)
		o.Theme = &t
	}
	return o
}

// Cell is a rendered terminal cell with resolved colors.
type Cell struct {
	Char      rune
	FG, BG    color.RGBA
	Bold      bool
	Italic    bool
	Underline bool
}

// Screen is a snapshot of the terminal.
type Screen struct {
	Cells         []Cell // Rows*Cols, row major
	Cols, Rows    int
	CursorX       int
	CursorY       int
	CursorVisible bool
}

// At returns the cell at column x, row y.
func (s *Screen) At(x, y int) Cell { return s.Cells[y*s.Cols+x] }

func (s *Screen) equal(o *Screen) bool {
	if s.Cols != o.Cols || s.Rows != o.Rows || s.CursorVisible != o.CursorVisible ||
		(s.CursorVisible && (s.CursorX != o.CursorX || s.CursorY != o.CursorY)) {
		return false
	}
	for i := range s.Cells {
		if s.Cells[i] != o.Cells[i] {
			return false
		}
	}
	return true
}

// Frame is a screen shown from Time for Duration seconds of the exported animation.
type Frame struct {
	Screen   *Screen
	Time     float64
	Duration float64
}

type term struct {
	vt    vt10x.Terminal
	theme Theme
	esc   bool // the last output ended in ESC, so a reset may continue in the next event
}

// A full reset (RIS, printed by the reset command) should clear the screen, but
// vt10x passes rows and columns to its clear the wrong way round and only blanks
// as many columns as there are rows. The screen is cleared again right after it.
const (
	ris         = "\x1bc"
	clearScreen = "\x1b[2J"
)

func newTerm(c *cast.Cast, theme Theme) *term {
	cols, rows := max(c.Header.Term.Cols, 1), max(c.Header.Term.Rows, 1)
	return &term{vt: vt10x.New(vt10x.WithSize(cols, rows)), theme: theme}
}

func (t *term) apply(e cast.Event) {
	switch e.Code {
	case cast.Output:
		t.write(e.Data)
	case cast.Resize:
		var cols, rows int
		if _, err := fmt.Sscanf(e.Data, "%dx%d", &cols, &rows); err == nil && cols > 0 && rows > 0 {
			t.vt.Resize(cols, rows)
		}
	default:
	}
}

// write feeds output to the emulator, fixing up full resets.
func (t *term) write(s string) {
	if t.esc && strings.HasPrefix(s, "c") {
		_, _ = t.vt.Write([]byte("c" + clearScreen))
		s = s[1:]
	}
	for {
		i := strings.Index(s, ris)
		if i < 0 {
			break
		}
		_, _ = t.vt.Write([]byte(s[:i+len(ris)] + clearScreen))
		s = s[i+len(ris):]
	}
	_, _ = t.vt.Write([]byte(s))
	t.esc = strings.HasSuffix(s, "\x1b")
}

func (t *term) snapshot() *Screen {
	t.vt.Lock()
	defer t.vt.Unlock()
	cols, rows := t.vt.Size()
	s := &Screen{Cols: cols, Rows: rows, Cells: make([]Cell, cols*rows)}
	for y := range rows {
		for x := range cols {
			g := t.vt.Cell(x, y)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			s.Cells[y*cols+x] = Cell{
				Char: ch, FG: t.theme.Color(g.FG), BG: t.theme.Color(g.BG),
				Bold: g.Mode&modeBold != 0, Italic: g.Mode&modeItalic != 0, Underline: g.Mode&modeUnderline != 0,
			}
		}
	}
	cur := t.vt.Cursor()
	s.CursorX, s.CursorY = min(cur.X, cols-1), min(cur.Y, rows-1)
	s.CursorVisible = t.vt.CursorVisible()
	return s
}

// times returns each event's time on the exported timeline (idle capped, sped up).
func times(c *cast.Cast, o Options) []float64 {
	ts := make([]float64, len(c.Events))
	t := 0.0
	for i, e := range c.Events {
		d := e.Delay
		if o.IdleTimeLimit > 0 && d > o.IdleTimeLimit {
			d = o.IdleTimeLimit
		}
		t += d / o.Speed
		ts[i] = t
	}
	return ts
}

// Frames replays the recording and returns the distinct screens over time.
// Events closer together than 1/FPS are folded into one frame.
func Frames(c *cast.Cast, o Options) []Frame {
	o = o.withDefaults(c)
	tm := newTerm(c, *o.Theme)
	ts := times(c, o)
	step := 1 / o.FPS

	frames := []Frame{{Time: 0, Screen: tm.snapshot()}}
	for i := 0; i < len(c.Events); {
		start := ts[i]
		for i < len(c.Events) && ts[i]-start < step {
			tm.apply(c.Events[i])
			i++
		}
		s := tm.snapshot()
		last := &frames[len(frames)-1]
		if s.equal(last.Screen) {
			continue
		}
		at := ts[i-1]
		if at-last.Time < step { // keep frames at least one step apart
			at = last.Time + step
		}
		frames = append(frames, Frame{Time: at, Screen: s})
	}
	end := 0.0
	if n := len(ts); n > 0 {
		end = ts[n-1]
	}
	for i := range frames {
		if i+1 < len(frames) {
			frames[i].Duration = frames[i+1].Time - frames[i].Time
		} else {
			frames[i].Duration = max(end-frames[i].Time, 0) + o.EndDelay
		}
	}
	if len(frames) > 1 && frames[0].Duration == 0 {
		frames = frames[1:]
	}
	return frames
}

// ScreenAt returns the screen at time t of the recording (t < 0: the end).
// Unlike Frames, t is in recording time (no speed or idle cap applied).
func ScreenAt(c *cast.Cast, t float64, o Options) *Screen {
	o = o.withDefaults(c)
	tm := newTerm(c, *o.Theme)
	acc := 0.0
	for _, e := range c.Events {
		acc += e.Delay
		if t >= 0 && acc > t+1e-9 {
			break
		}
		tm.apply(e)
	}
	return tm.snapshot()
}
