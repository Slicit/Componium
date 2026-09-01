package rig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A rig file decides what is on the end of every wire. Until the studio could
// edit one, nothing in this project had ever written one, so the encoder had
// never been asked to agree with the decoder. It did not.

func good() *Config {
	return &Config{
		Rig: Meta{Name: "bench"},
		Instruments: []InstConfig{
			{ID: "light.ambient", Kind: "light", Driver: "sacn",
				Addr: "192.168.1.90:5568", Universe: 1, Start: 1, Mode: "rgb",
				Latency: Duration(20 * time.Millisecond)},
			{ID: "wind.main", Kind: "wind", Driver: "cip",
				Addr: "192.168.1.91:5570", Latency: Duration(1200 * time.Millisecond)},
			{ID: "fog.left", Kind: "fog", Driver: "virtual",
				Position: &Position{X: -1.6, Y: 0.15, Z: 1.0}},
		},
	}
}

func TestARigSurvivesBeingWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Instruments) != 3 || back.Rig.Name != "bench" {
		t.Fatalf("came back as %+v", back.Rig)
	}
	first := back.Instruments[0]
	if first.ID != "light.ambient" || first.Universe != 1 || first.Start != 1 {
		t.Errorf("sacn fields lost: %+v", first)
	}
	// The one that could not have worked before. A duration encoded as an
	// integer of nanoseconds is not a duration UnmarshalText will read.
	if got := first.Latency.Duration(); got != 20*time.Millisecond {
		t.Errorf("latency came back as %v", got)
	}
	if back.Instruments[2].Position == nil || back.Instruments[2].Position.X != -1.6 {
		t.Errorf("position lost: %+v", back.Instruments[2])
	}
}

func TestEmptyFieldsDoNotBecomeZeroes(t *testing.T) {
	// A virtual fogger has no universe and no DMX start address. Writing them
	// as 0 makes a file that says the fogger is at DMX address 0, which is not
	// an address, and Validate would then refuse the file it just wrote.
	path := filepath.Join(t.TempDir(), "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	fog := text[strings.Index(text, "fog.left"):]
	for _, unwanted := range []string{"universe", "start", "mode", "secret"} {
		if strings.Contains(fog, unwanted) {
			t.Errorf("the virtual fogger was given a %s:\n%s", unwanted, fog)
		}
	}
}

func TestItSaysEverythingWrongAtOnce(t *testing.T) {
	// Somebody editing six instruments wants the list, not a game where fixing
	// one reveals the next.
	bad := &Config{Instruments: []InstConfig{
		{ID: "", Kind: "light"},
		{ID: "a.one", Kind: "nonsense"},
		{ID: "a.one", Kind: "light", Driver: "sacn", Start: 0},
		{ID: "b.one", Kind: "wind", Driver: "sacn"},
		{ID: "c.one", Kind: "wind", Driver: "cip"},
	}}
	problems := Validate(bad)
	if len(problems) < 5 {
		t.Fatalf("found only %d problems: %v", len(problems), problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"needs an id", "unknown kind", "named twice",
		"start address", "cannot be driven by", "needs an address",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("did not report %q:\n%s", want, joined)
		}
	}
}

func TestARigThatWouldNotLoadIsNotWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rig.toml")
	if err := Save(path, good()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	broken := good()
	broken.Instruments[0].Start = 0
	if err := Save(path, broken); err == nil {
		t.Fatal("saved a rig with an invalid DMX address")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a refused save still changed the file")
	}
	// And left nothing behind to be mistaken for a rig.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left %d files in the directory", len(entries))
	}
}

func TestWhichDriversSuitWhichKinds(t *testing.T) {
	// sACN builds a DMX light and nothing else. Offering it for a fogger is
	// offering a rig that will not start.
	if got := DriversFor("light"); !contains(got, "sacn") {
		t.Errorf("light cannot be sacn: %v", got)
	}
	if got := DriversFor("fog"); contains(got, "sacn") {
		t.Errorf("fog was offered sacn: %v", got)
	}
	if got := DriversFor("nonsense"); len(got) != 0 {
		t.Errorf("an unknown kind was offered %v", got)
	}
	// Every kind can at least be virtual, which is what makes a half built
	// rig a normal state rather than a broken one.
	for _, kind := range Kinds() {
		if !contains(DriversFor(kind), "virtual") {
			t.Errorf("%s cannot be virtual", kind)
		}
	}
}

// Validate is a method; this is here so the test above reads as a sentence.
func Validate(c *Config) []string { return c.Validate() }
