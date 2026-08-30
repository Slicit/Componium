package studio

import (
	"reflect"
	"testing"

	"github.com/Slicit/componium/internal/rig"
)

func demoRig() *rig.Config {
	return &rig.Config{Instruments: []rig.InstConfig{
		{ID: "wind.main", Kind: "wind"},
		{ID: "light.ambient", Kind: "light"},
		{ID: "light.event", Kind: "light"},
		{ID: "shake.seat", Kind: "shake"},
		{ID: "motion.platform", Kind: "motion"},
		{ID: "mist.main", Kind: "mist"},
		{ID: "fog.left", Kind: "fog"},
		{ID: "scent.main", Kind: "scent"},
	}}
}

func TestDeviceArgsNamesTheFoggerTheRigActuallyHas(t *testing.T) {
	// The whole reason this exists: the demo rig's fogger is fog.left, and the
	// composer's fallback would have addressed every smoke cue to fog.main.
	got := deviceArgs(demoRig())
	if !contains(got, "--fog-id", "fog.left") {
		t.Errorf("fogger not passed: %v", got)
	}
}

func TestDeviceArgsCoversEveryKindTheComposerCanAddress(t *testing.T) {
	got := deviceArgs(demoRig())
	for _, pair := range [][2]string{
		{"--wind-id", "wind.main"},
		{"--mist-id", "mist.main"},
		{"--fog-id", "fog.left"},
		{"--scent-id", "scent.main"},
		{"--shake-id", "shake.seat"},
	} {
		if !contains(got, pair[0], pair[1]) {
			t.Errorf("%s %s missing from %v", pair[0], pair[1], got)
		}
	}
}

func TestDeviceArgsIsStableAcrossRuns(t *testing.T) {
	// Built from a map, so without sorting this reshuffles per run and two
	// identical analyses look different in a log.
	first := deviceArgs(demoRig())
	for i := 0; i < 20; i++ {
		if got := deviceArgs(demoRig()); !reflect.DeepEqual(got, first) {
			t.Fatalf("argument order changed between runs:\n%v\n%v", first, got)
		}
	}
}

func TestDeviceArgsKeepsTheRigsOwnPreference(t *testing.T) {
	// An author listing fog.left first is expressing a choice.
	cfg := &rig.Config{Instruments: []rig.InstConfig{
		{ID: "fog.left", Kind: "fog"},
		{ID: "fog.right", Kind: "fog"},
	}}
	if !contains(deviceArgs(cfg), "--fog-id", "fog.left") {
		t.Errorf("the second fogger won: %v", deviceArgs(cfg))
	}
}

func TestDeviceArgsSaysNothingAboutKindsTheRigLacks(t *testing.T) {
	// A rig with no fogger must not be told to address smoke anywhere; the
	// composer's own default is the right answer there.
	cfg := &rig.Config{Instruments: []rig.InstConfig{{ID: "wind.main", Kind: "wind"}}}
	got := deviceArgs(cfg)
	for _, a := range got {
		if a == "--fog-id" {
			t.Errorf("a rig with no fogger produced %v", got)
		}
	}
}

func TestDeviceArgsIgnoresIncompleteEntries(t *testing.T) {
	cfg := &rig.Config{Instruments: []rig.InstConfig{
		{ID: "", Kind: "fog"},
		{ID: "fog.real", Kind: "fog"},
	}}
	if !contains(deviceArgs(cfg), "--fog-id", "fog.real") {
		t.Errorf("an entry with no id was used: %v", deviceArgs(cfg))
	}
}

func TestDeviceArgsWithNoRigSaysNothing(t *testing.T) {
	if got := deviceArgs(nil); got != nil {
		t.Errorf("no rig produced %v", got)
	}
}

func TestLightArgsSeparatesTheWashFromTheFlash(t *testing.T) {
	// Both lights through --light-id would put every lightning strike on the
	// ambient wash, which is a strike that makes the room slightly whiter.
	got := lightArgs(demoRig())
	if !contains(got, "--light-id", "light.ambient") {
		t.Errorf("ambient light wrong: %v", got)
	}
	if !contains(got, "--light-event-id", "light.event") {
		t.Errorf("event light wrong: %v", got)
	}
}

func TestLightArgsWithOneLightAssignsOnlyTheWash(t *testing.T) {
	cfg := &rig.Config{Instruments: []rig.InstConfig{{ID: "light.only", Kind: "light"}}}
	got := lightArgs(cfg)
	if !contains(got, "--light-id", "light.only") {
		t.Errorf("the only light was not used as the wash: %v", got)
	}
	for _, a := range got {
		if a == "--light-event-id" {
			t.Errorf("one light was split into two roles: %v", got)
		}
	}
}

func contains(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
