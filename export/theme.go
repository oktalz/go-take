// Package export renders recordings to GIF, PNG, SVG, HTML and text.
package export

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/hinshun/vt10x"

	"github.com/oktalz/go-take/cast"
)

// Theme holds the terminal colors used for rendering.
type Theme struct {
	FG, BG  color.RGBA
	Palette [16]color.RGBA
	// Transparent leaves the terminal background out (header theme bg "transparent").
	// BG is still the color anti-aliased text is blended against.
	Transparent bool
}

// TransparentBG is the header theme background that makes it transparent.
const TransparentBG = "transparent"

// DefaultTheme matches the web player's default theme.
func DefaultTheme() Theme {
	return Theme{
		BG: hex("#121314"), FG: hex("#d6d6d6"),
		Palette: [16]color.RGBA{
			hex("#1b1d1e"), hex("#e0525c"), hex("#7fb85e"), hex("#e5a50a"),
			hex("#5b9bd5"), hex("#c678dd"), hex("#4fb6be"), hex("#d6d6d6"),
			hex("#5c6370"), hex("#ff6e76"), hex("#a2d977"), hex("#ffcb4b"),
			hex("#7cb7ff"), hex("#dd99f0"), hex("#6fdbe3"), hex("#ffffff"),
		},
	}
}

// ThemeFor returns the theme stored in the recording header, or the default.
func ThemeFor(h cast.Header) Theme {
	t := DefaultTheme()
	th := h.Term.Theme
	if th == nil {
		return t
	}
	if c, ok := parseHex(th.FG); ok {
		t.FG = c
	}
	if c, ok := parseHex(th.BG); ok {
		t.BG = c
	}
	t.Transparent = strings.EqualFold(th.BG, TransparentBG)
	cols := strings.Split(th.Palette, ":")
	for i, s := range cols {
		c, ok := parseHex(s)
		if !ok || i >= 16 {
			continue
		}
		t.Palette[i] = c
		if len(cols) == 8 {
			t.Palette[i+8] = c // 8-color palettes reuse colors for bright
		}
	}
	return t
}

// Color resolves a terminal color to RGB.
func (t Theme) Color(c vt10x.Color) color.RGBA {
	switch {
	case c == vt10x.DefaultFG || c == vt10x.DefaultCursor:
		return t.FG
	case c == vt10x.DefaultBG:
		return t.BG
	case c < 16:
		return t.Palette[c]
	case c < 232:
		levels := [6]uint8{0, 95, 135, 175, 215, 255}
		i := int(c) - 16
		return color.RGBA{levels[i/36], levels[i/6%6], levels[i%6], 255}
	case c < 256:
		g := uint8(8 + 10*(int(c)-232))
		return color.RGBA{g, g, g, 255}
	}
	return color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255} // true color
}

func hex(s string) color.RGBA {
	c, _ := parseHex(s)
	return c
}

func parseHex(s string) (color.RGBA, bool) {
	var r, g, b uint8
	s = strings.TrimSpace(s)
	if len(s) == 4 { // #rgb
		s = string([]byte{'#', s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{r, g, b, 255}, true
}

// cssColor formats a color for SVG/CSS.
func cssColor(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
