package cip

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

/* The two implementations of this protocol, held to each other.
 *
 * There are two: a node in Go that every other test exercises, and firmware in
 * C that is the one people actually plug in. Go talking to Go agrees with
 * itself perfectly, which is why four separate faults this week were only ever
 * wrong on the board: a counter too precise to survive JSON, a configuration
 * that could be written and not read, a stack that could not hold a reply, and
 * a stop the firmware did not recognise.
 *
 * That last one is what these are for. A span ends when the conductor sends a
 * cue whose action is a stop, and the cue carries no values, so an
 * implementation that reads only the parameters leaves its output exactly where
 * it was. The light stayed on until something else happened to it, which from
 * the room looks like an off that arrives whenever it likes.
 *
 * Reading the C source is a blunt instrument and it is the only one available
 * without a board on the desk. It is aimed at the two things that were actually
 * wrong: which actions count, and whether the cue path asks at all.
 */

func firmware(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "firmware", "esp32", "main", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no firmware source here: %v", err)
	}
	return string(b)
}

func TestBothImplementationsEndASpanOnTheSameActions(t *testing.T) {
	// What Go accepts, asked of the function rather than of a list.
	var mine []string
	for _, a := range []string{
		"stop", "off", "safe", "neutral",
		"flash", "gust", "spray", "set", "", "stopping",
	} {
		if instrumentStop(a) {
			mine = append(mine, a)
		}
	}
	sort.Strings(mine)

	src := firmware(t, "devices.c")
	body := src[strings.Index(src, "bool device_action_stops"):]
	found := regexp.MustCompile(`strcmp\(action, "([a-z]+)"\)`).FindAllStringSubmatch(body, -1)

	var theirs []string
	for _, m := range found {
		theirs = append(theirs, m[1])
	}
	sort.Strings(theirs)

	if strings.Join(mine, ",") != strings.Join(theirs, ",") {
		t.Errorf("the software node ends a span on %v and the firmware on %v;\n"+
			"an action only one of them knows is a span that only one of them ends",
			mine, theirs)
	}
}

func TestTheFirmwareCuePathAsksWhetherTheActionStops(t *testing.T) {
	/* The wiring, which is what was missing. device_action_stops existed as a
	 * correct function would and the cue handler never called it, so every stop
	 * fell through to the parameter loop, found nothing, and left the output on.
	 *
	 * Checked by reading the source because the alternative is a board. Crude,
	 * and it fails when somebody deletes the one line that mattered. */
	src := firmware(t, "componium_node.c")

	cue := strings.Index(src, `strcmp(type->valuestring, "cue")`)
	if cue < 0 {
		t.Fatal("no cue handler in the firmware")
	}
	// As far as the next branch, so this is about the cue case and not the file.
	rest := src[cue:]
	if end := strings.Index(rest, `strcmp(type->valuestring, "configure")`); end > 0 {
		rest = rest[:end]
	}

	if !strings.Contains(rest, "device_action_stops") {
		t.Error("the firmware's cue handler never asks whether the action ends " +
			"the span, so a stop carries no values, changes nothing, and the " +
			"output stays where it was")
	}
	// And the asking has to come before the parameters are read, because a stop
	// carries none and reading them first is how it stayed on.
	if asks, reads := strings.Index(rest, "device_action_stops"), strings.Index(rest, `rgb[3]`); asks > 0 && reads > 0 && asks > reads {
		t.Error("the firmware reads a cue's parameters before asking whether it " +
			"is a stop; a stop has none, so the output keeps its old values")
	}
}
