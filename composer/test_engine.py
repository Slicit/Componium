"""Tests for the analysis engine: motion, dynamics, light layers, water."""

import unittest

import analysis
import dynamics
import light
import motion_est
import water


def gray(w, h, fill=0, bright_col=None, bright_row=None):
    px = bytearray([fill] * (w * h))
    if bright_col is not None:
        for y in range(h):
            px[y * w + bright_col] = 255
    if bright_row is not None:
        for x in range(w):
            px[bright_row * w + x] = 255
    return bytes(px)


class TestFeatures(unittest.TestCase):
    def test_projections_find_the_bright_column(self):
        f = analysis.features(gray(8, 4, 10, bright_col=5), 8, 4)
        self.assertEqual(max(range(8), key=lambda i: f.cols[i]), 5)

    def test_projections_find_the_bright_row(self):
        f = analysis.features(gray(8, 4, 10, bright_row=2), 8, 4)
        self.assertEqual(max(range(4), key=lambda i: f.rows[i]), 2)

    def test_mean_and_peak(self):
        f = analysis.features(gray(4, 4, 100), 4, 4)
        self.assertAlmostEqual(f.mean, 100.0)
        self.assertEqual(f.peak, 100)


class TestShift(unittest.TestCase):
    def test_recovers_a_known_shift(self):
        a = [0, 0, 0, 9, 5, 0, 0, 0, 0, 0]
        b = [0, 0, 0, 0, 0, 9, 5, 0, 0, 0]
        s, conf = motion_est.best_shift(a, b, 4)
        self.assertEqual(s, 2)
        self.assertGreater(conf, 0.2)

    def test_flat_input_is_not_believed(self):
        s, conf = motion_est.best_shift([5] * 10, [5] * 10, 4)
        self.assertLess(conf, 0.05)

    def test_brightness_change_alone_is_not_movement(self):
        a = [0, 0, 9, 0, 0, 0, 0, 0]
        b = [40, 40, 49, 40, 40, 40, 40, 40]
        s, _ = motion_est.best_shift(a, b, 3)
        self.assertEqual(s, 0)


class FakeMovement:
    def __init__(self, dx, dy, speed, confidence=1.0):
        self.dx, self.dy, self.speed, self.confidence = dx, dy, speed, confidence


class TestPlunge(unittest.TestCase):
    def test_finds_sustained_downward_movement(self):
        moves = [FakeMovement(0, -4, 0.06) for _ in range(20)]
        found = motion_est.find_plunges(moves, fps=4.0, min_seconds=1.0)
        self.assertEqual(len(found), 1)
        start, end, magnitude = found[0]
        self.assertLess(start, end)
        self.assertGreater(magnitude, 0)

    def test_ignores_a_brief_dip(self):
        moves = [FakeMovement(0, 0, 0.0) for _ in range(20)]
        moves[5] = FakeMovement(0, -6, 0.1)
        self.assertEqual(motion_est.find_plunges(moves, fps=4.0, min_seconds=1.0), [])

    def test_ignores_upward_movement(self):
        moves = [FakeMovement(0, 5, 0.08) for _ in range(20)]
        self.assertEqual(motion_est.find_plunges(moves, fps=4.0), [])

    def test_low_confidence_is_not_a_plunge(self):
        moves = [FakeMovement(0, -5, 0.08, confidence=0.01) for _ in range(20)]
        self.assertEqual(motion_est.find_plunges(moves, fps=4.0), [])


class TestCalm(unittest.TestCase):
    def test_finds_a_long_quiet_stretch(self):
        levels = [0.9] * 40 + [0.05] * 80 + [0.9] * 40
        regions = dynamics.calm_regions(levels, fps=4.0, min_seconds=12.0)
        self.assertEqual(len(regions), 1)
        lo, hi = regions[0]
        self.assertAlmostEqual(lo, 10.0, places=1)
        self.assertAlmostEqual(hi, 30.0, places=1)

    def test_a_breath_between_explosions_is_not_a_calm_scene(self):
        levels = [0.9] * 40 + [0.05] * 8 + [0.9] * 40
        self.assertEqual(dynamics.calm_regions(levels, fps=4.0, min_seconds=12.0), [])

    def test_no_signal_means_no_regions(self):
        self.assertEqual(dynamics.calm_regions([], fps=4.0), [])


class TestProtectCalm(unittest.TestCase):
    def setUp(self):
        self.regions = [(10.0, 30.0)]

    def test_drops_quiet_effects_inside_calm(self):
        cues = [{"t": 15.0, "action": "rumble", "params": {"intensity": 0.4}}]
        kept, dropped = dynamics.protect_calm(cues, self.regions)
        self.assertEqual(kept, [])
        self.assertEqual(len(dropped), 1)

    # A thunderclap in a silent scene is the reason the scene was silent.
    def test_keeps_a_loud_event_inside_calm(self):
        cues = [{"t": 15.0, "action": "hit", "params": {"intensity": 1.0}}]
        kept, _ = dynamics.protect_calm(cues, self.regions)
        self.assertEqual(len(kept), 1)

    def test_leaves_everything_outside_calm_alone(self):
        cues = [{"t": 50.0, "action": "rumble", "params": {"intensity": 0.2}}]
        kept, dropped = dynamics.protect_calm(cues, self.regions)
        self.assertEqual(len(kept), 1)
        self.assertEqual(dropped, [])


class TestBudget(unittest.TestCase):
    def test_caps_density_and_drops_the_weakest(self):
        cues = [{"t": float(i * 6), "action": "rumble",
                 "params": {"intensity": 0.1 + i * 0.04}, "duration": 4.0}
                for i in range(20)]
        kept, dropped = dynamics.enforce_budget(cues, window=120.0, max_active=0.25)
        self.assertGreater(len(dropped), 0)
        # What survives a cap should be the peaks, which is the whole point.
        self.assertGreater(
            min(dynamics.intensity_of(c) for c in kept),
            min(dynamics.intensity_of(c) for c in dropped),
        )

    def test_a_sparse_score_is_untouched(self):
        cues = [{"t": 0.0, "params": {"i": 1.0}, "duration": 2.0},
                {"t": 100.0, "params": {"i": 1.0}, "duration": 2.0}]
        kept, dropped = dynamics.enforce_budget(cues, window=120.0, max_active=0.25)
        self.assertEqual(len(kept), 2)
        self.assertEqual(dropped, [])


class Luma:
    def __init__(self, mean):
        self.mean = mean
        self.peak = int(mean)


class TestLightLayers(unittest.TestCase):
    def test_soft_layer_respects_its_ceiling(self):
        out = light.soft_curve([(1.0, 1.0, 1.0)] * 10, gain=2.0)
        for r, g, b in out:
            self.assertLessEqual(max(r, g, b), light.SOFT_CEILING + 1e-9)

    def test_soft_layer_is_desaturated(self):
        out = light.soft_curve([(1.0, 0.0, 0.0)] * 10)
        r, g, b = out[0]
        self.assertGreater(g, 0.0)

    # Fading up from black to a dim shot is a large rise and not a flash.
    def test_a_rise_that_stays_dark_is_not_a_flash(self):
        self.assertEqual(light.flashes([Luma(5), Luma(30)], [(0, 0, 0)] * 2, 4.0), [])

    def test_dim_to_bright_is_a_flash(self):
        got = light.flashes([Luma(40), Luma(210)], [(1.0, 1.0, 0.9)] * 2, 4.0)
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0]["action"], "flash")

    def test_flash_is_pushed_to_full_brightness(self):
        got = light.flashes([Luma(40), Luma(210)], [(0.5, 0.4, 0.2)] * 2, 4.0)
        self.assertAlmostEqual(max(got[0]["params"].values()), 1.0, places=3)

    def test_flicker_does_not_produce_forty_cues(self):
        frames = []
        for _ in range(20):
            frames += [Luma(40), Luma(220)]
        got = light.flashes(frames, [(1, 1, 1)] * 40, fps=20.0, min_gap=0.6)
        self.assertLess(len(got), 5)


class TestWater(unittest.TestCase):
    def blue(self):
        return bytes([20, 90, 200] * 64)

    def red(self):
        return bytes([200, 60, 40] * 64)

    def test_blue_scores_higher_than_red(self):
        self.assertGreater(water.blueness(self.blue())[0], water.blueness(self.red())[0])

    def test_near_black_tells_us_nothing(self):
        self.assertEqual(water.blueness(bytes([2, 3, 5] * 64))[0], 0.0)

    def test_candidates_need_to_last(self):
        frames = [self.blue()] * 2 + [self.red()] * 40
        self.assertEqual(water.candidates(frames, fps=4.0, min_seconds=3.0), [])

    def test_sustained_blue_is_nominated(self):
        got = water.candidates([self.blue()] * 40, fps=4.0, min_seconds=3.0)
        self.assertEqual(len(got), 1)

    # Nothing drives a mister from a nomination alone.
    def test_confirmation_requires_a_second_source(self):
        nominations = [(0.0, 10.0, 0.5)]
        self.assertEqual(water.confirmed(nominations, []), [])
        self.assertEqual(water.confirmed(nominations, [(5.0, "a blue office")]), [])
        self.assertEqual(len(water.confirmed(nominations, [(5.0, "rain patters")])), 1)


if __name__ == "__main__":
    unittest.main()


class TestPlungeGates(unittest.TestCase):
    # The first version found thirty plunges in a scrolling test pattern.
    def test_a_steady_slow_drift_is_not_a_plunge(self):
        moves = [FakeMovement(0, -2, 0.02) for _ in range(40)]
        self.assertEqual(motion_est.find_plunges(moves, fps=4.0), [])

    def test_a_fast_sustained_fall_still_registers(self):
        moves = [FakeMovement(0, -6, 0.12) for _ in range(20)]
        self.assertEqual(len(motion_est.find_plunges(moves, fps=4.0)), 1)

    def test_vertical_movement_without_overall_speed_is_rejected(self):
        # Steady vertical shift, but almost nothing is actually moving.
        moves = [FakeMovement(0, -5, 0.01) for _ in range(20)]
        self.assertEqual(motion_est.find_plunges(moves, fps=4.0), [])


class TestFeaturelessFrames(unittest.TestCase):
    # A static dark shot was measured as the fastest movement in the film,
    # because every shift scored identically and min() returned the first key,
    # which is -max_shift.
    def test_flat_projections_report_no_movement(self):
        s, conf = motion_est.best_shift([0] * 16, [0] * 16, 8)
        self.assertEqual(s, 0)
        self.assertEqual(conf, 0.0)

    def test_identical_frames_report_no_movement(self):
        s, _ = motion_est.best_shift([3, 9, 2, 7] * 4, [3, 9, 2, 7] * 4, 4)
        self.assertEqual(s, 0)

    def test_track_zeroes_low_confidence_movement(self):
        class Flat:
            cols = [0] * 16
            rows = [0] * 9

        moves = motion_est.track([Flat(), Flat(), Flat()], width=16)
        for m in moves:
            self.assertEqual(m.speed, 0.0)

    def test_a_dark_static_stretch_is_calm(self):
        # The end to end shape of the bug: no audio, no movement, so the
        # activity level must be near zero rather than near a third.
        # A quiet stretch is quiet relative to the film's own peak, so the
        # signal has to contain that peak for the question to mean anything.
        levels = dynamics.activity(audio=[0.007] * 80 + [1.0] * 20,
                                   speed=[0.0] * 100, fps=4.0)
        self.assertLess(max(levels[:80]), 0.05)
        self.assertEqual(len(dynamics.calm_regions(levels, 4.0, min_seconds=12.0)), 1)
