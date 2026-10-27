package export

import (
	"errors"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"io"
	"math"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/oktalz/go-take/cast"
)

// blendLevels is how many coverage steps are kept between background and
// foreground when anti-aliasing text into the GIF palette.
const blendLevels = 4

// rasterizer draws screens into paletted images.
type rasterizer struct {
	glyphs   map[glyphKey]*image.Alpha
	images   map[*Placement]*image.Paletted // Kitty graphics dithered to the palette
	captions map[string]*captionLayout
	pal      *paletteBuilder
	faces    faceCache
	families []*fontFamily // Options.Fonts, then Go Mono
	chrome   chrome        // window title bar, when Options.Window is set
	o        Options
	light    float64 // width of a light box drawing line
	cw, ch   int     // cell size in pixels
	baseline int
	barH     int // window title bar height
	pad      int
	cols     int
	rows     int
	// fullColor: the output is redrawn in full color (trueColor), so images
	// are not dithered to the palette.
	fullColor bool
	bgIndex   uint8
}

type glyphKey struct {
	r     rune
	style int
	cells int // 2 for wide characters
}

func newRasterizer(cols, rows int, o Options) (*rasterizer, error) {
	r := &rasterizer{
		o: o, glyphs: map[glyphKey]*image.Alpha{}, images: map[*Placement]*image.Paletted{},
		captions: map[string]*captionLayout{},
		cols:     cols, rows: rows, pad: o.Padding,
	}
	for _, p := range o.Fonts {
		fam, err := fileFamily(p)
		if err != nil {
			return nil, err
		}
		r.families = append(r.families, fam)
	}
	gm, err := goMonoFamily()
	if err != nil {
		return nil, err
	}
	r.families = append(r.families, gm)
	primary, err := r.faces.face(r.families[0].fonts[0], o.FontSize)
	if err != nil {
		return nil, err
	}
	adv, _ := primary.GlyphAdvance('M')
	r.cw = adv.Round()
	m := primary.Metrics()
	r.ch = int(math.Ceil(o.FontSize * 1.25))
	r.baseline = m.Ascent.Ceil() + (r.ch-m.Ascent.Ceil()-m.Descent.Ceil())/2 // center the line in the cell
	r.light = math.Max(1, math.Round(o.FontSize/12))
	if o.Window {
		r.chrome = windowChrome(o, float64(cols*r.cw+2*r.pad))
		r.barH = int(r.chrome.height)
	}
	return r, nil
}

func (r *rasterizer) size() image.Rectangle {
	return image.Rect(0, 0, r.cols*r.cw+2*r.pad, r.rows*r.ch+2*r.pad+r.barH)
}

// glyph returns the coverage mask of a rune over cells cells, rendered at the cell origin.
func (r *rasterizer) glyph(ch rune, style, cells int) *image.Alpha {
	k := glyphKey{ch, style, cells}
	if a, ok := r.glyphs[k]; ok {
		return a
	}
	var a *image.Alpha
	if cells == 1 {
		a = drawnGlyph(ch, r.cw, r.ch, r.light)
	}
	if a == nil {
		a = r.fontGlyph(ch, style, cells)
	}
	r.glyphs[k] = a
	return a
}

// fontGlyph draws ch with the first font of the chain that has it, then with a
// system font that has it. Without one, the primary font draws its missing-glyph box.
func (r *rasterizer) fontGlyph(ch rune, style, cells int) *image.Alpha {
	var fams []*fontFamily
	for _, f := range r.families {
		if f.has(ch) {
			fams = append(fams, f)
		}
	}
	if len(fams) == 0 && !r.o.NoSystemFonts {
		if f := systemFamilyFor(ch); f != nil {
			fams = append(fams, f)
		}
	}
	for _, f := range fams {
		if a := r.renderGlyph(f, ch, style, cells); a != nil && !blank(a) {
			return a
		}
	}
	if a := r.renderGlyph(r.families[0], ch, style, cells); a != nil {
		return a
	}
	return image.NewAlpha(image.Rect(0, 0, cells*r.cw, r.ch))
}

// renderGlyph draws ch from one font family into a mask cells cells wide. Glyphs
// wider than that (symbols from fallback fonts) are scaled down to fit and
// narrower ones are centered.
func (r *rasterizer) renderGlyph(fam *fontFamily, ch rune, style, cells int) *image.Alpha {
	f, embolden := fam.fonts[style], false
	if f == nil {
		f, embolden = fam.fonts[0], style&1 != 0
	}
	size := r.o.FontSize
	face, err := r.faces.face(f, size)
	if err != nil {
		return nil
	}
	cw := cells * r.cw
	adv, _ := face.GlyphAdvance(ch)
	if w := adv.Ceil(); w > cw+1 {
		size *= float64(cw) / float64(w)
		if face, err = r.faces.face(f, size); err != nil {
			return nil
		}
		adv, _ = face.GlyphAdvance(ch)
	}
	a := image.NewAlpha(image.Rect(0, 0, cw, r.ch))
	x := max(0, (fixed.I(cw)-adv)/2)
	d := font.Drawer{Dst: a, Src: image.Opaque, Face: face, Dot: fixed.Point26_6{X: x, Y: fixed.I(r.baseline)}}
	d.DrawString(string(ch))
	if embolden {
		for y := range r.ch {
			row := a.Pix[y*a.Stride : y*a.Stride+cw]
			for x := cw - 1; x > 0; x-- {
				row[x] = max(row[x], row[x-1])
			}
		}
	}
	return a
}

func blank(a *image.Alpha) bool {
	for _, v := range a.Pix {
		if v != 0 {
			return false
		}
	}
	return true
}

// paletteBuilder collects colors and fg/bg blend ramps into a GIF palette.
type paletteBuilder struct {
	index    map[color.RGBA]uint8
	ramps    map[[2]color.RGBA][blendLevels + 1]uint8
	colors   color.Palette
	overflow bool
}

func newPaletteBuilder() *paletteBuilder {
	return &paletteBuilder{index: map[color.RGBA]uint8{}, ramps: map[[2]color.RGBA][blendLevels + 1]uint8{}}
}

func (p *paletteBuilder) add(c color.RGBA) uint8 {
	if i, ok := p.index[c]; ok {
		return i
	}
	if len(p.colors) >= 256 {
		p.overflow = true
		return uint8(color.Palette(p.colors).Index(c))
	}
	i := uint8(len(p.colors))
	p.colors = append(p.colors, c)
	p.index[c] = i
	return i
}

// ramp returns palette indices blending bg (level 0) to fg (last level).
func (p *paletteBuilder) ramp(fg, bg color.RGBA) [blendLevels + 1]uint8 {
	k := [2]color.RGBA{fg, bg}
	if r, ok := p.ramps[k]; ok {
		return r
	}
	var r [blendLevels + 1]uint8
	for l := 0; l <= blendLevels; l++ {
		f := float64(l) / blendLevels
		r[l] = p.add(color.RGBA{
			R: uint8(float64(bg.R) + (float64(fg.R)-float64(bg.R))*f + 0.5),
			G: uint8(float64(bg.G) + (float64(fg.G)-float64(bg.G))*f + 0.5),
			B: uint8(float64(bg.B) + (float64(fg.B)-float64(bg.B))*f + 0.5),
			A: 255,
		})
	}
	p.ramps[k] = r
	return r
}

// prepare registers every color the given screens need, so the palette is shared by all frames.
func (r *rasterizer) prepare(screens []*Screen) {
	p := newPaletteBuilder()
	r.pal = p
	r.bgIndex = p.add(r.o.Theme.BG)
	if r.o.Window {
		for _, c := range r.chrome.colors() {
			p.ramp(c, r.chrome.bar)
		}
	}
	for _, s := range screens {
		for _, c := range s.Cells {
			p.ramp(c.FG, c.BG)
		}
		if s.CursorVisible && !r.o.HideCursor && s.CursorY < s.Rows {
			c := s.At(s.CursorX, s.CursorY)
			p.ramp(c.BG, c.FG)
		}
	}
	if p.overflow {
		// too many distinct colors (e.g. true-color gradients): fall back to a generic palette
		r.pal = newPaletteBuilder()
		r.pal.colors = append(color.Palette(nil), palette.Plan9...)
		for i, c := range palette.Plan9 {
			r.pal.index[c.(color.RGBA)] = uint8(i)
		}
		r.pal.overflow = true
		r.bgIndex = uint8(r.pal.colors.Index(r.o.Theme.BG))
	}
	for _, s := range screens {
		if s.Caption != "" {
			r.pal.ramp(captionColors(r.o.Theme))
			break
		}
	}
	r.prepareImages(placements(screens))
	if r.o.Theme.Transparent {
		// GIF and PNG encoders treat a palette entry with alpha 0 as transparent
		r.pal.colors[r.bgIndex] = color.RGBA{}
	}
}

// prepareImages fills the free palette entries with the images' colors and
// dithers each image to the palette.
func (r *rasterizer) prepareImages(ps []*Placement) {
	if len(ps) == 0 || r.fullColor {
		return
	}
	scaled := make([]*image.NRGBA, len(ps))
	for i, p := range ps {
		scaled[i] = p.scale(r.cw, r.ch)
	}
	for _, c := range quantize(scaled, r.o.Theme.BG, 256-len(r.pal.colors)) {
		r.pal.add(c)
	}
	bg := r.o.Theme.BG
	for i, p := range ps {
		src := scaled[i]
		flat := image.NewRGBA(src.Rect)
		for j := 0; j < len(src.Pix); j += 4 {
			c := over(src.Pix[j:j+4], bg)
			copy(flat.Pix[j:j+4], []uint8{c.R, c.G, c.B, 255})
		}
		img := image.NewPaletted(src.Rect, r.pal.colors)
		draw.FloydSteinberg.Draw(img, img.Rect, flat, image.Point{})
		if r.o.Theme.Transparent { // see-through pixels stay see-through
			for j := range img.Pix {
				if src.Pix[j*4+3] < 128 {
					img.Pix[j] = r.bgIndex
				}
			}
		}
		r.images[p] = img
	}
}

func (r *rasterizer) render(s *Screen) *image.Paletted {
	img := image.NewPaletted(r.size(), r.pal.colors)
	for i := range img.Pix {
		img.Pix[i] = r.bgIndex
	}
	if r.o.Window {
		r.drawWindow(img)
	}
	cursor := s.CursorVisible && !r.o.HideCursor
	for y := 0; y < s.Rows && y < r.rows; y++ {
		for x := 0; x < s.Cols && x < r.cols; x++ {
			c := s.At(x, y)
			fg, bg := c.FG, c.BG
			if cursor && x == s.CursorX && y == s.CursorY {
				fg, bg = bg, fg
			}
			if c.Image != nil {
				if !r.fullColor {
					r.drawImageCell(img, x, y, c)
				}
				continue
			}
			if c.WideTail && x > 0 && s.At(x-1, y).Wide {
				continue // drawn with the wide character
			}
			r.drawCell(img, x, y, c, fg, bg)
		}
	}
	r.drawCaption(img, s.Caption)
	return img
}

// imageCell returns the cell's piece of its image in the placement's pixels,
// and the cell's top left corner in the output.
func (r *rasterizer) imageCell(x, y int, c Cell) (piece image.Rectangle, at image.Point) {
	px, py := int(c.ImageCol)*r.cw, int(c.ImageRow)*r.ch
	return image.Rect(px, py, px+r.cw, py+r.ch), image.Pt(r.pad+x*r.cw, r.pad+r.barH+y*r.ch)
}

func (r *rasterizer) drawImageCell(img *image.Paletted, x, y int, c Cell) {
	src := r.images[c.Image]
	piece, at := r.imageCell(x, y, c)
	for py := range r.ch {
		copy(img.Pix[(at.Y+py)*img.Stride+at.X:][:r.cw], src.Pix[(piece.Min.Y+py)*src.Stride+piece.Min.X:])
	}
}

// trueColor redraws the cells of s over a full color copy of img: images,
// and text with exact colors and smooth edges, which a palette may not hold.
func (r *rasterizer) trueColor(img *image.Paletted, s *Screen) image.Image {
	out := image.NewNRGBA(img.Rect)
	draw.Draw(out, out.Rect, img, image.Point{}, draw.Src)
	scaled := map[*Placement]*image.NRGBA{}
	cursor := s.CursorVisible && !r.o.HideCursor
	for y := 0; y < s.Rows && y < r.rows; y++ {
		for x := 0; x < s.Cols && x < r.cols; x++ {
			c := s.At(x, y)
			if c.Image == nil {
				if c.WideTail && x > 0 && s.At(x-1, y).Wide {
					continue
				}
				fg, bg := c.FG, c.BG
				if cursor && x == s.CursorX && y == s.CursorY {
					fg, bg = bg, fg
				}
				r.drawCellRGBA(out, x, y, c, fg, bg)
				continue
			}
			src := scaled[c.Image]
			if src == nil {
				src = c.Image.scale(r.cw, r.ch)
				scaled[c.Image] = src
			}
			piece, at := r.imageCell(x, y, c)
			dst := image.Rectangle{Min: at, Max: at.Add(piece.Size())}
			bg := c.BG
			if r.o.Theme.Transparent && bg == r.o.Theme.BG {
				bg = color.RGBA{}
			}
			draw.Draw(out, dst, image.NewUniform(bg), image.Point{}, draw.Src)
			draw.Draw(out, dst, src, piece.Min, draw.Over)
		}
	}
	r.drawCaptionRGBA(out, s.Caption)
	return out
}

// drawCellRGBA is drawCell in full color.
func (r *rasterizer) drawCellRGBA(img *image.NRGBA, x, y int, c Cell, fg, bg color.RGBA) {
	ox, oy := r.pad+x*r.cw, r.pad+r.barH+y*r.ch
	mask, cells := r.cellGlyph(x, c)
	seeThrough := r.o.Theme.Transparent && bg == r.o.Theme.BG
	for py := range r.ch {
		row := img.Pix[(oy+py)*img.Stride+ox*4:]
		for px := range cells * r.cw {
			a := 0
			if mask != nil {
				a = int(mask.Pix[py*mask.Stride+px])
			}
			if c.Underline && py == r.ch-2 {
				a = 255
			}
			p := row[px*4 : px*4+4]
			if seeThrough { // text over a see-through background
				p[0], p[1], p[2], p[3] = fg.R, fg.G, fg.B, uint8(a)
				continue
			}
			mix := func(f, b uint8) uint8 { return uint8((int(f)*a + int(b)*(255-a) + 127) / 255) }
			p[0], p[1], p[2], p[3] = mix(fg.R, bg.R), mix(fg.G, bg.G), mix(fg.B, bg.B), 255
		}
	}
}

// cellGlyph returns the coverage mask of the cell at column x (nil for a
// space) and how many cells it covers.
func (r *rasterizer) cellGlyph(x int, c Cell) (mask *image.Alpha, cells int) {
	style := 0
	if c.Bold {
		style |= 1
	}
	if c.Italic {
		style |= 2
	}
	cells = 1
	if c.Wide && x+1 < r.cols {
		cells = 2
	}
	if c.Char != ' ' {
		mask = r.glyph(c.Char, style, cells)
	}
	return mask, cells
}

func (r *rasterizer) drawCell(img *image.Paletted, x, y int, c Cell, fg, bg color.RGBA) {
	ramp := r.pal.ramp(fg, bg)
	ox, oy := r.pad+x*r.cw, r.pad+r.barH+y*r.ch
	mask, cells := r.cellGlyph(x, c)
	for py := range r.ch {
		row := img.Pix[(oy+py)*img.Stride+ox:]
		for px := range cells * r.cw {
			level := 0
			if mask != nil {
				level = (int(mask.Pix[py*mask.Stride+px])*blendLevels + 127) / 255
			}
			if c.Underline && py == r.ch-2 {
				level = blendLevels
			}
			row[px] = ramp[level]
		}
	}
}

// GIF writes an animated GIF of the recording.
func GIF(w io.Writer, c *cast.Cast, o Options) error {
	o = o.withDefaults(c)
	frames := Frames(c, o)
	cols, rows := maxSize(frames)
	r, err := newRasterizer(cols, rows, o)
	if err != nil {
		return err
	}
	screens := make([]*Screen, len(frames))
	for i, f := range frames {
		screens[i] = f.Screen
	}
	r.prepare(screens)

	anim := &gif.GIF{Config: image.Config{ColorModel: r.pal.colors, Width: r.size().Dx(), Height: r.size().Dy()}}
	var prev *image.Paletted
	shown := 0 // centiseconds already emitted, avoids rounding drift
	for _, f := range frames {
		img := r.render(f.Screen)
		end := int(math.Round((f.Time + f.Duration) * 100))
		delay := max(end-shown, 2)
		shown += delay
		frame := img
		if prev != nil && r.o.Theme.Transparent {
			if diffBounds(prev, img).Empty() {
				anim.Delay[len(anim.Delay)-1] += delay
				continue
			}
		} else if prev != nil {
			changed := diffBounds(prev, img)
			if changed.Empty() { // identical after rendering: extend the previous frame
				anim.Delay[len(anim.Delay)-1] += delay
				continue
			}
			sub, ok := img.SubImage(changed).(*image.Paletted)
			if !ok {
				return errors.New("gif: unexpected image type")
			}
			frame = sub
		}
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, delay)
		// a transparent background would let earlier frames show through,
		// so those are whole frames that clear the canvas when they end
		disposal := byte(gif.DisposalNone)
		if r.o.Theme.Transparent {
			disposal = gif.DisposalBackground
		}
		anim.Disposal = append(anim.Disposal, disposal)
		prev = img
	}
	return gif.EncodeAll(w, anim)
}

// PNG writes a single frame: the screen at recording time o.At (<0 = end).
func PNG(w io.Writer, c *cast.Cast, o Options) error {
	o = o.withDefaults(c)
	s := ScreenAt(c, o.At, o)
	r, err := newRasterizer(s.Cols, s.Rows, o)
	if err != nil {
		return err
	}
	r.fullColor = true
	r.prepare([]*Screen{s})
	img := r.render(s)
	// pictures, and more colors than a palette holds, need full color; else a palette is smaller
	if r.pal.overflow || len(placements([]*Screen{s})) > 0 {
		return png.Encode(w, r.trueColor(img, s))
	}
	return png.Encode(w, img)
}

func maxSize(frames []Frame) (cols, rows int) {
	for _, f := range frames {
		cols, rows = max(cols, f.Screen.Cols), max(rows, f.Screen.Rows)
	}
	return cols, rows
}

// diffBounds returns the smallest rectangle containing every changed pixel.
func diffBounds(a, b *image.Paletted) image.Rectangle {
	minX, minY, maxX, maxY := b.Rect.Max.X, b.Rect.Max.Y, -1, -1
	for y := b.Rect.Min.Y; y < b.Rect.Max.Y; y++ {
		ra := a.Pix[y*a.Stride : y*a.Stride+a.Rect.Dx()]
		rb := b.Pix[y*b.Stride : y*b.Stride+b.Rect.Dx()]
		for x := range rb {
			if ra[x] != rb[x] {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}
