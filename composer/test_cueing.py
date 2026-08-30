"""Slow movement becomes a tilt and is held; quick movement stays a shove.

A platform can hold a tilt forever and cannot hold a shift at all: gravity does
the work of a sustained tilt, and a sustained shift runs out of rail. So the
camera's movement is split by speed and the halves go to different axes.

Before this, everything was washed out — and a washout does not slow a movement
down, it deletes it. A six second plunge reached seven per cent of full travel
at the moment the camera had fully plunged.
"""

import unittest

from motion_est import Movement, limit_return, pose_series, split

FPS = 4.0

# Half the frame height, which is what pose_series treats as full deflection:
# a movement of half a frame between samples is as far as it goes.
FULL = 18.0


def ramp(seconds, hold=6.0, back=2.0, rest=3.0, to=FULL):
    """A camera that moves steadily one way, holds, and comes back."""
    out = []
    n = int(seconds * FPS)
    for i in range(n):
        out.append(to * (i / max(1, n - 1)))
    out += [to] * int(hold * FPS)
    n = int(back * FPS)
    for i in range(n):
        out.append(to * (1 - i / max(1, n - 1)))
    return out + [0.0] * int(rest * FPS)


def moved(dys):
    """Movements carrying only vertical travel.

    Speed is derived from dx and dy rather than passed, so a vertical-only
    movement carries a speed of its own accord.
    """
    return [Movement(dx=0, dy=dy, confidence=1.0, width=64) for dy in dys]


class TestSplit(unittest.TestCase):
    def test_the_halves_add_back_to_what_went_in(self):
        """The property the whole thing rests on: nothing is invented and
        nothing is lost, the movement only goes to two places."""
        values = ramp(6, to=1.0)
        slow, fast = split(values, int(4 * FPS))
        for v, s, f in zip(values, slow, fast):
            self.assertAlmostEqual(v, s + f, places=9)

    def test_a_slow_move_lands_almost_entirely_in_the_slow_half(self):
        slow, fast = split(ramp(8, to=1.0), int(4 * FPS))
        self.assertGreater(max(slow), 0.9)
        self.assertLess(max(abs(v) for v in fast), 0.35)

    def test_a_sudden_move_lands_in_the_fast_half(self):
        # A single frame jump: nothing slow about it.
        values = [0.0] * 20 + [1.0] * 4 + [0.0] * 20
        slow, fast = split(values, int(4 * FPS))
        self.assertGreater(max(abs(v) for v in fast), max(slow))

    def test_a_signal_shorter_than_the_window_is_all_fast(self):
        # Nothing to take a slow average from, and pretending otherwise would
        # invent a tilt out of three samples.
        slow, fast = split([1.0, 1.0, 1.0], 20)
        self.assertEqual(slow, [0.0, 0.0, 0.0])
        self.assertEqual(fast, [1.0, 1.0, 1.0])


class TestLimitReturn(unittest.TestCase):
    def test_going_out_is_not_slowed(self):
        # Going out is the effect and should be as quick as the film is.
        out = limit_return([0.0, 1.0, 1.0], FPS, 0.25)
        self.assertEqual(out[1], 1.0)

    def test_coming_back_is_capped(self):
        # Below about three degrees a second a returning tilt cannot be told
        # from a held one, which is what lets a platform pretend to sustain an
        # acceleration at all.
        out = limit_return([1.0, 0.0, 0.0, 0.0], FPS, 0.25)
        step = 0.25 / FPS
        self.assertAlmostEqual(out[1], 1.0 - step, places=6)

    def test_it_gets_there_in_the_end(self):
        out = limit_return([1.0] + [0.0] * 200, FPS, 0.25)
        self.assertAlmostEqual(out[-1], 0.0, places=6)

    def test_it_works_the_same_on_the_negative_side(self):
        out = limit_return([-1.0, 0.0, 0.0], FPS, 0.25)
        step = 0.25 / FPS
        self.assertAlmostEqual(out[1], -1.0 + step, places=6)

    def test_a_rate_of_zero_leaves_it_alone(self):
        values = [1.0, 0.0, 0.0]
        self.assertEqual(limit_return(values, FPS, 0), values)


class TestTheSixSecondPlunge(unittest.TestCase):
    """The shot this was built for: a camera that sinks over six seconds.

    The requirement, stated as it was asked for: the plunge should take the six
    seconds to go from nothing to everything, in the right direction, and stay
    there while the shot does.
    """

    def poses(self):
        return pose_series(moved(ramp(6)), FPS, gain=1.0, tilt_rate=0.25)

    def test_the_tilt_reaches_full_travel_by_the_end_of_the_move(self):
        poses = self.poses()
        at_six = poses[int(6 * FPS) - 1]
        self.assertGreater(abs(at_six["pitch"]), 0.8,
                           "the tilt is still at %.2f when the camera has "
                           "finished moving" % at_six["pitch"])

    def test_the_tilt_is_still_there_while_the_shot_is(self):
        poses = self.poses()
        during = [abs(p["pitch"]) for p in poses[int(7 * FPS):int(11 * FPS)]]
        self.assertGreater(min(during), 0.8, "the tilt let go during the shot")

    def test_it_climbs_rather_than_jumping(self):
        # Six seconds of camera should be six seconds of seat, not a step.
        poses = self.poses()[:int(6 * FPS)]
        rises = [abs(poses[i]["pitch"]) - abs(poses[i - 1]["pitch"])
                 for i in range(1, len(poses))]
        self.assertLess(max(rises), 0.25, "the tilt arrived in a jump")

    def test_the_shift_does_not_try_to_hold_it(self):
        # Heave is the transient axis and has no business sustaining anything;
        # holding it there is what walks a platform into its end stops.
        poses = self.poses()
        during = [abs(p["heave"]) for p in poses[int(7 * FPS):int(11 * FPS)]]
        self.assertLess(max(during), 0.3)

    def test_it_comes_back_without_being_felt(self):
        poses = pose_series(moved(ramp(6)), FPS, gain=1.0, tilt_rate=0.25)
        pitches = [p["pitch"] for p in poses]
        falls = [abs(pitches[i - 1]) - abs(pitches[i])
                 for i in range(1, len(pitches))
                 if abs(pitches[i]) < abs(pitches[i - 1])]
        if falls:
            self.assertLessEqual(max(falls), 0.25 / FPS + 1e-6,
                                 "the platform recentred faster than the rate cap")

    def test_a_quick_movement_still_arrives_as_a_shove(self):
        # The point of the split is that it does not smooth everything: a
        # sharp movement is supposed to be sharp.
        poses = pose_series(moved(ramp(0.5, hold=1.0, back=0.5)), FPS, gain=1.0)
        self.assertGreater(max(abs(p["heave"]) for p in poses), 0.3)


if __name__ == "__main__":
    unittest.main()
