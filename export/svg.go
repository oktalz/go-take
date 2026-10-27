package export

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"

	"github.com/oktalz/go-take/cast"
)

const svgFonts = `ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono','DejaVu Sans Mono',monospace`

type svgLayout struct {
	images map[*Placement]int // id of each Kitty graphics image in <defs>
	o      Options
	cw, ch float64 // cell size
	barH   float64
	pad    float64
	cols   int
	rows   int
}

func newSVGLayout(cols, rows int, o Options) svgLayout {
	l := svgLayout{
		o: o, cols: cols, rows: rows,
		cw: o.FontSize * 0.6, ch: math.Ceil(o.FontSize * 1.25), pad: float64(o.Padding),
	}
	if o.Window {
		l.barH = math.Round(o.FontSize * 1.9)
	}
	return l
}

func (l svgLayout) termW() float64 { return float64(l.cols) * l.cw }
func (l svgLayout) termH() float64 { return float64(l.rows) * l.ch }

// SVG writes an animated SVG. It is crisp at any size and usually much
// smaller than a GIF; text is rendered with the viewer's monospace font.
func SVG(w io.Writer, c *cast.Cast, o Options) error {
	o = o.withDefaults(c)
	frames := Frames(c, o)
	cols, rows := maxSize(frames)
	return writeSVG(w, newSVGLayout(cols, rows, o), frames)
}

// SVGFrame writes a still SVG of the screen at recording time o.At (<0 = end).
func SVGFrame(w io.Writer, c *cast.Cast, o Options) error {
	o = o.withDefaults(c)
	s := ScreenAt(c, o.At, o)
	return writeSVG(w, newSVGLayout(s.Cols, s.Rows, o), []Frame{{Screen: s, Duration: 1}})
}

func writeSVG(w io.Writer, l svgLayout, frames []Frame) error {
	bw := bufio.NewWriter(w)
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(bw, format, a...) }
	width, height := l.termW()+2*l.pad, l.termH()+2*l.pad+l.barH
	bg := cssColor(l.o.Theme.BG)

	p(`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" font-size="%g">`+"\n",
		width, height, width, height, l.o.FontSize)
	p(`<style>text{font-family:%s;white-space:pre}`, svgFonts)
	p(`.b{font-weight:bold}.i{font-style:italic}.u{text-decoration:underline}`)
	total := 0.0
	for _, f := range frames {
		total += f.Duration
	}
	if len(frames) > 1 {
		p(`#f{animation:k %.3fs step-end infinite}@keyframes k{`, total)
		for i, f := range frames {
			p(`%.4f%%{transform:translateY(%gpx)}`, f.Time/total*100, -float64(i)*l.termH())
		}
		p(`}`)
	}
	p("</style>\n")
	if !l.o.Theme.Transparent {
		p(`<rect width="100%%" height="100%%" rx="8" fill="%s"/>`+"\n", bg)
	}
	if l.o.Window {
		writeSVGWindow(bw, windowChrome(l.o, width), width)
	}
	screens := make([]*Screen, len(frames))
	for i, f := range frames {
		screens[i] = f.Screen
	}
	if err := l.writeImages(bw, placements(screens)); err != nil {
		return err
	}
	p(`<svg x="%g" y="%g" width="%g" height="%g"><g id="f">`+"\n", l.pad, l.pad+l.barH, l.termW(), l.termH())
	for i, f := range frames {
		p(`<g transform="translate(0 %g)">`, float64(i)*l.termH())
		l.writeScreen(bw, f.Screen)
		p("</g>\n")
	}
	p("</g></svg></svg>\n")
	return bw.Flush()
}

// writeScreen writes background rectangles and text runs for one screen.
func (l svgLayout) writeScreen(w *bufio.Writer, s *Screen) {
	for y := range s.Rows {
		l.writeBackgrounds(w, s, y)
		l.writeText(w, s, y)
	}
	for y := range s.Rows {
		l.writeImageCells(w, s, y)
	}
}

// writeImages writes each Kitty graphics placement once, as a PNG sized to its cells.
func (l *svgLayout) writeImages(w *bufio.Writer, ps []*Placement) error {
	if len(ps) == 0 {
		return nil
	}
	l.images = map[*Placement]int{}
	_, _ = w.WriteString("<defs>")
	for i, p := range ps {
		var buf bytes.Buffer
		if err := png.Encode(&buf, p.Image); err != nil {
			return err
		}
		aspect := map[fit]string{fitStretch: "none", fitTop: "xMinYMin meet", fitCenter: "xMidYMid meet"}[p.fit]
		_, _ = fmt.Fprintf(w,
			`<image id="img%d" width="%g" height="%g" preserveAspectRatio="%s" href="data:image/png;base64,%s"/>`,
			i, float64(p.Cols)*l.cw, float64(p.Rows)*l.ch, aspect, base64.StdEncoding.EncodeToString(buf.Bytes()))
		l.images[p] = i
	}
	_, _ = w.WriteString("</defs>\n")
	return nil
}

// writeImageCells shows runs of image cells as windows onto their image.
func (l svgLayout) writeImageCells(w *bufio.Writer, s *Screen, y int) {
	for x := 0; x < s.Cols; {
		c := s.At(x, y)
		end := x + 1
		if c.Image == nil {
			x = end
			continue
		}
		for end < s.Cols {
			n := s.At(end, y)
			if n.Image != c.Image || n.ImageRow != c.ImageRow || int(n.ImageCol) != int(c.ImageCol)+end-x {
				break
			}
			end++
		}
		width := float64(end-x) * l.cw
		_, _ = fmt.Fprintf(w,
			`<svg x="%g" y="%g" width="%g" height="%g" viewBox="%g %g %g %g"><use href="#img%d"/></svg>`,
			float64(x)*l.cw, float64(y)*l.ch, width, l.ch,
			float64(c.ImageCol)*l.cw, float64(c.ImageRow)*l.ch, width, l.ch, l.images[c.Image])
		x = end
	}
}

// colors returns the colors of a cell, swapped under the cursor.
func (l svgLayout) colors(s *Screen, x, y int) (fg, bg color.RGBA) {
	c := s.At(x, y)
	if s.CursorVisible && !l.o.HideCursor && x == s.CursorX && y == s.CursorY {
		return c.BG, c.FG
	}
	return c.FG, c.BG
}

// writeBackgrounds writes runs of cells whose background differs from the terminal's.
func (l svgLayout) writeBackgrounds(w *bufio.Writer, s *Screen, y int) {
	for x := 0; x < s.Cols; {
		_, bg := l.colors(s, x, y)
		end := x + 1
		for end < s.Cols {
			if _, b := l.colors(s, end, y); b != bg {
				break
			}
			end++
		}
		if bg != l.o.Theme.BG {
			_, _ = fmt.Fprintf(w, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`,
				float64(x)*l.cw, float64(y)*l.ch, float64(end-x)*l.cw, l.ch, cssColor(bg))
		}
		x = end
	}
}

// writeText writes runs of characters that share color and style.
func (l svgLayout) writeText(w *bufio.Writer, s *Screen, y int) {
	baseline := (l.ch-l.o.FontSize)/2 + l.o.FontSize*0.8
	sameStyle := func(a, b Cell) bool { return a.Bold == b.Bold && a.Italic == b.Italic && a.Underline == b.Underline }
	for x := 0; x < s.Cols; {
		c := s.At(x, y)
		fg, _ := l.colors(s, x, y)
		if c.WideTail {
			x++
			continue
		}
		if c.Wide { // on its own, centered on its two cells: fonts do not make it exactly two wide
			_, _ = fmt.Fprintf(w, `<text x="%g" y="%g" fill="%s" text-anchor="middle"%s>%s</text>`,
				float64(x+1)*l.cw, float64(y)*l.ch+baseline, cssColor(fg), styleClass(c),
				html.EscapeString(string(c.Char)))
			x += 2
			continue
		}
		var run strings.Builder
		end := x
		for ; end < s.Cols; end++ {
			n := s.At(end, y)
			if nfg, _ := l.colors(s, end, y); nfg != fg || !sameStyle(n, c) || n.Wide || n.WideTail {
				break
			}
			_, _ = run.WriteString(string(n.Char))
		}
		if text := strings.TrimRight(run.String(), " "); strings.TrimSpace(text) != "" || c.Underline {
			_, _ = fmt.Fprintf(w, `<text x="%g" y="%g" fill="%s"%s>%s</text>`,
				float64(x)*l.cw, float64(y)*l.ch+baseline, cssColor(fg), styleClass(c), html.EscapeString(text))
		}
		x = end
	}
}

func styleClass(c Cell) string {
	var cls []string
	if c.Bold {
		cls = append(cls, "b")
	}
	if c.Italic {
		cls = append(cls, "i")
	}
	if c.Underline {
		cls = append(cls, "u")
	}
	if len(cls) == 0 {
		return ""
	}
	return ` class="` + strings.Join(cls, " ") + `"`
}
