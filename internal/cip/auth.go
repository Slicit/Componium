package cip

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
)

// TagLen is the length of the authentication tag prefixed to every datagram
// when a secret is configured.
//
// 16 bytes of HMAC-SHA256 is 128 bits, which is far beyond what an attacker on
// a home LAN could brute force and keeps the overhead off a 50Hz curve stream.
const TagLen = 16

// Auth authenticates datagrams with a pre-shared secret.
//
// This is authentication, not encryption. Anyone on the network can still read
// what the rig is doing; they cannot make it do anything. That is the right
// trade for this protocol: the content is a fan speed, and the risk is somebody
// turning on a fog machine.
//
// The tag is a prefix on the raw datagram rather than a field inside the JSON,
// so that verifying it means hashing bytes rather than canonicalising a
// document. That matters because the other implementation is C on a
// microcontroller, and re-serialising JSON to check a signature there would be
// both slow and easy to get subtly wrong.
type Auth struct {
	secret []byte
}

// NewAuth returns an Auth, or nil when no secret is configured. A nil *Auth is
// usable and passes everything through unchanged, so callers do not need to
// branch on whether security is on.
func NewAuth(secret string) *Auth {
	if secret == "" {
		return nil
	}
	return &Auth{secret: []byte(secret)}
}

// Wrap prefixes body with its authentication tag.
func (a *Auth) Wrap(body []byte) []byte {
	if a == nil {
		return body
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(body)
	sum := mac.Sum(nil)

	out := make([]byte, TagLen+len(body))
	copy(out, sum[:TagLen])
	copy(out[TagLen:], body)
	return out
}

// Unwrap verifies and strips the tag.
//
// A datagram that fails verification is rejected outright rather than logged
// and processed, and the comparison is constant time.
func (a *Auth) Unwrap(datagram []byte) ([]byte, error) {
	if a == nil {
		return datagram, nil
	}
	if len(datagram) < TagLen {
		return nil, fmt.Errorf("cip: datagram too short to be authenticated")
	}
	body := datagram[TagLen:]
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(body)
	sum := mac.Sum(nil)

	if subtle.ConstantTimeCompare(sum[:TagLen], datagram[:TagLen]) != 1 {
		return nil, fmt.Errorf("cip: bad authentication tag")
	}
	return body, nil
}

// Enabled reports whether a secret is configured.
func (a *Auth) Enabled() bool { return a != nil }

// replayGuard rejects control messages whose counter has been seen before.
//
// Without it, an attacker who cannot forge a tag can still record a valid
// "gust at full intensity" and send it again whenever they like. Curve frames
// are deliberately not covered: they carry no counter, and a replayed one is
// superseded 20ms later by the next genuine frame.
type replayGuard struct {
	highest uint64
}

// accept reports whether a counter is new, and records it.
//
// Zero is accepted always, so that a peer with no secret and no counter still
// works and the guard is inert when authentication is off.
func (g *replayGuard) accept(counter uint64) bool {
	if counter == 0 {
		return true
	}
	if counter <= g.highest {
		return false
	}
	g.highest = counter
	return true
}
