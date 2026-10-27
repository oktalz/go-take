package export

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"  // iTerm2 images may be GIF
	_ "image/jpeg" // or JPEG
	"math"
	"strconv"
	"strings"
)

// Inline images besides Kitty's: Sixel (DCS P1;P2;P3 q DATA ST) and iTerm2
// (OSC 1337 ; File=ARGS : BASE64 BEL). Both are shown at the cursor, through
// the same image cells as Kitty's direct placements.

// graphics command kinds, found by nextGraphics.
const (
	gfxNone = iota
	gfxKitty
	gfxSixel
	gfxITerm
)

var gfxIntros = []string{kittyAPC, "\x1bP", itermIntro}

const itermIntro = "\x1b]1337;File="

// nextGraphics returns where the next graphics command starts and its kind.
// A DCS that is not Sixel is gfxNone at its index: it is not one.
func nextGraphics(s string) (at, kind int) {
	at = -1
	for k, intro := range gfxIntros {
		if i := strings.Index(s, intro); i >= 0 && (at < 0 || i < at) {
			at, kind = i, k+1
		}
	}
	return at, kind
}

// sixelStart reports whether a DCS (s starts with ESC P) is Sixel: digits and
// semicolons, then q. done is false when s ends before that is known.
func sixelStart(s string) (sixel, done bool) {
	for i := 2; i < len(s); i++ {
		switch c := s[i]; {
		case c == 'q':
			return true, true
		case c == ';' || c >= '0' && c <= '9':
		default:
			return false, true
		}
	}
	return false, false
}

// graphicsEnd finds the end of the command at the start of s: its body, and
// the length of the whole command. complete is false when it has not ended
// yet; body is "" with a length for a command cut short by another escape.
func graphicsEnd(s string, kind int) (body string, n int, complete bool) {
	intro := gfxIntros[kind-1]
	for i := len(intro); i < len(s); i++ {
		switch s[i] {
		case 0x07:
			if kind == gfxITerm {
				return s[len(intro):i], i + 1, true
			}
		case 0x1b:
			if i+1 == len(s) {
				return "", 0, false
			}
			if s[i+1] == '\\' {
				return s[len(intro):i], i + 2, true
			}
			return "", i, true
		default:
		}
	}
	return "", 0, false
}

// heldBack returns how many bytes at the end of s may start a graphics
// command, to wait for the rest of it.
func heldBack(s string) int {
	for n := min(len(s), len(itermIntro)-1); n > 0; n-- {
		tail := s[len(s)-n:]
		for _, intro := range gfxIntros {
			if strings.HasPrefix(intro, tail) {
				return n
			}
		}
	}
	return 0
}

// sixel shows a Sixel image at the cursor.
func (k *kitty) sixel(body string, scr kittyScreen) {
	q := strings.IndexByte(body, 'q')
	img := decodeSixel(body[q+1:])
	if img == nil {
		return
	}
	k.placeDirect(&Placement{
		Image: img, Cols: ceilDiv(img.Rect.Dx(), k.cellW), Rows: ceilDiv(img.Rect.Dy(), k.cellH),
		fit: fitTop, cursor: cursorBelow,
	}, scr)
}

// iterm shows an iTerm2 inline image at the cursor.
func (k *kitty) iterm(body string, scr kittyScreen, cols, rows int) {
	head, data, ok := strings.Cut(body, ":")
	if !ok {
		return
	}
	args := map[string]string{}
	for kv := range strings.SplitSeq(head, ";") {
		if key, v, ok := strings.Cut(kv, "="); ok {
			args[key] = v
		}
	}
	if args["inline"] != "1" { // a file download
		return
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil || cfg.Width*cfg.Height > maxImagePixels {
		return
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}
	img := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)

	// the size in pixels: N cells, Npx, N% of the terminal, or auto (the image's own)
	size := func(v string, total, cell, own int) (int, bool) {
		switch {
		case v == "" || v == "auto":
			return own, false
		case strings.HasSuffix(v, "px"):
			n, _ := strconv.Atoi(strings.TrimSuffix(v, "px"))
			return n, true
		case strings.HasSuffix(v, "%"):
			n, _ := strconv.Atoi(strings.TrimSuffix(v, "%"))
			return total * cell * n / 100, true
		default:
			n, _ := strconv.Atoi(v)
			return n * cell, true
		}
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	pw, wSet := size(args["width"], cols, k.cellW, w)
	ph, hSet := size(args["height"], rows, k.cellH, h)
	keep := args["preserveAspectRatio"] != "0"
	switch {
	case keep && wSet && !hSet:
		ph = pw * h / max(w, 1)
	case keep && hSet && !wSet:
		pw = ph * w / max(h, 1)
	default:
	}
	if pw <= 0 || ph <= 0 {
		return
	}
	p := &Placement{
		Image: img, Cols: min(ceilDiv(pw, k.cellW), cols), Rows: ceilDiv(ph, k.cellH),
		fit: fitStretch, cursor: cursorRight,
	}
	if keep {
		p.fit = fitTop
	}
	k.placeDirect(p, scr)
}

// maxSixelSide bounds a Sixel image, so a broken one cannot take all memory.
const maxSixelSide = 8192

// decodeSixel decodes Sixel data (after the q). Pixels it does not set stay
// transparent, so the terminal's background shows.
func decodeSixel(data string) *image.NRGBA {
	palette := make([]color.NRGBA, 256)
	copy(palette, vt340Palette[:])
	var rows [][]color.NRGBA // rows of pixels set so far
	x, y, cur := 0, 0, 0
	declW, declH, maxW := 0, 0, 0
	set := func(x, y int, c color.NRGBA) {
		if x >= maxSixelSide || y >= maxSixelSide {
			return
		}
		for len(rows) <= y {
			rows = append(rows, nil)
		}
		for len(rows[y]) <= x {
			rows[y] = append(rows[y], color.NRGBA{})
		}
		rows[y][x] = c
		maxW = max(maxW, x+1)
	}
	for i := 0; i < len(data); {
		c := data[i]
		i++
		switch {
		case c == '"': // raster attributes: Pan;Pad;Ph;Pv
			var ps []int
			ps, i = sixelParams(data, i)
			if len(ps) >= 4 {
				declW, declH = min(ps[2], maxSixelSide), min(ps[3], maxSixelSide)
			}
		case c == '#': // color: select, or define with Pu;Px;Py;Pz
			var ps []int
			ps, i = sixelParams(data, i)
			if len(ps) == 0 || ps[0] < 0 || ps[0] >= len(palette) {
				continue
			}
			cur = ps[0]
			if len(ps) >= 5 {
				palette[cur] = sixelColor(ps[1], ps[2], ps[3], ps[4])
			}
		case c == '!': // repeat: !N then a sixel
			var ps []int
			ps, i = sixelParams(data, i)
			if i < len(data) && len(ps) > 0 && data[i] >= '?' && data[i] <= '~' {
				bits := data[i] - '?'
				i++
				for range min(ps[0], maxSixelSide) {
					drawSixel(set, x, y, bits, palette[cur])
					x++
				}
			}
		case c == '$':
			x = 0
		case c == '-':
			x, y = 0, y+6
		case c >= '?' && c <= '~':
			drawSixel(set, x, y, c-'?', palette[cur])
			x++
		default:
		}
	}
	w, h := max(maxW, declW), max(len(rows), declH)
	if w == 0 || h == 0 {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y, row := range rows {
		for x, c := range row {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func drawSixel(set func(x, y int, c color.NRGBA), x, y int, bits byte, c color.NRGBA) {
	for b := range 6 {
		if bits&(1<<b) != 0 {
			set(x, y+b, c)
		}
	}
}

// sixelParams reads numbers separated by semicolons from data at i.
func sixelParams(data string, i int) ([]int, int) {
	var ps []int
	n, has := 0, false
	for ; i < len(data); i++ {
		c := data[i]
		switch {
		case c >= '0' && c <= '9':
			n, has = min(n*10+int(c-'0'), 1<<20), true
		case c == ';':
			ps = append(ps, n)
			n, has = 0, false
		default:
			if has || len(ps) > 0 {
				ps = append(ps, n)
			}
			return ps, i
		}
	}
	if has || len(ps) > 0 {
		ps = append(ps, n)
	}
	return ps, i
}

// sixelColor converts a Sixel color definition: Pu 2 is RGB in percent, Pu 1
// is HLS (hue in degrees from blue, lightness and saturation in percent).
func sixelColor(pu, px, py, pz int) color.NRGBA {
	pct := func(v int) uint8 { return uint8(min(max(v, 0), 100) * 255 / 100) }
	if pu == 2 {
		return color.NRGBA{pct(px), pct(py), pct(pz), 255}
	}
	h := math.Mod(float64(px)+240, 360) / 360 // Sixel hue 0 is blue
	l, s := float64(py)/100, float64(pz)/100
	hue := func(p, q, t float64) float64 {
		t -= math.Floor(t)
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}
	q := l + s - l*s
	if l < 0.5 {
		q = l * (1 + s)
	}
	p := 2*l - q
	to8 := func(v float64) uint8 { return uint8(math.Round(min(max(v, 0), 1) * 255)) }
	return color.NRGBA{to8(hue(p, q, h+1.0/3)), to8(hue(p, q, h)), to8(hue(p, q, h-1.0/3)), 255}
}

// vt340Palette is the default Sixel palette.
var vt340Palette = [16]color.NRGBA{
	{0, 0, 0, 255},
	{51, 51, 204, 255},
	{204, 33, 33, 255},
	{51, 204, 51, 255},
	{204, 51, 204, 255},
	{51, 204, 204, 255},
	{204, 204, 51, 255},
	{120, 120, 120, 255},
	{69, 69, 69, 255},
	{87, 87, 153, 255},
	{153, 69, 69, 255},
	{87, 153, 87, 255},
	{153, 87, 153, 255},
	{87, 153, 153, 255},
	{153, 153, 87, 255},
	{204, 204, 204, 255},
}
