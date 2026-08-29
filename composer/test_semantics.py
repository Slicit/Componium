"""Tests for subtitle mining and scene handling.

Extraction needs ffmpeg and a real file; everything that interprets the
extracted data is pure and is tested here.
"""

import unittest

import scenes
import subtitles

SAMPLE_SRT = """1
00:00:05,000 --> 00:00:07,000
I don't like the look of that sky.

2
00:00:12,100 --> 00:00:14,000
[thunder rumbles]

3
00:00:20,000 --> 00:00:22,000
(rain patters on the roof)

4
00:00:30,500 --> 00:00:32,000
[explosion]

5
00:00:40,000 --> 00:00:41,000
Nothing happens here.
"""


class TestParse(unittest.TestCase):
    def test_parses_entries(self):
        got = subtitles.parse(SAMPLE_SRT)
        self.assertEqual(len(got), 5)
        self.assertAlmostEqual(got[1][0], 12.1)
        self.assertEqual(got[1][2], "[thunder rumbles]")

    def test_tolerates_full_stop_separators_and_junk(self):
        srt = "\n\n7\n00:01:02.500 --> 00:01:03.000\n[wind howling]\n\n\n"
        got = subtitles.parse(srt)
        self.assertEqual(len(got), 1)
        self.assertAlmostEqual(got[0][0], 62.5)


class TestDescriptions(unittest.TestCase):
    def test_only_bracketed_text_counts(self):
        got = subtitles.descriptions(subtitles.parse(SAMPLE_SRT))
        phrases = [p for _, p in got]
        self.assertIn("thunder rumbles", phrases)
        self.assertIn("rain patters on the roof", phrases)
        # Dialogue is not an effect. Firing the rig on every spoken line would
        # be considerably worse than missing something.
        self.assertNotIn("I don't like the look of that sky.", phrases)
        self.assertNotIn("Nothing happens here.", phrases)


class TestCues(unittest.TestCase):
    def test_maps_words_to_effects(self):
        got = subtitles.cues(subtitles.parse(SAMPLE_SRT))
        kinds = {c["instrument"] for c in got}
        self.assertIn("shake.main", kinds)
        self.assertIn("mist.main", kinds)
        self.assertIn("light.main", kinds)

    def test_respects_rig_instrument_names(self):
        got = subtitles.cues(subtitles.parse(SAMPLE_SRT), kinds={"wind": "wind.left"})
        srt = "1\n00:00:01,000 --> 00:00:02,000\n[wind howling]\n"
        got = subtitles.cues(subtitles.parse(srt), kinds={"wind": "wind.left"})
        self.assertTrue(all(c["instrument"] == "wind.left" for c in got))

    def test_one_description_does_not_fire_the_same_thing_three_times(self):
        srt = "1\n00:00:01,000 --> 00:00:02,000\n[thunder rumbles and crashes]\n"
        got = subtitles.cues(subtitles.parse(srt))
        shakes = [c for c in got if c["instrument"] == "shake.main"]
        self.assertEqual(len(shakes), 1, f"got {shakes}")

    def test_cues_are_sorted(self):
        got = subtitles.cues(subtitles.parse(SAMPLE_SRT))
        self.assertEqual([c["t"] for c in got], sorted(c["t"] for c in got))


class TestSceneSnap(unittest.TestCase):
    def test_inserts_a_holding_point_before_a_cut(self):
        points = [(0.0, (0.0,)), (10.0, (1.0,))]
        out = scenes.snap(points, [5.0])
        self.assertEqual(len(out), 3)
        held = [p for p in out if abs(p[0] - (5.0 - 0.04)) < 1e-9]
        self.assertEqual(len(held), 1)
        # It repeats the previous value, so the curve holds flat into the cut.
        self.assertEqual(held[0][1], (0.0,))

    def test_ignores_cuts_outside_the_curve(self):
        points = [(2.0, (0.0,)), (4.0, (1.0,))]
        self.assertEqual(scenes.snap(points, [0.5, 99.0]), points)

    def test_no_cuts_changes_nothing(self):
        points = [(0.0, (0.0,)), (1.0, (1.0,))]
        self.assertEqual(scenes.snap(points, []), points)

    def test_output_stays_sorted(self):
        points = [(0.0, (0.0,)), (10.0, (1.0,)), (20.0, (0.5,))]
        out = scenes.snap(points, [15.0, 5.0])
        self.assertEqual([p[0] for p in out], sorted(p[0] for p in out))


if __name__ == "__main__":
    unittest.main()
