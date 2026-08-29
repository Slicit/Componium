// Package studio serves the timeline editor.
//
// It is a single HTML page with no build step, no bundler and no node_modules.
// That is a deliberate departure from the React plan in LOGBOOK.md: the editor
// is a few hundred lines of DOM and SVG, and the cost of a JavaScript
// toolchain would fall on every contributor who only wanted to fix a cue time.
// If the editor ever outgrows this, the toolchain can be added then.
//
// The server holds one score file. It reads it on start, serves it as JSON,
// and writes it back through the same parser and writer the CLI uses, so the
// studio cannot produce a score that `componium play` will not accept.
package studio

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/score"
)

//go:embed assets
var assets embed.FS

// Server edits one score file.
type Server struct {
	mu   sync.Mutex
	path string
	sc   *score.Score
}

// New loads a score and prepares the editor.
func New(path string) (*Server, error) {
	sc, err := score.Load(path)
	if err != nil {
		return nil, err
	}
	return &Server{path: path, sc: sc}, nil
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/score", s.handleScore)
	return mux
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
	T      float64            `json:"t"`
	Action string             `json:"action"`
	Params map[string]float64 `json:"params,omitempty"`
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
		// studio must never be able to write a score that play would refuse.
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
			"saved": s.path,
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
			})
		}
		for _, p := range t.Points {
			tr.Points = append(tr.Points, score.Point{
				T: score.Timecode(seconds(p.T)), Value: p.Value,
			})
		}
		// Preserve interpolation, which the editor does not expose yet.
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
