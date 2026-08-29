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
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/score", s.handleScore)
	mux.HandleFunc("/api/rig", s.handleRig)
	mux.HandleFunc("/media", s.handleMedia)
	return mux
}

// handleMedia serves the film.
//
// http.ServeFile implements range requests, which is the whole game: without
// them a browser must download a two hour film before it can seek, and
// scrubbing a timeline is the entire point of previewing.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if s.media == "" {
		http.Error(w, "no media loaded; start the studio with -media", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, s.media)
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

	out := wireRig{Name: "no rig loaded", HasMedia: s.media != ""}
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
