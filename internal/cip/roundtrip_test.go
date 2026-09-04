package cip_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* Everything a board can be told, it can be asked.
 *
 * Written as a walk over the fields rather than a list of assertions, because
 * the failure this guards against is somebody adding a thirteenth setting and
 * carrying it in three of the four places it has to travel. A test naming the
 * twelve that exist today would pass for ever and say nothing about the
 * thirteenth.
 *
 * A field that does not come back is worse than a field that is never shown: it
 * reads as empty, and the next write sets it to empty. Fetching a fan with an
 * 1800ms ramp and pressing write would flatten it.
 */

// everything is a device with no field left at its default, so that a value
// that fails to survive is distinguishable from one that was never set.
func everything() []cip.Device {
	return []cip.Device{
		{
			ID: "wind.main", Type: cip.DevicePWM, GPIO: 19, Kind: "wind",
			FreqHz:     18000,
			LatencyMS:  1234,
			RampUpMS:   1800,
			RampDownMS: 2900,
			Safe:       0.25,
		},
		{
			ID: "light.strip", Type: cip.DeviceWS28xx, GPIO: 27, Kind: "light",
			Pixels:    60,
			Order:     "rgb",
			LatencyMS: 21,
		},
		{
			ID: "fog.left", Type: cip.DeviceRelay, GPIO: 23, Kind: "fog",
			Active:    "low",
			LatencyMS: 2100,
			Safe:      1,
		},
	}
}

func TestEveryConfiguredFieldComesBack(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Secret: secret, Timeout: 5 * time.Second})
	c, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	sent := everything()
	if err := c.Configure(sent); err != nil {
		t.Fatal(err)
	}

	for _, want := range sent {
		d, ok := c.Device(want.ID)
		if !ok {
			t.Fatalf("%s did not come back at all", want.ID)
		}
		got := d.Wiring()

		/* Field by field, by name, from the configuration's own struct. Adding
		 * a setting to Device and forgetting to announce it fails here without
		 * anybody having to remember to extend this list. */
		wv := reflect.ValueOf(want)
		gv := reflect.ValueOf(got)
		for i := 0; i < wv.NumField(); i++ {
			name := wv.Type().Field(i).Name
			set := wv.Field(i)
			if set.IsZero() {
				// Not configured, so nothing is claimed about it.
				continue
			}
			back := gv.FieldByName(name)
			if !back.IsValid() {
				t.Errorf("%s: %s is configurable and is not announced at all",
					want.ID, name)
				continue
			}
			if !reflect.DeepEqual(set.Interface(), back.Interface()) {
				t.Errorf("%s: %s was set to %v and came back as %v",
					want.ID, name, set.Interface(), back.Interface())
			}
		}
	}
}

func TestTheAnnouncementCoversTheConfiguration(t *testing.T) {
	/* The same guarantee stated structurally: every field a Device has, an
	 * Instrument has too. It catches the case the walk above cannot, which is a
	 * field nobody has thought to set in a test yet. */
	dev := reflect.TypeOf(cip.Device{})
	ann := reflect.TypeOf(cip.Instrument{})

	for i := 0; i < dev.NumField(); i++ {
		name := dev.Field(i).Name
		if _, ok := ann.FieldByName(name); !ok {
			t.Errorf("Device.%s can be configured and cannot be read back; "+
				"a fetched configuration would show it empty and the next "+
				"write would clear it", name)
		}
	}
}
