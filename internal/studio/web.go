package studio

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The new studio, built from web/ by Vite.
//
// Served at /v2 while the original keeps working at /, so the two can be
// compared on the same score instead of one replacing the other on trust.
//
// all: is needed because Vite names its output with a leading underscore in
// some configurations and embed skips those by default; the directory is
// generated, so opting everything in is both simpler and less surprising than
// finding out later that one chunk is missing.
//
//go:embed all:webdist
var webAssets embed.FS

// hasWeb reports whether a build of the new studio is embedded. A plain
// `go build` with no npm run produces an empty directory, and the honest
// response to that is a message saying how to build it rather than a blank
// page or a 404 that looks like a routing bug.
func hasWeb(sub fs.FS) bool {
	_, err := fs.Stat(sub, "index.html")
	return err == nil
}

func (s *Server) handleWeb() http.Handler {
	sub, err := fs.Sub(webAssets, "webdist")
	if err != nil {
		panic(err) // embedded at build time; cannot fail at runtime
	}
	files := noCache(http.StripPrefix("/v2/", http.FileServer(http.FS(sub))))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hasWeb(sub) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(
				"The new studio is not in this binary.\n\n" +
					"It is built separately:\n\n" +
					"    cd web && npm ci && npm run build\n\n" +
					"then rebuild the Go binary so it can embed the result.\n" +
					"The original studio is unaffected and is at /.\n"))
			return
		}

		// Anything that is not a file is the app: a single page owns its own
		// routing, and serving index.html for unknown paths is what lets a
		// deep link work after a reload.
		path := strings.TrimPrefix(r.URL.Path, "/v2/")
		if path == "" || !strings.Contains(path, ".") {
			page, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store, must-revalidate")
			_, _ = w.Write(page)
			return
		}
		files.ServeHTTP(w, r)
	})
}
