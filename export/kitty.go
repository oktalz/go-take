package export

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
	"io"
	"strconv"
	"strings"
)

// Kitty graphics protocol: https://sw.kovidgoyal.net/kitty/graphics-protocol/
//
// Images are sent in APC sequences (ESC _ G keys ; base64 ESC \). They are shown
// either at the cursor (a direct placement) or through Unicode placeholders: text
// cells holding U+10EEEE plus combining marks for the row and column of the image
// piece, with the image id as the foreground color (a virtual placement).
//
// vt10x knows neither, so both become cells it can keep: each image cell is a
// rune in Supplementary Private Use Area-B holding the piece's row and column,
// colored with an id that names the placement. The cells then scroll, get
// overwritten and cleared like any text, which is how Kitty treats images too.

const (
	kittyPlaceholder = '\U0010EEEE'
	imageCellBase    = 0x100000 // image cell rune: base + row<<8 + col
	imageCellMax     = 254      // largest row and column an image cell can hold
	// Image size in cells when a placement does not give one: Kitty uses its own
	// cell size in pixels, which the recording does not know.
	assumedCellW, assumedCellH = 8, 16
	maxImagePixels             = 64 << 20
	directIDs                  = 0xffffff // direct placement ids count down from here
	numberedIDs                = 1 << 31  // ids for images sent with a number only
)

var diacriticIndex = func() map[rune]int {
	m := make(map[rune]int, len(kittyDiacritics))
	for i, r := range kittyDiacritics {
		m[r] = i
	}
	return m
}()

// fit is how an image fills its placement rectangle.
type fit uint8

const (
	fitStretch fit = iota // fill the rectangle (Kitty's c and r both given)
	fitTop                // keep aspect, anchored top left
	fitCenter             // keep aspect, centered (Unicode placeholders)
)

// Placement is a Kitty graphics image shown over a rectangle of cells.
// A new Placement is made whenever an image is placed again, so screens can
// compare them by pointer.
type Placement struct {
	Image      *image.NRGBA // the source pixels, already cropped
	Cols, Rows int
	z          int
	imageID    uint32
	id         uint32 // placement id (p)
	fit        fit
	stay       bool // leave the cursor where it is (C=1)
}

// kittyScreen is the terminal that graphics commands act on.
type kittyScreen interface {
	// place draws a direct placement's cells at the cursor, colored with id.
	place(id uint32, p *Placement)
	// imageAt returns the color of the image cell at x, y.
	imageAt(x, y int) (id uint32, ok bool)
	cursor() (x, y int)
	size() (cols, rows int)
}

// imageCell returns the rune that stands for the piece at row, col of an image.
func imageCell(row, col int) rune { return rune(imageCellBase + row<<8 + col) }

// imageCellPos reports whether r is an image cell, and which piece it shows.
func imageCellPos(r rune) (row, col int, ok bool) {
	if r < imageCellBase || r > imageCell(imageCellMax, imageCellMax) {
		return 0, 0, false
	}
	v := int(r - imageCellBase)
	return v >> 8, v & 0xff, true
}

// kittyCmd holds the keys of one graphics command.
type kittyCmd struct {
	keys    map[byte]string
	payload string
}

func (c kittyCmd) char(k, def byte) byte {
	if v := c.keys[k]; v != "" {
		return v[0]
	}
	return def
}

func (c kittyCmd) num(k byte) int {
	n, _ := strconv.ParseInt(c.keys[k], 10, 64)
	return int(n)
}

func (c kittyCmd) id(k byte) uint32 {
	n, _ := strconv.ParseUint(c.keys[k], 10, 32)
	return uint32(n)
}

func parseKittyCmd(s string) kittyCmd {
	head, payload, _ := strings.Cut(s, ";")
	c := kittyCmd{keys: map[byte]string{}, payload: payload}
	for kv := range strings.SplitSeq(head, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && len(k) == 1 {
			c.keys[k[0]] = v
		}
	}
	return c
}

// kitty is the graphics state of one terminal.
type kitty struct {
	images  map[uint32]*image.NRGBA
	numbers map[uint32]uint32     // image number (I) -> image id
	virtual map[uint32]*Placement // image id -> its Unicode placeholder placement
	direct  map[uint32]*Placement // cell color -> direct placement
	chunk   *kittyCmd             // a transmission still receiving chunks
	data    strings.Builder       // its base64 payload so far
	nextDir uint32
	nextNum uint32
	row     int // last placeholder piece, for marks left out
	col     int
}

func newKitty() *kitty {
	return &kitty{
		images: map[uint32]*image.NRGBA{}, numbers: map[uint32]uint32{},
		virtual: map[uint32]*Placement{}, direct: map[uint32]*Placement{},
		nextDir: directIDs, nextNum: numberedIDs, row: -1,
	}
}

// placement returns what the image cell with color id shows.
func (k *kitty) placement(id uint32) *Placement {
	if p := k.direct[id]; p != nil {
		return p
	}
	return k.virtual[id]
}

// placeholders rewrites Unicode placeholders to image cells.
func (k *kitty) placeholders(s string) string {
	if !strings.ContainsRune(s, kittyPlaceholder) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != kittyPlaceholder {
			_, _ = b.WriteRune(rs[i])
			continue
		}
		var marks []int
		for len(marks) < 3 && i+1 < len(rs) {
			n, ok := diacriticIndex[rs[i+1]]
			if !ok {
				break
			}
			marks = append(marks, n)
			i++
		}
		// missing marks continue the placeholder to the left
		row, col := k.row, k.col+1
		if len(marks) > 0 && marks[0] != k.row {
			row, col = marks[0], 0
		}
		if len(marks) > 1 {
			col = marks[1]
		}
		k.row, k.col = row, col
		if row < 0 || row > imageCellMax || col > imageCellMax {
			_ = b.WriteByte(' ')
			continue
		}
		_, _ = b.WriteRune(imageCell(row, col))
	}
	return b.String()
}

// command runs one graphics command on scr.
func (k *kitty) command(s string, scr kittyScreen) {
	c := parseKittyCmd(s)
	if k.chunk != nil { // continuation: only m (and q) count
		k.data.WriteString(c.payload)
		if c.char('m', '0') == '1' {
			return
		}
		c = *k.chunk
		c.payload = k.data.String()
		k.chunk = nil
		k.data.Reset()
	} else if c.char('m', '0') == '1' && strings.ContainsRune("tTf", rune(c.char('a', 't'))) {
		k.chunk = &c
		k.data.WriteString(c.payload)
		return
	}

	switch c.char('a', 't') {
	case 't':
		k.transmit(c)
	case 'T':
		if id := k.transmit(c); id != 0 {
			k.put(c, id, scr)
		}
	case 'p':
		id := c.id('i')
		if id == 0 {
			id = k.numbers[c.id('I')]
		}
		k.put(c, id, scr)
	case 'd':
		k.delete(c, scr)
	default: // queries and animation frames are not shown
	}
}

// transmit decodes and stores an image, returning its id (0 when it cannot be shown).
func (k *kitty) transmit(c kittyCmd) uint32 {
	if c.char('t', 'd') != 'd' { // files and shared memory are not in the recording
		return 0
	}
	img := decodeKittyImage(c)
	if img == nil {
		return 0
	}
	id := c.id('i')
	if id == 0 {
		id = k.nextNum
		k.nextNum++
		if n := c.id('I'); n != 0 {
			k.numbers[n] = id
		}
	}
	k.images[id] = img
	return id
}

func decodeKittyImage(c kittyCmd) *image.NRGBA {
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(c.payload, "="))
	if err != nil {
		return nil
	}
	if c.char('o', 0) == 'z' {
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		data, err = io.ReadAll(io.LimitReader(zr, maxImagePixels*4+1))
		if err != nil {
			return nil
		}
	}
	w, h := c.num('s'), c.num('v')
	switch f := c.num('f'); f {
	case 100:
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width*cfg.Height > maxImagePixels {
			return nil
		}
		src, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		img := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
		draw.Draw(img, img.Rect, src, src.Bounds().Min, draw.Src)
		return img
	case 0, 24, 32:
		bpp := 4
		if f == 24 {
			bpp = 3
		}
		if w <= 0 || h <= 0 || w*h > maxImagePixels || len(data) < w*h*bpp {
			return nil
		}
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		if bpp == 4 {
			copy(img.Pix, data)
			return img
		}
		for i := range w * h {
			copy(img.Pix[i*4:], data[i*3:i*3+3])
			img.Pix[i*4+3] = 255
		}
		return img
	}
	return nil
}

// put places image id, at the cursor or (U=1) for Unicode placeholders.
func (k *kitty) put(c kittyCmd, id uint32, scr kittyScreen) {
	img := k.images[id]
	if img == nil {
		return
	}
	src := image.Rect(c.num('x'), c.num('y'), c.num('x')+c.num('w'), c.num('y')+c.num('h'))
	if c.num('w') <= 0 {
		src.Max.X = img.Rect.Max.X
	}
	if c.num('h') <= 0 {
		src.Max.Y = img.Rect.Max.Y
	}
	src = src.Intersect(img.Rect)
	if src.Empty() {
		return
	}
	if sub, ok := img.SubImage(src).(*image.NRGBA); ok {
		img = sub
	}
	p := &Placement{
		Image: img, Cols: c.num('c'), Rows: c.num('r'),
		imageID: id, id: c.id('p'), z: c.num('z'), stay: c.num('C') == 1,
	}
	w, h := src.Dx(), src.Dy()
	switch {
	case p.Cols > 0 && p.Rows > 0:
	case p.Cols > 0:
		p.Rows = ceilDiv(p.Cols*assumedCellW*h, w*assumedCellH)
	case p.Rows > 0:
		p.Cols = ceilDiv(p.Rows*assumedCellH*w, h*assumedCellW)
	default:
		p.Cols, p.Rows = ceilDiv(w, assumedCellW), ceilDiv(h, assumedCellH)
	}
	p.Cols, p.Rows = min(max(p.Cols, 1), imageCellMax+1), min(max(p.Rows, 1), imageCellMax+1)

	if c.num('U') == 1 {
		p.fit = fitCenter
		k.virtual[id] = p
		return
	}
	if c.num('c') == 0 || c.num('r') == 0 {
		p.fit = fitTop
	}
	color := k.nextDir
	k.nextDir--
	k.direct[color] = p
	scr.place(color, p)
}

// delete removes placements (lowercase d) and also the image data (uppercase d).
func (k *kitty) delete(c kittyCmd, scr kittyScreen) {
	d := c.char('d', 'a')
	removed := k.deletePlacements(c, d|0x20, scr)
	if d < 'A' || d > 'Z' {
		return
	}
	for _, p := range removed {
		delete(k.images, p.imageID)
	}
	switch d {
	case 'I':
		delete(k.images, c.id('i'))
	case 'N':
		delete(k.images, k.numbers[c.id('I')])
	default:
	}
}

// deletePlacements removes the placements a delete command (d in lowercase)
// selects and returns them.
func (k *kitty) deletePlacements(c kittyCmd, d byte, scr kittyScreen) []*Placement {
	pid := c.id('p')
	byImage := func(id uint32) func(*Placement) bool {
		return func(p *Placement) bool { return p.imageID == id && (pid == 0 || p.id == pid) }
	}
	var match func(*Placement) bool
	switch d {
	case 'a':
		match = func(*Placement) bool { return true }
	case 'i':
		match = byImage(c.id('i'))
	case 'n':
		match = byImage(k.numbers[c.id('I')])
	case 'r':
		lo, hi := c.id('x'), c.id('y')
		match = func(p *Placement) bool { return p.imageID >= lo && p.imageID <= hi }
	case 'z':
		z := c.num('z')
		match = func(p *Placement) bool { return p.z == z }
	case 'c', 'p', 'q', 'x', 'y':
		return k.deleteAt(c, d, scr)
	default:
		return nil
	}
	var removed []*Placement
	for color, p := range k.direct {
		if match(p) {
			delete(k.direct, color)
			removed = append(removed, p)
		}
	}
	if d != 'a' { // deleting all visible placements leaves the virtual ones
		for id, p := range k.virtual {
			if match(p) {
				delete(k.virtual, id)
				removed = append(removed, p)
			}
		}
	}
	return removed
}

// deleteAt removes the direct placements under the cursor, a cell, a column or a row.
func (k *kitty) deleteAt(c kittyCmd, d byte, scr kittyScreen) []*Placement {
	cols, rows := scr.size()
	var cells [][2]int
	switch d {
	case 'c':
		x, y := scr.cursor()
		cells = [][2]int{{x, y}}
	case 'p', 'q':
		cells = [][2]int{{c.num('x') - 1, c.num('y') - 1}}
	case 'x':
		for y := range rows {
			cells = append(cells, [2]int{c.num('x') - 1, y})
		}
	case 'y':
		for x := range cols {
			cells = append(cells, [2]int{x, c.num('y') - 1})
		}
	default:
	}
	var removed []*Placement
	for _, xy := range cells {
		if xy[0] < 0 || xy[1] < 0 || xy[0] >= cols || xy[1] >= rows {
			continue
		}
		color, ok := scr.imageAt(xy[0], xy[1])
		p := k.direct[color]
		if !ok || p == nil || (d == 'q' && p.z != c.num('z')) {
			continue
		}
		delete(k.direct, color)
		removed = append(removed, p)
	}
	return removed
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }
