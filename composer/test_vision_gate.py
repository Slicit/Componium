"""Labels that are only worth believing in company.

A model asked whether spray is water or sand is judging a material from a
still, and it is bad at it: crabs kicking up sand on a beach came back as a
splash, and the score sprayed the audience with water for it. It is good at
whether a scene contains water at all, which is a much easier question about a
much larger part of the frame. So the weak judgement is gated on the strong
one.
"""

import unittest

from vision import CORROBORATES, gate


class TestGate(unittest.TestCase):
    def test_a_splash_beside_water_is_kept(self):
        found = [(10.0, "splash"), (12.0, "water")]
        self.assertIn((10.0, "splash"), gate(found))

    def test_a_splash_with_no_water_anywhere_near_is_dropped(self):
        # The crab on the beach: sand thrown up, no sea in the frame.
        found = [(90.0, "splash"), (95.0, "dust")]
        self.assertEqual(gate(found), [(95.0, "dust")])

    def test_water_may_come_before_or_after(self):
        # It is asking whether this is a watery part of the film, not whether
        # two things happened in a particular order.
        self.assertIn((50.0, "splash"), gate([(40.0, "water"), (50.0, "splash")]))
        self.assertIn((50.0, "splash"), gate([(50.0, "splash"), (60.0, "water")]))

    def test_water_too_far_away_does_not_count(self):
        found = [(10.0, "water"), (500.0, "splash")]
        self.assertEqual(gate(found), [(10.0, "water")])

    def test_the_window_is_configurable(self):
        found = [(10.0, "water"), (100.0, "splash")]
        self.assertEqual(len(gate(found, window=200.0)), 2)

    def test_labels_needing_nothing_pass_untouched(self):
        found = [(1.0, "smoke"), (2.0, "dust"), (3.0, "explosion"), (4.0, "lightning")]
        self.assertEqual(gate(found), found)

    def test_the_corroborating_label_is_itself_kept(self):
        # water is evidence, not a cue, but dropping it here would also
        # remove it from the confirmations the water nominator reads.
        self.assertIn((12.0, "water"), gate([(10.0, "splash"), (12.0, "water")]))

    def test_nothing_in_nothing_out(self):
        self.assertEqual(gate([]), [])
        self.assertEqual(gate(None), None)

    def test_only_splash_needs_corroboration_today(self):
        # If this list grows, the growth should be deliberate: every entry is a
        # judgement that the model is unreliable about one thing and reliable
        # about another.
        self.assertEqual(CORROBORATES, {"splash": "water"})

    def test_several_splashes_share_one_sighting_of_water(self):
        found = [(10.0, "splash"), (12.0, "water"), (14.0, "splash")]
        kept = gate(found)
        self.assertIn((10.0, "splash"), kept)
        self.assertIn((14.0, "splash"), kept)


if __name__ == "__main__":
    unittest.main()
