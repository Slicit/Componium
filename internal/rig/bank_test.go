package rig

import "testing"

func bank() InstConfig {
	return InstConfig{
		ID:   "scent.main",
		Kind: "scent",
		Scents: map[string]string{
			"1": "smoke",
			"2": "petrichor",
			"3": " Sea ",
		},
	}
}

func TestScentResolvesToAReservoir(t *testing.T) {
	got, ok := bank().Scent("petrichor")
	if !ok || got != 2 {
		t.Errorf("petrichor resolved to %d, %v", got, ok)
	}
}

func TestScentIgnoresCaseAndSpace(t *testing.T) {
	// The table is written by hand by somebody filling bottles.
	for _, name := range []string{"sea", "SEA", " Sea "} {
		if got, ok := bank().Scent(name); !ok || got != 3 {
			t.Errorf("%q resolved to %d, %v", name, got, ok)
		}
	}
}

func TestAScentThisRigDoesNotHoldIsNotFired(t *testing.T) {
	// Not approximated. An approximation of a smell is a different smell, and
	// the failure mode is a room that cannot be aired out for twenty minutes.
	if _, ok := bank().Scent("gunpowder"); ok {
		t.Error("a rig without gunpowder offered a reservoir for it")
	}
}

func TestAnEmptyNameIsNotAScent(t *testing.T) {
	if _, ok := bank().Scent(""); ok {
		t.Error("an unnamed scent resolved to something")
	}
	if _, ok := bank().Scent("   "); ok {
		t.Error("a blank scent resolved to something")
	}
}

func TestAnUnfilledRigHoldsNothing(t *testing.T) {
	// A scent instrument with no bank declared is one nobody has filled, and
	// the honest response is silence rather than reservoir one.
	empty := InstConfig{ID: "scent.main", Kind: "scent"}
	if empty.Holds() {
		t.Error("an instrument with no bank said it holds something")
	}
	if _, ok := empty.Scent("smoke"); ok {
		t.Error("an unfilled rig offered a reservoir")
	}
}

func TestAReservoirThatIsNotANumberIsIgnored(t *testing.T) {
	// Rather than firing whatever atoi made of it.
	odd := InstConfig{Scents: map[string]string{"left": "smoke"}}
	if _, ok := odd.Scent("smoke"); ok {
		t.Error("a non-numeric reservoir was offered")
	}
}

func TestReservoirZeroIsNotAReservoir(t *testing.T) {
	odd := InstConfig{Scents: map[string]string{"0": "smoke"}}
	if _, ok := odd.Scent("smoke"); ok {
		t.Error("reservoir zero was offered")
	}
}
