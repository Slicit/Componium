"""Tests for subtitle mining and scene handling.

Extraction needs ffmpeg and a real file; everything that interprets the
extracted data is pure and is tested here.
"""

import unittest

import analysis
import compose
import light
import motion_est
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


class ColourSpace(unittest.TestCase):
    """A light is authored as hue, saturation and intensity.

    The composer was already doing this arithmetic by hand — desaturating by
    lerping toward grey, and dividing RGB by its peak with the comment
    "keeping the hue". Those are saturation and intensity moves written in a
    space that cannot express them.
    """

    def test_primaries_round_trip(self):
        for rgb in [(1, 0, 0), (0, 1, 0), (0, 0, 1), (1, 1, 1), (0.5, 0.25, 0)]:
            h, s, i = light.to_hsi(rgb)
            self.assertGreaterEqual(h, 0.0)
            self.assertLess(h, 1.0)
            self.assertAlmostEqual(i, max(rgb), places=6)

    def test_intensity_is_the_largest_channel(self):
        # Which is what "how bright is this" has always meant here, and what
        # the duty cycle and the rest budget read.
        self.assertAlmostEqual(light.to_hsi((0.4, 0.2, 0.1))[2], 0.4, places=6)

    def test_grey_has_no_hue_and_no_saturation(self):
        for v in (0.0, 0.25, 1.0):
            h, s, i = light.to_hsi((v, v, v))
            self.assertEqual(h, 0.0)
            self.assertEqual(s, 0.0)
            self.assertAlmostEqual(i, v, places=6)

    def test_black_is_not_a_division_by_zero(self):
        self.assertEqual(light.to_hsi((0, 0, 0)), (0.0, 0.0, 0.0))

    def test_flashes_are_full_intensity_and_keep_their_hue(self):
        # A warm frame must flash warm and a cold one cold, at full output:
        # a flash that is not bright is not a flash.
        # Luma is on the byte scale the decoder produces, not a fraction.
        frames = [analysis.Luma(40), analysis.Luma(210)]
        warm = [(1.0, 0.4, 0.1)] * len(frames)
        cues = light.flashes(frames, warm, 4.0)
        self.assertTrue(cues, "no flash was found")
        for cue in cues:
            self.assertEqual(set(cue["params"]), {"h", "s", "i"})
            self.assertEqual(cue["params"]["i"], 1.0)
            # Warm is near the red end of the wheel, not the cyan end.
            self.assertLess(cue["params"]["h"], 0.15)
            self.assertGreater(cue["params"]["s"], 0.5)


class CalmIsRecorded(unittest.TestCase):
    """Where the analysis decided to leave the film alone is written down.

    Advisory only — the player never reads it. It is recorded because it is
    the answer to the only question a sparse stretch of timeline provokes, and
    because these regions were computed to decide what *not* to play and then
    thrown away.
    """

    def test_regions_appear_in_the_score(self):
        text = compose.render(
            {"title": "t", "duration": 120.0},
            [],
            calm=[(10.0, 40.0), (80.5, 95.25)],
        )
        self.assertIn("[[calm]]", text)
        self.assertIn('from = "00:00:10.000"', text)
        self.assertIn('to = "00:00:40.000"', text)
        self.assertIn('from = "00:01:20.500"', text)
        self.assertEqual(text.count("[[calm]]"), 2)

    def test_a_film_with_no_calm_writes_none(self):
        text = compose.render({"title": "t", "duration": 60.0}, [], calm=[])
        self.assertNotIn("[[calm]]", text)

    def test_the_default_is_still_a_valid_score(self):
        # render() is called from tests and tools without the argument.
        text = compose.render({"title": "t", "duration": 60.0}, [])
        self.assertIn("[score]", text)
        self.assertNotIn("[[calm]]", text)


class ThreeAxes(unittest.TestCase):
    """Three axes by default, six for a rig that has them.

    A platform with three actuators under a triangle produces exactly heave,
    roll and pitch; six needs a Stewart platform. Folding rather than dropping
    matters: surge becomes a backward tilt, which is how a rig with
    centimetres of travel conveys sustained acceleration at all.
    """

    def test_only_the_three_a_platform_has(self):
        pose = [{"surge": 0.5, "sway": 0.2, "heave": 0.3,
                 "roll": 0.0, "pitch": 0.1, "yaw": 0.9}]
        got = motion_est.to_3dof(pose)
        self.assertEqual(set(got[0]), {"heave", "roll", "pitch"})

    def test_surge_becomes_a_backward_tilt(self):
        # Pitching back is how a seat says "accelerating forward" when it has
        # centimetres of travel rather than metres.
        flat = motion_est.to_3dof([{"pitch": 0.0, "surge": 0.0}])[0]["pitch"]
        pushed = motion_est.to_3dof([{"pitch": 0.0, "surge": 0.8}])[0]["pitch"]
        self.assertEqual(flat, 0.0)
        self.assertLess(pushed, 0.0)

    def test_sway_becomes_roll(self):
        self.assertGreater(motion_est.to_3dof([{"sway": 0.8}])[0]["roll"], 0.0)

    def test_heave_survives_untouched(self):
        # Heave is where a plunge lives, and it is the one axis a three
        # actuator platform does natively.
        self.assertEqual(motion_est.to_3dof([{"heave": 0.42}])[0]["heave"], 0.42)

    def test_nothing_leaves_the_unit_range(self):
        pose = [{"pitch": 0.9, "surge": 1.0, "sway": 1.0, "roll": 0.9, "heave": 1.0}]
        got = motion_est.to_3dof(pose)[0]
        for axis, v in got.items():
            self.assertGreaterEqual(v, -1.0, axis)
            self.assertLessEqual(v, 1.0, axis)

    def test_a_yaw_only_pan_moves_nothing(self):
        # A pan is something the camera looked at, not a motion a seated
        # person feels — and a three actuator platform cannot yaw at all.
        got = motion_est.to_3dof([{"yaw": 1.0}])[0]
        self.assertEqual(got, {"heave": 0.0, "roll": 0.0, "pitch": 0.0})
