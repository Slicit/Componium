package cip

import (
	"testing"
)

// A frame carrying every output due this tick.
//
// The reason it exists is timing rather than bandwidth: four datagrams for four
// outputs that must move together arrive at four different times, and one
// datagram arrives once. The tests that matter here are the ones about a frame
// arriving from a stranger, because that is what a UDP port is.

func TestABundleRoundTrips(t *testing.T) {
	in := []Outputs{
		{Index: 0, Values: []float32{0.5}},
		{Index: 3, Values: []float32{1, 0, 0.25}},
		{Index: 7, Values: []float32{}},
	}
	b, err := MarshalBundle(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("%d outputs came back", len(out))
	}
	if out[1].Index != 3 || len(out[1].Values) != 3 || out[1].Values[2] != 0.25 {
		t.Errorf("second output came back as %+v", out[1])
	}
	// An output with nothing to say is still an output, and must not shift the
	// ones after it.
	if out[2].Index != 7 || len(out[2].Values) != 0 {
		t.Errorf("third output came back as %+v", out[2])
	}
}

func TestAnEmptyBundleIsAFrame(t *testing.T) {
	// Nothing due this tick. Sent anyway, because a heartbeat's absence is the
	// message and a curve stream's absence should not be.
	b, err := MarshalBundle(nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalBundle(b)
	if err != nil || len(out) != 0 {
		t.Fatalf("%v, %v", out, err)
	}
}

func TestAnOldFrameStillReads(t *testing.T) {
	/* A 0.2 conductor sends one unnamed output, and a node with one device is
	 * entitled to keep receiving it. It comes back as index 0, because that is
	 * what it addressed. */
	old := MarshalCurve([]float32{0.75, 0.25})
	out, err := UnmarshalBundle(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Index != 0 || out[0].Values[0] != 0.75 {
		t.Errorf("came back as %+v", out)
	}
}

func TestAFrameFromTheFutureIsRefused(t *testing.T) {
	// Refused rather than half understood, which is the rule the whole
	// protocol is versioned for.
	b, _ := MarshalBundle([]Outputs{{Index: 0, Values: []float32{1}}})
	b[2] = 9
	if _, err := UnmarshalBundle(b); err == nil {
		t.Error("read a frame version it does not speak")
	}
}

func TestATruncatedFrameIsRefusedRatherThanRead(t *testing.T) {
	/* This arrives over UDP from whoever can reach the port. A length that
	 * walks off the end of the datagram is the cheapest possible attack on a
	 * device with no memory protection worth the name, and the parser is the
	 * same code on the microcontroller. */
	full, err := MarshalBundle([]Outputs{
		{Index: 0, Values: []float32{1, 2}},
		{Index: 1, Values: []float32{3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for cut := 4; cut < len(full); cut++ {
		if _, err := UnmarshalBundle(full[:cut]); err == nil {
			t.Errorf("read a frame cut to %d of %d bytes", cut, len(full))
		}
	}
}

func TestAFrameThatLiesAboutItsLengths(t *testing.T) {
	// Not truncated: complete, and claiming more than it carries.
	b, _ := MarshalBundle([]Outputs{{Index: 0, Values: []float32{1}}})
	b[5] = 200 // channel count for an output that has one value
	if _, err := UnmarshalBundle(b); err == nil {
		t.Error("believed a channel count the frame could not hold")
	}

	b, _ = MarshalBundle([]Outputs{{Index: 0, Values: []float32{1}}})
	b[3] = 40 // output count for a frame with one
	if _, err := UnmarshalBundle(b); err == nil {
		t.Error("believed an output count the frame could not hold")
	}
}

func TestNotACurveFrameAtAll(t *testing.T) {
	for _, junk := range [][]byte{
		nil, {}, {'C'}, {'C', 'F'}, []byte("hello there"),
	} {
		if _, err := UnmarshalBundle(junk); err == nil {
			t.Errorf("read %q as a curve frame", junk)
		}
	}
}

func TestTooManyToCount(t *testing.T) {
	// The counts are one byte each, which is plenty and is still a limit.
	many := make([]Outputs, 256)
	if _, err := MarshalBundle(many); err == nil {
		t.Error("wrote a frame with 256 outputs")
	}
	wide := []Outputs{{Index: 0, Values: make([]float32, 256)}}
	if _, err := MarshalBundle(wide); err == nil {
		t.Error("wrote an output with 256 channels")
	}
	far := []Outputs{{Index: 300, Values: []float32{1}}}
	if _, err := MarshalBundle(far); err == nil {
		t.Error("wrote an instrument index of 300")
	}
}
