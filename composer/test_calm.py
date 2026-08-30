"""Deciding which half of a film to leave alone.

The complaint that started this was movement continuing through scenes that
were plainly quiet, so most of these are about the two ways of getting that
wrong: not quieting what should be quiet, and quieting what should not.
"""

import unittest

import calm


def readings(*pairs):
    """(time, active) pairs as the observations the pass reads."""
    return [{"t": t, "labels": (["scene-active"] if a else []), "seen": ""}
            for t, a in pairs]


def flat(seconds, active, step=2.0):
    return readings(*[(i * step, active) for i in range(int(seconds / step))])


class TestActivity(unittest.TestCase):
    def test_the_model_alone_moves_the_score(self):
        quiet = calm.activity(readings((0.0, False)))
        busy = calm.activity(readings((0.0, True)))
        self.assertLess(quiet[0][1], busy[0][1])

    def test_the_signals_move_it_too(self):
        # No single signal can be trusted: the audio discriminates well on a
        # film and badly on a music video, and the camera does the reverse.
        obs = readings((10.0, False))
        without = calm.activity(obs)
        with_audio = calm.activity(obs, audio=[(0.0, {"i": 1.0}), (20.0, {"i": 1.0})])
        self.assertGreater(with_audio[0][1], without[0][1])

    def test_it_stays_inside_nought_to_one(self):
        loud = [(0.0, {"i": 5.0}), (20.0, {"i": 5.0})]
        got = calm.activity(readings((10.0, True)), audio=loud, camera=loud)
        self.assertLessEqual(got[0][1], 1.0)


class TestFloor(unittest.TestCase):
    def test_a_story_film_must_be_calmer(self):
        self.assertAlmostEqual(calm.floor_for(flat(100, False)), calm.FLOOR_MAX)

    def test_an_action_film_may_stay_busier(self):
        self.assertAlmostEqual(calm.floor_for(flat(100, True)), calm.FLOOR_MIN)

    def test_it_slides_between_them(self):
        # A third active: between the story bound and the action one, which
        # is where the floor is allowed to slide. Half would be past the
        # action bound and would correctly clamp.
        third = readings(*[(i * 2.0, i % 3 == 0) for i in range(60)])
        got = calm.floor_for(third)
        self.assertGreater(got, calm.FLOOR_MIN)
        self.assertLess(got, calm.FLOOR_MAX)

    def test_it_never_leaves_the_range(self):
        for obs in (flat(100, False), flat(100, True)):
            self.assertGreaterEqual(calm.floor_for(obs), calm.FLOOR_MIN)
            self.assertLessEqual(calm.floor_for(obs), calm.FLOOR_MAX)


class TestSmoothing(unittest.TestCase):
    def test_it_evens_out_a_flicker(self):
        """The model changes its mind constantly, and most of that is not the
        film changing. Ranking the raw readings produced stretches too short to
        act on."""
        flicker = [(i * 2.0, float(i % 2)) for i in range(40)]
        smoothed = calm.smooth_scores(flicker, 14.0)
        spread = max(v for _, v in smoothed) - min(v for _, v in smoothed)
        self.assertLess(spread, 0.5)

    def test_it_keeps_a_real_change(self):
        # Half quiet, half busy: that is the film, not a flicker.
        step = [(i * 2.0, 0.0 if i < 20 else 1.0) for i in range(40)]
        smoothed = calm.smooth_scores(step, 14.0)
        self.assertLess(smoothed[2][1], 0.2)
        self.assertGreater(smoothed[-3][1], 0.8)

    def test_it_keeps_the_times(self):
        rows = [(i * 2.0, 0.5) for i in range(10)]
        self.assertEqual([t for t, _ in calm.smooth_scores(rows)],
                         [t for t, _ in rows])

    def test_it_survives_too_little_to_average(self):
        self.assertEqual(calm.smooth_scores([(0.0, 1.0)]), [(0.0, 1.0)])
        self.assertEqual(calm.smooth_scores([]), [])


class TestRegions(unittest.TestCase):
    def test_a_short_lull_is_not_a_calm_scene(self):
        # Switching a platform off for four seconds and on again is more
        # noticeable than leaving it running.
        scores = [(i * 2.0, 1.0) for i in range(10)]
        scores[4] = (8.0, 0.0)
        self.assertEqual(calm.regions(scores, 0.5, min_seconds=8.0), [])

    def test_a_long_quiet_stretch_is(self):
        scores = [(i * 2.0, 0.0 if i < 20 else 1.0) for i in range(40)]
        got = calm.regions(scores, 0.5, min_seconds=8.0)
        self.assertEqual(len(got), 1)
        self.assertGreater(got[0][1] - got[0][0], 30)

    def test_a_film_that_never_rests_gets_nothing(self):
        self.assertEqual(calm.regions([(i * 2.0, 1.0) for i in range(40)], 0.5), [])


class TestBudget(unittest.TestCase):
    def test_the_budget_is_spent_in_seconds(self):
        """Half the readings of sintel fall below the median, but scattered —
        only the ones forming a long enough run become a stretch, so half the
        readings bought twenty-nine per cent of the film. The rule is about
        time, so the search is over time."""
        scores = calm.smooth_scores(
            [(i * 2.0, 0.0 if i % 4 < 2 else 1.0) for i in range(200)], 20.0)
        level = calm.threshold_for_time(scores, 0.5, min_seconds=8.0)
        got = calm.covered(calm.regions(scores, level, 8.0))
        span = scores[-1][0] - scores[0][0]
        self.assertGreaterEqual(got / span, 0.45)

    def test_a_film_already_calmer_than_the_floor_is_left_alone(self):
        """The budget is a floor, never a target. A rig that adds movement to a
        still film is the fault this pass exists to correct."""
        scores = [(i * 2.0, 0.0) for i in range(100)]
        level = calm.threshold_for_time(scores, 0.5, min_seconds=8.0)
        got = calm.covered(calm.regions(scores, level, 8.0))
        span = scores[-1][0] - scores[0][0]
        # Everything is quiet, so everything may be quieted: well past the floor.
        self.assertGreater(got / span, 0.9)

    def test_it_gives_what_it_can_when_the_floor_cannot_be_met(self):
        # A film too fragmented to reach its floor should still be quieted as
        # much as it can be, rather than not at all.
        scores = [(i * 2.0, 1.0) for i in range(50)]
        self.assertIsInstance(calm.threshold_for_time(scores, 0.5), float)


class TestGate(unittest.TestCase):
    def test_it_is_open_outside_and_shut_inside(self):
        self.assertEqual(calm.gate_at(100.0, [(10.0, 20.0)]), 1.0)
        self.assertEqual(calm.gate_at(15.0, [(10.0, 20.0)]), 0.0)

    def test_it_closes_over_the_ramp_rather_than_at_once(self):
        # A curve snapped to zero at a boundary is its own event.
        half = calm.gate_at(10.0 - 0.4, [(10.0, 20.0)], ramp=0.8)
        self.assertGreater(half, 0.2)
        self.assertLess(half, 0.8)

    def test_it_quiets_a_curve_where_the_film_is_quiet(self):
        points = [(t, {"intensity": 1.0}) for t in range(0, 60, 2)]
        got = calm.quiet(points, [(20.0, 40.0)])
        inside = [v["intensity"] for t, v in got if 20.0 <= t <= 40.0]
        outside = [v["intensity"] for t, v in got if t < 19.0 or t > 41.0]
        self.assertEqual(max(inside), 0.0)
        self.assertEqual(min(outside), 1.0)

    def test_it_puts_points_at_the_edges_so_the_ramp_is_the_ramp(self):
        """Without them a stretch with no points near its boundary fades over
        minutes instead of over the ramp."""
        points = [(0.0, {"i": 1.0}), (600.0, {"i": 1.0})]
        got = calm.quiet(points, [(300.0, 400.0)], ramp=0.8)
        times = [t for t, _ in got]
        for edge in (299.2, 300.0, 400.0, 400.8):
            self.assertIn(edge, times)

    def test_nothing_to_quiet_changes_nothing(self):
        points = [(0.0, {"i": 1.0}), (10.0, {"i": 0.5})]
        self.assertEqual(calm.quiet(points, []), points)


if __name__ == "__main__":
    unittest.main()
