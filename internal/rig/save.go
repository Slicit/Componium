package rig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Writing a rig file back out.
//
// The rig has always been a file somebody edits, and it stays one: this is not
// a second place to keep what is in the room, it is a second way to edit the
// same place. That distinction is the whole reason this is safe. A studio that
// kept its own idea of the hardware would be a studio that disagrees with the
// conductor, and the conductor is the one holding the mains.
//
// What it costs is that everything here has to survive a round trip. A field
// that writes but does not read back is a field that quietly disappears the
// first time somebody presses save, which is worse than not being editable.

// Drivers that can carry each kind.
//
// sACN builds a DMX light and nothing else; motion builds a platform bridge.
// CIP asks the device what it is, so it carries anything, and virtual stands in
// for anything. Offering an impossible pairing in a menu is offering a rig that
// will not start.
var driversByKind = map[string][]string{
	"light":  {"virtual", "sacn", "cip"},
	"wind":   {"virtual", "cip"},
	"shake":  {"virtual", "cip"},
	"fog":    {"virtual", "cip"},
	"mist":   {"virtual", "cip"},
	"scent":  {"virtual", "cip"},
	"motion": {"virtual", "motion", "cip"},
}

// Kinds returns every instrument kind a rig can hold, in a stable order.
func Kinds() []string {
	out := make([]string, 0, len(driversByKind))
	for k := range driversByKind {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DriversFor returns the drivers that can carry a kind, in a stable order.
// Unknown kinds get nothing, which is how a menu refuses rather than guesses.
func DriversFor(kind string) []string {
	return append([]string(nil), driversByKind[kind]...)
}

// Validate reports everything wrong with a rig, rather than the first thing.
//
// All of it at once on purpose: somebody editing six instruments in a browser
// wants the list, not a game where fixing one reveals the next.
func (c *Config) Validate() []string {
	var problems []string
	seen := map[string]bool{}
	// Which entry already claimed a CIP address.
	speaksFor := map[string]string{}

	for i, in := range c.Instruments {
		where := fmt.Sprintf("instrument %d", i+1)
		if in.ID != "" {
			where = in.ID
		}

		switch {
		case in.ID == "":
			problems = append(problems, where+": needs an id")
		case seen[in.ID]:
			// Two instruments with one name is not a duplicate row, it is a
			// score that addresses one of them and nobody knows which.
			problems = append(problems, in.ID+": named twice")
		}
		seen[in.ID] = true

		if in.Kind == "" {
			problems = append(problems, where+": needs a kind")
			continue
		}
		allowed := driversByKind[in.Kind]
		if allowed == nil {
			problems = append(problems, where+": unknown kind "+quoted(in.Kind))
			continue
		}
		driver := in.Driver
		if driver == "" {
			driver = "virtual"
		}
		if !contains(allowed, driver) {
			problems = append(problems, fmt.Sprintf(
				"%s: a %s cannot be driven by %s (try %s)",
				where, in.Kind, quoted(driver), strings.Join(allowed, ", ")))
			continue
		}

		switch driver {
		case "sacn":
			if in.Start < 1 || in.Start > 512 {
				problems = append(problems, fmt.Sprintf(
					"%s: DMX start address %d is not between 1 and 512", where, in.Start))
			}
			if in.Mode != "" && in.Mode != "dimmer" && in.Mode != "rgb" && in.Mode != "rgbw" {
				problems = append(problems, where+": unknown mode "+quoted(in.Mode))
			}
		case "cip", "motion":
			// An address is the whole point of these two: without one there is
			// nothing to send to and the rig fails at startup rather than here.
			if in.Addr == "" {
				want := "host:port"
				if p := DefaultPort(driver); p != "" {
					want = "something like 192.168.1.145:" + p
				}
				problems = append(problems, where+": needs an address, "+want)
			}
		}
		// Asked properly. "Contains a colon" said yes to http://192.168.1.145/,
		// which was then written to the rig and failed when the show started.
		if in.Addr != "" {
			if bad := AddrProblem(in.Addr, driver); bad != "" {
				problems = append(problems, where+": "+bad)
			}
		}

		// One node is one instrument. A CIP node reports its own manifest, so
		// two entries at one address come back as the same instrument and the
		// rig refuses to start. Caught here, where the address is being typed,
		// rather than at the moment somebody presses go.
		if driver == "cip" && in.Addr != "" {
			if first, twice := speaksFor[in.Addr]; twice {
				problems = append(problems, fmt.Sprintf(
					"%s and %s are both CIP at %s, and one node is one "+
						"instrument. An LED strip on that board is reached by "+
						"sACN on its own port, not by CIP", first, where, in.Addr))
			} else {
				speaksFor[in.Addr] = where
			}
		}
	}
	return problems
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}

func quoted(s string) string { return "\"" + s + "\"" }

// Save writes a rig file, refusing anything that would not load again.
//
// Written and renamed, so an interrupted save leaves the previous rig rather
// than half of a new one. A rig file is what the conductor reads to decide what
// is on the end of every wire, and a truncated one is worse than a stale one.
func Save(path string, c *Config) error {
	if problems := c.Validate(); len(problems) > 0 {
		return fmt.Errorf("rig: %s", strings.Join(problems, "; "))
	}

	text := encode(c)

	// Read it back before it replaces anything. A writer and a reader that
	// disagree is a class of bug that otherwise shows up as a rig which was
	// fine until somebody pressed save.
	var back Config
	if _, err := toml.Decode(text, &back); err != nil {
		return fmt.Errorf("rig: wrote a file it could not read back: %w", err)
	}
	if len(back.Instruments) != len(c.Instruments) {
		return fmt.Errorf("rig: %d instruments went in and %d came back",
			len(c.Instruments), len(back.Instruments))
	}

	return writeFile(path, []byte(text))
}

// writeFile puts the bytes there, atomically where the filesystem allows it.
//
// The preferred way is a temp file beside the target and a rename, so that an
// interrupted save leaves the previous rig rather than half of a new one: a
// rig file is what the conductor reads to decide what is on the end of every
// wire, and a truncated one is worse than a stale one.
//
// That needs a writable *directory*, and there is a normal way to run this
// where the directory is read only and the file is not: a single file bind
// mount, which is how the deployment hands the studio its rig. The temp file
// cannot be created, and even if it could, renaming over the target would
// replace a mount point rather than a file. There is no atomic option there,
// so the fallback is an ordinary write in place, with the small risk that
// comes with it.
//
// Discovered the way these things are: the studio refused every save with
// "permission denied" on a file it had just been told was writable.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err == nil {
		if err := os.Rename(tmp, path); err == nil {
			return nil
		}
		os.Remove(tmp)
	}
	return os.WriteFile(path, data, 0o644)
}

const header = `# Written by the Componium studio. Editing it by hand is fine and always was:
# this is the file, not a copy of it, and the studio reads whatever is here the
# next time it starts.
#
# The conductor reads this once, when it starts. A change here reaches a running
# show only when that show is restarted.

`

// Dir is where a rig file lives, for callers that need to put one somewhere.
func Dir(path string) string { return filepath.Dir(path) }
