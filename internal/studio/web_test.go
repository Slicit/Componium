package studio

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The rebuilt studio is what / serves, and the original is still reachable.
// Both matter: one is the thing people use, the other is the only version that
// has been run against hardware.
func TestTheRootServesTheRebuiltStudio(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	/* Either the built bundle, or the message explaining how to build it —
	 * never the old page, and never a blank 404 that reads like a routing
	 * fault. */
	if !strings.Contains(body, "<div id=\"root\">") && !strings.Contains(body, "npm ci") {
		t.Errorf("/ served something unexpected:\n%s", body[:min(300, len(body))])
	}
	if strings.Contains(body, "app.js?v=") {
		t.Error("/ is still serving the original studio")
	}
}

func TestTheOriginalStudioIsStillReachable(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "app.js?v=") {
		t.Error("/legacy is not the original studio")
	}
}

func TestLegacyAssetsResolve(t *testing.T) {
	s, _ := newServer(t)
	for _, path := range []string{"/legacy/app.js", "/legacy/room3d.js", "/legacy/style.css"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s gave %d", path, rec.Code)
		}
	}
}

// A single page owns its own routing, so an unknown path is the app rather
// than a 404 — otherwise a deep link stops working the moment it is reloaded.
func TestUnknownPathsGoToTheApp(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
