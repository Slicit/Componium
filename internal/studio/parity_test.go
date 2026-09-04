package studio

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Slicit/componium/internal/cip"
)

/* A setting has to survive four layers, and three of four looks like four.
 *
 * The page sends a device to the studio, the studio sends it to the board, the
 * board stores it and announces it, the studio hands the announcement back, and
 * the page fills its table from that. A field carried by every layer but one is
 * a field that can be set and not read, or read and not set, and both of those
 * end the same way: the next write clears it and nothing says so.
 *
 * That has now happened twice, in both directions, which is what a rule is for.
 * These walk the structs rather than listing the fields, so a thirteenth setting
 * added tomorrow fails by existing.
 */

// wireName is the JSON name a field travels under, ignoring the difference
// between the studio's camelCase and the protocol's snake_case, because that is
// a spelling and not a fact about whether the value is carried.
func wireName(tag string) string {
	name := strings.Split(tag, ",")[0]
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func namesOf(t *testing.T, v any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		out[wireName(tag)] = true
	}
	return out
}

func TestTheAdminCanSetEverythingABoardStores(t *testing.T) {
	/* The gap this found: order was on the wire and not on the form, so a strip
	 * with a colour order could be read back and never written, and saving the
	 * table you had just fetched would drop it. */
	wire := namesOf(t, cip.Device{})
	form := namesOf(t, wireDevice{})

	for name := range wire {
		if !form[name] {
			t.Errorf("a board stores %q and the admin cannot set it; a fetched "+
				"configuration would show it and the next write would clear it", name)
		}
	}
}

func TestTheAdminIsShownEverythingABoardAnnounces(t *testing.T) {
	// The other direction. A field the board reports and the studio drops is a
	// field the page fills in from a default that looks like an answer.
	announced := namesOf(t, cip.Instrument{})
	shown := namesOf(t, wireNodeInstrument{})

	// Announced but not part of a configuration: these describe how to drive a
	// device rather than how it is attached, and the table does not edit them.
	notConfiguration := map[string]bool{
		"maxcontinuousms": true,
		"dutycycle":       true,
		"safestate":       true,
		"channels":        true,
	}

	for name := range announced {
		if notConfiguration[name] || shown[name] {
			continue
		}
		t.Errorf("a board announces %q and the studio does not pass it on", name)
	}
}

func TestWhatTheAdminSendsArrivesAsItself(t *testing.T) {
	/* The struct walk above proves the fields exist in both places. This proves
	 * toCIP actually copies them, which is a different mistake and one a field
	 * list cannot catch: adding a field to both structs and forgetting the line
	 * between them. */
	full := wireDevice{
		ID: "light.strip", Type: "ws28xx", GPIO: 27, Kind: "light",
		FreqHz: 18000, Pixels: 60, Active: "low", Order: "rgb",
		LatencyMS: 21, RampUpMS: 1800, RampDownMS: 2900, Safe: 0.25,
	}
	got := full.toCIP()

	// Compared through JSON, so a field that exists on both sides and is never
	// assigned shows up as a zero next to a value.
	a, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var from, to map[string]any
	if err := json.Unmarshal(a, &from); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &to); err != nil {
		t.Fatal(err)
	}

	normalised := map[string]any{}
	for k, v := range to {
		normalised[wireName(k)] = v
	}
	for k, v := range from {
		name := wireName(k)
		got, carried := normalised[name]
		if !carried {
			t.Errorf("%s was set on the form and is not on the wire", name)
			continue
		}
		if !reflect.DeepEqual(v, got) {
			t.Errorf("%s was sent as %v and went out as %v", name, v, got)
		}
	}
}
