package cip

import (
	"encoding/json"
	"math"
	"testing"
)

/* The replay counter, as the other end actually receives it.
 *
 * Inside this package, because the thing worth testing is unexported and
 * because the bug it exists for was invisible from outside: Go decodes `n` into
 * a uint64 and gets every bit, so a Go client talking to a Go node agreed with
 * itself perfectly while the firmware silently refused every message after the
 * first.
 *
 * The firmware parses with cJSON, which like every JSON parser hands numbers
 * back as a double. So the honest test is not "is the counter increasing" but
 * "is it still increasing after it has been a double".
 */

func TestTheCounterStaysDistinctThroughAJSONNumber(t *testing.T) {
	c := &Client{auth: NewAuth("a secret")}

	seen := map[uint64]uint64{}
	for i := 0; i < 16; i++ {
		n := c.next()
		// What a parser reading a JSON number gives back.
		through := uint64(float64(n))
		if was, clash := seen[through]; clash {
			t.Fatalf("counters %d and %d are the same number once they have been "+
				"a double (%d); the second message of every connection would be "+
				"refused as a replay", was, n, through)
		}
		seen[through] = n
	}
}

func TestTheCounterIsSmallEnoughToBeExact(t *testing.T) {
	/* 2^53 is where a double stops being able to count. Above it the increments
	 * this whole mechanism relies on stop existing, and nothing says so: the
	 * arithmetic keeps working and the values quietly stop changing. */
	const exact = uint64(1) << 53

	c := &Client{auth: NewAuth("a secret")}
	n := c.next()
	if n >= exact {
		t.Fatalf("counter %d is past 2^53 (%d), so it cannot survive a JSON number", n, exact)
	}
	if float64(n) != math.Trunc(float64(n)) || uint64(float64(n)) != n {
		t.Errorf("counter %d does not round trip through a double", n)
	}
}

func TestTheCounterSurvivesTheRealEncoder(t *testing.T) {
	/* Not a model of the wire format: the wire format. Encoded by the code that
	 * encodes it, decoded the way a parser holding doubles decodes it. */
	c := &Client{auth: NewAuth("a secret")}

	var through []uint64
	for i := 0; i < 4; i++ {
		b, err := Encode(&Message{Type: TypeHello, N: c.next()})
		if err != nil {
			t.Fatal(err)
		}
		// A parser with no int64: what cJSON does, and what JavaScript does.
		var loose struct {
			N float64 `json:"n"`
		}
		if err := json.Unmarshal(b, &loose); err != nil {
			t.Fatal(err)
		}
		through = append(through, uint64(loose.N))
	}

	for i := 1; i < len(through); i++ {
		if through[i] <= through[i-1] {
			t.Fatalf("counter %d did not increase past %d after the trip through a "+
				"double: a node would refuse it as a replay",
				through[i], through[i-1])
		}
	}
}

func TestAnUnauthenticatedClientHasNoCounter(t *testing.T) {
	// Nothing to replay guard, and a counter on the wire would only be noise.
	c := &Client{}
	if n := c.next(); n != 0 {
		t.Errorf("counter %d on a client with no secret", n)
	}
}
