package rig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rig.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAndBuild(t *testing.T) {
	p := write(t, `
[rig]
name = "living room"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "virtual"
latency = "1.2s"

[[instrument]]
id = "light.ambient"
kind = "light"
driver = "sacn"
latency = "20ms"
universe = 1
start = 10
mode = "rgb"
addr = "127.0.0.1:15568"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rig.Name != "living room" {
		t.Errorf("name %q", c.Rig.Name)
	}
	built, err := c.Build()
	if err != nil {
		t.Fatal(err)
	}
	defer built.Close()

	if len(built.Instruments) != 2 {
		t.Fatalf("%d instruments, want 2", len(built.Instruments))
	}
	if got := built.Instruments["wind.main"].Manifest().Latency; got != 1200*time.Millisecond {
		t.Errorf("wind latency %v", got)
	}
	if got := built.Instruments["light.ambient"].Manifest().Kind; got != "light" {
		t.Errorf("light kind %q", got)
	}
}

// A rig that quietly pretends to drive hardware is worse than one that refuses
// to start.
func TestUnknownDriverIsRefused(t *testing.T) {
	p := write(t, "[[instrument]]\nid = \"x\"\ndriver = \"telepathy\"\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Build(); err == nil {
		t.Error("unknown driver accepted")
	}
}

func TestRejectsDuplicateAndNamelessInstruments(t *testing.T) {
	dup := write(t, "[[instrument]]\nid = \"x\"\n[[instrument]]\nid = \"x\"\n")
	if _, err := Load(dup); err == nil {
		t.Error("duplicate id accepted")
	}
	none := write(t, "[[instrument]]\nkind = \"wind\"\n")
	if _, err := Load(none); err == nil {
		t.Error("instrument without an id accepted")
	}
	empty := write(t, "[rig]\nname = \"x\"\n")
	if _, err := Load(empty); err == nil {
		t.Error("rig with no instruments accepted")
	}
}

func TestBadSacnConfigFailsAtBuild(t *testing.T) {
	p := write(t, "[[instrument]]\nid = \"l\"\ndriver = \"sacn\"\nstart = 0\nmode = \"rgb\"\n")
	c, _ := Load(p)
	if _, err := c.Build(); err == nil {
		t.Error("DMX start address 0 accepted, but DMX is 1 based")
	}
}
