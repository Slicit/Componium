package rig

import (
	"strings"
	"testing"

	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/instrument"
)

func TestTwoEntriesOnOneDeviceAreNamed(t *testing.T) {
	// What a pair of CIP entries pointing at one board comes back as: two rig
	// ids, one manifest. The conductor used to refuse this with "instrument
	// wind.main already registered", naming an id that appears once in the file.
	b := &Built{Instruments: map[string]instrument.Instrument{
		"wind.main":     virtual.New(instrument.Manifest{ID: "wind.main", Kind: "wind"}),
		"light.ambient": virtual.New(instrument.Manifest{ID: "wind.main", Kind: "wind"}),
	}}
	err := b.Collisions()
	if err == nil {
		t.Fatal("did not notice")
	}
	said := err.Error()
	for _, want := range []string{"light.ambient", "wind.main", "one device"} {
		if !strings.Contains(said, want) {
			t.Errorf("did not mention %q: %s", want, said)
		}
	}
}

func TestAnOrdinaryRigHasNone(t *testing.T) {
	b := &Built{Instruments: map[string]instrument.Instrument{
		"wind.main":     virtual.New(instrument.Manifest{ID: "wind.main", Kind: "wind"}),
		"light.ambient": virtual.New(instrument.Manifest{ID: "light.ambient", Kind: "light"}),
	}}
	if err := b.Collisions(); err != nil {
		t.Errorf("complained about a fine rig: %v", err)
	}
}

func TestAnEmptyRigHasNone(t *testing.T) {
	if err := (&Built{Instruments: map[string]instrument.Instrument{}}).Collisions(); err != nil {
		t.Error(err)
	}
}
