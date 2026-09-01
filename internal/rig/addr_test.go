package rig

import "testing"

// Reported as: the studio refused the ESP32 at http://192.168.1.145/.
//
// It refused a bare IP for having no port, without saying which port, from a
// row that named the driver that decides it. And it would have *accepted* the
// URL, because the check was "contains a colon" and `http:` contains one, then
// written it to the rig and failed at the only moment that matters.

func TestWhatSomebodyActuallyTypes(t *testing.T) {
	for _, c := range []struct {
		typed, driver, want string
	}{
		// The one from the report, copied out of a browser.
		{"http://192.168.1.145/", "cip", "192.168.1.145:5570"},
		{"https://192.168.1.145/", "cip", "192.168.1.145:5570"},
		{"http://192.168.1.145", "cip", "192.168.1.145:5570"},
		{"192.168.1.145/", "cip", "192.168.1.145:5570"},
		// A bare address, which is the commonest thing of all.
		{"192.168.1.145", "cip", "192.168.1.145:5570"},
		{"192.168.1.145", "sacn", "192.168.1.145:5568"},
		{"  192.168.1.145  ", "cip", "192.168.1.145:5570"},
		// A name, not an address. Still a host.
		{"componium-node.local", "cip", "componium-node.local:5570"},
		// Already right, and left alone.
		{"192.168.1.145:5570", "cip", "192.168.1.145:5570"},
		{"192.168.1.145:9999", "cip", "192.168.1.145:9999"},
		{"node:5570", "cip", "node:5570"},
		// IPv6, which is the reason this uses SplitHostPort rather than
		// counting colons.
		{"fe80::1", "cip", "[fe80::1]:5570"},
		{"[fe80::1]:5570", "cip", "[fe80::1]:5570"},
		// Nothing in, nothing out.
		{"", "cip", ""},
		{"   ", "cip", ""},
		{"http://", "cip", ""},
	} {
		if got := NormaliseAddr(c.typed, c.driver); got != c.want {
			t.Errorf("%q on %s became %q, want %q", c.typed, c.driver, got, c.want)
		}
	}
}

func TestAMotionBridgeHasNoPortToAssume(t *testing.T) {
	// It bridges to somebody else's simulator on somebody else's port. Making
	// one up would be worse than asking.
	if got := NormaliseAddr("192.168.1.145", "motion"); got != "192.168.1.145" {
		t.Errorf("invented a port for motion: %q", got)
	}
	if p := DefaultPort("motion"); p != "" {
		t.Errorf("motion offered port %q", p)
	}
}

func TestThePortsComeFromTheDrivers(t *testing.T) {
	// Typing them again here is how they drift apart.
	if DefaultPort("cip") != "5570" {
		t.Errorf("cip port is %q", DefaultPort("cip"))
	}
	if DefaultPort("sacn") != "5568" {
		t.Errorf("sacn port is %q", DefaultPort("sacn"))
	}
}

func TestAnAddressThatIsNotOneIsRefusedWithAWayOut(t *testing.T) {
	c := &Config{Instruments: []InstConfig{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.145"},
	}}
	problems := c.Validate()
	if len(problems) != 1 {
		t.Fatalf("problems: %v", problems)
	}
	// Naming the port it wanted, since the row already said which driver.
	if want := "192.168.1.145:5570"; !contains([]string{problems[0]}, problems[0]) ||
		!containsText(problems[0], want) {
		t.Errorf("did not suggest %s: %s", want, problems[0])
	}
}

func TestAUrlIsNotAnAddress(t *testing.T) {
	// The false accept. This used to pass validation and break the show.
	c := &Config{Instruments: []InstConfig{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "http://192.168.1.145/"},
	}}
	if problems := c.Validate(); len(problems) == 0 {
		t.Fatal("accepted a URL as an address")
	}
}

func TestAPortThatIsNotANumber(t *testing.T) {
	c := &Config{Instruments: []InstConfig{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.145:kettle"},
	}}
	if problems := c.Validate(); len(problems) == 0 {
		t.Fatal("accepted a port named kettle")
	}
}

func containsText(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
