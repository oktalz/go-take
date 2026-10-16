package export

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// FontEnv lists font files used by default when Options.Fonts is empty,
// separated like PATH (":" on Unix, ";" on Windows).
const FontEnv = "GOTAKE_FONT"

// Text in GIF and PNG is drawn with a chain of fonts: the font files given in
// Options.Fonts, then the built-in Go Mono, then, for characters none of them
// has, a font found on the system with fontconfig (fc-match).

// fontStyle files of one family: regular, bold, italic, bold italic. A nil
// style is drawn with the regular font (bold is emboldened).
type fontFamily struct {
	name  string
	fonts [4]*sfnt.Font
}

// has reports whether the regular font has a glyph for ch.
func (f *fontFamily) has(ch rune) bool {
	var b sfnt.Buffer
	i, err := f.fonts[0].GlyphIndex(&b, ch)
	return err == nil && i != 0
}

// hasOutline reports whether f draws ch with an outline. Color emoji fonts have
// the character but only as a bitmap, which cannot be drawn here.
func hasOutline(f *sfnt.Font, ch rune) bool {
	var b sfnt.Buffer
	i, err := f.GlyphIndex(&b, ch)
	if err != nil || i == 0 {
		return false
	}
	segs, err := f.LoadGlyph(&b, i, fixed.I(64), nil)
	return err == nil && len(segs) > 0
}

var (
	goMonoOnce sync.Once
	goMono     *fontFamily
	goMonoErr  error
)

func goMonoFamily() (*fontFamily, error) {
	goMonoOnce.Do(func() {
		goMono = &fontFamily{name: "Go Mono"}
		for i, data := range [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF} {
			if goMono.fonts[i], goMonoErr = opentype.Parse(data); goMonoErr != nil {
				return
			}
		}
	})
	return goMono, goMonoErr
}

// parsedFonts caches font files by "path#index".
var parsedFonts sync.Map

// loadFont parses a font file; index picks the font in a collection (.ttc).
func loadFont(path string, index int) (*sfnt.Font, error) {
	key := path + "#" + strconv.Itoa(index)
	if f, ok := parsedFonts.Load(key); ok {
		return f.(*sfnt.Font), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f *sfnt.Font
	if bytes.HasPrefix(data, []byte("ttcf")) {
		c, err := opentype.ParseCollection(data)
		if err != nil {
			return nil, fmt.Errorf("font %s: %w", path, err)
		}
		if f, err = c.Font(index); err != nil {
			return nil, fmt.Errorf("font %s: %w", path, err)
		}
	} else if f, err = opentype.Parse(data); err != nil {
		return nil, fmt.Errorf("font %s: %w", path, err)
	}
	parsedFonts.Store(key, f)
	return f, nil
}

// fileFamily loads a font file as the regular style and looks for its bold and
// italic files next to it (JetBrainsMono-Regular.ttf -> JetBrainsMono-Bold.ttf).
func fileFamily(path string) (*fontFamily, error) {
	regular, err := loadFont(path, 0)
	if err != nil {
		return nil, err
	}
	fam := &fontFamily{name: filepath.Base(path)}
	fam.fonts[0] = regular
	for style, names := range map[int][]string{
		1: {"Bold"},
		2: {"Italic", "Oblique"},
		3: {"BoldItalic", "BoldOblique", "Bold-Italic", "Bold Italic"},
	} {
		for _, p := range styleSiblings(path, names) {
			if f, err := loadFont(p, 0); err == nil {
				fam.fonts[style] = f
				break
			}
		}
	}
	return fam, nil
}

// styleSiblings guesses the file names of other styles of the font at path.
func styleSiblings(path string, names []string) []string {
	dir, base := filepath.Split(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var out []string
	for _, n := range names {
		if strings.Contains(stem, "Regular") {
			out = append(out, dir+strings.Replace(stem, "Regular", n, 1)+ext)
		}
		out = append(out, dir+stem+"-"+n+ext)
	}
	return out
}

// systemFallback finds installed fonts for characters the font chain lacks.
var systemFallback = struct {
	sync.Mutex
	byRune map[rune]*fontFamily // nil value: nothing found
}{byRune: map[rune]*fontFamily{}}

// systemFamilyFor asks fontconfig for an installed font that has ch.
func systemFamilyFor(ch rune) *fontFamily {
	systemFallback.Lock()
	defer systemFallback.Unlock()
	if f, ok := systemFallback.byRune[ch]; ok {
		return f
	}
	var found *fontFamily
	for _, c := range fontconfigCandidates(ch) {
		f, err := loadFont(c.path, c.index)
		if err != nil {
			continue
		}
		fam := &fontFamily{name: filepath.Base(c.path), fonts: [4]*sfnt.Font{f}}
		if fam.has(ch) && hasOutline(f, ch) {
			found = fam
			break
		}
	}
	systemFallback.byRune[ch] = found
	return found
}

type fontFile struct {
	path  string
	index int
}

// fontconfigCandidates lists font files that may have ch, best first: a monospace
// match, then any font fontconfig knows to contain it. Without fontconfig there are none.
func fontconfigCandidates(ch rune) []fontFile {
	var out []fontFile
	pattern := fmt.Sprintf(":charset=%x", ch)
	if exe, err := exec.LookPath("fc-match"); err == nil {
		if b, err := exec.Command(exe, "--format=%{file}|%{index}\n", "monospace"+pattern).Output(); err == nil {
			out = append(out, parseFontList(b)...)
		}
	}
	if exe, err := exec.LookPath("fc-list"); err == nil {
		if b, err := exec.Command(exe, "--format=%{file}|%{index}\n", pattern).Output(); err == nil {
			list := parseFontList(b)
			if len(list) > 8 {
				list = list[:8]
			}
			out = append(out, list...)
		}
	}
	return out
}

func parseFontList(b []byte) []fontFile {
	var out []fontFile
	for line := range strings.Lines(string(b)) {
		path, idx, _ := strings.Cut(strings.TrimSpace(line), "|")
		if path == "" {
			continue
		}
		i, _ := strconv.Atoi(idx)
		out = append(out, fontFile{path, i})
	}
	return out
}

// faceCache creates font faces at the sizes a rasterizer needs.
type faceCache struct {
	faces map[faceKey]font.Face
}

type faceKey struct {
	f    *sfnt.Font
	size float64
}

func (c *faceCache) face(f *sfnt.Font, size float64) (font.Face, error) {
	k := faceKey{f, size}
	if fc, ok := c.faces[k]; ok {
		return fc, nil
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	if c.faces == nil {
		c.faces = map[faceKey]font.Face{}
	}
	c.faces[k] = fc
	return fc, nil
}
