package rig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The deployment hands the studio its rig as a single file bind mount: the
// file is writable and the directory it sits in is not. Write and rename needs
// a writable directory, so every save failed with "permission denied" on a file
// the container had just been shown was writable.
//
// t.TempDir gives a writable directory, which is why nothing caught it.

func TestSavingIntoAReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this is about")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	// The shape a single file bind mount leaves: the file is ours, the
	// directory is not.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	changed := good()
	changed.Instruments[1].Addr = "192.168.1.145:5570"
	if err := Save(path, changed); err != nil {
		t.Fatalf("could not save into a read only directory: %v", err)
	}

	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Instruments[1].Addr != "192.168.1.145:5570" {
		t.Errorf("the edit did not land: %q", back.Instruments[1].Addr)
	}
	// And left no temp file behind, since it could not have made one anyway.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}

func TestAFileThatCannotBeWrittenAtAllStillFails(t *testing.T) {
	// The fallback must not turn a real permission problem into silence.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this is about")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := Save(path, good()); err == nil {
		t.Error("claimed to save a file it cannot write")
	}
}

func TestTheOrdinaryCaseIsStillAtomic(t *testing.T) {
	// Where the directory allows it, an interrupted save must leave the
	// previous rig rather than half of a new one.
	dir := t.TempDir()
	path := filepath.Join(dir, "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left %d files behind, want just the rig", len(entries))
	}
}
