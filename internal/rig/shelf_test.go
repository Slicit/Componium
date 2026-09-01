package rig

import (
	"os"
	"path/filepath"
	"testing"
)

func shelf(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := Save(filepath.Join(dir, n), good()); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAShelfListsItsRigs(t *testing.T) {
	dir := shelf(t, "bench.toml", "demo.toml", "room.toml")
	// Alphabetical, so a picker is in the same order every time it opens.
	got, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bench.toml", "demo.toml", "room.toml"}
	if len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Fatalf("listed %v", got)
	}
}

func TestItIgnoresWhatIsNotARig(t *testing.T) {
	dir := shelf(t, "bench.toml")
	for _, junk := range []string{"notes.md", ".chosen", ".hidden.toml"} {
		if err := os.WriteFile(filepath.Join(dir, junk), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := Files(dir); len(got) != 1 {
		t.Errorf("listed %v", got)
	}
}

func TestNothingChosenIsTheFirstOne(t *testing.T) {
	// A shelf with rigs on it always answers with one of them. A studio that
	// will not open because of a marker file nobody knew about is worse than
	// one that opens on the wrong rig, which you can see and change.
	dir := shelf(t, "bench.toml", "demo.toml")
	got, err := Selected(dir)
	if err != nil || got != "bench.toml" {
		t.Fatalf("%q, %v", got, err)
	}
}

func TestChoosingSticks(t *testing.T) {
	dir := shelf(t, "bench.toml", "demo.toml")
	if err := Select(dir, "demo.toml"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Selected(dir); got != "demo.toml" {
		t.Errorf("chose demo, got %q", got)
	}
	// And the conductor, pointed at the shelf rather than at a file, follows.
	resolved, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(resolved) != "demo.toml" {
		t.Errorf("resolved to %q", resolved)
	}
}

func TestAChoiceThatIsNoLongerThereFallsBack(t *testing.T) {
	dir := shelf(t, "bench.toml", "demo.toml")
	if err := Select(dir, "demo.toml"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "demo.toml")); err != nil {
		t.Fatal(err)
	}
	if got, _ := Selected(dir); got != "bench.toml" {
		t.Errorf("got %q after the chosen rig was deleted", got)
	}
}

func TestAShelfWillNotBeTalkedIntoAPath(t *testing.T) {
	// This name arrives from a browser, and it decides which file the thing
	// holding the mains is about to read.
	dir := shelf(t, "bench.toml")
	for _, bad := range []string{
		"../../etc/passwd", "/etc/passwd", "sub/other.toml", "", ".chosen",
	} {
		if err := Select(dir, bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, _ := Selected(dir); got != "bench.toml" {
		t.Errorf("the shelf moved to %q", got)
	}
}

func TestAFileIsStillJustAFile(t *testing.T) {
	// Everything that passed -rig a path to one rig keeps working.
	dir := shelf(t, "bench.toml")
	path := filepath.Join(dir, "bench.toml")
	got, err := Resolve(path)
	if err != nil || got != path {
		t.Fatalf("%q, %v", got, err)
	}
	if Shelf(path) {
		t.Error("called a file a shelf")
	}
}

func TestAnEmptyShelfSaysSo(t *testing.T) {
	if _, err := Selected(t.TempDir()); err == nil {
		t.Error("found a rig in an empty directory")
	}
}
