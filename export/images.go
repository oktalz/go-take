package export

import (
	"cmp"
	"image"
	"image/color"
	"slices"

	"golang.org/x/image/draw"
)

// placements returns the Kitty graphics placements shown on the screens, in order of appearance.
func placements(screens []*Screen) []*Placement {
	var ps []*Placement
	seen := map[*Placement]bool{}
	for _, s := range screens {
		for _, c := range s.Cells {
			if c.Image != nil && !seen[c.Image] {
				seen[c.Image] = true
				ps = append(ps, c.Image)
			}
		}
	}
	return ps
}

// fitRect returns where an image of size w x h goes in a box of bw x bh pixels.
func (p *Placement) fitRect(bw, bh int) image.Rectangle {
	w, h := p.Image.Rect.Dx(), p.Image.Rect.Dy()
	if p.fit == fitStretch {
		return image.Rect(0, 0, bw, bh)
	}
	// scale to fit, keeping aspect ratio
	sw, sh := bw, h*bw/w
	if sh > bh {
		sw, sh = w*bh/h, bh
	}
	sw, sh = max(sw, 1), max(sh, 1)
	if p.fit == fitCenter {
		x, y := (bw-sw)/2, (bh-sh)/2
		return image.Rect(x, y, x+sw, y+sh)
	}
	return image.Rect(0, 0, sw, sh)
}

// scale draws the placement's image into its rectangle of cols*cw x rows*ch pixels.
func (p *Placement) scale(cw, ch int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, p.Cols*cw, p.Rows*ch))
	draw.CatmullRom.Scale(out, p.fitRect(out.Rect.Dx(), out.Rect.Dy()), p.Image, p.Image.Rect, draw.Src, nil)
	return out
}

// colorBin is a 5 bit per channel histogram bin.
type colorBin struct {
	c     [3]uint8
	count int
}

// quantize picks up to n colors that represent the images well (median cut
// over a 5 bit per channel histogram), for images composed over bg.
func quantize(imgs []*image.NRGBA, bg color.RGBA, n int) []color.RGBA {
	if n <= 0 {
		return nil
	}
	boxes := [][]colorBin{histogram(imgs, bg)}
	if len(boxes[0]) == 0 {
		return nil
	}
	for len(boxes) < n {
		best := largestBox(boxes)
		if best < 0 {
			break
		}
		a, b := splitBox(boxes[best])
		boxes[best] = a
		boxes = append(boxes, b)
	}
	colors := make([]color.RGBA, 0, len(boxes))
	for _, box := range boxes {
		var sum [3]int
		cnt := 0
		for _, x := range box {
			for ch := range 3 {
				sum[ch] += int(x.c[ch]) * x.count
			}
			cnt += x.count
		}
		// back from 5 to 8 bits, at the middle of the bin
		mid := func(ch int) uint8 { return uint8(sum[ch]*8/cnt + 4) }
		colors = append(colors, color.RGBA{mid(0), mid(1), mid(2), 255})
	}
	return colors
}

func histogram(imgs []*image.NRGBA, bg color.RGBA) []colorBin {
	var hist [1 << 15]int
	for _, img := range imgs {
		for i := 0; i < len(img.Pix); i += 4 {
			c := over(img.Pix[i:i+4], bg)
			hist[int(c.R>>3)<<10|int(c.G>>3)<<5|int(c.B>>3)]++
		}
	}
	var bins []colorBin
	for i, cnt := range hist {
		if cnt > 0 {
			bins = append(bins, colorBin{[3]uint8{uint8(i >> 10), uint8(i >> 5 & 31), uint8(i & 31)}, cnt})
		}
	}
	return bins
}

// widest returns the channel with the largest range in box, and that range.
func widest(box []colorBin) (ch, span int) {
	for c := range 3 {
		lo, hi := uint8(31), uint8(0)
		for _, b := range box {
			lo, hi = min(lo, b.c[c]), max(hi, b.c[c])
		}
		if int(hi-lo) > span {
			ch, span = c, int(hi-lo)
		}
	}
	return ch, span
}

// largestBox returns the box with the most pixels that can still be split, or -1.
func largestBox(boxes [][]colorBin) int {
	best, bestCount := -1, 0
	for i, box := range boxes {
		if _, span := widest(box); span == 0 {
			continue
		}
		cnt := 0
		for _, b := range box {
			cnt += b.count
		}
		if cnt > bestCount {
			best, bestCount = i, cnt
		}
	}
	return best
}

// splitBox splits a box at the pixel median of its widest channel.
func splitBox(box []colorBin) (a, b []colorBin) {
	ch, _ := widest(box)
	slices.SortFunc(box, func(a, b colorBin) int { return cmp.Compare(a.c[ch], b.c[ch]) })
	total := 0
	for _, x := range box {
		total += x.count
	}
	half, acc := 1, 0
	for i, x := range box[:len(box)-1] {
		acc += x.count
		if acc >= total/2 {
			half = i + 1
			break
		}
	}
	return box[:half], box[half:]
}

// over composes a straight-alpha RGBA pixel over bg.
func over(px []uint8, bg color.RGBA) color.RGBA {
	a := int(px[3])
	mix := func(c, b uint8) uint8 { return uint8((int(c)*a + int(b)*(255-a) + 127) / 255) }
	return color.RGBA{mix(px[0], bg.R), mix(px[1], bg.G), mix(px[2], bg.B), 255}
}
