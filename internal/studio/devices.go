package studio

import (
	"sort"

	"github.com/Slicit/componium/internal/rig"
)

// deviceArgs tells the composer which instrument to address each kind of
// effect to, taken from the rig the studio is actually holding.
//
// Without this the composer falls back to "<kind>.main" for anything it has no
// flag for, and the demo rig calls its fogger fog.left. So every smoke cue the
// analysis produced named a device that does not exist — silently, because a
// score is a proposal and nothing checks a proposal against a rig until
// somebody tries to play it.
//
// Only kinds the composer has a flag for are sent. Passing --motion-id is
// handled separately because the analysis is what decides whether there is a
// motion track at all.
func deviceArgs(cfg *rig.Config) []string {
	if cfg == nil {
		return nil
	}

	flags := map[string]string{
		"wind":  "--wind-id",
		"mist":  "--mist-id",
		"fog":   "--fog-id",
		"scent": "--scent-id",
		"shake": "--shake-id",
	}

	// First one wins per kind, and the rig's own order decides which that is —
	// an author listing fog.left before fog.right is expressing a preference,
	// and re-sorting the instruments here would silently overrule it.
	chosen := map[string]string{}
	for _, inst := range cfg.Instruments {
		if inst.ID == "" || inst.Kind == "" {
			continue
		}
		if _, ok := flags[inst.Kind]; !ok {
			continue
		}
		if _, taken := chosen[inst.Kind]; !taken {
			chosen[inst.Kind] = inst.ID
		}
	}

	// Sorted by kind so the command line is the same every run. An argument
	// list that reshuffles itself makes two identical analyses look different
	// in a log, and makes this impossible to assert on.
	kinds := make([]string, 0, len(chosen))
	for kind := range chosen {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	out := make([]string, 0, len(kinds)*2)
	for _, kind := range kinds {
		out = append(out, flags[kind], chosen[kind])
	}
	return out
}

// lightArgs is separate because a rig has two lights doing different jobs.
//
// The ambient light is the wash the whole room sits in and the event light is
// what a flash comes out of. Handing both to --light-id would put every
// lightning strike on whichever one came first in the file, which for the demo
// rig is the ambient wash — a strike lighting the room by turning the
// background a bit whiter.
func lightArgs(cfg *rig.Config) []string {
	if cfg == nil {
		return nil
	}
	var ambient, event string
	for _, inst := range cfg.Instruments {
		if inst.Kind != "light" || inst.ID == "" {
			continue
		}
		// By convention the id says which is which; falling back to order
		// only when it does not.
		switch {
		case ambient == "" && inst.ID != "light.event":
			ambient = inst.ID
		case event == "" && inst.ID != ambient:
			event = inst.ID
		}
	}
	var out []string
	if ambient != "" {
		out = append(out, "--light-id", ambient)
	}
	if event != "" {
		out = append(out, "--light-event-id", event)
	}
	return out
}
