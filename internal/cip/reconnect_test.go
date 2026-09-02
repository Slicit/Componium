package cip_test

import (
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

func TestAnAuthenticatedNodeCanBeDialledMoreThanOnce(t *testing.T) {
	/* It could not. The replay guard keeps the highest counter it has seen for
	 * as long as the node runs, and every client began counting at one, so the
	 * second connection of a node's life was refused as a replay: a conductor
	 * restarting, a studio asking a board what it has, a rig reloaded after an
	 * edit. All of them presented as "no hello", which is also what a wrong
	 * secret and an absent board look like.
	 *
	 * Nothing caught it because every other test starts a fresh node. It took
	 * driving a real board twice from the studio. */
	n := startNode(t, cip.NodeConfig{
		Manifest: fanManifest(), Secret: secret, Timeout: 5 * time.Second,
	})

	for attempt := 1; attempt <= 3; attempt++ {
		c, err := cip.Dial(n.Addr(), time.Second, secret)
		if err != nil {
			t.Fatalf("connection %d refused: %v", attempt, err)
		}
		if got := c.Names(); len(got) != 1 {
			t.Fatalf("connection %d announced %v", attempt, got)
		}
		c.Close()
	}
	if n.Rejected() != 0 {
		t.Errorf("the node rejected %d datagrams from a client it should trust",
			n.Rejected())
	}
}
