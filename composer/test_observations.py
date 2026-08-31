"""What gets written down about what the model saw.

The times are the whole of it. A chunk sees the film through its own clock and
everything it produces has to be moved into the film's before it leaves —
tracks go through span.place(), and this file did not, so nine chunks of a two
hour film recorded their frames as nine overlapping copies of the first fifteen
minutes. The scores were fine, because cues are tracks. The description was
not, and the description is what a rebuild reuses.
"""

import json
import os
import tempfile
import unittest

import compose
import span as span_mod


class Args:
    def __init__(self, out):
        self.out = out


def written(observations, span):
    with tempfile.TemporaryDirectory() as tmp:
        out = os.path.join(tmp, "film.componium")
        name = compose.write_observations(Args(out), observations, span)
        if not name:
            return []
        with open(out + ".seen.jsonl", encoding="utf-8") as f:
            return [json.loads(line) for line in f if line.strip()]


class TestObservationTimes(unittest.TestCase):
    def test_a_whole_film_is_written_as_it_was_seen(self):
        span = span_mod.Span(0.0, 0.0, 0.0)
        rows = written([{"t": 5.0, "labels": ["dust"], "seen": "Sand."}], span)
        self.assertEqual([r["t"] for r in rows], [5.0])

    def test_a_chunk_is_moved_into_the_film_clock(self):
        # The fault. A chunk starting an hour in sees its own frame five
        # seconds along; the film has that frame an hour and five seconds in.
        span = span_mod.Span(3600.0, 4500.0, 0.0)
        rows = written([{"t": 5.0, "labels": ["dust"], "seen": "Sand."}], span)
        self.assertEqual([r["t"] for r in rows], [3605.0])

    def test_two_chunks_do_not_land_on_top_of_each_other(self):
        # What the merge produced: every chunk piled into the first
        # chunk-length, so a two hour film reported fifteen minutes of
        # observations several times over.
        first = written([{"t": 10.0, "labels": [], "seen": "a"}],
                        span_mod.Span(0.0, 900.0, 0.0))
        second = written([{"t": 10.0, "labels": [], "seen": "b"}],
                         span_mod.Span(900.0, 1800.0, 0.0))
        self.assertNotEqual(first[0]["t"], second[0]["t"])
        self.assertEqual(second[0]["t"], 910.0)

    def test_the_lead_in_is_dropped(self):
        # A chunk decodes from before its range so motion has something to
        # compare against. Those frames belong to the chunk before it, and
        # keeping them would report the same moment twice.
        span = span_mod.Span(900.0, 1800.0, 4.0)
        rows = written([
            {"t": 1.0, "labels": [], "seen": "in the lead"},
            {"t": 10.0, "labels": [], "seen": "in the range"},
        ], span)
        self.assertEqual([r["seen"] for r in rows], ["in the range"])

    def test_what_the_model_said_is_kept_whole(self):
        span = span_mod.Span(0.0, 0.0, 0.0)
        rows = written([{"t": 1.0, "labels": ["dust", "water"], "seen": "Sand."}], span)
        self.assertEqual(rows[0]["labels"], ["dust", "water"])
        self.assertEqual(rows[0]["seen"], "Sand.")

    def test_a_frame_that_saw_nothing_is_still_a_frame(self):
        # Knowing the model was shown a moment and had nothing to say is not
        # the same as never having shown it.
        span = span_mod.Span(0.0, 0.0, 0.0)
        rows = written([{"t": 1.0, "labels": [], "seen": ""}], span)
        self.assertEqual(len(rows), 1)

    def test_nothing_seen_writes_no_file(self):
        self.assertEqual(written([], span_mod.Span(0.0, 0.0, 0.0)), [])


if __name__ == "__main__":
    unittest.main()
