package export

import (
	"bufio"
	"fmt"
	"html"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Captions (cast.Caption events) are drawn as white text on a dark box at the
// bottom of the terminal, in the sans-serif font of window titles.

const (
	captionLines = 3   // at most this many lines; the rest is cut short with …
	captionWidth = 0.9 // of the terminal's width
)

// captionColors returns the caption's text and box colors for a theme: the
// background a fifth of the way to the text color, and white or black text,
// whichever stands out.
func captionColors(t *Theme) (text, box color.RGBA) {
	mix := func(a, b uint8) uint8 { return uint8((int(a)*4 + int(b) + 2) / 5) }
	box = color.RGBA{mix(t.BG.R, t.FG.R), mix(t.BG.G, t.FG.G), mix(t.BG.B, t.FG.B), 0xff}
	if 299*int(box.R)+587*int(box.G)+114*int(box.B) > 128*1000 {
		return color.RGBA{0, 0, 0, 0xff}, box
	}
	return color.RGBA{0xff, 0xff, 0xff, 0xff}, box
}

// wrapCaption breaks text into lines that fit, at spaces where it can.
func wrapCaption(text string, fits func(string) bool) []string {
	var lines []string
	for para := range strings.SplitSeq(text, "\n") {
		line := ""
		for word := range strings.FieldsSeq(para) {
			switch {
			case line == "":
				line = word
			case fits(line + " " + word):
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
		}
		lines = append(lines, line)
	}
	if len(lines) > captionLines {
		lines = lines[:captionLines]
		lines[captionLines-1] += "…"
	}
	for i, l := range lines { // a single word may still be too long
		for len(l) > 1 && !fits(l) {
			rs := []rune(strings.TrimSuffix(l, "…"))
			l = string(rs[:len(rs)-1]) + "…"
		}
		lines[i] = l
	}
	return lines
}

// caption sizes for a font size: line height and box padding.
func captionMetrics(size float64) (lineH, padX, padY float64) {
	return math.Ceil(size * 1.35), math.Round(size * 0.8), math.Round(size * 0.45)
}

// captionLayout is a caption laid out for raster output.
type captionLayout struct {
	lines []captionLine
	box   image.Rectangle
}

type captionLine struct {
	mask *image.Alpha
	at   image.Point
}

// layoutCaption lays a caption out over the terminal area, once per caption.
func (r *rasterizer) layoutCaption(caption string) *captionLayout {
	if l, ok := r.captions[caption]; ok {
		return l
	}
	size := r.o.FontSize * 0.95
	face, err := r.faces.face(titleFont(shape{}), size)
	if err != nil {
		r.captions[caption] = nil
		return nil
	}
	termW, termH := r.cols*r.cw, r.rows*r.ch
	lineH, padX, padY := captionMetrics(size)
	maxW := int(float64(termW)*captionWidth - 2*padX)
	lines := wrapCaption(caption, func(s string) bool { return font.MeasureString(face, s).Ceil() <= maxW })
	textW := 0
	for _, l := range lines {
		textW = max(textW, font.MeasureString(face, l).Ceil())
	}
	boxW, boxH := textW+int(2*padX), int(float64(len(lines))*lineH+2*padY)
	x0 := r.pad + (termW-boxW)/2
	y0 := r.pad + r.barH + termH - boxH - r.ch/2
	l := &captionLayout{box: image.Rect(x0, y0, x0+boxW, y0+boxH)}
	m := face.Metrics()
	for i, s := range lines {
		w := font.MeasureString(face, s).Ceil()
		mask := image.NewAlpha(image.Rect(0, 0, w+2, int(lineH)))
		baseline := (int(lineH) + m.Ascent.Ceil() - m.Descent.Ceil()) / 2
		d := font.Drawer{Dst: mask, Src: image.Opaque, Face: face, Dot: fixed.P(1, baseline)}
		d.DrawString(s)
		l.lines = append(l.lines, captionLine{mask, image.Pt(r.pad+(termW-w)/2, y0+int(padY+float64(i)*lineH))})
	}
	r.captions[caption] = l
	return l
}

// drawCaption draws a caption over the terminal area of a paletted image.
func (r *rasterizer) drawCaption(img *image.Paletted, caption string) {
	l := r.layoutCaption(caption)
	if caption == "" || l == nil {
		return
	}
	text, boxColor := captionColors(r.o.Theme)
	box := r.pal.add(boxColor)
	b := l.box.Intersect(img.Rect)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.Pix[y*img.Stride+x] = box
		}
	}
	ramp := r.pal.ramp(text, boxColor)
	for _, ln := range l.lines {
		for y := range ln.mask.Rect.Dy() {
			for x := range ln.mask.Rect.Dx() {
				p := ln.at.Add(image.Pt(x, y))
				if a := ln.mask.Pix[y*ln.mask.Stride+x]; a > 0 && p.In(img.Rect) {
					img.Pix[p.Y*img.Stride+p.X] = ramp[(int(a)*blendLevels+127)/255]
				}
			}
		}
	}
}

// drawCaptionRGBA draws a caption over the terminal area of a full color image.
func (r *rasterizer) drawCaptionRGBA(img draw.Image, caption string) {
	l := r.layoutCaption(caption)
	if caption == "" || l == nil {
		return
	}
	text, box := captionColors(r.o.Theme)
	draw.Draw(img, l.box, image.NewUniform(box), image.Point{}, draw.Src)
	for _, ln := range l.lines {
		at := ln.mask.Rect.Add(ln.at)
		draw.DrawMask(img, at, image.NewUniform(text), image.Point{}, ln.mask, image.Point{}, draw.Over)
	}
}

// writeSVGCaption writes the caption of one screen. Text widths are estimated:
// the viewer picks the font.
func (l svgLayout) writeSVGCaption(w *bufio.Writer, caption string) {
	if caption == "" {
		return
	}
	size := l.o.FontSize * 0.95
	charW := size * 0.56
	lineH, padX, padY := captionMetrics(size)
	maxW := l.termW()*captionWidth - 2*padX
	lines := wrapCaption(caption, func(s string) bool { return float64(len([]rune(s)))*charW <= maxW })
	textW := 0.0
	for _, s := range lines {
		textW = max(textW, float64(len([]rune(s)))*charW)
	}
	boxW, boxH := textW+2*padX, float64(len(lines))*lineH+2*padY
	y0 := l.termH() - boxH - l.ch/2
	text, box := captionColors(l.o.Theme)
	_, _ = fmt.Fprintf(w, `<rect x="%g" y="%g" width="%g" height="%g" rx="6" fill="%s"/>`,
		(l.termW()-boxW)/2, y0, boxW, boxH, cssColor(box))
	_, _ = fmt.Fprintf(w, `<text fill="%s" font-size="%g" text-anchor="middle" dominant-baseline="central" `+
		`style="font-family:system-ui,-apple-system,'Segoe UI',Cantarell,Ubuntu,sans-serif;white-space:pre">`,
		cssColor(text), size)
	for i, s := range lines {
		_, _ = fmt.Fprintf(w, `<tspan x="%g" y="%g">%s</tspan>`,
			l.termW()/2, y0+padY+(float64(i)+0.5)*lineH, html.EscapeString(s))
	}
	_, _ = w.WriteString("</text>")
}
