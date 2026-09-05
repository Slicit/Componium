package cip

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

// Telling a board to replace its own firmware.
//
// The largest thing this protocol can ask for, and the only message that
// replaces the code checking every other message. So it is authenticated twice,
// and the two answer different questions.
//
// The message is signed like every other control message, which says the
// instruction came from somebody holding the secret. That authenticates the
// instruction and nothing else: it names a URL, and whatever answers that URL
// is authenticated by nothing at all. A board that trusted it would run
// whatever the network handed back.
//
// So the message carries an HMAC of the image over the same secret, and the
// board checks it against what actually arrived before the image is made
// bootable. The image is not private and does not need to be, which is why it
// can be fetched over plain HTTP from wherever is convenient. It needs to be
// provably the one that was meant, and that is what the HMAC says.

// UpdateTimeout is how long a node has to accept an update.
//
// Accepting, not finishing. The download outlasts any socket this was sent on,
// so the answer to whether it worked is that the board either comes back
// running something new or comes back running what it had.
const UpdateTimeout = 5 * time.Second

// ImageMAC is the signature a node checks a downloaded image against.
func ImageMAC(secret string, image []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(image)
	return hex.EncodeToString(mac.Sum(nil))
}

// ImageMACOf is the same, for an image on disk rather than in memory.
//
// Streamed, because a firmware image is most of a megabyte and there is no
// reason for the studio to hold one while it hashes it.
func ImageMACOf(secret, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := io.Copy(mac, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Update tells the node to fetch an image and boot it.
//
// url must be reachable from the board, which is not the same as reachable from
// here: a studio on localhost is not somewhere a board on a shelf can go.
//
// Returns when the node has accepted the instruction. It will then stop
// answering for as long as the download and restart take, which is expected and
// is not a failure.
func (c *Client) Update(url, mac string) error {
	if !c.auth.Enabled() {
		// Said here rather than discovered as a silence, and refused for the
		// same reason the node refuses it: an update with no secret is a board
		// that runs whatever anybody sends.
		return fmt.Errorf("cip: updating a node needs its secret")
	}
	if url == "" {
		return fmt.Errorf("cip: no url to update from")
	}
	if len(mac) != sha256.Size*2 {
		return fmt.Errorf("cip: the image signature should be %d hex characters, not %d",
			sha256.Size*2, len(mac))
	}
	if _, err := hex.DecodeString(mac); err != nil {
		return fmt.Errorf("cip: the image signature is not hex: %w", err)
	}

	c.mu.Lock()
	c.seq++
	seq := c.seq
	ch := make(chan string, 1)
	c.acks[seq] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.acks, seq)
		c.mu.Unlock()
	}()

	b, err := Encode(&Message{
		Type: TypeUpdate, Seq: seq, N: c.next(), URL: url, MAC: mac,
	})
	if err != nil {
		return err
	}
	if err := c.send(b); err != nil {
		return err
	}

	select {
	case why := <-ch:
		if why != "" {
			// The node's own words. It refuses an update for reasons worth
			// reading: no secret, a signature that is not a signature, an
			// update already running.
			return fmt.Errorf("cip: %s", why)
		}
		return nil
	case <-time.After(UpdateTimeout):
		// Not retried. Sending it twice could start a second download over the
		// first, and two writers on one flash partition is the fault this
		// project has spent a week not having.
		return fmt.Errorf("cip: no answer to the update within %v", UpdateTimeout)
	}
}
