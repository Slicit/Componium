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
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/rig"
	"github.com/Slicit/componium/internal/score"
)

//go:embed assets
var assets embed.FS

// Options configures a studio. Everything except Score is optional: the editor
// works without a rig, a film or a composer, it just has less to show and
// fewer things it can do.
type Options struct {
	// Score is the score to open. When Media is a directory and Scores is
	// set, the score follows whichever film is selected instead.
	Score string
	// Rig describes what is in the room, for the preview.
	Rig string
	// Media is a film, or a directory of them.
	Media string
	// Scores is where generated scores live, one per film.
	Scores string
	// Composer is the path to compose.py. Without it, analysis cannot run and
	// the library says so rather than offering a button that does nothing.
	Composer string
	// Firmware is a directory of node images the admin page can flash to a
	// board over USB. Empty is the ordinary case: the image belongs to a
	// different toolchain on a different release schedule, so it is a
	// directory on disk rather than something built into this binary.
	Firmware string
}

// Server edits scores and previews them against a rig and a film.
type Server struct {
	mu     sync.Mutex
	path   string
	sc     *score.Score
	rig    *rig.Config
	media  string
	scores string
	// firmware is a directory of images a browser can flash. Empty when the
	// studio was started without one, which is the ordinary case.
	firmware string
	// rigPath is the file the rig was read from, so the admin can write it
	// back. Empty when no rig was given, and then the rig is not editable
	// rather than editable into nowhere.
	rigPath string
	// rigDir is the shelf the rig came off, when -rig named a directory of
	// them. Empty for a single file, and the picker then has nothing to offer.
	rigDir string
	jobs   *Jobs
}

// New opens a studio.
func New(o Options) (*Server, error) {
	s := &Server{path: o.Score, media: o.Media, scores: o.Scores,
		firmware: o.Firmware, rigPath: o.Rig}

	if o.Score != "" {
		sc, err := score.Load(o.Score)
		if err != nil {
			return nil, err
		}
		s.sc = sc
	}
	if o.Rig != "" {
		// A directory is a shelf of rigs with one of them chosen; a path is
		// that one rig. Both are ordinary things to pass.
		if rig.Shelf(o.Rig) {
			s.rigDir = o.Rig
			resolved, err := rig.Resolve(o.Rig)
			if err != nil {
				return nil, err
			}
			s.rigPath = resolved
		}
		rc, err := rig.Load(o.Rig)
		if err != nil {
			return nil, err
		}
		s.rig = rc
	}
	if o.Media != "" {
		if _, err := os.Stat(o.Media); err != nil {
			return nil, fmt.Errorf("media: %w", err)
		}
	}

	scores := o.Scores
	if scores == "" {
		// Default to sitting beside the films. Keeping a score next to the
		// film it belongs to is what people do by hand anyway.
		if info, err := os.Stat(o.Media); err == nil && info.IsDir() {
			scores = o.Media
		}
	}
	s.scores = scores
	s.jobs = NewJobs(o.Composer, scores, o.Media)
	s.jobs.WithDevices(append(deviceArgs(s.rig), lightArgs(s.rig)...))
	// Keep whatever scores already exist, so there is a baseline to compare
	// against on the very first run after history was switched on. Those are
	// the scores whose behaviour prompted the work, so they are worth more
	// than the ones made after, not less.
	var names []string
	for _, f := range s.mediaFiles() {
		names = append(names, f.Name)
	}
	s.jobs.SeedHistory(names)

	// With no score named, open whichever film's score already exists, so a
	// studio started against a library is useful immediately.
	if s.sc == nil {
		s.openFirstAvailable()
	}
	if s.sc == nil {
		return nil, fmt.Errorf("no score given and none found in %s", scores)
	}
	return s, nil
}

// openFirstAvailable loads the score of the first film that has one.
func (s *Server) openFirstAvailable() {
	for _, f := range s.mediaFiles() {
		path := s.jobs.ScorePath(f.Name)
		if sc, err := score.Load(path); err == nil {
			s.sc, s.path = sc, path
			return
		}
	}
}

// openForFilm switches the editor to a film's score.
//
// Returns false when that film has no score yet, which is not an error: it is
// the normal state of a film nobody has analysed, and the library offers a
// button for exactly that.
func (s *Server) openForFilm(film string) bool {
	path := s.jobs.ScorePath(film)
	sc, err := score.Load(path)
	if err != nil {
		return false
	}
	s.sc, s.path = sc, path
	return true
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	mux := http.NewServeMux()
	files := noCache(http.FileServer(http.FS(sub)))
	// The rebuilt studio is the studio. The original is still here, at
	// /legacy, because it is the only thing that has been run against the
	// hardware and because two implementations of the same view are worth
	// having while one of them is young.
	web := s.handleWeb()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		web.ServeHTTP(w, r)
	}))
	mux.Handle("/legacy/", http.StripPrefix("/legacy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "" {
			s.servePage(w, sub)
			return
		}
		files.ServeHTTP(w, r)
	})))
	mux.Handle("/legacy", http.RedirectHandler("/legacy/", http.StatusFound))
	mux.HandleFunc("/api/score", s.handleScore)
	mux.HandleFunc("/api/rig", s.handleRig)
	mux.HandleFunc("/api/rig/options", s.handleRigOptions)
	mux.HandleFunc("/api/rigs", s.handleRigs)
	mux.HandleFunc("/media", s.handleMedia)
	mux.HandleFunc("/api/media", s.handleMediaList)
	mux.HandleFunc("/api/library", s.handleLibrary)
	mux.HandleFunc("/api/build", s.handleBuild)
	mux.HandleFunc("/api/prepare", s.handlePrepare)
	mux.HandleFunc("/api/layout", s.handleLayout)
	mux.HandleFunc("/api/versions", s.handleVersions)
	mux.HandleFunc("/api/seen", s.handleSeen)
	mux.HandleFunc("/api/context", s.handleContext)
	mux.HandleFunc("/api/firmware", s.handleFirmwareInfo)
	mux.HandleFunc("/firmware/", s.handleFirmwareFile)
	// The rebuilt studio, alongside the original rather than instead of it.
	mux.Handle("/v2/", s.handleWeb())
	mux.Handle("/v2", http.RedirectHandler("/v2/", http.StatusFound))
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/delete", s.handleDelete)
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
	// Which films have a browser-playable copy beside them. Gathered first
	// because the preview for a film is discovered independently of the order
	// the directory happens to be read in.
	previews := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && isPreview(e.Name()) {
			previews[e.Name()] = true
		}
	}

	var out []mediaFile
	for _, e := range entries {
		// A preview is not a film. Listing it as one would offer to analyse
		// it and produce a second score for the same picture.
		if e.IsDir() || !playable(e.Name()) || isPreview(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, mediaFile{
			Name:    e.Name(),
			Size:    fi.Size(),
			Preview: previews[previewName(e.Name())],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type mediaFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Preview is true when a browser-playable copy exists beside the film.
	// The film itself may well be unplayable in a browser; this says whether
	// there is something to play instead.
	Preview bool `json:"preview"`
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

// servePage stamps a content version onto every asset URL.
//
// Cache-Control alone was not enough. A browser that had already cached the
// old scripts kept serving them from cache alongside a freshly fetched page,
// which produced the worst possible combination: new HTML, old JavaScript,
// and an application that failed silently. Changing the URL leaves the
// browser no choice.
func (s *Server) servePage(w http.ResponseWriter, sub fs.FS) {
	page, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body := strings.ReplaceAll(string(page), "__V__", assetVersion(sub))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	io.WriteString(w, body)
}

var versionOnce struct {
	sync.Once
	value string
}

// assetVersion is a short hash of every embedded asset, so any change to any
// of them changes every asset URL. Computed once: the files cannot change
// while the process runs.
func assetVersion(sub fs.FS) string {
	versionOnce.Do(func() {
		h := sha256.New()
		fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, err := fs.ReadFile(sub, path)
			if err != nil {
				return nil
			}
			h.Write([]byte(path))
			h.Write(b)
			return nil
		})
		versionOnce.value = hex.EncodeToString(h.Sum(nil))[:12]
	})
	return versionOnce.value
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
			// Prefer the prepared copy. The original is very often something
			// no browser can decode — that is the whole reason the preview
			// exists — so serving it here would hand back bytes the <video>
			// element is going to refuse.
			name := f.Name
			if f.Preview {
				name = previewName(f.Name)
			}
			http.ServeFile(w, r, filepath.Join(s.media, name))
			return
		}
	}
	http.Error(w, "no such media", http.StatusNotFound)
}

// wireInstrument is what the room needs to draw a device.
type wireInstrument struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Driver  string  `json:"driver"`
	Latency float64 `json:"latency"`
	// Where it is plugged in. These were missing, which meant the admin's
	// device list showed a dash for every address it had: the page was
	// rendering fields the server had never sent.
	Addr     string     `json:"addr,omitempty"`
	Universe uint16     `json:"universe,omitempty"`
	Start    int        `json:"start,omitempty"`
	Mode     string     `json:"mode,omitempty"`
	Position [3]float64 `json:"position"`
}

type wireRig struct {
	Name     string `json:"name"`
	HasMedia bool   `json:"hasMedia"`
	// Editable is false when the studio was started without a rig file. The
	// page then shows what it inferred from the score and does not pretend it
	// can be saved.
	Editable    bool             `json:"editable"`
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
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		s.handleRigSave(w, r)
		return
	}

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
	out.Editable = s.rigPath != ""
	for _, in := range s.rig.Instruments {
		pos := defaultPosition(in.Kind)
		if in.Position != nil {
			pos = [3]float64{in.Position.X, in.Position.Y, in.Position.Z}
		}
		out.Instruments = append(out.Instruments, wireInstrument{
			ID: in.ID, Kind: in.Kind, Driver: in.Driver,
			Latency:  in.Latency.Duration().Seconds(),
			Addr:     in.Addr,
			Universe: in.Universe,
			Start:    in.Start,
			Mode:     in.Mode,
			Position: pos,
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
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	// FPS is what the editor counts frames in. Sent because an editor
	// addresses a film in frames, not decimal seconds, and the browser has no
	// way to discover a video's frame rate on its own.
	//
	// Read only: the analysis records it from the film and nothing in the
	// editor may change it, so it is deliberately not read back in fromWire.
	FPS    float64     `json:"fps,omitempty"`
	Path   string      `json:"path"`
	Tracks []wireTrack `json:"tracks"`
	// Calm is advisory and read only: the editor draws it, nothing edits it,
	// and it is not read back in fromWire.
	Calm []wireRegion `json:"calm,omitempty"`
}

type wireRegion struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
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
		// Selecting a film in the picker switches the editor to that film's
		// score. Without this the picker changed the picture and left the
		// score alone, so a fifteen minute film played against a three cue
		// demo and looked like nothing was happening after the first minute.
		// A kept score, opened for review rather than for editing. Loaded
		// without becoming the live score: comparing an old version against a
		// new one should not quietly make the old one current.
		if id := r.URL.Query().Get("version"); id != "" {
			film := r.URL.Query().Get("film")
			path, ok := s.jobs.VersionPath(film, id)
			if !ok {
				s.mu.Unlock()
				writeJSON(w, http.StatusNotFound,
					map[string]string{"error": "no such version"})
				return
			}
			sc, err := score.Load(path)
			if err != nil {
				s.mu.Unlock()
				writeJSON(w, http.StatusInternalServerError,
					map[string]string{"error": err.Error()})
				return
			}
			out := toWire(sc, path)
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, out)
			return
		}
		if film := r.URL.Query().Get("film"); film != "" {
			// A film with no score gets an honest refusal. Leaving the
			// previous score loaded would hand back another film's score
			// under this film's name, which is precisely the confusion this
			// whole feature exists to end.
			if !s.openForFilm(film) {
				s.mu.Unlock()
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": "no score for " + film + " yet",
					"film":  film,
				})
				return
			}
		}
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
		FPS:      sc.Meta.Media.FPS,
		Path:     path,
	}
	for _, r := range sc.Calm {
		out.Calm = append(out.Calm, wireRegion{
			From: r.From.Duration().Seconds(),
			To:   r.To.Duration().Seconds(),
		})
	}
	for _, t := range sc.Tracks {
		wt := wireTrack{Instrument: t.Instrument, Type: string(t.Type)}
		for _, c := range t.Cues {
			wt.Cues = append(wt.Cues, wireCue{
				T: c.T.Duration().Seconds(), Action: c.Action, Params: c.Params,
				Duration: c.Duration.Duration().Seconds(), Source: c.Source,
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
				// Carried back, so editing a score in the studio does not
				// quietly strip what proposed each cue.
				Source: c.Source,
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

// --- the library ---

type libraryEntry struct {
	Film      string  `json:"film"`
	Size      int64   `json:"size"`
	HasScore  bool    `json:"hasScore"`
	ScoreName string  `json:"scoreName,omitempty"`
	Tracks    int     `json:"tracks,omitempty"`
	Cues      int     `json:"cues,omitempty"`
	Duration  float64 `json:"duration,omitempty"`
	Job       *Job    `json:"job,omitempty"`
	// Builds is every score kept for this film, newest first, with what each
	// run did and cost. Sent with the listing so a row can show the history
	// without a request of its own — the listing is already polled while
	// anything is running, and a second request per film would multiply that.
	Builds []Version `json:"builds,omitempty"`
	// Seen is whether a kept description exists — what the model said, which
	// a rebuild reuses unless it is told not to. Only whether, not how much:
	// this is answered for every film every time the library polls, and
	// counting the lines of a feature would mean reading half a megabyte a
	// second to render a button.
	Seen bool `json:"seen"`
	// Preview is whether a browser-playable copy exists, and Prepare is the
	// job making one. Separate from Job, because a film can legitimately be
	// being analysed and prepared at the same time.
	Preview bool `json:"preview"`
	Prepare *Job `json:"prepare,omitempty"`
}

type libraryView struct {
	Scores string `json:"scores"`
	// Free space where films live, since running out of it is the reason
	// anybody deletes one.
	Free      int64 `json:"free"`
	CanBuild  bool  `json:"canBuild"`
	CanUpload bool  `json:"canUpload"`
	// CanPrepare is whether ffmpeg is around to make browser-playable copies.
	CanPrepare bool           `json:"canPrepare"`
	Current    string         `json:"current"`
	Entries    []libraryEntry `json:"entries"`
}

// handleLibrary answers what films exist, which have scores, and what is being
// analysed right now.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	jobs := s.jobs.Snapshot()

	s.mu.Lock()
	current := filepath.Base(s.path)
	s.mu.Unlock()

	out := libraryView{
		Scores:     s.scores,
		CanBuild:   s.jobs.Available(),
		Current:    current,
		Free:       freeBytes(s.mediaDir()),
		CanUpload:  s.mediaDir() != "",
		CanPrepare: s.mediaDir() != "" && ffmpegAvailable(),
	}
	for _, f := range s.mediaFiles() {
		entry := libraryEntry{Film: f.Name, Size: f.Size}
		path := s.jobs.ScorePath(f.Name)
		entry.ScoreName = filepath.Base(path)

		if sc, err := score.Load(path); err == nil {
			entry.HasScore = true
			entry.Tracks = len(sc.Tracks)
			entry.Cues = len(sc.Cues())
			entry.Duration = sc.Meta.Media.Duration.Duration().Seconds()
		}
		entry.Preview = f.Preview
		entry.Seen = s.jobs.HasSeen(f.Name)
		if job, ok := jobs[jobKey(JobAnalyse, f.Name)]; ok {
			j := job
			entry.Job = &j
		}
		entry.Builds = s.jobs.Versions(f.Name)
		if job, ok := jobs[jobKey(JobPrepare, f.Name)]; ok {
			j := job
			entry.Prepare = &j
		}
		out.Entries = append(out.Entries, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSeen returns what the model said about one film.
//
// Read only, and deliberately the whole thing rather than a page of it. It is
// read by a person deciding whether the description is worth keeping, and that
// judgement is made by scrolling it — a page at a time would mean deciding
// whether to pay for a new one based on the first twenty frames.
func (s *Server) handleSeen(w http.ResponseWriter, r *http.Request) {
	film := r.URL.Query().Get("film")
	if film == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no film given"})
		return
	}
	// The same rule as serving media: only a name that appeared in the
	// listing, so this cannot be walked out of the media directory.
	known := false
	for _, f := range s.mediaFiles() {
		if f.Name == film {
			known = true
			break
		}
	}
	if !known {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such film"})
		return
	}

	out := map[string]any{
		"film":         film,
		"observations": []Observation{},
		// What the film is, so the panel that shows the descriptions is also
		// where the reason they read as they do can be changed.
		"context": s.jobs.ReadContext(film),
	}
	// What produced it, when there is a record of it. Which model answered is
	// the first thing anybody asks of a description they are judging.
	if versions := s.jobs.Versions(film); len(versions) > 0 {
		out["note"] = versions[0].Note
		out["made"] = versions[0].Made
		// The film's own length, so a reader can see what share of it was
		// looked at. Fifteen minutes of observations reads as a complete
		// description until it is put next to the two hours it describes.
		out["duration"] = versions[0].Duration
	}
	obs, err := s.jobs.ReadSeen(film)
	if err != nil {
		// Not an error to the caller: a film that has never been looked at
		// has no description, which is an answer rather than a failure.
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["observations"] = obs
	writeJSON(w, http.StatusOK, out)
}

// handleContext records what a film is, for the model to describe it by.
//
// A POST rather than a PUT because everything else that changes state here is a
// POST, and consistency inside one small API is worth more than the distinction.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	film := r.URL.Query().Get("film")
	if film == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no film given"})
		return
	}
	// The same rule as everywhere else: only a name that appeared in the
	// listing, so a path cannot be walked out of the scores directory.
	known := false
	for _, f := range s.mediaFiles() {
		if f.Name == film {
			known = true
			break
		}
	}
	if !known {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such film"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, contextLimit*2))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.jobs.WriteContext(film, string(body)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"film": film, "context": s.jobs.ReadContext(film),
	})
}

// handlePrepare queues a browser-playable copy of a film. With all=1 it
// queues every film that has not got one, which is the useful bulk operation:
// a library that has just been dropped onto the machine is mostly Matroska.
//
// Deliberately separate from analysis rather than folded into it. Analysis
// reads the original and does not care what container it is in, and preparing
// a preview does not make analysis faster — measured on this project's own
// box, re-encoding a 110 minute film costs about 95 minutes to save about 15
// minutes of decoding, which is not a trade worth making automatically.
func (s *Server) handlePrepare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !ffmpegAvailable() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "ffmpeg and ffprobe are not installed, so previews cannot be made",
		})
		return
	}

	files := s.mediaFiles()
	if r.URL.Query().Get("all") == "1" {
		var queued []string
		for _, f := range files {
			if f.Preview {
				continue
			}
			s.jobs.Enqueue(JobPrepare, f.Name)
			queued = append(queued, f.Name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
		return
	}

	want := r.URL.Query().Get("file")
	// Same rule as serving media: only a name that appeared in the listing.
	for _, f := range files {
		if f.Name == want {
			writeJSON(w, http.StatusOK, s.jobs.Enqueue(JobPrepare, want))
			return
		}
	}
	http.Error(w, "no such film", http.StatusNotFound)
}

// handleBuild starts an analysis. With all=1 it queues every film, which is
// the "rebuild everything" the library offers.
// handleVersions lists the scores kept for one film, newest first.
func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	film := r.URL.Query().Get("film")
	if film == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no film given"})
		return
	}
	versions := s.jobs.Versions(film)
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		out = append(out, map[string]any{
			"id":       v.ID,
			"label":    v.Label(),
			"note":     v.Note,
			"from":     v.From,
			"to":       v.To,
			"duration": v.Duration,
			"complete": v.Complete,
			"cues":     v.Cues,
			"points":   v.Points,
			"tracks":   v.Tracks,
			"steps":    v.Steps,
			"seconds":  Elapsed(v.Steps),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"film": film, "versions": out})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.jobs.Available() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "no composer available; start the studio with -composer path/to/compose.py",
		})
		return
	}

	files := s.mediaFiles()
	if r.URL.Query().Get("all") == "1" {
		var queued []string
		for _, f := range files {
			s.jobs.Enqueue(JobAnalyse, f.Name)
			queued = append(queued, f.Name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
		return
	}

	want := r.URL.Query().Get("file")
	// Same rule as serving media: only a name that appeared in the listing.
	for _, f := range files {
		if f.Name == want {
			// Reset throws away the finished pieces so the run starts from
			// nothing. It is a separate request rather than a flag on this
			// one because a rebuild that quietly discarded twenty minutes of
			// finished work would be the single most expensive misclick in
			// the studio. Without it, a rebuild resumes.
			if r.URL.Query().Get("reset") == "1" {
				if err := s.jobs.ResetAnalysis(want); err != nil {
					writeJSON(w, http.StatusInternalServerError,
						map[string]string{"error": err.Error()})
					return
				}
			}
			// Showing the film to a model again is the one expensive thing
			// here, so it is asked for rather than assumed. Without this a
			// rebuild reuses what the model already said.
			if r.URL.Query().Get("vision") == "redo" {
				s.jobs.update(JobAnalyse, want, true, func(job *Job) {
					job.LookAgain = true
				})
			}
			// minutes analyses only the opening of a film, for judging a
			// change without paying for a feature to find out.
			if m := r.URL.Query().Get("minutes"); m != "" {
				if mins, err := strconv.ParseFloat(m, 64); err == nil && mins > 0 {
					writeJSON(w, http.StatusOK, s.jobs.EnqueueLimited(want, mins*60))
					return
				}
			}
			writeJSON(w, http.StatusOK, s.jobs.Enqueue(JobAnalyse, want))
			return
		}
	}
	http.Error(w, "no such film", http.StatusNotFound)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.jobs.Snapshot())
}
