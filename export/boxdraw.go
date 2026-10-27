package export

import (
	"image"
	"math"
)

// Box drawing, block elements, braille and powerline separators are drawn in
// code instead of taken from a font, the way terminals such as kitty do: lines
// then fill the whole cell and join with their neighbors, and fonts that lack
// them (Go Mono has no rounded corners or braille) do not leave gaps.

// boxSegments holds the line weight of each box drawing character towards up,
// right, down and left: 0 none, 1 light, 2 heavy, 3 double. Dashed lines and
// arcs are handled separately.
var boxSegments = map[rune][4]uint8{}

func init() {
	// consecutive code points, each as "urdl" digits
	table := []struct {
		first rune
		urdl  string
	}{
		{0x2500, "0101 0202 1010 2020"},
		{0x250C, "0110 0210 0120 0220 0011 0012 0021 0022 1100 1200 2100 2200 1001 1002 2001 2002"},
		{0x251C, "1110 1210 2110 1120 2120 2210 1220 2220 1011 1012 2011 1021 2021 2012 1022 2022"},
		{0x252C, "0111 0112 0211 0212 0121 0122 0221 0222 1101 1102 1201 1202 2101 2102 2201 2202"},
		{0x253C, "1111 1112 1211 1212 2111 1121 2121 2112 2211 1122 1221 2212 1222 2122 2221 2222"},
		{0x2550, "0303 3030 0310 0130 0330 0013 0031 0033 1300 3100 3300 1003 3001 3003"},
		{0x255E, "1310 3130 3330 1013 3031 3033 0313 0131 0333 1303 3101 3303 1313 3131 3333"},
		{0x2574, "0001 1000 0100 0010 0002 2000 0200 0020 0201 1020 0102 2010"},
	}
	for _, t := range table {
		r := t.first
		for i := 0; i+4 <= len(t.urdl); i += 5 {
			var s [4]uint8
			for k := range 4 {
				s[k] = t.urdl[i+k] - '0'
			}
			boxSegments[r] = s
			r++
		}
	}
}

// canvas is a supersampled coverage grid for one cell.
type canvas struct {
	cov  []bool
	w, h float64 // cell size in pixels
	ss   int     // samples per pixel side
}

const supersample = 4

func newCanvas(w, h int) *canvas {
	return &canvas{w: float64(w), h: float64(h), ss: supersample, cov: make([]bool, w*h*supersample*supersample)}
}

// fill marks every sample whose center (in pixel units) satisfies in.
func (c *canvas) fill(in func(x, y float64) bool) {
	sw := int(c.w) * c.ss
	for i := range c.cov {
		sx, sy := i%sw, i/sw
		x := (float64(sx) + 0.5) / float64(c.ss)
		y := (float64(sy) + 0.5) / float64(c.ss)
		if !c.cov[i] && in(x, y) {
			c.cov[i] = true
		}
	}
}

func (c *canvas) rect(x0, y0, x1, y1 float64) {
	c.fill(func(x, y float64) bool { return x >= x0 && x < x1 && y >= y0 && y < y1 })
}

// alpha downsamples the canvas, scaled by level (1 = solid).
func (c *canvas) alpha(level float64) *image.Alpha {
	w, h := int(c.w), int(c.h)
	a := image.NewAlpha(image.Rect(0, 0, w, h))
	sw := w * c.ss
	for py := range h {
		for px := range w {
			n := 0
			for sy := range c.ss {
				for sx := range c.ss {
					if c.cov[(py*c.ss+sy)*sw+px*c.ss+sx] {
						n++
					}
				}
			}
			a.Pix[py*a.Stride+px] = uint8(math.Round(float64(n) / float64(c.ss*c.ss) * level * 255))
		}
	}
	return a
}

// drawnGlyph returns the coverage of ch drawn in code, or nil when ch is not one of those characters.
// light is the thickness of a light line in pixels.
func drawnGlyph(ch rune, w, h int, light float64) *image.Alpha {
	c := newCanvas(w, h)
	level := 1.0
	switch {
	case ch >= 0x2500 && ch <= 0x257F:
		if !c.box(ch, light) {
			return nil
		}
	case ch >= 0x2580 && ch <= 0x259F:
		level = c.block(ch)
	case ch >= 0x2800 && ch <= 0x28FF:
		c.braille(ch)
	case ch >= 0xE0B0 && ch <= 0xE0B6 && ch != 0xE0B5:
		c.powerline(ch, light)
	default:
		return nil
	}
	return c.alpha(level)
}

// snap rounds a line position so light lines land on whole pixels.
func snap(v float64) float64 { return math.Floor(v) }

// heavyWidth is the width of a heavy line: light plus an even number of pixels, so it stays centered on whole pixels.
func heavyWidth(light float64) float64 { return light + 2*math.Max(1, math.Round(light/2)) }

func (c *canvas) box(ch rune, light float64) bool {
	heavy := heavyWidth(light)
	cx, cy := snap(c.w/2), snap(c.h/2)
	if c.boxShape(ch, cx, cy, light, heavy) {
		return true
	}
	seg, ok := boxSegments[ch]
	if !ok {
		return false
	}
	g := light                       // distance of each line of a double line from the center
	xc, yc := cx+light/2, cy+light/2 // center of a light line through the middle of the cell
	width := func(k uint8) float64 {
		switch k {
		case 1:
			return light
		case 2:
			return heavy
		case 3:
			return 2*g + light
		default:
			return 0
		}
	}
	// reach is how far a light or heavy segment runs past the center: far enough to
	// cover the lines crossing it, or only up to the near line of a double line it ends at
	reach := func(dir int) float64 {
		p1, p2 := seg[(dir+1)%4], seg[(dir+3)%4]
		if (p1 == 3 || p2 == 3) && seg[(dir+2)%4] == 0 {
			return light/2 - g
		}
		return math.Max(math.Max(width(p1), width(p2))/2, light/2)
	}
	// inner is where a line of a double segment starts: at the near line of a
	// perpendicular double line, otherwise at the far one, so corners close
	inner := func(other uint8) float64 {
		if other == 3 {
			return g
		}
		return -g
	}
	for dir, k := range seg {
		switch {
		case k == 0:
		case k != 3:
			t := width(k)
			switch dir {
			case 0:
				c.rect(xc-t/2, 0, xc+t/2, yc+reach(0))
			case 1:
				c.rect(xc-reach(1), yc-t/2, c.w, yc+t/2)
			case 2:
				c.rect(xc-t/2, yc-reach(2), xc+t/2, c.h)
			case 3:
				c.rect(0, yc-t/2, xc+reach(3), yc+t/2)
			default:
			}
		case dir == 1:
			c.rect(xc+inner(seg[0])-light/2, yc-g-light/2, c.w, yc-g+light/2)
			c.rect(xc+inner(seg[2])-light/2, yc+g-light/2, c.w, yc+g+light/2)
		case dir == 3:
			c.rect(0, yc-g-light/2, xc-inner(seg[0])+light/2, yc-g+light/2)
			c.rect(0, yc+g-light/2, xc-inner(seg[2])+light/2, yc+g+light/2)
		case dir == 2:
			c.rect(xc-g-light/2, yc+inner(seg[3])-light/2, xc-g+light/2, c.h)
			c.rect(xc+g-light/2, yc+inner(seg[1])-light/2, xc+g+light/2, c.h)
		default: // dir == 0
			c.rect(xc-g-light/2, 0, xc-g+light/2, yc-inner(seg[3])+light/2)
			c.rect(xc+g-light/2, 0, xc+g+light/2, yc-inner(seg[1])+light/2)
		}
	}
	return true
}

// boxShape draws the box drawing characters that are not made of straight segments.
func (c *canvas) boxShape(ch rune, cx, cy, light, heavy float64) bool {
	switch {
	case ch >= 0x2504 && ch <= 0x250B, ch >= 0x254C && ch <= 0x254F:
		c.dashed(ch, cx, cy, light, heavy)
	case ch >= 0x256D && ch <= 0x2570:
		c.arc(ch, cx, cy, light)
	case ch >= 0x2571 && ch <= 0x2573:
		c.diagonal(ch, light)
	default:
		return false
	}
	return true
}

func (c *canvas) dashed(ch rune, cx, cy, light, heavy float64) {
	n := 3.0
	switch {
	case ch >= 0x2508 && ch <= 0x250B:
		n = 4
	case ch >= 0x254C:
		n = 2
	default:
	}
	t := light
	if (ch-0x2504)%2 == 1 || (ch >= 0x254C && (ch-0x254C)%2 == 1) {
		t = heavy
	}
	vertical := (ch >= 0x2506 && ch <= 0x2507) || (ch >= 0x250A && ch <= 0x250B) || ch >= 0x254E
	length := c.w
	if vertical {
		length = c.h
	}
	step := length / n
	for i := range int(n) {
		a, b := float64(i)*step, float64(i)*step+step*0.6
		if vertical {
			c.rect(cx-t/2+light/2, a, cx+t/2+light/2, b)
		} else {
			c.rect(a, cy-t/2+light/2, b, cy+t/2+light/2)
		}
	}
}

// arc draws ╭ ╮ ╯ ╰: a quarter circle joining the middle of two cell edges.
func (c *canvas) arc(ch rune, cx, cy, light float64) {
	x0, y0 := cx+light/2, cy+light/2 // where straight lines through the cell run
	r := math.Min(c.w, c.h) / 2
	sx, sy := 1.0, 1.0 // direction of the arc's center from the line crossing
	switch ch {
	case 0x256E: // ╮ down and left
		sx = -1
	case 0x256F: // ╯ up and left
		sx, sy = -1, -1
	case 0x2570: // ╰ up and right
		sy = -1
	default: // ╭ down and right
	}
	ax, ay := x0+sx*r, y0+sy*r // arc center
	c.fill(func(x, y float64) bool {
		dx, dy := x-ax, y-ay
		switch {
		case dx*sx <= 0 && dy*sy <= 0: // the quarter between the two edges
			return math.Abs(math.Hypot(dx, dy)-r) <= light/2
		case dx*sx > 0 && dy*sy <= 0: // straight run towards the side edge
			return math.Abs(y-y0) <= light/2
		case dy*sy > 0 && dx*sx <= 0: // straight run towards the top or bottom edge
			return math.Abs(x-x0) <= light/2
		}
		return false
	})
}

func (c *canvas) diagonal(ch rune, light float64) {
	line := func(x0, y0, x1, y1 float64) {
		dx, dy := x1-x0, y1-y0
		n := math.Hypot(dx, dy)
		c.fill(func(x, y float64) bool { return math.Abs((x-x0)*dy-(y-y0)*dx)/n <= light/2+0.2 })
	}
	if ch == 0x2571 || ch == 0x2573 {
		line(c.w, 0, 0, c.h)
	}
	if ch == 0x2572 || ch == 0x2573 {
		line(0, 0, c.w, c.h)
	}
}

// block draws U+2580..U+259F and returns the opacity (below 1 for the shades ░▒▓).
func (c *canvas) block(ch rune) float64 {
	w, h := c.w, c.h
	eighthH := func(n float64) float64 { return math.Round(h * n / 8) }
	eighthW := func(n float64) float64 { return math.Round(w * n / 8) }
	switch {
	case ch == 0x2580: // ▀
		c.rect(0, 0, w, eighthH(4))
	case ch >= 0x2581 && ch <= 0x2588: // ▁..█ lower eighths
		c.rect(0, h-eighthH(float64(ch-0x2580)), w, h)
	case ch >= 0x2589 && ch <= 0x258F: // ▉..▏ left eighths
		c.rect(0, 0, eighthW(float64(0x2590-ch)), h)
	case ch == 0x2590: // ▐
		c.rect(eighthW(4), 0, w, h)
	case ch >= 0x2591 && ch <= 0x2593: // ░ ▒ ▓
		c.rect(0, 0, w, h)
		return float64(ch-0x2590) / 4
	case ch == 0x2594: // ▔
		c.rect(0, 0, w, eighthH(1))
	case ch == 0x2595: // ▕
		c.rect(w-eighthW(1), 0, w, h)
	default: // quadrants ▖..▟ as upper left, upper right, lower left, lower right
		quads := map[rune]uint8{
			0x2596: 0b0010, 0x2597: 0b0001, 0x2598: 0b1000, 0x2599: 0b1011, 0x259A: 0b1001,
			0x259B: 0b1110, 0x259C: 0b1101, 0x259D: 0b0100, 0x259E: 0b0110, 0x259F: 0b0111,
		}[ch]
		mx, my := eighthW(4), eighthH(4)
		if quads&0b1000 != 0 {
			c.rect(0, 0, mx, my)
		}
		if quads&0b0100 != 0 {
			c.rect(mx, 0, w, my)
		}
		if quads&0b0010 != 0 {
			c.rect(0, my, mx, h)
		}
		if quads&0b0001 != 0 {
			c.rect(mx, my, w, h)
		}
	}
	return 1
}

// braille draws the dots of U+2800..U+28FF in a 2x4 grid.
func (c *canvas) braille(ch rune) {
	bits := ch - 0x2800
	// bit -> (column, row): dots 1-3 left, 4-6 right, 7 and 8 at the bottom
	pos := [8][2]float64{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	r := math.Min(c.w/4, c.h/8) * 0.75
	for b, p := range pos {
		if bits&(1<<b) == 0 {
			continue
		}
		x, y := c.w*(0.5+2*p[0])/4, c.h*(0.5+2*p[1])/8
		c.fill(func(px, py float64) bool { return math.Hypot(px-x, py-y) <= r })
	}
}

// powerline draws the separators U+E0B0..U+E0B6 used by fancy prompts.
func (c *canvas) powerline(ch rune, light float64) {
	w, h := c.w, c.h
	switch ch {
	case 0xE0B0: // solid triangle pointing right
		c.fill(func(x, y float64) bool { return x <= w*(1-math.Abs(2*y/h-1)) })
	case 0xE0B2: // solid triangle pointing left
		c.fill(func(x, y float64) bool { return w-x <= w*(1-math.Abs(2*y/h-1)) })
	case 0xE0B1, 0xE0B3: // thin chevrons > and <
		c.fill(func(x, y float64) bool {
			want := w * (1 - math.Abs(2*y/h-1))
			if ch == 0xE0B3 {
				x = w - x
			}
			return math.Abs(x-want) <= light
		})
	case 0xE0B4: // solid half circle on the left edge
		c.fill(func(x, y float64) bool { return (x/w)*(x/w)+((y-h/2)/(h/2))*((y-h/2)/(h/2)) <= 1 })
	case 0xE0B6: // solid half circle on the right edge
		c.fill(func(x, y float64) bool { return ((w-x)/w)*((w-x)/w)+((y-h/2)/(h/2))*((y-h/2)/(h/2)) <= 1 })
	default:
	}
}
