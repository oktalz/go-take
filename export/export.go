package export

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/oktalz/go-take/cast"
)

// Format describes an export format.
type Format struct {
	Name        string // identifier used by the CLI and the editor
	Label       string
	Ext         string
	ContentType string
	Still       bool // a single frame, taken at Options.At
}

// Formats lists every export format.
var Formats = []Format{
	{Name: "gif", Label: "Animated GIF", Ext: ".gif", ContentType: "image/gif"},
	{Name: "svg", Label: "Animated SVG", Ext: ".svg", ContentType: "image/svg+xml"},
	{
		Name: "html", Label: "HTML page (self-contained)", Ext: ".html",
		ContentType: "text/html; charset=utf-8",
	},
	{Name: "png", Label: "PNG image (one frame)", Ext: ".png", ContentType: "image/png", Still: true},
	{Name: "svg-frame", Label: "SVG image (one frame)", Ext: ".svg", ContentType: "image/svg+xml", Still: true},
	{Name: "mp4", Label: "MP4 video (needs ffmpeg)", Ext: ".mp4", ContentType: "video/mp4"},
	{Name: "webm", Label: "WebM video (needs ffmpeg)", Ext: ".webm", ContentType: "video/webm"},
	{Name: "txt", Label: "Text transcript", Ext: ".txt", ContentType: "text/plain; charset=utf-8"},
	{
		Name: "screen", Label: "Screen text (one frame)", Ext: ".txt",
		ContentType: "text/plain; charset=utf-8", Still: true,
	},
	{Name: "take", Label: "gotake recording", Ext: cast.TakeExt, ContentType: "text/plain; charset=utf-8"},
	{Name: "cast", Label: "asciicast v3", Ext: ".cast", ContentType: "application/x-asciicast"},
	{Name: "v2", Label: "asciicast v2", Ext: ".cast", ContentType: "application/x-asciicast"},
}

// Lookup finds a format by name.
func Lookup(name string) (Format, error) {
	for _, f := range Formats {
		if f.Name == name {
			return f, nil
		}
	}
	names := make([]string, len(Formats))
	for i, f := range Formats {
		names[i] = f.Name
	}
	return Format{}, fmt.Errorf("unknown format %q (use one of: %s)", name, strings.Join(names, ", "))
}

// ForPath guesses the format from a file extension (first match wins: .svg is animated, .cast is v3).
func ForPath(path string) (Format, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, f := range Formats {
		if f.Ext == ext {
			return f, true
		}
	}
	return Format{}, false
}

// Write exports c in the given format.
func Write(w io.Writer, f Format, c *cast.Cast, o Options, h HTMLOptions) error {
	switch f.Name {
	case "gif":
		return GIF(w, c, o)
	case "svg":
		return SVG(w, c, o)
	case "svg-frame":
		return SVGFrame(w, c, o)
	case "png":
		return PNG(w, c, o)
	case "html":
		return HTML(w, c, h)
	case "mp4", "webm":
		return Video(w, c, o, f.Name)
	case "txt":
		return Transcript(w, c)
	case "screen":
		return ScreenText(w, c, o)
	case "take":
		return c.Write(w, cast.FormatTake)
	case "cast":
		return c.Write(w, cast.FormatAsciicast)
	case "v2":
		return c.WriteV2(w)
	default:
		return fmt.Errorf("unknown format %q", f.Name)
	}
}
