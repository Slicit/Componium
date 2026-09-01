package rig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A shelf of rigs, and which one is in use.
//
// One installation is not one rig. A bench with a board on it, the room as it
// actually stands, and the demonstration that needs no hardware at all are
// three different files, and switching between them by editing a flag and
// restarting is how people end up with one file that is none of them.
//
// The selection is a file in the directory rather than a setting in the studio,
// and that is the point. `-rig` accepts a directory as well as a file, so the
// conductor pointed at the shelf plays whatever was chosen in the browser. A
// selection only the studio knew about would be a selection the thing holding
// the mains does not.

// Chosen names the file that a directory of rigs is currently pointing at.
// Dot prefixed so it never appears in the shelf it describes.
const Chosen = ".chosen"

// Shelf reports whether a path is a directory of rigs rather than one rig.
func Shelf(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Files lists the rigs on a shelf, by name, alphabetically.
func Files(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if filepath.Ext(name) == ".toml" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Selected returns the chosen rig's file name on a shelf.
//
// Falls back to the first one alphabetically when nothing has been chosen or
// when what was chosen is no longer there. A shelf with rigs on it always
// answers with one of them, because the alternative is a studio that will not
// open because of a marker file nobody knew existed.
func Selected(dir string) (string, error) {
	files, err := Files(dir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("rig: no .toml files in %s", dir)
	}
	if b, err := os.ReadFile(filepath.Join(dir, Chosen)); err == nil {
		want := strings.TrimSpace(string(b))
		for _, f := range files {
			if f == want {
				return f, nil
			}
		}
	}
	return files[0], nil
}

// Select records which rig a shelf is pointing at.
func Select(dir, name string) error {
	// Only a name, never a path. This comes from a browser, and a directory of
	// rig files is a directory the conductor will read and act on.
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return fmt.Errorf("rig: %q is not a rig on this shelf", name)
	}
	files, err := Files(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f == name {
			return writeFile(filepath.Join(dir, Chosen), []byte(name+"\n"))
		}
	}
	return fmt.Errorf("rig: no rig called %q here", name)
}

// Resolve turns what somebody passed to -rig into the file to read.
//
// A file is itself. A directory is whichever rig on that shelf is chosen, which
// is what lets the conductor and the studio agree without either of them
// telling the other anything.
func Resolve(path string) (string, error) {
	if !Shelf(path) {
		return path, nil
	}
	name, err := Selected(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(path, name), nil
}
