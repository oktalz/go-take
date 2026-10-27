package export

import (
	"fmt"
	"html"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Window title bar styles for Options.WindowStyle.
const (
	WindowLinux   = "linux"   // GNOME header bar: centered title, round close button
	WindowWindows = "windows" // title on the left, minimize, maximize and close on the right
	WindowMac     = "mac"     // red, yellow and green buttons on the left, centered title
)

// WindowStyles lists the accepted Options.WindowStyle values.
var WindowStyles = []string{WindowLinux, WindowWindows, WindowMac}

// DefaultWindowStyle is the style of the system gotake runs on. WSL counts as
// Windows, since that is the desktop the recording is watched on.
func DefaultWindowStyle() string {
	switch runtime.GOOS {
	case "darwin":
		return WindowMac
	case "windows":
		return WindowWindows
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return WindowWindows
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft") {
		return WindowWindows
	}
	return WindowLinux
}

// windowStyle checks a style name, accepting a few spellings; "" means the default.
func windowStyle(s string) (string, error) {
	switch strings.ToLower(s) {
	case "", "auto", "true":
		return DefaultWindowStyle(), nil
	case "linux", "gnome":
		return WindowLinux, nil
	case "windows", "win":
		return WindowWindows, nil
	case "mac", "macos", "darwin":
		return WindowMac, nil
	}
	return "", fmt.Errorf("unknown window style %q (use %s)", s, strings.Join(WindowStyles, ", "))
}

// windowTitle is the text shown in the title bar: the recording's title, else its command.
func windowTitle(title, command string) string {
	if title != "" {
		return title
	}
	if f := strings.Fields(command); len(f) > 0 {
		return filepath.Base(f[0])
	}
	return "terminal"
}

// mix blends a towards b by f (0..1).
func mix(a, b color.RGBA, f float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

type shapeKind int

const (
	shapeCircle shapeKind = iota // center x0, y0, radius r
	shapeLine                    // from x0, y0 to x1, y1, width w
	shapeText                    // text at x0 (left, or center when centered), vertically centered
)

// shape is one element of a title bar, in pixels from the top left of the image.
type shape struct {
	text           string
	x0, y0, x1, y1 float64
	r, w, size     float64
	kind           shapeKind
	color          color.RGBA
	centered, bold bool
}

// chrome is a window title bar: its background and the shapes drawn on it.
type chrome struct {
	shapes []shape
	bar    color.RGBA
	height float64
}

// windowChrome lays out the title bar of the given style for an image width wide.
// Its colors follow the theme, so the bar matches the terminal below it.
func windowChrome(o Options, width float64) chrome {
	t := o.Theme
	c := chrome{height: math.Round(o.FontSize * 1.9), bar: mix(t.BG, t.FG, 0.09)}
	h, pad := c.height, float64(o.Padding)
	ink := mix(c.bar, t.FG, 0.8)
	text := func(x float64, centered, bold bool) shape {
		return shape{
			kind: shapeText, text: o.WindowTitle, x0: x, y0: h / 2, size: o.FontSize * 0.85,
			color: ink, centered: centered, bold: bold,
		}
	}
	lw := math.Max(1, math.Round(h/30))
	line := func(x0, y0, x1, y1 float64) shape {
		return shape{kind: shapeLine, x0: x0, y0: y0, x1: x1, y1: y1, w: lw, color: ink}
	}
	switch o.WindowStyle {
	case WindowMac:
		r := h * 0.2
		for i, col := range []color.RGBA{{0xff, 0x5f, 0x57, 0xff}, {0xfe, 0xbc, 0x2e, 0xff}, {0x28, 0xc8, 0x40, 0xff}} {
			c.shapes = append(c.shapes, shape{kind: shapeCircle, x0: pad + r + float64(i)*r*3.2, y0: h / 2, r: r, color: col})
		}
		c.shapes = append(c.shapes, text(width/2, true, false))
	case WindowWindows:
		c.shapes = append(c.shapes, text(pad, false, false))
		bw, g, cy := h*1.5, math.Round(h*0.32), math.Floor(h/2)+0.5
		closeX, maxX, minX := width-bw/2, width-bw*1.5, width-bw*2.5
		l, rt, tp, bt := -g/2, g/2, cy-g/2, cy+g/2                 // glyph box around each button's center
		c.shapes = append(c.shapes, line(minX+l, cy, minX+rt, cy)) // minimize
		c.shapes = append(c.shapes,                                // maximize: a square
			line(maxX+l, tp, maxX+rt, tp), line(maxX+rt, tp, maxX+rt, bt),
			line(maxX+rt, bt, maxX+l, bt), line(maxX+l, bt, maxX+l, tp))
		c.shapes = append(c.shapes, line(closeX+l, tp, closeX+rt, bt), line(closeX+rt, tp, closeX+l, bt)) // close
	default: // WindowLinux
		r := h * 0.3
		cx, cy, g := width-pad-r, h/2, r*0.45
		c.shapes = append(c.shapes, text(width/2, true, true),
			shape{kind: shapeCircle, x0: cx, y0: cy, r: r, color: mix(c.bar, t.FG, 0.12)},
			line(cx-g, cy-g, cx+g, cy+g), line(cx+g, cy-g, cx-g, cy+g))
	}
	return c
}

// colors lists every color the bar uses, for the GIF palette.
func (c chrome) colors() []color.RGBA {
	out := []color.RGBA{c.bar}
	for _, s := range c.shapes {
		out = append(out, s.color)
	}
	return out
}

// drawWindow paints the title bar into the top of img.
func (r *rasterizer) drawWindow(img *image.Paletted) {
	c := r.chrome
	bar := r.pal.ramp(c.bar, c.bar)[0]
	w := img.Rect.Dx()
	for y := range r.barH {
		for x := range w {
			img.Pix[y*img.Stride+x] = bar
		}
	}
	for _, s := range c.shapes {
		var mask *image.Alpha
		var at image.Point
		switch s.kind {
		case shapeText:
			mask, at = r.titleMask(s, w)
		default:
			mask, at = shapeMask(s)
		}
		if mask == nil {
			continue
		}
		ramp := r.pal.ramp(s.color, c.bar)
		b := mask.Bounds()
		for y := range b.Dy() {
			for x := range b.Dx() {
				px, py := at.X+x, at.Y+y
				a := mask.Pix[y*mask.Stride+x]
				if a == 0 || px < 0 || py < 0 || px >= w || py >= r.barH {
					continue
				}
				img.Pix[py*img.Stride+px] = ramp[(int(a)*blendLevels+127)/255]
			}
		}
	}
}

// shapeMask draws a circle or line into a mask covering its bounding box, and returns where the box starts.
func shapeMask(s shape) (*image.Alpha, image.Point) {
	var x0, y0, x1, y1 float64
	var in func(x, y float64) bool
	switch s.kind {
	case shapeCircle:
		x0, y0, x1, y1 = s.x0-s.r, s.y0-s.r, s.x0+s.r, s.y0+s.r
		in = func(x, y float64) bool { return math.Hypot(x-s.x0, y-s.y0) <= s.r }
	case shapeLine:
		x0, y0 = math.Min(s.x0, s.x1)-s.w, math.Min(s.y0, s.y1)-s.w
		x1, y1 = math.Max(s.x0, s.x1)+s.w, math.Max(s.y0, s.y1)+s.w
		in = func(x, y float64) bool { return segmentDist(x, y, s.x0, s.y0, s.x1, s.y1) <= s.w/2 }
	default:
		return nil, image.Point{}
	}
	ox, oy := int(math.Floor(x0)), int(math.Floor(y0))
	cv := newCanvas(int(math.Ceil(x1))-ox+1, int(math.Ceil(y1))-oy+1)
	cv.fill(func(x, y float64) bool { return in(x+float64(ox), y+float64(oy)) })
	return cv.alpha(1), image.Point{ox, oy}
}

// segmentDist is the distance from (px, py) to the segment (x0, y0)-(x1, y1).
func segmentDist(px, py, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-x0)*dx+(py-y0)*dy)/l))
	}
	return math.Hypot(px-(x0+t*dx), py-(y0+t*dy))
}

var (
	titleFontsOnce sync.Once
	titleFonts     [2]*sfnt.Font // regular, bold
)

// titleFont is the sans-serif Go font (Go Mono's sibling) used for window titles, as real title bars use.
func titleFont(bold bool) *sfnt.Font {
	titleFontsOnce.Do(func() {
		titleFonts[0], _ = opentype.Parse(goregular.TTF)
		titleFonts[1], _ = opentype.Parse(gobold.TTF)
	})
	if bold {
		return titleFonts[1]
	}
	return titleFonts[0]
}

// titleMask draws the title text, cut short with "…" when it does not fit.
func (r *rasterizer) titleMask(s shape, width int) (*image.Alpha, image.Point) {
	f := titleFont(s.bold)
	if f == nil {
		return nil, image.Point{}
	}
	face, err := r.faces.face(f, s.size)
	if err != nil || s.text == "" {
		return nil, image.Point{}
	}
	text := s.text
	room := width - 2*r.pad
	if r.o.WindowStyle != WindowWindows {
		room = width - 2*int(float64(r.barH)*3) // keep clear of the buttons
	}
	for len(text) > 1 && font.MeasureString(face, text).Ceil() > room {
		rs := []rune(strings.TrimSuffix(text, "…"))
		text = string(rs[:len(rs)-1]) + "…"
	}
	tw := font.MeasureString(face, text).Ceil()
	m := face.Metrics()
	mask := image.NewAlpha(image.Rect(0, 0, tw+2, r.barH))
	baseline := (r.barH+m.Ascent.Ceil()-m.Descent.Ceil())/2 + 0
	d := font.Drawer{Dst: mask, Src: image.Opaque, Face: face, Dot: fixed.P(1, baseline)}
	d.DrawString(text)
	x := int(s.x0)
	if s.centered {
		x = int(s.x0) - tw/2
	}
	return mask, image.Point{x, 0}
}

// writeSVGWindow writes the title bar of an SVG export width wide.
func writeSVGWindow(w io.Writer, c chrome, width float64) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p(`<path d="M0 8a8 8 0 0 1 8-8h%gv%gH0z" fill="%s"/>`, width-16, c.height, cssColor(c.bar))
	p(`<rect x="%g" width="8" height="%g" fill="%s"/>`, width-8, c.height, cssColor(c.bar))
	for _, s := range c.shapes {
		switch s.kind {
		case shapeCircle:
			p(`<circle cx="%g" cy="%g" r="%g" fill="%s"/>`, s.x0, s.y0, s.r, cssColor(s.color))
		case shapeLine:
			p(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="%g" stroke-linecap="round"/>`,
				s.x0, s.y0, s.x1, s.y1, cssColor(s.color), s.w)
		case shapeText:
			anchor, weight := "start", ""
			if s.centered {
				anchor = "middle"
			}
			if s.bold {
				weight = ` font-weight="bold"`
			}
			// an inline style: the export's stylesheet gives every <text> the terminal's monospace font
			p(`<text x="%g" y="%g" fill="%s" font-size="%g" text-anchor="%s" dominant-baseline="central"%s `+
				`style="font-family:system-ui,-apple-system,'Segoe UI',Cantarell,Ubuntu,sans-serif;white-space:pre">%s</text>`,
				s.x0, s.y0, cssColor(s.color), s.size, anchor, weight, html.EscapeString(s.text))
		}
	}
	p("\n")
}
