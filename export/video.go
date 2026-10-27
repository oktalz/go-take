package export

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"github.com/oktalz/go-take/cast"
)

// videoCodecs are the ffmpeg arguments for each container: H.264 in MP4 (plays
// everywhere) and VP9 in WebM. yuv420p is what players expect.
var videoCodecs = map[string][]string{
	"mp4": {
		"-c:v", "libx264", "-preset", "medium", "-tune", "animation", "-crf", "20",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart",
	},
	"webm": {
		"-c:v", "libvpx-vp9", "-crf", "32", "-b:v", "0", "-deadline", "good", "-cpu-used", "4", "-row-mt", "1",
		"-pix_fmt", "yuv420p",
	},
}

// Video writes the recording as an MP4 or WebM video (container "mp4" or
// "webm"), encoded by ffmpeg, which must be installed. Frames are drawn in
// full color at Options.FPS; a transparent theme gets its background color.
func Video(w io.Writer, c *cast.Cast, o Options, container string) error {
	codec, ok := videoCodecs[container]
	if !ok {
		return fmt.Errorf("unknown video container %q", container)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("%s export needs ffmpeg (https://ffmpeg.org) in PATH", container)
	}
	o = o.withDefaults(c)
	if o.Theme.Transparent {
		t := *o.Theme
		t.Transparent = false
		o.Theme = &t
	}
	frames := Frames(c, o)
	cols, rows := maxSize(frames)
	r, err := newRasterizer(cols, rows, o)
	if err != nil {
		return err
	}
	r.fullColor = true
	size := r.size()
	// yuv420p needs even sizes
	canvas := image.NewRGBA(image.Rect(0, 0, size.Dx()+size.Dx()%2, size.Dy()+size.Dy()%2))

	tmp, err := os.CreateTemp("", "gotake-*."+container)
	if err != nil {
		return err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(path) }()

	fps := strconv.FormatFloat(o.FPS, 'f', -1, 64)
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "rgba", "-s", fmt.Sprintf("%dx%d", canvas.Rect.Dx(), canvas.Rect.Dy()),
		"-r", fps, "-i", "-",
	}
	args = append(append(args, codec...), "-r", fps, path)
	cmd := exec.Command(ffmpeg, args...) //nolint:gosec // ffmpeg from PATH with our own arguments
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	werr := r.writeVideoFrames(in, canvas, frames)
	err = errors.Join(werr, in.Close(), cmd.Wait())
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg: %s", msg)
		}
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(w, f)
	return err
}

// writeVideoFrames writes each frame as raw RGBA, repeated for as many video
// frames as it lasts.
func (r *rasterizer) writeVideoFrames(w io.Writer, canvas *image.RGBA, frames []Frame) error {
	draw.Draw(canvas, canvas.Rect, image.NewUniform(r.o.Theme.BG), image.Point{}, draw.Src)
	shown := 0
	for _, f := range frames {
		end := int(math.Round((f.Time + f.Duration) * r.o.FPS))
		if end <= shown {
			continue
		}
		r.prepare([]*Screen{f.Screen}) // a palette per frame keeps every color exact
		img := r.trueColor(r.render(f.Screen), f.Screen)
		draw.Draw(canvas, canvas.Rect.Intersect(r.size()), img, image.Point{}, draw.Src)
		for ; shown < end; shown++ {
			if _, err := w.Write(canvas.Pix); err != nil {
				return err
			}
		}
	}
	return nil
}
