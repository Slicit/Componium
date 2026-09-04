// Package boards remembers which ESP32s this installation has.
//
// A board is not an instrument and not a rig. A rig says what the show has and
// where to reach it; a board is a piece of hardware on a shelf that may carry
// several instruments, or none yet, and which exists whether or not any rig
// currently mentions it. Attaching one in the admin used to leave no trace at
// all: the address was typed, used once, and forgotten the moment the page was
// closed.
//
// A file, not the database. ADR 0006 draws the line at authored versus derived:
// nothing here can be recomputed from anything, so it belongs beside the rigs
// and the scores rather than in Postgres.
//
// # The secret
//
// Each board's shared secret is stored here, and that is a real decision rather
// than an oversight. A board that has one ignores every unauthenticated
// datagram, so without it this package could not tell a board that is switched
// off from one that is sitting there working: there is no reachability test
// that does not involve being allowed in. Storing it is what makes the online
// column mean anything, and what lets a board be reconfigured without typing it
// again.
//
// The consequence: this file is credentials. It is written 0600, it belongs
// outside the repository, and anybody who can read it can reconfigure every
// board it names.
package boards

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Slicit/componium/internal/rig"
)

// A Board is one piece of hardware, by the name a person gave it.
type Board struct {
	// Name is what it is called here, and the key everything else uses. A
	// person's word, not the board's: the board announces its own name from
	// its configuration, and that changes when the configuration does.
	Name string `toml:"name"`
	// Addr is host:port, where the port is CIP's.
	Addr string `toml:"addr"`
	// Secret authenticates everything, including finding out whether the board
	// is switched on. See the note on this package.
	Secret string `toml:"secret,omitempty"`
	// Note is whatever the person wants to remember. Which shelf it is on,
	// which fan it drives, that its case is the cracked one.
	Note string `toml:"note,omitempty"`
}

// file is the shape on disk. A named table, so the file can grow other
// top-level keys later without every reader having to guess.
type file struct {
	Board []Board `toml:"board"`
}

// A Shelf is the remembered set, and the file it came from.
type Shelf struct {
	path   string
	boards []Board
}

// DefaultPort is CIP's, added to an address that names only a host. Typing an
// address without one is the common case and the port is never interesting.
//
// Taken from the rig rather than written again, so there is one answer to what
// port a board listens on.
var DefaultPort = rig.DefaultPort("cip")

// Open reads a shelf, or returns an empty one when the file is not there yet.
//
// Missing is an ordinary state and not an error: an installation with no boards
// is every installation until somebody attaches the first.
func Open(path string) (*Shelf, error) {
	s := &Shelf{path: path}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("boards: %s: %w", path, err)
	}
	s.boards = f.Board
	return s, nil
}

// Path is the file this shelf was read from, empty when it has none.
func (s *Shelf) Path() string { return s.path }

// Editable reports whether this shelf can be written back.
func (s *Shelf) Editable() bool { return s.path != "" }

// All returns the boards, sorted by name, as a copy.
//
// A copy because the caller is usually about to hand these to a template or an
// encoder, and a shared slice of things containing secrets is the sort of thing
// that ends up aliased into a response by accident.
func (s *Shelf) All() []Board {
	out := make([]Board, len(s.boards))
	copy(out, s.boards)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find returns the board with a name, and whether there was one.
func (s *Shelf) Find(name string) (Board, bool) {
	for _, b := range s.boards {
		if b.Name == name {
			return b, true
		}
	}
	return Board{}, false
}

// Validate checks a proposed set, and normalises the addresses in place.
//
// Refused whole. Half a shelf is a shelf somebody has to reconcile by hand
// against what is actually plugged in, which is the job this was meant to do
// for them.
func Validate(in []Board) ([]Board, error) {
	out := make([]Board, 0, len(in))
	seenName := map[string]bool{}
	seenAddr := map[string]bool{}

	for i, b := range in {
		b.Name = strings.TrimSpace(b.Name)
		b.Note = strings.TrimSpace(b.Note)
		if b.Name == "" {
			return nil, fmt.Errorf("board %d has no name", i+1)
		}
		if seenName[b.Name] {
			return nil, fmt.Errorf("two boards called %q", b.Name)
		}
		seenName[b.Name] = true

		addr := rig.NormaliseAddr(b.Addr, "cip")
		if addr == "" {
			return nil, fmt.Errorf("%s: %q is not an address", b.Name, b.Addr)
		}
		// The same check the rig makes of an instrument's address, because a
		// board is reached the same way and two copies of the rule would drift.
		if bad := rig.AddrProblem(addr, "cip"); bad != "" {
			return nil, fmt.Errorf("%s: %s is not an address (%s)", b.Name, b.Addr, bad)
		}
		b.Addr = addr

		// Two names for one address is the mistake that makes the online column
		// lie: both rows go green off the same board and one of them is a
		// board nobody has actually plugged in.
		if seenAddr[b.Addr] {
			return nil, fmt.Errorf("%s: %s is already another board's address", b.Name, b.Addr)
		}
		seenAddr[b.Addr] = true

		out = append(out, b)
	}
	return out, nil
}

// Save replaces the shelf with a new set and writes it.
func (s *Shelf) Save(in []Board) error {
	if !s.Editable() {
		return fmt.Errorf("boards: there is nowhere to save to")
	}
	next, err := Validate(in)
	if err != nil {
		return err
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.boards = next
	return nil
}

const header = `# The boards this installation knows about.
#
# Written by the Componium studio. Editing it by hand is fine: this is the file,
# not a copy of it.
#
# THIS FILE HOLDS CREDENTIALS. Each secret authenticates everything a board
# accepts, including being told which pin a relay is on, and there is no way to
# change a board's secret except over USB. Keep it out of version control and
# off shared disks.

`

func (s *Shelf) write(list []Board) error {
	var sb strings.Builder
	sb.WriteString(header)
	if err := toml.NewEncoder(&sb).Encode(file{Board: list}); err != nil {
		return err
	}

	// Written beside the target and renamed over it, so an interrupted save
	// leaves the previous shelf rather than half of the next one.
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".boards-*.toml")
	if err != nil {
		// A single file bind mount has no writable directory to put a temp
		// file in, which is how this is deployed. Falling back to writing in
		// place is worse and is still better than refusing to save at all.
		return os.WriteFile(s.path, []byte(sb.String()), 0o600)
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.WriteString(sb.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// 0600 before it is in place, not after: the window where a file full of
	// secrets is world readable is exactly as long as you let it be.
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// SecretFor returns the stored secret for a board at an address.
//
// By address rather than by name, because that is what a rig entry has. Both
// sides are normalised first, so an entry written without a port matches a
// board saved with one.
//
// Empty when there is no such board, which leaves the caller exactly where it
// was: dialling without a secret, and being ignored by a board that has one.
func (s *Shelf) SecretFor(addr string) string {
	want := rig.NormaliseAddr(addr, "cip")
	for _, b := range s.boards {
		if rig.NormaliseAddr(b.Addr, "cip") == want {
			return b.Secret
		}
	}
	return ""
}
