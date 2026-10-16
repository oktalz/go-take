// Package web serves the embeddable player and the recording editor.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/oktalz/go-take/cast"
	"github.com/oktalz/go-take/export"
)

//go:embed static
var staticFS embed.FS

// Static returns the embedded static files (gotake-player.js, editor assets).
func Static() fs.FS {
	sub, _ := fs.Sub(staticFS, "static")
	return sub
}

// PlayerJS returns the embeddable player script.
func PlayerJS() []byte {
	b, _ := fs.ReadFile(Static(), "gotake-player.js")
	return b
}

// ---------------------------------------------------------------- editor

const maxHistory = 300

// Editor holds a recording being edited, with undo/redo history.
type Editor struct {
	cast    *cast.Cast
	path    string
	undo    []*cast.Cast
	redo    []*cast.Cast
	version int // incremented on every change
	saved   int // version at last save
	mu      sync.Mutex
}

// NewEditor loads path for editing. A missing file starts an empty recording.
func NewEditor(path string) (*Editor, error) {
	c, err := cast.Load(path)
	if os.IsNotExist(err) {
		c, err = cast.New(80, 24), nil
	}
	if err != nil {
		return nil, err
	}
	return &Editor{path: path, cast: c}, nil
}

type state struct {
	Path     string       `json:"path"`
	Message  string       `json:"message,omitempty"`
	Events   []cast.Event `json:"events"`
	Header   cast.Header  `json:"header"`
	Version  int          `json:"version"`
	Duration float64      `json:"duration"`
	Dirty    bool         `json:"dirty"`
	CanUndo  bool         `json:"can_undo"`
	CanRedo  bool         `json:"can_redo"`
}

func (ed *Editor) state(msg string) state {
	ev := ed.cast.Events
	if ev == nil {
		ev = []cast.Event{}
	}
	return state{
		Path: ed.path, Header: ed.cast.Header, Events: ev, Version: ed.version,
		Dirty: ed.version != ed.saved, CanUndo: len(ed.undo) > 0, CanRedo: len(ed.redo) > 0,
		Duration: ed.cast.Duration(), Message: msg,
	}
}

// OpRequest is a single edit operation sent by the editor UI.
type OpRequest struct {
	Event   *cast.Event          `json:"event"`
	Inject  *cast.InjectOptions  `json:"inject"`
	Replace *cast.ReplaceOptions `json:"replace"`
	Header  *cast.Header         `json:"header"`
	Op      string               `json:"op"`
	Label   string               `json:"label"`
	Events  []cast.Event         `json:"events"`
	From    int                  `json:"from"`
	To      int                  `json:"to"`
	At      int                  `json:"at"`
	Factor  float64              `json:"factor"`
	Seconds float64              `json:"seconds"`
	Jitter  float64              `json:"jitter"`
}

// Apply runs op against the recording. Exported so it can be tested and reused.
func Apply(c *cast.Cast, r OpRequest) (string, error) {
	switch r.Op {
	case "delete":
		return fmt.Sprintf("deleted %d events", r.To-r.From), c.Delete(r.From, r.To)
	case "stretch":
		return "", c.Stretch(r.From, r.To, r.Factor)
	case "stretch_to":
		return "", c.StretchTo(r.From, r.To, r.Seconds)
	case "set_delay":
		return "", c.SetDelay(r.From, r.To, r.Seconds)
	case "cap_idle":
		return "", c.CapIdle(r.From, r.To, r.Seconds)
	case "pause":
		c.Pause(r.At, r.Seconds)
		return "", nil
	case "typing":
		opts := cast.TypingOptions{Delay: r.Seconds, Jitter: r.Jitter, Seed: uint64(c.Duration() * 1000)}
		n, err := c.NormalizeTyping(r.From, r.To, opts)
		return fmt.Sprintf("adjusted %d keystrokes", n), err
	case "inject":
		if r.Inject == nil {
			return "", errors.New("inject: missing options")
		}
		n := c.InjectCommand(r.At, *r.Inject)
		return fmt.Sprintf("inserted %d events", n), nil
	case "insert":
		evs := r.Events
		if r.Event != nil {
			evs = append(evs, *r.Event)
		}
		c.Insert(r.At, evs...)
		return "", nil
	case "update":
		if r.Event == nil {
			return "", errors.New("update: missing event")
		}
		return "", c.Update(r.At, *r.Event)
	case "duplicate":
		return "", c.Duplicate(r.From, r.To)
	case "move":
		return "", c.Move(r.From, r.To, r.At)
	case "merge":
		return "", c.Merge(r.From, r.To)
	case "split":
		return "", c.Split(r.From, r.To)
	case "marker":
		c.Insert(r.At, cast.Event{Code: cast.Marker, Data: r.Label})
		return "", nil
	case "trim_exit":
		if n := c.TrimExit(); n > 0 {
			return fmt.Sprintf("removed the trailing exit (%d events)", n), nil
		}
		return "", errors.New("no exit typed at a prompt at the end of the recording")
	case "replace":
		if r.Replace == nil {
			return "", errors.New("replace: missing options")
		}
		m, ev, err := c.Replace(r.From, r.To, *r.Replace)
		return fmt.Sprintf("replaced %d matches in %d events", m, ev), err
	case "header":
		if r.Header == nil {
			return "", errors.New("header: missing header")
		}
		h := *r.Header
		h.Version = 3
		if h.Term.Cols <= 0 || h.Term.Rows <= 0 {
			return "", errors.New("terminal size must be positive")
		}
		c.Header = h
		return "", nil
	}
	return "", fmt.Errorf("unknown op %q", r.Op)
}

// Handler returns the HTTP handler for the editor UI and its API.
func (ed *Editor) Handler() http.Handler {
	mux := http.NewServeMux()
	static := http.FileServerFS(Static())
	mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, Static(), "editor.html")
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		writeJSON(w, ed.state(""))
	})
	mux.HandleFunc("GET /recording", func(w http.ResponseWriter, _ *http.Request) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		f := cast.FormatFor(ed.path)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if f == cast.FormatAsciicast {
			w.Header().Set("Content-Type", "application/x-asciicast")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(ed.path)))
		_ = ed.cast.Write(w, f)
	})
	mux.HandleFunc("POST /api/op", func(w http.ResponseWriter, r *http.Request) {
		var req OpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err)
			return
		}
		ed.mu.Lock()
		defer ed.mu.Unlock()
		next := ed.cast.Clone()
		msg, err := Apply(next, req)
		if err != nil {
			httpError(w, err)
			return
		}
		ed.push(next)
		writeJSON(w, ed.state(msg))
	})
	mux.HandleFunc("POST /api/find", ed.handleFind)
	mux.HandleFunc("GET /api/export", ed.handleExport)
	mux.HandleFunc("POST /api/undo", func(w http.ResponseWriter, _ *http.Request) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		if n := len(ed.undo); n > 0 {
			ed.redo = append(ed.redo, ed.cast)
			ed.cast = ed.undo[n-1]
			ed.undo = ed.undo[:n-1]
			ed.version++
		}
		writeJSON(w, ed.state(""))
	})
	mux.HandleFunc("POST /api/redo", func(w http.ResponseWriter, _ *http.Request) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		if n := len(ed.redo); n > 0 {
			ed.undo = append(ed.undo, ed.cast)
			ed.cast = ed.redo[n-1]
			ed.redo = ed.redo[:n-1]
			ed.version++
		}
		writeJSON(w, ed.state(""))
	})
	mux.HandleFunc("POST /api/save", func(w http.ResponseWriter, _ *http.Request) {
		ed.mu.Lock()
		defer ed.mu.Unlock()
		if err := ed.cast.Save(ed.path); err != nil {
			httpError(w, err)
			return
		}
		ed.saved = ed.version
		writeJSON(w, ed.state("saved "+ed.path))
	})
	return mux
}

// handleFind reports which events match, without changing the recording.
func (ed *Editor) handleFind(w http.ResponseWriter, r *http.Request) {
	var req OpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err)
		return
	}
	if req.Replace == nil {
		httpError(w, errors.New("find: missing options"))
		return
	}
	ed.mu.Lock()
	events, n, err := ed.cast.Find(req.From, req.To, *req.Replace)
	ed.mu.Unlock()
	if err != nil {
		httpError(w, err)
		return
	}
	if events == nil {
		events = []int{}
	}
	writeJSON(w, map[string]any{"events": events, "matches": n})
}

// handleExport renders the recording (or a range of it) in the requested format.
// Query: format, from/to (event indices), at (recording time for still frames),
// speed, idle, fps, font_size, window, cursor, title, autoplay, loop.
func (ed *Editor) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := export.Lookup(q.Get("format"))
	if err != nil {
		httpError(w, err)
		return
	}
	ed.mu.Lock()
	c := ed.cast.Clone()
	name := strings.TrimSuffix(filepath.Base(ed.path), filepath.Ext(ed.path))
	ed.mu.Unlock()

	o := export.Options{
		Speed: floatParam(q, "speed", 1), IdleTimeLimit: floatParam(q, "idle", 0), FPS: floatParam(q, "fps", 30),
		FontSize: floatParam(q, "font_size", 16), At: floatParam(q, "at", -1),
		Window: q.Get("window") == "1", HideCursor: q.Get("cursor") == "0",
	}
	suffix := ""
	if q.Has("from") || q.Has("to") {
		from, to := int(floatParam(q, "from", 0)), int(floatParam(q, "to", float64(len(c.Events))))
		if from > 0 && from < len(c.Events) && o.At >= 0 {
			o.At -= c.Times()[from] - c.Events[from].Delay // section time starts where it begins
		}
		if c, err = c.Section(from, to); err != nil {
			httpError(w, err)
			return
		}
		suffix = "-selection"
	}
	if f.Still {
		suffix += "-frame"
	}
	if f.Ext == ".cast" {
		suffix += "-" + f.Name
	}
	h := export.HTMLOptions{
		Title: q.Get("title"), PlayerJS: PlayerJS(),
		Autoplay: q.Get("autoplay") == "1", Loop: q.Get("loop") == "1",
	}
	var buf bytes.Buffer
	if err := export.Write(&buf, f, c, o, h); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+suffix+f.Ext))
	_, _ = w.Write(buf.Bytes())
}

func floatParam(q url.Values, key string, def float64) float64 {
	v, err := strconv.ParseFloat(q.Get(key), 64)
	if err != nil {
		return def
	}
	return v
}

func (ed *Editor) push(next *cast.Cast) {
	ed.undo = append(ed.undo, ed.cast)
	if len(ed.undo) > maxHistory {
		ed.undo = ed.undo[len(ed.undo)-maxHistory:]
	}
	ed.redo = nil
	ed.cast = next
	ed.version++
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ---------------------------------------------------------------- gallery

//revive:disable:line-length-limit
var galleryTmpl = template.Must(template.New("gallery").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>gotake recordings</title>
<style>
:root{color-scheme:dark;--bg:#0d0e0f;--fg:#e6e6e6;--muted:#8b949e;--line:#24272b}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,sans-serif}
main{max-width:980px;margin:0 auto;padding:32px 16px}
h1{font-size:20px;margin:0 0 4px}
p.sub{color:var(--muted);margin:0 0 28px}
section{margin:0 0 40px}
h2{font:600 14px ui-monospace,monospace;margin:0 0 10px;color:var(--muted)}
code{background:#1a1c1f;padding:2px 6px;border-radius:4px;font-size:13px}
a{color:#e5a50a}
pre{background:#1a1c1f;padding:12px;border-radius:6px;overflow:auto;font-size:13px}
</style></head><body><main>
<h1>Recordings</h1>
<p class="sub">Served by <code>gotake serve</code>. Embed on any page:</p>
{{if .Pages}}<p class="sub">Pages: {{range $i, $p := .Pages}}{{if $i}}, {{end}}<a href="casts/{{$p}}">{{$p}}</a>{{end}}</p>{{end}}
<pre>&lt;script src="{{.Base}}/gotake-player.js"&gt;&lt;/script&gt;
&lt;div data-gotake-src="{{.Base}}/casts/{{if .Files}}{{index .Files 0}}{{else}}demo.take{{end}}" data-idle-time-limit="2"&gt;&lt;/div&gt;</pre>
{{range .Files}}<section><h2>{{.}}</h2><div data-gotake-src="casts/{{.}}" data-idle-time-limit="2"></div></section>
{{else}}<p>No .take or .cast files found.</p>{{end}}
</main><script src="gotake-player.js"></script></body></html>`))

//revive:enable:line-length-limit

// isRecording reports whether a file name has a recording extension.
func isRecording(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == cast.TakeExt || ext == ".cast"
}

// Gallery serves the player script, the .take and .cast files in dir and an index page
// that embeds a player for each of them.
func Gallery(dir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /gotake-player.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(PlayerJS())
	})
	casts := http.StripPrefix("/casts/", http.FileServer(http.Dir(dir)))
	mux.HandleFunc("GET /casts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		casts.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		var files, pages []string
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			switch {
			case isRecording(p):
				files = append(files, filepath.ToSlash(rel))
			case strings.EqualFold(filepath.Ext(p), ".html"):
				pages = append(pages, filepath.ToSlash(rel))
			}
			return nil
		})
		slices.Sort(files)
		slices.Sort(pages)
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = galleryTmpl.Execute(w, map[string]any{"Files": files, "Pages": pages, "Base": base})
	})
	return mux
}
