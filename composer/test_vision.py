"""Tests for the vision seam.

Everything that needs a model or ffmpeg is exercised by running the composer;
the selection and parsing logic is pure and is tested here.
"""

import io
import tempfile
import unittest

import vision


class TestGrid(unittest.TestCase):
    def test_covers_the_whole_film(self):
        got = vision.grid(20.0, every=2.0)
        self.assertEqual(got[0], 1.0)
        self.assertEqual(got[-1], 19.0)
        self.assertEqual(len(got), 10)

    def test_a_budget_widens_rather_than_truncates(self):
        # The point of the whole arrangement. Five frames over a hundred
        # seconds must be spread across the hundred, not be the first five.
        got = vision.grid(100.0, every=2.0, limit=5)
        self.assertEqual(len(got), 5)
        self.assertGreater(got[-1], 80.0)

    def test_no_budget_leaves_the_spacing_alone(self):
        self.assertEqual(len(vision.grid(100.0, every=2.0)), 50)

    def test_nothing_from_nothing(self):
        self.assertEqual(vision.grid(0.0), [])
        self.assertEqual(vision.grid(-5.0), [])
        self.assertEqual(vision.grid(10.0, every=0.0), [])


class TestCadence(unittest.TestCase):
    def test_is_the_spacing_asked_for_when_it_fits(self):
        self.assertEqual(vision.cadence(100.0, 2.0, limit=0), 2.0)
        self.assertEqual(vision.cadence(100.0, 2.0, limit=500), 2.0)

    def test_widens_to_meet_a_budget(self):
        self.assertEqual(vision.cadence(100.0, 2.0, limit=5), 20.0)

    def test_the_grid_uses_it(self):
        # The two must agree. They once did not, and a cap of five returned a
        # hundred because the grid widened and nothing else was told.
        for limit in (0, 3, 5, 50, 5000):
            step = vision.cadence(400.0, 2.0, limit)
            got = vision.grid(400.0, 2.0, limit)
            if len(got) > 1:
                self.assertAlmostEqual(got[1] - got[0], step, places=2)


class TestSpacingOf(unittest.TestCase):
    def test_reads_the_step_off_an_even_grid(self):
        self.assertEqual(vision.spacing_of([1.0, 3.0, 5.0, 7.0]), 2.0)

    def test_survives_a_few_odd_moments_among_the_grid(self):
        self.assertEqual(vision.spacing_of([1.0, 3.0, 4.2, 5.0, 7.0, 9.0]), 2.0)

    def test_no_grid_in_scattered_times(self):
        self.assertEqual(vision.spacing_of([1.0, 8.0, 30.0]), 0.0)

    def test_too_few_to_tell(self):
        self.assertEqual(vision.spacing_of([1.0, 3.0]), 0.0)
        self.assertEqual(vision.spacing_of([]), 0.0)


class TestEvenlySpaced(unittest.TestCase):
    def test_separates_the_run_from_the_rest(self):
        run, rest = vision.evenly_spaced([1.0, 3.0, 4.2, 5.0, 7.0])
        self.assertEqual(run, [1.0, 3.0, 5.0, 7.0])
        self.assertEqual(rest, [4.2])

    def test_a_hole_ends_the_run(self):
        # One ffmpeg pass emits frames at a fixed cadence from where it starts,
        # so the nth output is only the nth grid point if none were skipped.
        run, rest = vision.evenly_spaced([1.0, 3.0, 7.0, 9.0])
        self.assertEqual(run, [1.0, 3.0])
        self.assertEqual(rest, [7.0, 9.0])

    def test_everything_is_accounted_for(self):
        times = [1.0, 3.0, 4.2, 5.0, 7.0, 20.5]
        run, rest = vision.evenly_spaced(times)
        self.assertEqual(sorted(run + rest), sorted(times))

    def test_scattered_times_are_all_left_over(self):
        run, rest = vision.evenly_spaced([1.0, 8.0, 30.0])
        self.assertEqual(run, [])
        self.assertEqual(rest, [1.0, 8.0, 30.0])


class TestCandidates(unittest.TestCase):
    def test_looks_at_the_whole_film_not_just_the_loud_parts(self):
        # The change that found the dust. Nomination by loudness cannot see
        # dust, mist or smoke, because they are quiet.
        env = [0.0] * 100
        env[10] = 0.9
        got = vision.candidates(env, rate=1.0, threshold=0.5, every=2.0)
        self.assertEqual(len(got), 50)
        self.assertLess(min(got), 2.0)
        self.assertGreater(max(got), 98.0)

    def test_quiet_films_are_still_looked_at(self):
        # Under the old shortlist this returned nothing at all: a film with no
        # loud moments was never shown to the model.
        got = vision.candidates([0.1] * 100, rate=1.0, threshold=0.5, every=2.0)
        self.assertEqual(len(got), 50)

    def test_a_loud_moment_between_grid_points_is_added(self):
        env = [0.0] * 100
        env[10] = 0.9
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0,
                                every=20.0)
        self.assertIn(10.0, got)

    def test_a_loud_moment_the_grid_already_covers_is_not_added_twice(self):
        # Including one landing exactly between two grid points, which is the
        # case that used to slip through and add a frame nobody needed.
        env = [0.0] * 100
        env[10] = 0.9
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0,
                                every=2.0)
        self.assertEqual(len(got), len(set(got)))
        self.assertEqual(len(got), 50)

    def test_the_cap_caps(self):
        # Including the nominations. The frames are what costs a GPU, so a
        # number called a cap is a bill and has to hold.
        env = [0.9 if i % 20 == 0 else 0.0 for i in range(2000)]
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0,
                                limit=5)
        self.assertLessEqual(len(got), 5)

    def test_scene_cuts_are_looked_at_when_the_grid_misses_them(self):
        got = vision.candidates([], rate=1.0, cuts=[10.0, 40.0, 70.0], limit=10)
        self.assertEqual(got, [10.0, 40.0, 70.0])

    def test_output_is_sorted(self):
        env = [0.0] * 200
        env[150] = 0.9
        env[20] = 0.95
        got = vision.candidates(env, rate=1.0, threshold=0.5, spacing=1.0)
        self.assertEqual(got, sorted(got))


class TestExtractUsesFilmTime(unittest.TestCase):
    """A chunk's times are counted from where the decode started.

    The frames are in the film. Handing a chunk-relative time to a seek that
    reads the film means a chunk starting an hour in asks for the frame one
    second from the film's beginning, gets it, and files it under the hour
    mark — so every chunk of a feature after the first described the opening,
    consistently and with complete confidence.

    Crab rave is one chunk, which is why this survived being looked at.
    """

    class Span:
        def __init__(self, decode_start):
            self.decode_start = decode_start

        def to_film_time(self, t):
            return self.decode_start + t

    def setUp(self):
        self.seeked = []
        self.original = vision.keyframe

        def watching(path, at, out_path):
            self.seeked.append(at)
            io.open(out_path, "w").write("x")
            return True

        vision.keyframe = watching

    def tearDown(self):
        vision.keyframe = self.original

    def test_a_seeked_frame_is_asked_for_in_film_time(self):
        # Two moments now: the frame, and the one a second before it that says
        # what was moving. Both in the film's clock.
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [4.0], tmp, span=self.Span(3600.0))
        self.assertEqual(self.seeked, [3603.0, 3604.0])

    def test_one_frame_when_no_gap_is_asked_for(self):
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [4.0], tmp, span=self.Span(3600.0), gap=0)
        self.assertEqual(self.seeked, [3604.0])

    def test_without_a_span_the_time_is_already_film_time(self):
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [4.0], tmp, span=None)
        self.assertEqual(self.seeked, [3.0, 4.0])

    def test_the_grid_pass_starts_at_the_chunk_not_the_film(self):
        # The grid does not go through keyframe(); it is one ffmpeg over the
        # range, so the offset it is given is the thing to check.
        seen = {}
        real_run = vision.subprocess.run

        def watching(args, **kwargs):
            if "-ss" in args:
                seen["ss"] = float(args[args.index("-ss") + 1])
            return real_run(["true"], **kwargs)

        vision.subprocess.run = watching
        try:
            with tempfile.TemporaryDirectory() as tmp:
                vision.extract("film.mkv", [1.0, 3.0, 5.0, 7.0], tmp,
                               span=self.Span(3600.0))
        finally:
            vision.subprocess.run = real_run
        # An hour in, and a second before the first grid frame so that frame
        # has a predecessor to be compared against.
        self.assertEqual(seen.get("ss"), 3600.0)

    def test_the_grid_pass_still_lands_on_the_chunk_without_a_pair(self):
        seen = {}
        real_run = vision.subprocess.run

        def watching(args, **kwargs):
            if "-ss" in args:
                seen["ss"] = float(args[args.index("-ss") + 1])
            return real_run(["true"], **kwargs)

        vision.subprocess.run = watching
        try:
            with tempfile.TemporaryDirectory() as tmp:
                vision.extract("film.mkv", [1.0, 3.0, 5.0, 7.0], tmp,
                               span=self.Span(3600.0), gap=0)
        finally:
            vision.subprocess.run = real_run
        self.assertEqual(seen.get("ss"), 3601.0)

    def test_observations_stay_in_chunk_time(self):
        # What is seeked for is film time; what is recorded is chunk time,
        # because the span moves everything into the film's clock at the end
        # and doing it twice would be as wrong as not doing it at all.
        with tempfile.TemporaryDirectory() as tmp:
            got = vision.observe("film.mkv", [4.0], "echo dust",
                                 span=self.Span(3600.0), workers=1)
        self.assertEqual([o["t"] for o in got], [4.0])
        self.assertEqual(self.seeked, [3603.0, 3604.0])


class TestObserveInParallel(unittest.TestCase):
    def test_every_frame_is_answered_for(self):
        # The pool must not lose or reorder frames.
        original = vision.keyframe

        def fake(path, at, out_path):
            io.open(out_path, "w").write("x")
            return True

        vision.keyframe = fake
        try:
            times = [1.0, 8.0, 30.0, 44.0, 61.0]
            with tempfile.TemporaryDirectory() as tmp:
                got = vision.observe("film.mkv", times, "echo dust", workers=4)
        finally:
            vision.keyframe = original
        self.assertEqual([o["t"] for o in got], times)
        # echo prints the image path too, which the parser reads as a label.
        self.assertTrue(all("dust" in o["labels"] for o in got))

    def test_one_bad_frame_does_not_cost_the_rest(self):
        original = vision.keyframe

        def fake(path, at, out_path):
            io.open(out_path, "w").write("x")
            return True

        vision.keyframe = fake
        try:
            with tempfile.TemporaryDirectory() as tmp:
                got = vision.observe("film.mkv", [1.0, 8.0, 30.0],
                                     "definitely-not-a-real-command", workers=4)
        finally:
            vision.keyframe = original
        self.assertEqual(got, [])


class TestPairs(unittest.TestCase):
    """A frame, and the one before it.

    Every question the model gets wrong is temporal — dust is thrown and snow
    is settled, a splash needs water that moved, activity is motion — and none
    of those is answerable from a still. Measured on sintel: SCENE settles from
    114 changes to 40 while EFFECTS does not move.
    """

    def setUp(self):
        self.given = []
        self.original = vision.subprocess.run

        def watching(args, **kwargs):
            # Everything after the command is an image path.
            self.given.append([a for a in args if a.endswith(".jpg")])
            return self.original(["true"], **kwargs)

        vision.subprocess.run = watching

    def tearDown(self):
        vision.subprocess.run = self.original

    def test_the_frame_in_question_is_handed_over_first(self):
        # The whole of the seam's compatibility. A wrapper written when this
        # passed one image reads argument one and gets the frame being asked
        # about, not the context frame, and ignores the rest.
        vision.observe_frame("look", ["/tmp/early.jpg", "/tmp/late.jpg"])
        self.assertEqual(self.given[-1], ["/tmp/late.jpg", "/tmp/early.jpg"])

    def test_a_single_image_still_works(self):
        vision.observe_frame("look", "/tmp/one.jpg")
        self.assertEqual(self.given[-1], ["/tmp/one.jpg"])

    def test_a_list_of_one_is_the_same_thing(self):
        vision.observe_frame("look", ["/tmp/one.jpg"])
        self.assertEqual(self.given[-1], ["/tmp/one.jpg"])


class TestPairsCostOneDecode(unittest.TestCase):
    """The grid is sampled at the gap, not extracted twice.

    ffmpeg decodes every frame whatever rate is asked of it — the fps filter
    only chooses which to keep — so a second pass shifted by a second would pay
    for the whole film again to gain frames it already had in its hands.
    """

    def setUp(self):
        self.calls = []
        self.original = vision.subprocess.run

        def watching(args, **kwargs):
            self.calls.append(list(args))
            return self.original(["true"], **kwargs)

        vision.subprocess.run = watching

    def tearDown(self):
        vision.subprocess.run = self.original

    def passes(self):
        return [c for c in self.calls if any("fps=" in str(a) for a in c)]

    def test_one_pass_over_the_film(self):
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [1.0, 3.0, 5.0, 7.0], tmp, gap=1.0)
        self.assertEqual(len(self.passes()), 1)

    def test_sampled_at_the_gap_rather_than_the_grid(self):
        # A two second grid with a one second gap has to be sampled at 1Hz, or
        # the frame before each one was never decoded.
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [1.0, 3.0, 5.0, 7.0], tmp, gap=1.0)
        graph = " ".join(str(a) for a in self.passes()[0])
        self.assertIn("fps=1.000000000", graph)

    def test_sampled_at_the_grid_when_no_gap_is_wanted(self):
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [1.0, 3.0, 5.0, 7.0], tmp, gap=0)
        graph = " ".join(str(a) for a in self.passes()[0])
        self.assertIn("fps=0.500000000", graph)

    def test_it_starts_a_gap_early(self):
        # The first grid frame needs a predecessor too, so the pass begins
        # before the grid does.
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [4.0, 6.0, 8.0], tmp, gap=1.0)
        args = self.passes()[0]
        self.assertEqual(float(args[args.index("-ss") + 1]), 3.0)

    def test_it_does_not_start_before_the_film_does(self):
        # A grid beginning at one second has no room for a predecessor, and
        # asking ffmpeg for a negative seek is not a way to find out.
        with tempfile.TemporaryDirectory() as tmp:
            vision.extract("film.mkv", [1.0, 3.0, 5.0], tmp, gap=1.0)
        args = self.passes()[0]
        self.assertGreaterEqual(float(args[args.index("-ss") + 1]), 0.0)


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
