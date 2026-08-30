"""Tests for the pure parts of the composer.

Extraction needs ffmpeg and a real file, so it is exercised by running the
composer against a clip. Everything that turns signals into a score is pure,
and is tested here.
"""

import array
import unittest

import compose


class TestTimecode(unittest.TestCase):
    def test_formats(self):
        self.assertEqual(compose.timecode(0), "00:00:00.000")
        self.assertEqual(compose.timecode(3661.5), "01:01:01.500")

    def test_rounds_without_producing_1000ms(self):
        self.assertEqual(compose.timecode(59.9995), "00:01:00.000")

    def test_negative_clamps(self):
        self.assertEqual(compose.timecode(-5), "00:00:00.000")


class TestRMS(unittest.TestCase):
    def test_normalises_to_peak(self):
        samples = array.array("h", [0] * 10 + [10000] * 10)
        out = compose.rms_windows(samples, 10)
        self.assertEqual(len(out), 2)
        self.assertAlmostEqual(out[0], 0.0)
        self.assertAlmostEqual(out[1], 1.0)

    def test_silence_does_not_divide_by_zero(self):
        out = compose.rms_windows(array.array("h", [0] * 100), 10)
        self.assertEqual(set(out), {0.0})


class TestCompress(unittest.TestCase):
    def test_drops_points_within_threshold(self):
        points = [(i * 0.25, (0.5,)) for i in range(100)]
        out = compose.compress(points, 0.02)
        self.assertEqual(len(out), 2)

    def test_keeps_real_changes(self):
        points = [(0.0, (0.0,)), (1.0, (0.0,)), (2.0, (1.0,)), (3.0, (1.0,))]
        out = compose.compress(points, 0.02)
        self.assertIn((2.0, (1.0,)), out)

    def test_always_keeps_the_ends(self):
        points = [(0.0, (0.0,)), (1.0, (0.001,)), (2.0, (0.002,))]
        out = compose.compress(points, 0.5)
        self.assertEqual(out[0], points[0])
        self.assertEqual(out[-1], points[-1])

    def test_short_input_is_returned_whole(self):
        self.assertEqual(len(compose.compress([(0.0, (0.0,))], 0.1)), 1)


class TestRender(unittest.TestCase):
    def setUp(self):
        self.meta = {"title": "Dune", "duration": 9312.0,
                     "hash": "sha256:abc", "fps": 24.0}
        self.tracks = [{"instrument": "light.ambient",
                        "points": [(0.0, {"r": 0.0, "g": 0.0, "b": 0.0}),
                                   (10.0, {"r": 1.0, "g": 0.5, "b": 0.25})]}]

    def test_renders_the_expected_fields(self):
        out = compose.render(self.meta, self.tracks)
        self.assertIn('componium = "0.1"', out)
        self.assertIn('title = "Dune"', out)
        self.assertIn('duration = "02:35:12.000"', out)
        self.assertIn('instrument = "light.ambient"', out)
        self.assertIn('t = "00:00:10.000"', out)

    def test_warns_that_output_is_a_proposal(self):
        # The header is a safety control, not decoration: a generated score
        # has not been checked against what a rig can survive.
        out = compose.render(self.meta, self.tracks)
        self.assertIn("proposal", out.lower())

    def test_omits_hash_when_absent(self):
        meta = dict(self.meta, hash="")
        self.assertNotIn("hash =", compose.render(meta, self.tracks))


class TestSourceSurvivesRendering(unittest.TestCase):
    """What a cue says about where it came from, written whole.

    The source is the only trace of why a cue exists. It is read by a person
    reviewing a score and asking whether the machine was right, so it has to
    come out the way it went in. It once came out as
    " v i s i o n :   d u s t " because a replace() lost its escape and replaced
    the empty string instead of a backslash.
    """

    def render_source(self, said):
        meta = {"title": "T", "duration": 10, "fps": 24, "hash": ""}
        track = {
            "instrument": "fog.left",
            "type": "cue",
            "cues": [{"t": 1.0, "action": "burst",
                      "params": {"output": 0.7}, "duration": 3.0,
                      "source": said}],
        }
        for line in compose.render(meta, [track]).splitlines():
            if "source =" in line:
                return line.split("source = ")[1].strip().rstrip(" },").strip('"')
        return None

    def test_a_vision_source_reads_as_written(self):
        self.assertEqual(self.render_source("vision: dust"), "vision: dust")

    def test_every_character_is_not_spaced_out(self):
        # The exact fault: replacing the empty string puts a space between
        # every character, so the length gives it away on its own.
        said = "vision: dust"
        self.assertEqual(len(self.render_source(said)), len(said))

    def test_a_backslash_cannot_end_the_string_early(self):
        # What the replace is actually for. A trailing backslash would escape
        # the closing quote and make the score unparseable.
        out = self.render_source("vision: dust\\")
        self.assertNotIn(chr(92), out)  # no backslash survives
        self.assertTrue(out.startswith("vision: dust"))

    def test_a_quote_cannot_end_the_string_early(self):
        out = self.render_source('he said "go"')
        self.assertNotIn(chr(34), out)  # no quote survives


if __name__ == "__main__":
    unittest.main()
