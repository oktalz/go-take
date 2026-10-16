package export

import (
	"bytes"
	"html/template"
	"io"
	"strings"

	"github.com/oktalz/go-take/cast"
)

// HTMLOptions configures the standalone HTML page.
type HTMLOptions struct {
	Title    string
	PlayerJS []byte // gotake-player.js, inlined into the page
	Autoplay bool
	Loop     bool
}

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: dark; }
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
         background: #0b0c0d; font: 14px system-ui, sans-serif; color: #c9ccd1; }
  main { width: min(100% - 32px, 1000px); padding: 24px 0; }
  h1 { font-size: 15px; font-weight: 600; margin: 0 0 12px; }
</style>
</head>
<body>
<main>
{{if .Title}}<h1>{{.Title}}</h1>{{end}}
<div id="player"></div>
</main>
<script type="application/x-asciicast" id="cast">{{.Cast}}</script>
<script>{{.Player}}</script>
<script>
GoTakePlayer.create('#player', document.getElementById('cast').textContent, {
  autoplay: {{.Autoplay}}, loop: {{.Loop}}, idleTimeLimit: {{.Idle}}
});
</script>
</body>
</html>
`))

// HTML writes a single self-contained page that plays the recording.
// The page loads xterm.js from a CDN; everything else is inlined.
func HTML(w io.Writer, c *cast.Cast, o HTMLOptions) error {
	var buf bytes.Buffer
	if err := c.Write(&buf, cast.FormatAsciicast); err != nil {
		return err
	}
	title := o.Title
	if title == "" {
		title = c.Header.Title
	}
	// "</" may appear inside JSON strings; "<\/" is the same JSON string and cannot end the script tag.
	castText := strings.ReplaceAll(buf.String(), "</", `<\/`)
	player := strings.ReplaceAll(string(o.PlayerJS), "</script", `<\/script`)
	idle := c.Header.IdleTimeLimit
	return pageTmpl.Execute(w, map[string]any{
		"Title":    title,
		"Cast":     template.HTML(castText), // inside a non-JS script element, "</" escaped above
		"Player":   template.JS(player),     // our own embedded player
		"Autoplay": o.Autoplay,
		"Loop":     o.Loop,
		"Idle":     idle,
	})
}
