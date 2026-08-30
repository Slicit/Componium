"""Rebuilding conclusions without looking at the film again.

What remap touches and what it leaves alone is the whole design, so most of
these are about what it does NOT change.
"""

import io
import json
import os
import tempfile
import unittest

import remap


def score_with(*tracks) -> dict:
    return {
        "score": {"componium": "0.1", "title": "test",
                  "media": {"duration": "00:10:00.000"}},
        "track": list(tracks),
    }


def cue(t, action, source=None, output=0.5):
    out = {"t": t, "action": action, "params": {"output": output}, "duration": "3.0s"}
    if source:
        out["source"] = source
    return out


MAPPING = {
    "smoke": [{"kind": "fog", "action": "burst", "params": {"output": 0.6}, "duration": 4.0}],
}
KINDS = {"fog": "fog.left"}


def observed(*rows):
    return [{"t": t, "labels": list(labels), "seen": seen}
            for t, labels, seen in rows]


class TestRemap(unittest.TestCase):
    def test_rebuilds_the_cues_a_model_proposed(self):
        score = score_with({"instrument": "fog.left", "type": "cue",
                            "cues": [cue("00:00:05.000", "burst", "vision: smoke")]})
        out = remap.remap(score, observed((100.0, ["smoke"], "Smoke in a street.")),
                          MAPPING, KINDS)
        cues = out["track"][0]["cues"]
        self.assertEqual(len(cues), 1)
        # The old one is gone and the new one is at the new time.
        self.assertIn("00:01:40", cues[0]["t"])

    def test_leaves_alone_a_cue_no_model_proposed(self):
        """The luminance pass, the subtitles and anything a person added by
        hand all stay. That is only possible because a cue records what
        proposed it, which is why source stopped being a comment."""
        score = score_with({"instrument": "light.event", "type": "cue", "cues": [
            cue("00:00:05.000", "flash"),
            cue("00:00:09.000", "flash", "subtitle: [thunder]"),
        ]})
        out = remap.remap(score, observed((100.0, ["smoke"], "")), MAPPING, KINDS)
        kept = [t for t in out["track"] if t["instrument"] == "light.event"][0]
        self.assertEqual(len(kept["cues"]), 2)

    def test_never_touches_a_curve(self):
        score = score_with({"instrument": "shake.seat", "type": "curve",
                            "interpolation": "linear",
                            "points": [{"t": "00:00:00.000", "value": {"intensity": 0.2}},
                                       {"t": "00:01:00.000", "value": {"intensity": 0.4}}]})
        out = remap.remap(score, observed((10.0, ["smoke"], "")), MAPPING, KINDS)
        curve = [t for t in out["track"] if t["instrument"] == "shake.seat"][0]
        self.assertEqual(len(curve["points"]), 2)
        self.assertEqual(curve["points"][1]["value"]["intensity"], 0.4)

    def test_adds_a_track_for_an_instrument_that_had_none(self):
        # A new vocabulary word, or a rig that gained a device.
        score = score_with({"instrument": "wind.main", "type": "curve",
                            "points": [{"t": "00:00:00.000", "value": {"intensity": 0}},
                                       {"t": "00:01:00.000", "value": {"intensity": 0}}]})
        out = remap.remap(score, observed((30.0, ["smoke"], "")), MAPPING, KINDS)
        names = [t["instrument"] for t in out["track"]]
        self.assertIn("fog.left", names)

    def test_drops_a_cue_track_left_with_nothing(self):
        # Every cue on it came from a model, and the model no longer says so.
        score = score_with({"instrument": "fog.left", "type": "cue",
                            "cues": [cue("00:00:05.000", "burst", "vision: smoke")]})
        out = remap.remap(score, observed((10.0, [], "A quiet room.")), MAPPING, KINDS)
        self.assertEqual([t["instrument"] for t in out["track"]], [])

    def test_does_not_modify_what_it_was_given(self):
        # The caller may well want to compare the two.
        score = score_with({"instrument": "fog.left", "type": "cue",
                            "cues": [cue("00:00:05.000", "burst", "vision: smoke")]})
        remap.remap(score, observed((100.0, ["smoke"], "")), MAPPING, KINDS)
        self.assertEqual(score["track"][0]["cues"][0]["t"], "00:00:05.000")

    def test_cues_come_out_in_time_order(self):
        score = score_with({"instrument": "fog.left", "type": "cue",
                            "cues": [cue("00:05:00.000", "burst")]})
        out = remap.remap(score, observed((10.0, ["smoke"], ""), (600.0, ["smoke"], "")),
                          MAPPING, KINDS)
        times = [remap.seconds_of(c["t"]) for c in out["track"][0]["cues"]]
        self.assertEqual(times, sorted(times))

    def test_a_gate_can_refuse_labels(self):
        seen = observed((10.0, ["smoke"], ""), (20.0, ["smoke"], ""))
        out = remap.remap(score_with(), seen, MAPPING, KINDS,
                          gate=lambda pairs: [p for p in pairs if p[0] > 15])
        fog = [t for t in out["track"] if t["instrument"] == "fog.left"][0]
        self.assertEqual(len(fog["cues"]), 1)


class TestWrittenShape(unittest.TestCase):
    def test_a_proposed_cue_is_written_in_the_scores_units(self):
        """Seconds and bare floats are what the mapping deals in; the file
        wants timecodes and durations with units. Not converting was worth a
        parse error: a duration of 6.0 came back as "missing unit"."""
        got = remap.as_written({"t": 100.0, "action": "burst",
                                "params": {"output": 0.6}, "duration": 4.0,
                                "source": "vision: smoke"})
        self.assertEqual(got["t"], "00:01:40.000")
        self.assertTrue(got["duration"].endswith("s"), got["duration"])

    def test_seconds_of_reads_both_shapes(self):
        self.assertAlmostEqual(remap.seconds_of("00:01:40.000"), 100.0)
        self.assertAlmostEqual(remap.seconds_of("4.5s"), 4.5)
        self.assertAlmostEqual(remap.seconds_of(12.5), 12.5)
        self.assertEqual(remap.seconds_of("nonsense"), 0.0)


class TestLoadObservations(unittest.TestCase):
    def test_reads_a_description(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "seen.jsonl")
            with io.open(p, "w", encoding="utf-8", newline="\n") as f:
                f.write(json.dumps({"t": 1.0, "labels": ["smoke"], "seen": "A fire."}) + "\n")
            self.assertEqual(len(remap.load_observations(p)), 1)

    def test_skips_a_torn_line_rather_than_refusing_the_file(self):
        """The file is appended to a chunk at a time by something that can be
        interrupted. One torn line at the end is not a reason to refuse the
        other two thousand."""
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "seen.jsonl")
            with io.open(p, "w", encoding="utf-8", newline="\n") as f:
                f.write(json.dumps({"t": 1.0, "labels": [], "seen": "one"}) + "\n")
                f.write('{"t": 2.0, "labels": ["smo\n')
            self.assertEqual(len(remap.load_observations(p)), 1)

    def test_skips_a_row_with_no_time(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "seen.jsonl")
            with io.open(p, "w", encoding="utf-8", newline="\n") as f:
                f.write('{"labels": ["smoke"]}\n')
            self.assertEqual(remap.load_observations(p), [])


class TestDump(unittest.TestCase):
    def test_round_trips_a_score_it_did_not_change(self):
        score = score_with(
            {"instrument": "light.event", "type": "cue",
             "cues": [cue("00:00:05.000", "flash")]},
            {"instrument": "shake.seat", "type": "curve", "interpolation": "linear",
             "points": [{"t": "00:00:00.000", "value": {"intensity": 0.2}},
                        {"t": "00:01:00.000", "value": {"intensity": 0.4}}]})
        text = remap.dump(score)
        import tomllib
        back = tomllib.loads(text)
        self.assertEqual(len(back["track"]), 2)
        self.assertEqual(back["track"][0]["cues"][0]["action"], "flash")
        self.assertEqual(back["track"][1]["points"][1]["value"]["intensity"], 0.4)
        self.assertEqual(back["score"]["media"]["duration"], "00:10:00.000")

    def test_keeps_the_source_it_was_given(self):
        score = score_with({"instrument": "fog.left", "type": "cue",
                            "cues": [cue("00:00:05.000", "burst", "vision: smoke")]})
        import tomllib
        back = tomllib.loads(remap.dump(score))
        self.assertEqual(back["track"][0]["cues"][0]["source"], "vision: smoke")


if __name__ == "__main__":
    unittest.main()
