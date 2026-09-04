package rig

import (
	"net"
	"strconv"
	"strings"

	"github.com/Slicit/componium/instruments/sacn"
	"github.com/Slicit/componium/internal/cip"
)

// What somebody types, and what a socket needs.
//
// These are not the same thing and pretending they are produced two bugs at
// once. A person told to put in the address of a device they just found in a
// browser types what the browser showed them: `http://192.168.1.145/`. The
// first version of the check asked whether the string contained a colon, so
// that sailed through, got written to the rig, and failed at the only moment
// that matters, which is when the show starts and the driver tries to dial it.
// Meanwhile a bare `192.168.1.145` was refused outright, told it needed a port,
// and not told which one, by a studio that knew perfectly well: the driver
// decides the port and the driver was right there in the same row.
//
// So: forgiving on the way in, strict on the way out. Normalise first, then
// validate what came of it.

// DefaultPort is where a driver expects to find its device, or empty when the
// driver has no convention. Read from the drivers themselves rather than typed
// again here, so a port that moves moves everywhere at once.
func DefaultPort(driver string) string {
	switch driver {
	case "cip":
		return strconv.Itoa(cip.Port)
	case "sacn":
		return strconv.Itoa(sacn.Port)
	}
	// Motion bridges to somebody else's simulator on somebody else's port.
	// There is no convention to offer, so it stays required.
	return ""
}

// NormaliseAddr turns what a person plausibly typed into a host and a port.
//
// It removes a scheme and a path, because the address of a device is very often
// first met as a URL, and it supplies the driver's port when none was given.
// What it will not do is guess at a host: an address it cannot make sense of
// comes back unchanged, so that Validate can say so rather than this quietly
// inventing something.
func NormaliseAddr(addr, driver string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}
	for _, scheme := range []string{"http://", "https://", "udp://", "tcp://", "//"} {
		a = strings.TrimPrefix(a, scheme)
	}
	// Anything from the first slash on is a path, and a path is not part of an
	// address. `192.168.1.145/` and `192.168.1.145/api` are both the same host.
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a = a[:i]
	}
	a = strings.TrimSpace(a)
	if a == "" {
		return ""
	}
	// Already a host and a port, including the bracketed IPv6 spelling.
	if _, _, err := net.SplitHostPort(a); err == nil {
		return a
	}
	if port := DefaultPort(driver); port != "" {
		return net.JoinHostPort(a, port)
	}
	return a
}

// AddrProblem describes what is wrong with an address, or returns empty.
//
// Exported because a board is reached the same way an instrument is, and two
// copies of "is this an address" would agree with each other until one of them
// was fixed.
func AddrProblem(addr, driver string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if want := DefaultPort(driver); want != "" {
			return "address " + quoted(addr) + " is not host:port, try " + addr + ":" + want
		}
		return "address " + quoted(addr) + " is not host:port"
	}
	if host == "" {
		return "address " + quoted(addr) + " has no host"
	}
	// A host that is neither an IP nor something that could be a name. Worth
	// saying because NormaliseAddr will cheerfully append a port to any text at
	// all, so "not an address" arrives here as "not an address:5570" and looks
	// structurally fine.
	if !hostLooksReal(host) {
		return "address " + quoted(addr) + " does not name a host"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "port " + quoted(port) + " is not a port number"
	}
	return ""
}

// hostLooksReal reports whether a host is an IP or a plausible name.
//
// Deliberately loose: this is not a resolver and a name that does not exist yet
// is a normal thing to type. It only refuses what cannot be a host at all,
// which in practice means text somebody pasted from somewhere else.
func hostLooksReal(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return host != ""
}
