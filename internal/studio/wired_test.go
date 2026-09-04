package studio

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTheApiPassesOnHowABoardIsWired(t *testing.T) {
	/* The studio is the only thing between the board and the page, and the page
	 * has no other source for a pin number. When this stopped being carried, the
	 * page filled it in from the defaults for a new row, so a strip configured
	 * on gpio 5 read back as a pwm output on gpio 18 and looked exactly like a
	 * board that had forgotten what it was just told.
	 *
	 * The pins below are deliberately not the ones the page defaults to, so
	 * that a value passed through is distinguishable from a value invented.
	 */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`)

	w := do(t, s, "POST", "/api/node",
		`{"board":"bench","configure":true,"devices":[
			{"id":"light.strip","type":"ws28xx","gpio":27,"kind":"light","pixels":60},
			{"id":"wind.main","type":"pwm","gpio":19,"kind":"wind","freqHz":18000}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("configure said %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Instruments []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			GPIO   *int   `json:"gpio"`
			Pixels int    `json:"pixels"`
			FreqHz int    `json:"freqHz"`
		} `json:"instruments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Instruments) != 2 {
		t.Fatalf("came back with %d instruments: %s", len(got.Instruments), w.Body.String())
	}

	strip := got.Instruments[0]
	if strip.Type != "ws28xx" {
		t.Errorf("the strip came back as type %q", strip.Type)
	}
	if strip.GPIO == nil || *strip.GPIO != 27 {
		t.Errorf("the strip came back on gpio %v, not the 27 it was given", strip.GPIO)
	}
	if strip.Pixels != 60 {
		t.Errorf("pixels came back as %d", strip.Pixels)
	}

	fan := got.Instruments[1]
	if fan.Type != "pwm" || fan.GPIO == nil || *fan.GPIO != 19 || fan.FreqHz != 18000 {
		t.Errorf("the fan came back as %+v", fan)
	}
}

func TestABoardThatSaysNothingAboutWiringOmitsIt(t *testing.T) {
	/* A node with a compiled-in manifest has no pins to report. It must leave
	 * them out rather than send zeroes, because gpio 0 is a real pin and the
	 * page needs to be able to say "this board did not tell us" instead of
	 * showing a number that looks like an answer. */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`)

	w := do(t, s, "POST", "/api/node", `{"board":"bench"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("ask said %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Instruments []map[string]any `json:"instruments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Instruments) == 0 {
		t.Fatal("the node announced nothing at all")
	}
	for _, in := range got.Instruments {
		if _, said := in["gpio"]; said {
			t.Errorf("a node with no configuration reported a gpio: %v", in)
		}
		if _, said := in["type"]; said {
			t.Errorf("a node with no configuration reported a type: %v", in)
		}
	}
}
