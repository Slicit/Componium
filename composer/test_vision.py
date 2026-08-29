"""Tests for the vision seam.

Everything that needs a model or ffmpeg is exercised by running the composer;
the selection and parsing logic is pure and is tested here.
"""

import unittest

import vision


class TestCandidates(unittest.TestCase):
    def test_picks_the_loudest_moments(self):
        env = [0.0] * 100
        env[10] = 0.9
        env[50] = 0.8
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0)
        self.assertEqual(got, [10.0, 50.0])

    def test_ignores_quiet_moments(self):
        got = vision.candidates([0.1] * 100, rate=1.0, threshold=0.5)
        self.assertEqual(got, [])

    def test_spaces_picks_out(self):
        # Four consecutive loud seconds are one event, not four.
        env = [0.0, 0.9, 0.9, 0.9, 0.9, 0.0]
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=8.0)
        self.assertEqual(len(got), 1)

    def test_respects_the_limit(self):
        env = [0.9 if i % 20 == 0 else 0.0 for i in range(2000)]
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0, limit=5)
        self.assertEqual(len(got), 5)

    def test_scene_cuts_fill_the_remaining_budget(self):
        got = vision.candidates([], rate=1.0, cuts=[10.0, 40.0, 70.0], limit=10)
        self.assertEqual(got, [10.0, 40.0, 70.0])

    def test_output_is_sorted(self):
        env = [0.0] * 200
        env[150] = 0.9
        env[20] = 0.95
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0)
        self.assertEqual(got, sorted(got))


class TestParseLabels(unittest.TestCase):
    def test_one_label_per_line(self):
        self.assertEqual(vision.parse_labels("explosion\nfire\n"), ["explosion", "fire"])

    def test_ignores_blanks_and_comments(self):
        self.assertEqual(vision.parse_labels("\n# a note\nrain\n\n"), ["rain"])

    def test_strips_confidences(self):
        # Models emit confidences and nobody should have to strip them by hand.
        self.assertEqual(vision.parse_labels("0.92 explosion"), ["explosion"])
        self.assertEqual(vision.parse_labels("explosion: 0.92"), ["explosion"])
        self.assertEqual(vision.parse_labels("rain, 0.5, wind"), ["rain", "wind"])

    def test_lowercases(self):
        self.assertEqual(vision.parse_labels("Thunder"), ["thunder"])

    def test_empty_input(self):
        self.assertEqual(vision.parse_labels(""), [])
        self.assertEqual(vision.parse_labels(None), [])


class TestLabelFrame(unittest.TestCase):
    def test_a_command_that_does_not_exist_returns_nothing(self):
        # A missing model must not abort a run three quarters through a film.
        self.assertEqual(vision.label_frame("definitely-not-a-real-command", "/tmp/x.jpg"), [])

    def test_a_failing_command_returns_nothing(self):
        self.assertEqual(vision.label_frame("false", "/tmp/x.jpg"), [])

    def test_a_working_command_is_read(self):
        got = vision.label_frame("echo explosion", "/tmp/x.jpg")
        self.assertIn("explosion", got)


if __name__ == "__main__":
    unittest.main()
