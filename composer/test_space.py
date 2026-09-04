"""The colour space survives being written, and being written again.

A flash is authored in hue and every light driver reads red. The conversion
between them is turned on by the track saying which space it is in, so a track
that loses that declaration reaches its fixture with no channel any driver
reads: the light stays dark while the cue is acknowledged, counted and logged as
delivered.

Both writers dropped it on cue tracks, and only on cue tracks. A chunked
analysis reads and rewrites every score it merges, so the second writer lost it
on exactly the films long enough to be chunked.
"""

import unittest

import compose
import remap


FLASHES = {
    "instrument": "light.event",
    "type": "cue",
    "space": "hsi",
    "cues": [
        {"t": "00:00:01.000", "action": "flash", "duration": "0.2s",
         "params": {"h": 0.5, "s": 1.0, "i": 1.0}},
    ],
}


class TheSpaceReachesTheFile(unittest.TestCase):
    def test_compose_writes_it_for_a_cue_track(self):
        text = compose.render(
            {"title": "t", "duration": 1.0, "fps": 24.0},
            [{"instrument": "light.event", "type": "cue", "space": "hsi",
              "cues": [{"t": 1.0, "action": "flash", "duration": 0.2,
                        "params": {"h": 0.5, "s": 1.0, "i": 1.0}}]}])
        self.assertIn('space = "hsi"', text)

    def test_a_merge_does_not_drop_it(self):
        # remap reads a score and writes it back, which is what a chunked
        # analysis does on every join. Anything this loses is lost silently.
        text = remap.dump({"score": {"componium": "0.1", "title": "t"},
                           "track": [FLASHES]})
        self.assertIn('space = "hsi"', text)

    def test_a_merge_keeps_the_parameters_that_need_it(self):
        # The declaration is only worth anything while the values it describes
        # are still there.
        text = remap.dump({"score": {"componium": "0.1", "title": "t"},
                           "track": [FLASHES]})
        for want in ("h = ", "s = ", "i = "):
            self.assertIn(want, text)


if __name__ == "__main__":
    unittest.main()
