package rig

import "strings"

// A scent instrument's reservoirs, and which smell is in each.
//
// The score names a scent and the rig says which bottle that is, exactly as a
// score names light.ambient and the rig says what that is on this hardware.
// Storing the reservoir number in the score instead would mean a score made
// here fills a different room with a different smell, and a score is meant to
// outlive the hardware it was made on.
//
// A rig that does not hold a scent does not fire it. Not an approximation —
// an approximation of a smell is a different smell, and the failure mode of
// getting it wrong is a room that cannot be aired out for twenty minutes.

// Scent finds which reservoir holds this smell, and whether it is held at all.
//
// Names are compared case-insensitively and with surrounding space ignored,
// because this table is written by hand by somebody filling bottles.
func (i InstConfig) Scent(name string) (int, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return 0, false
	}
	for slot, held := range i.Scents {
		if strings.ToLower(strings.TrimSpace(held)) == want {
			n, err := atoi(slot)
			if err != nil || n <= 0 {
				continue
			}
			return n, true
		}
	}
	return 0, false
}

// Holds reports whether this instrument can produce a scent at all.
//
// A scent instrument with no bank declared is one nobody has filled yet, and
// the honest response is silence rather than reservoir one.
func (i InstConfig) Holds() bool {
	return len(i.Scents) > 0
}

// atoi is strconv.Atoi without the import, kept local so the whole of this
// file is about one thing.
func atoi(s string) (int, error) {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return 0, errNotANumber
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 0, errNotANumber
	}
	return n, nil
}

type bankError string

func (e bankError) Error() string { return string(e) }

const errNotANumber = bankError("reservoir is not a positive number")
