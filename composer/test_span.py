"""What a range promises: seek fast, warm up, and answer in the film's time."""

import unittest

from span import Span, place, place_regions


class TestSpan(unittest.TestCase):
    def test_whole_film_seeks_nothing(self):
        s = Span()
        self.assertTrue(s.whole)
        self.assertEqual(s.input_args(), [])
        self.assertEqual(s.to_film_time(12.5), 12.5)

    def test_range_seeks_before_the_input(self):
        s = Span(start=600, end=900)
        self.assertEqual(s.input_args(), ["-ss", "600.000", "-t", "300.000"])

    def test_warmup_decodes_early_and_records_the_lead(self):
        s = Span(start=600, end=900, warmup=2)
        self.assertEqual(s.decode_start, 598)
        self.assertEqual(s.lead, 2)
        # The extra two seconds are decoded as well as the range itself, or the
        # range would come up two seconds short at the far end.
        self.assertEqual(s.input_args(), ["-ss", "598.000", "-t", "302.000"])

    def test_warmup_cannot_reach_before_the_film_starts(self):
        s = Span(start=1, end=100, warmup=2)
        self.assertEqual(s.decode_start, 0)
        self.assertEqual(s.lead, 1)
        self.assertEqual(s.input_args(), ["-t", "100.000"])

    def test_open_ended_range_asks_for_no_duration(self):
        s = Span(start=600, warmup=2)
        self.assertEqual(s.decode_duration, 0)
        self.assertEqual(s.input_args(), ["-ss", "598.000"])

    def test_time_is_reported_in_the_films_own_clock(self):
        s = Span(start=600, end=900, warmup=2)
        # Two seconds into what was decoded is the moment the range begins.
        self.assertEqual(s.to_film_time(2.0), 600.0)


class TestPlace(unittest.TestCase):
    def test_shifts_and_drops_the_lead(self):
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            "instrument": "wind.main", "type": "cue",
            "cues": [
                {"t": 0.0, "action": "gust"},    # inside the warmup
                {"t": 1.5, "action": "gust"},    # still the warmup
                {"t": 2.0, "action": "gust"},    # the range begins
                {"t": 60.0, "action": "gust"},
            ],
        }]
        out = place(tracks, span)
        self.assertEqual([c["t"] for c in out[0]["cues"]], [600.0, 658.0])

    def test_moves_curve_points_which_are_tuples_not_dicts(self):
        """A curve point is a bare (time, values) pair, not a dict.

        This is the shape the renderer actually iterates. Getting it wrong
        raised AttributeError on the first real chunked run rather than being
        caught here, which is what this test is for.
        """
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            "instrument": "shake.seat", "type": "curve",
            "points": [(0.0, {"intensity": 0.1}),
                       (2.0, {"intensity": 0.3}),
                       (60.0, {"intensity": 0.4})],
        }]
        out = place(tracks, span)
        self.assertEqual(out[0]["points"],
                         [(600.0, {"intensity": 0.3}), (658.0, {"intensity": 0.4})])

    def test_keeps_a_point_a_tuple(self):
        span = Span(start=10)
        tracks = [{"instrument": "a", "type": "curve", "points": [(1.0, {"i": 0.5})]}]
        moved = place(tracks, span)[0]["points"][0]
        self.assertIsInstance(moved, tuple)
        self.assertEqual(moved[1], {"i": 0.5})

    def test_leaves_the_payload_alone(self):
        span = Span(start=100, warmup=0)
        tracks = [{
            "instrument": "wind.main", "type": "cue",
            "cues": [{"t": 5.0, "action": "gust",
                      "params": {"intensity": 0.5}, "duration": 3}],
        }]
        cue = place(tracks, span)[0]["cues"][0]
        self.assertEqual(cue["t"], 105.0)
        self.assertEqual(cue["action"], "gust")
        self.assertEqual(cue["params"], {"intensity": 0.5})
        self.assertEqual(cue["duration"], 3)

    def test_does_not_mutate_what_it_was_given(self):
        span = Span(start=100)
        tracks = [{"instrument": "a", "type": "cue", "cues": [{"t": 1.0}]}]
        place(tracks, span)
        self.assertEqual(tracks[0]["cues"][0]["t"], 1.0)

    def test_drops_a_track_the_trim_emptied(self):
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            "instrument": "mist.main", "type": "cue",
            "cues": [{"t": 0.5, "action": "spray"}],   # entirely in the warmup
        }]
        self.assertEqual(place(tracks, span), [])

    def test_drops_anything_past_the_end(self):
        span = Span(start=0, end=100)
        tracks = [{
            "instrument": "a", "type": "curve",
            "points": [(50.0, {}), (150.0, {})],
        }]
        out = place(tracks, span)
        self.assertEqual([p[0] for p in out[0]["points"]], [50.0])

    def test_whole_film_is_left_exactly_as_it_was(self):
        tracks = [{"instrument": "a", "type": "curve", "points": [(1.0, {})]}]
        self.assertIs(place(tracks, Span()), tracks)

    def test_a_curve_holds_its_value_at_the_boundary(self):
        """A curve steady across the boundary still has a point at it.

        Compression runs before the trim and keeps a point only where the
        signal changed, so a curve can have nothing between the range's start
        and some moment well inside it. Without a holding point the merged
        curve ramps from the previous chunk's last value to that one, inventing
        a slow slide through a stretch the film spent still.
        """
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            'instrument': 'light.ambient', 'type': 'curve',
            'points': [(0.0, {'i': 0.4}), (60.0, {'i': 0.9})],
        }]
        out = place(tracks, span)
        self.assertEqual(out[0]['points'],
                         [(600.0, {'i': 0.4}), (658.0, {'i': 0.9})])

    def test_a_curve_with_nothing_inside_the_range_still_says_its_value(self):
        span = Span(start=600, end=900, warmup=2)
        tracks = [{'instrument': 'a', 'type': 'curve', 'points': [(0.0, {'i': 0.4})]}]
        self.assertEqual(place(tracks, span)[0]['points'], [(600.0, {'i': 0.4})])

    def test_a_cue_is_never_moved_to_the_boundary(self):
        """Moving an event to the boundary reports something that did not
        happen there, which is the opposite of what a holding point does for a
        curve."""
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            'instrument': 'wind.main', 'type': 'cue',
            'cues': [{'t': 0.0, 'action': 'gust'}],
        }]
        self.assertEqual(place(tracks, span), [])

    def test_a_curve_already_starting_at_the_boundary_gains_nothing(self):
        span = Span(start=600, end=900, warmup=2)
        tracks = [{
            'instrument': 'a', 'type': 'curve',
            'points': [(0.0, {'i': 0.4}), (2.0, {'i': 0.7})],
        }]
        self.assertEqual(place(tracks, span)[0]['points'], [(600.0, {'i': 0.7})])


class TestPlaceRegions(unittest.TestCase):
    def test_clipped_not_dropped_at_a_boundary(self):
        span = Span(start=600, end=900, warmup=2)
        # A calm stretch running from before the range to inside it.
        self.assertEqual(place_regions([(0.0, 62.0)], span), [(600.0, 660.0)])

    def test_past_the_far_edge_is_clipped_to_it(self):
        span = Span(start=0, end=100)
        self.assertEqual(place_regions([(80.0, 140.0)], span), [(80.0, 100.0)])

    def test_entirely_in_the_lead_is_dropped(self):
        span = Span(start=600, end=900, warmup=2)
        self.assertEqual(place_regions([(0.0, 1.0)], span), [])


if __name__ == '__main__':
    unittest.main()
