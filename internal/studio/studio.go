// Package studio serves the authoring application.
//
// It is a single page with no build step, no bundler and no node_modules. That
// decision was made when the editor was a list of cues; it survives the
// addition of video and a 3D room because the room is CSS transforms rather
// than a 3D engine, and because the cost of a JavaScript toolchain falls on
// every contributor rather than on the one person who wanted it.
//
// The server holds one score file, optionally a rig and a film. It reads the
// score on start, serves it as JSON, and writes it back through the same
// parser and writer the CLI uses, so the studio cannot produce a score that
// `componium play` will not accept.
package studio

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/rig"
	"github.com/Slicit/componium/internal/score"
)

//go:embed assets
var assets embed.FS

// Server edits one score, and previews it against a rig and a film.
type Server struct {
	mu    sync.Mutex
	path  string
	sc    *score.Score
	rig   *rig.Config
	media string
}

// New loads a score, and optionally the rig and film to preview it against.
// Both may be empty: the editor works without them, it just has less to show.
func New(scorePath, rigPath, mediaPath string) (*Server, error) {
	sc, err := score.Load(scorePath)
	if err != nil {
		return nil, err
	}
	s := &Server{path: scorePath, sc: sc, media: mediaPath}

	if rigPath != "" {
		rc, err := rig.Load(rigPath)
		if err != nil {
			return nil, err
		}
		s.rig = rc
	}
	if mediaPath != "" {
		if _, err := os.Stat(mediaPath); err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
	}
	return s, nil
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	mux := http.NewServeMux()
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))
	mux.HandleFunc("/api/score", s.handleScore)
	mux.HandleFunc("/api/rig", s.handleRig)
	mux.HandleFunc("/media", s.handleMedia)
	mux.HandleFunc("/api/media", s.handleMediaList)
	return mux
}

// mediaFiles lists what can be previewed.
//
// The media path may be a single file or a directory of them. A directory
// is what makes the picker useful: point the studio at a folder of films
// and choose between them without restarting.
func (s *Server) mediaFiles() []mediaFile {
	if s.media == "" {
		return nil
	}
	info, err := os.Stat(s.media)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return []mediaFile{{Name: filepath.Base(s.media), Size: info.Size()}}
	}

	entries, err := os.ReadDir(s.media)
	if err != nil {
		return nil
	}
	var out []mediaFile
	for _, e := range entries {
		if e.IsDir() || !playable(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, mediaFile{Name: e.Name(), Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type mediaFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func playable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v", ".mkv", ".webm", ".mov", ".avi", ".ogv":
		return true
	}
	return false
}

func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mediaFiles())
}

// noCache stops a browser holding on to a stale page.
//
// The assets are embedded, and embed.FS reports a zero modification time, so
// Go cannot send Last-Modified and a browser has nothing to revalidate
// against. Left alone it caches heuristically and keeps showing an old
// build after an upgrade, which is a genuinely confusing way to lose an
// afternoon.
//
// This is a local authoring tool. Never caching costs nothing and removes
// the whole class of problem.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

// handleMedia serves one film.
//
// http.ServeFile implements range requests, which is the whole game: without
// them a browser must download a two hour film before it can seek, and
// scrubbing a timeline is the entire point of previewing.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	files := s.mediaFiles()
	if len(files) == 0 {
		http.Error(w, "no media loaded; start the studio with -media", http.StatusNotFound)
		return
	}

	info, err := os.Stat(s.media)
	if err == nil && !info.IsDir() {
		http.ServeFile(w, r, s.media)
		return
	}

	want := r.URL.Query().Get("file")
	if want == "" {
		want = files[0].Name
	}
	// Only ever serve a name that appeared in the listing. Comparing against
	// the listing rather than sanitising the input means path traversal is
	// not something that has to be got right, it is something that cannot be
	// expressed.
	for _, f := range files {
		if f.Name == want {
			http.ServeFile(w, r, filepath.Join(s.media, f.Name))
			return
		}
	}
	http.Error(w, "no such media", http.StatusNotFound)
}

// wireInstrument is what the room needs to draw a device.
type wireInstrument struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Driver   string     `json:"driver"`
	Latency  float64    `json:"latency"`
	Position [3]float64 `json:"position"`
}

type wireRig struct {
	Name        string           `json:"name"`
	HasMedia    bool             `json:"hasMedia"`
	Instruments []wireInstrument `json:"instruments"`
}

// defaultPosition places an instrument in the room when the rig does not say.
//
// Metres, origin at the centre of the screen wall: x right, y up, z toward the
// audience. The numbers describe a small home cinema, which is what this is
// for.
func defaultPosition(kind string) [3]float64 {
	switch kind {
	case "light":
		return [3]float64{0, 1.4, -0.1} // washing the wall behind the screen
	case "wind":
		return [3]float64{0, 1.6, 0.6} // in front, blowing back at the seats
	case "shake":
		return [3]float64{0, 0.35, 3.0} // under the seat
	case "motion":
		return [3]float64{0, 0.5, 3.0} // the seat itself
	case "mist":
		return [3]float64{0, 2.3, 2.2} // overhead
	case "fog":
		return [3]float64{-1.6, 0.15, 1.0} // low and to one side
	case "scent":
		return [3]float64{1.6, 1.1, 2.6}
	default:
		return [3]float64{0, 1.0, 1.5}
	}
}

func (s *Server) handleRig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := wireRig{Name: "no rig loaded", HasMedia: len(s.mediaFiles()) > 0}
	if s.rig == nil {
		// Invent one from the score, so the room still has something to draw.
		// A preview with no devices in it is not a preview.
		out.Name = "inferred from the score"
		for _, id := range s.sc.Instruments() {
			kind := id
			if i := indexByte(id, '.'); i > 0 {
				kind = id[:i]
			}
			out.Instruments = append(out.Instruments, wireInstrument{
				ID: id, Kind: kind, Driver: "unknown",
				Position: defaultPosition(kind),
			})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.Name = s.rig.Rig.Name
	for _, in := range s.rig.Instruments {
		pos := defaultPosition(in.Kind)
		if in.Position != nil {
			pos = [3]float64{in.Position.X, in.Position.Y, in.Position.Z}
		}
		out.Instruments = append(out.Instruments, wireInstrument{
			ID: in.ID, Kind: in.Kind, Driver: in.Driver,
			Latency: in.Latency.Duration().Seconds(), Position: pos,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// wireScore is the shape the page works with. It is flatter than the file
// format on purpose: the editor cares about tracks and points, not about how
// TOML nests them.
type wireScore struct {
	Title    string      `json:"title"`
	Duration float64     `json:"duration"`
	Path     string      `json:"path"`
	Tracks   []wireTrack `json:"tracks"`
}

type wireTrack struct {
	Instrument string      `json:"instrument"`
	Type       string      `json:"type"`
	Cues       []wireCue   `json:"cues,omitempty"`
	Points     []wirePoint `json:"points,omitempty"`
}

type wireCue struct {
	T        float64            `json:"t"`
	Action   string             `json:"action"`
	Params   map[string]float64 `json:"params,omitempty"`
	Duration float64            `json:"duration,omitempty"`
	Source   string             `json:"source,omitempty"`
}

type wirePoint struct {
	T     float64            `json:"t"`
	Value map[string]float64 `json:"value"`
}

func (s *Server) handleScore(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		out := toWire(s.sc, s.path)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)

	case http.MethodPut:
		var in wireScore
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()

		next := fromWire(&in, s.sc)
		// Round trip through the real parser before touching the file. The
		// studio must never be able to write a score play would refuse.
		b, err := next.Marshal()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		checked, err := score.Parse(b)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := checked.Save(s.path); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.sc = checked
		writeJSON(w, http.StatusOK, map[string]any{
			"saved": filepath.Base(s.path),
			"cues":  len(checked.Cues()),
		})

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func toWire(sc *score.Score, path string) wireScore {
	out := wireScore{
		Title:    sc.Meta.Title,
		Duration: sc.Meta.Media.Duration.Duration().Seconds(),
		Path:     path,
	}
	for _, t := range sc.Tracks {
		wt := wireTrack{Instrument: t.Instrument, Type: string(t.Type)}
		for _, c := range t.Cues {
			wt.Cues = append(wt.Cues, wireCue{
				T: c.T.Duration().Seconds(), Action: c.Action, Params: c.Params,
				Duration: c.Duration.Duration().Seconds(),
			})
		}
		for _, p := range t.Points {
			wt.Points = append(wt.Points, wirePoint{
				T: p.T.Duration().Seconds(), Value: p.Value,
			})
		}
		out.Tracks = append(out.Tracks, wt)
	}
	return out
}

// fromWire rebuilds a score, carrying over the metadata the editor does not
// touch. Losing a media hash because the page never displayed it would break
// the binding between a score and its film.
func fromWire(in *wireScore, prev *score.Score) *score.Score {
	out := &score.Score{Meta: prev.Meta}
	out.Meta.Title = in.Title
	for _, t := range in.Tracks {
		tr := score.Track{Instrument: t.Instrument, Type: score.TrackType(t.Type)}
		for _, c := range t.Cues {
			tr.Cues = append(tr.Cues, score.CueSpec{
				T:      score.Timecode(seconds(c.T)),
				Action: c.Action, Params: c.Params,
				Duration: score.Span(seconds(c.Duration)),
			})
		}
		for _, p := range t.Points {
			tr.Points = append(tr.Points, score.Point{
				T: score.Timecode(seconds(p.T)), Value: p.Value,
			})
		}
		// Preserve interpolation, which the editor does not expose.
		for _, old := range prev.Tracks {
			if old.Instrument == t.Instrument && old.Interpolation != "" {
				tr.Interpolation = old.Interpolation
			}
		}
		out.Tracks = append(out.Tracks, tr)
	}
	return out
}

func seconds(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Fprintf(w, `{"error":%q}`, err.Error())
	}
}
