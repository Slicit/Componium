"""Giving a film its shape back.

The calm pass has only ever taken things away, which is the right answer to
"too much shake, too brutal, even on calm scenes" and half an answer to what a
score should be. A budget spent entirely on holding back leaves every action
sequence at the level of the scene before it, so the loud parts are loud only
by comparison with silence.

These are about the other half, and about the two ways it could go wrong: a
lurch at the edges, and asking for more than a rig said it can do.
"""

import unittest

import calm
import dynamics


def rising(n=100, step=1.0):
    """Scores that climb, so the busiest stretch is at the end."""
    return [(i * step, i / float(n)) for i in range(n)]


class TestBusy(unittest.TestCase):
    def test_finds_the_busiest_stretch(self):
        got = calm.busy(rising(), share=0.2)
        self.assertEqual(len(got), 1)
        # The top fifth of a climbing film is its end.
        self.assertGreater(got[0][0], 70)

    def test_spends_a_share_rather_than_meeting_a_threshold(self):
        # The property that made the calm pass survive a signal measuring the
        # wrong thing: a share moves with the film and a threshold does not.
        quietly = [(i, 0.01 * i / 100.0) for i in range(100)]
        loudly = [(i, 0.9 + 0.001 * i) for i in range(100)]
        for scores in (quietly, loudly):
            got = calm.busy(scores, share=0.2, min_seconds=5.0)
            self.assertEqual(len(got), 1, "a share should find one either way")

    def test_a_brief_peak_is_not_a_sequence(self):
        # Lifting for eight seconds and dropping again is a lurch, where
        # calming for eight seconds is merely a rest.
        spike = [(i, 1.0 if 50 <= i <= 55 else 0.0) for i in range(100)]
        self.assertEqual(calm.busy(spike, share=0.2), [])

    def test_a_film_that_does_nothing_is_not_lifted(self):
        self.assertEqual(calm.busy([(i, 0.0) for i in range(100)]), [])

    def test_nothing_from_nothing(self):
        self.assertEqual(calm.busy([]), [])


class TestBoostAt(unittest.TestCase):
    def test_full_inside(self):
        self.assertAlmostEqual(calm.boost_at(50.0, [(40.0, 60.0)]), calm.BOOST)

    def test_none_outside(self):
        self.assertAlmostEqual(calm.boost_at(0.0, [(40.0, 60.0)]), 1.0)

    def test_ramped_at_the_edge(self):
        # A curve stepped up at a boundary is its own event, and a step in a
        # platform is one a body notices more than a step in a light.
        just_before = calm.boost_at(40.0 - calm.RAMP_SECONDS / 2, [(40.0, 60.0)])
        self.assertGreater(just_before, 1.0)
        self.assertLess(just_before, calm.BOOST)


class TestLift(unittest.TestCase):
    def test_a_curve_gets_more_of_itself(self):
        points = [(t, {"intensity": 0.4}) for t in range(0, 100, 10)]
        got = calm.lift(points, [(30.0, 70.0)])
        inside = [v["intensity"] for t, v in got if 30 <= t <= 70]
        self.assertTrue(all(v > 0.4 for v in inside), inside)

    def test_it_cannot_ask_for_more_than_the_rig_allows(self):
        # Everything here is normalised to what a rig declared it can survive.
        # Asking past one is asking somebody else's clamp to decide.
        points = [(t, {"intensity": 0.95}) for t in range(0, 100, 10)]
        got = calm.lift(points, [(0.0, 100.0)])
        self.assertTrue(all(v["intensity"] <= 1.0 for _t, v in got))

    def test_outside_is_untouched(self):
        points = [(t, {"intensity": 0.4}) for t in range(0, 100, 10)]
        got = calm.lift(points, [(60.0, 90.0)])
        early = [v["intensity"] for t, v in got if t < 40]
        self.assertTrue(all(abs(v - 0.4) < 1e-6 for v in early), early)

    def test_nothing_to_lift_leaves_it_alone(self):
        points = [(t, {"intensity": 0.4}) for t in range(0, 100, 10)]
        self.assertEqual(calm.lift(points, []), points)


class TestScentSurvivesCalm(unittest.TestCase):
    """Calm is about not shaking and not flashing, not about smells.

    The scenes most worth a scent are very often the quiet ones — a forest, a
    church, a kitchen — and the first scent the scene pass ever chose was
    dropped for landing in one.
    """

    def cue(self, instrument, at=10.0, output=0.6):
        return {"instrument": instrument, "t": at, "action": "puff",
                "params": {"output": output}}

    def test_a_scent_in_a_quiet_scene_survives(self):
        kept, dropped = dynamics.protect_calm([self.cue("scent.main")],
                                              [(0.0, 100.0)])
        self.assertEqual(len(kept), 1)
        self.assertEqual(dropped, [])

    def test_a_shake_in_the_same_scene_does_not(self):
        kept, dropped = dynamics.protect_calm([self.cue("shake.seat")],
                                              [(0.0, 100.0)])
        self.assertEqual(kept, [])
        self.assertEqual(len(dropped), 1)

    def test_outside_a_calm_stretch_everything_survives(self):
        cues = [self.cue("scent.main", at=200.0), self.cue("shake.seat", at=200.0)]
        kept, dropped = dynamics.protect_calm(cues, [(0.0, 100.0)])
        self.assertEqual(len(kept), 2)
        self.assertEqual(dropped, [])


if __name__ == "__main__":
    unittest.main()
