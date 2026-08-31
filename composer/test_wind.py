"""Wind, and the quantity it is made of.

The fault was not sensitivity. Wind was built on apparent speed, which is a
single translation of the whole frame, so a pan across a static room read as
maximal and a forward dolly — driving, running, flying, the one case where air
actually rushes past — expanded about the centre, cancelled, and read as
nothing. Measured on synthetic clips where the move was known exactly:

    static   speed 0.0000   wind 0.000
    pan      speed 0.0156   wind 1.000
    dolly    speed 0.0004   wind 0.109

These work on projections directly rather than on films, because that is where
the distinction lives: a pan shifts both halves of a frame the same way and a
forward move pushes them apart.
"""

import unittest

import motion_est


class Frame:
    """Just enough of analysis.Frame for the tracker.

    The rows need structure as well as the columns. track() takes the lower of
    the two confidences and reports no movement when it is poor, so a flat
    vertical projection zeroes a perfectly good horizontal match — which is the
    estimator refusing to guess, and made the first version of this fixture
    look like a bug in the code.
    """

    def __init__(self, cols, rows=None):
        self.cols = cols
        self.rows = rows if rows is not None else ramp(36)


def ramp(n=64):
    """A projection with enough structure to match against, and no period.

    A repeating pattern makes a shift ambiguous — under a period of eight, a
    shift of three matches as well as one of minus five, and best_shift breaks
    the tie toward standing still. That is the estimator being careful, and it
    made the first version of this fixture untestable.
    """
    return [float((i * i) % 61) for i in range(n)]


def shifted(values, by):
    """The same projection moved along, wrapping."""
    return values[-by:] + values[:-by] if by else list(values)


def stretched(values, by):
    """Both halves moved apart by `by`, which is what moving forward does."""
    half = len(values) // 2
    left = shifted(values[:half], -by)
    right = shifted(values[half:], by)
    return left + right


class TestExpansion(unittest.TestCase):
    def test_a_pan_is_not_expansion(self):
        base = ramp()
        got = motion_est.track([Frame(base), Frame(shifted(base, 3))],
                               width=64)
        self.assertEqual(got[0].dx, 3)
        self.assertAlmostEqual(got[0].expansion, 0.0, places=6)

    def test_a_forward_move_is_expansion(self):
        # The case the old signal could not see at all. A stretch is not a
        # shift, so the global matcher still lands on something — asserting it
        # lands on exactly zero would be a claim about the artefacts of a
        # synthetic fixture. What matters is that expansion separates the two
        # where speed does not.
        base = ramp()
        forward = motion_est.track([Frame(base), Frame(stretched(base, 2))],
                                   width=64)[0]
        pan = motion_est.track([Frame(base), Frame(shifted(base, 3))],
                               width=64)[0]
        self.assertGreater(forward.expansion, 0.0)
        self.assertAlmostEqual(pan.expansion, 0.0, places=6)
        # And the old signal cannot: the pan is the faster of the two.
        self.assertGreater(pan.speed, forward.speed)

    def test_pulling_back_is_expansion_the_other_way(self):
        base = ramp()
        got = motion_est.track([Frame(base), Frame(stretched(base, -2))],
                               width=64)
        self.assertLess(got[0].expansion, 0.0)

    def test_a_still_frame_is_neither(self):
        base = ramp()
        got = motion_est.track([Frame(base), Frame(base)], width=64)
        self.assertEqual(got[0].dx, 0)
        self.assertAlmostEqual(got[0].expansion, 0.0, places=6)


class TestWindSeries(unittest.TestCase):
    FPS = 12.0

    def make(self, expansions):
        """Movements whose expansion is given as a rate, per second.

        wind_series multiplies by the sampling rate to get there, so a test
        that wants a movement worth full wind divides by the same rate. Saying
        it in the units the constant is in keeps the arithmetic in one place.
        """
        out = []
        for e in expansions:
            m = motion_est.Movement(0, 0, 1.0, 64)
            m.expansion = e / self.FPS
            out.append(m)
        return out

    def test_a_pan_produces_no_wind(self):
        # Movement with no expansion is a camera crossing the scene, not one
        # travelling through it, and a fan has nothing to say about it.
        movements = [motion_est.Movement(8, 0, 1.0, 64) for _ in range(40)]
        got = motion_est.wind_series(movements, self.FPS)
        self.assertEqual(max(got), 0.0)

    def test_moving_forward_produces_wind(self):
        got = motion_est.wind_series(
            self.make([motion_est.FULL_WIND_RATE] * 40), 12.0)
        self.assertGreater(max(got), 0.9)

    def test_pulling_back_does_not(self):
        # Also movement through air, and not what a fan in front of a seat is
        # for. Blowing on every cut to a wider shot is the failure this avoids.
        got = motion_est.wind_series(
            self.make([-motion_est.FULL_WIND_RATE] * 40), 12.0)
        self.assertEqual(max(got), 0.0)

    def test_the_scale_is_absolute(self):
        # The old series divided by the film's own peak, so a film of two
        # people talking had its mildest camera move rendered as a gale. A
        # gentle move must read gently however gentle the rest of the film is.
        gentle = motion_est.wind_series(
            self.make([motion_est.FULL_WIND_RATE * 0.1] * 40), 12.0)
        self.assertLess(max(gentle), 0.25)

    def test_it_cannot_exceed_full(self):
        got = motion_est.wind_series(
            self.make([motion_est.FULL_WIND_RATE * 10] * 40), 12.0)
        self.assertLessEqual(max(got), 1.0)

    def test_a_brief_lurch_is_smoothed_away(self):
        # A fan takes over a second to change speed, so one frame of movement
        # is not something it can render and not something worth sending.
        one = [0.0] * 20 + [motion_est.FULL_WIND_RATE] + [0.0] * 20
        got = motion_est.wind_series(self.make(one), self.FPS)
        self.assertLess(max(got), 0.3)

    def test_a_sustained_move_is_not(self):
        held = [0.0] * 10 + [motion_est.FULL_WIND_RATE] * 30 + [0.0] * 10
        got = motion_est.wind_series(self.make(held), self.FPS)
        self.assertGreater(max(got), 0.8)

    def test_nothing_from_nothing(self):
        self.assertEqual(motion_est.wind_series([], self.FPS), [])


if __name__ == "__main__":
    unittest.main()
