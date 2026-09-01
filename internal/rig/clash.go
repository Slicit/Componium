package rig

import (
	"fmt"
	"sort"
)

// Two rig entries that turn out to be one device.
//
// A CIP node reports its own manifest, because the device is the only thing
// that actually knows its own latency and a rig file that disagrees with the
// hardware is worse than no rig file at all. The consequence is easy to reach
// and hard to read: point two entries at the same node and both come back
// calling themselves whatever that node calls itself. The conductor then
// refuses the second one with "instrument already registered", naming an id
// that appears exactly once in the rig file.
//
// Reached by pointing a light and a fan at one board while testing, which is
// not a silly thing to do. It is the obvious first move when you have one board
// and two effects, and the rig is right to refuse it: one CIP node is one
// instrument. What was wrong was that it refused in a way that sent you looking
// for a duplicate id that was not there.

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
		bad = append(bad, fmt.Sprintf("%v all answer to %q", entries, manifest))
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("rig: %v; a CIP node reports its own id, so two entries "+
		"pointing at one device are one instrument. Give the device a second "+
		"node, or drive the other effect another way", bad)
}
