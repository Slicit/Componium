package rig

import (
	"strings"
	"testing"

	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/instrument"
)

func TestTwoEntriesOnOneDeviceAreNamed(t *testing.T) {
	/* Two rig ids resolving to one instrument.
	 *
	 * This used to be what pointing two entries at one CIP board produced,
	 * because the board reported one manifest and both entries adopted it.
	 * Since ADR 0007 a board carries several devices and two entries at one
	 * address are ordinary; what is left is the narrower fault of two ids
	 * naming one device, where the second silently does nothing. */
	b := &Built{Instruments: map[string]instrument.Instrument{
		"wind.main":     virtual.New(instrument.Manifest{ID: "wind.main", Kind: "wind"}),
		"light.ambient": virtual.New(instrument.Manifest{ID: "wind.main", Kind: "wind"}),
	}}
	err := b.Collisions()
	if err == nil {
		t.Fatal("did not notice")
	}
	said := err.Error()
	for _, want := range []string{"light.ambient", "wind.main", "name a different instrument"} {
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
