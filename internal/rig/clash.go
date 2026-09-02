package rig

import (
	"fmt"
	"sort"
)

// Two rig entries that turn out to be one device.
//
// This used to be the ordinary consequence of pointing two entries at one CIP
// node: the node reported one manifest, both entries adopted it, and the
// conductor refused the second by an id that appeared once in the file.
//
// Since ADR 0007 a board carries several devices and two entries at one address
// are an ordinary thing to write. What is left is the case this can still
// catch: two entries that resolve to the same instrument anyway, which now
// means two rig ids naming one device rather than one device wearing two ids.
// Rare, and still worth refusing, because the second one silently does nothing.

// Collisions reports rig entries that resolved to the same instrument.
func (b *Built) Collisions() error {
	claimed := map[string][]string{}
	for id, inst := range b.Instruments {
		claimed[inst.Manifest().ID] = append(claimed[inst.Manifest().ID], id)
	}
	var bad []string
	for manifest, entries := range claimed {
		if len(entries) < 2 {
			continue
		}
		sort.Strings(entries)
		bad = append(bad, fmt.Sprintf("%v all resolve to %q", entries, manifest))
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("rig: %v; each entry has to name a different instrument, "+
		"and a node announces one manifest per device", bad)
}
