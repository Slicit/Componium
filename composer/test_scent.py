"""Choosing a scent, and refusing most of them.

Scent is the one effect that cannot be taken back. A light is instant, a fan
decays in seconds, and a puff hangs in a room for minutes — so two inside a
minute are not two events, they are mud, and the interesting behaviour here is
what gets refused rather than what gets chosen.
"""

import unittest

import scent


class TestChoose(unittest.TestCase):
    def test_reads_a_place(self):
        self.assertEqual(scent.choose("A battlefield strewn with wreckage on fire"),
                         "smoke")
        self.assertEqual(scent.choose("A cave lit by torches"), "earth")
        self.assertEqual(scent.choose("Waves breaking on the shore"), "sea")

    def test_says_nothing_about_a_room(self):
        # Most of most films is people talking somewhere with no smell. A bank
        # that fires at those is a bank nobody keeps filled.
        self.assertIsNone(scent.choose("Two people talking in a corridor"))
        self.assertIsNone(scent.choose(""))
        self.assertIsNone(scent.choose(None))

    def test_the_stronger_scent_wins_a_tie(self):
        # A burning village is both, and it smells of the fire. Bank order is
        # the tiebreak, and the earlier entries are the ones that carry more of
        # a library.
        self.assertEqual(scent.choose("A village on fire, mud underfoot"), "smoke")

    def test_a_smaller_bank_only_offers_what_it_holds(self):
        # A rig with five reservoirs must not be sent the sixth scent. It gets
        # silence rather than an approximation, because an approximation of a
        # smell is a different smell.
        self.assertEqual(scent.choose("A tavern full of drinkers"), "spirits")
        self.assertIsNone(scent.choose("A tavern full of drinkers",
                                       bank=scent.NECESSARY))

    def test_the_bank_is_the_three_tiers(self):
        self.assertEqual(len(scent.BANK), 15)
        self.assertEqual(scent.BANK[:5], scent.NECESSARY)
        self.assertEqual(len(set(scent.BANK)), 15)

    def test_every_scent_in_the_bank_can_be_reached(self):
        # A reservoir nothing can address is a reservoir nobody should buy.
        reachable = {name for name, _rx in scent.MATCH}
        for name in scent.BANK:
            self.assertIn(name, reachable, name + " is in the bank and unreachable")


def seen(rows):
    return [{"t": t, "place": p, "doing": ""} for t, p in rows]


class TestScenes(unittest.TestCase):
    def test_neighbours_that_agree_are_one_stretch(self):
        got = scent.scenes(seen([(0, "a forest"), (10, "a forest"),
                                 (20, "a forest"), (30, "a forest"),
                                 (40, "a forest"), (50, "a forest")]))
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0][2], "pine")

    def test_passing_through_somewhere_is_not_being_there(self):
        # Ten seconds of forest between two rooms is not a forest.
        got = scent.scenes(seen([(0, "a corridor"), (10, "a forest"),
                                 (20, "a corridor"), (30, "a corridor")]))
        self.assertEqual(got, [])

    def test_a_scene_that_holds_earns_one(self):
        rows = [(t, "a forest") for t in range(0, 120, 10)]
        got = scent.scenes(seen(rows))
        self.assertEqual(len(got), 1)
        self.assertGreaterEqual(got[0][1] - got[0][0], scent.HOLD_SECONDS)

    def test_nothing_from_nothing(self):
        self.assertEqual(scent.scenes([]), [])


class TestRation(unittest.TestCase):
    def test_one_puff_per_stretch(self):
        got = scent.ration([(0.0, 100.0, "smoke")])
        self.assertEqual(got, [(0.0, "smoke")])

    def test_a_second_scent_too_soon_is_refused(self):
        # Not delayed. Delaying it puts the smell of one scene into the next,
        # which is the exact failure the wait exists to prevent.
        got = scent.ration([(0.0, 60.0, "smoke"), (60.0, 120.0, "sea")])
        self.assertEqual(got, [(0.0, "smoke")])

    def test_far_enough_apart_and_both_fire(self):
        got = scent.ration([(0.0, 100.0, "smoke"),
                            (scent.LINGER_SECONDS + 1, 400.0, "sea")])
        self.assertEqual([s for _t, s in got], ["smoke", "sea"])

    def test_it_lands_at_the_start(self):
        # A scent takes time to arrive, and a scene that has already ended does
        # not want to start smelling of itself.
        got = scent.ration([(30.0, 200.0, "pine")])
        self.assertEqual(got[0][0], 30.0)


class TestCues(unittest.TestCase):
    def test_a_feature_gets_a_handful(self):
        # Two hours of film alternating between two places. Without rationing
        # this is hundreds of puffs; with it, one every four minutes at most.
        rows = []
        for t in range(0, 7200, 10):
            rows.append((t, "a forest" if (t // 600) % 2 else "a burning village"))
        got = scent.cues(seen(rows), "scent.main")
        self.assertLessEqual(len(got), 7200 / scent.LINGER_SECONDS + 1)
        self.assertGreater(len(got), 2)

    def test_the_cue_names_its_scent(self):
        rows = [(t, "a burning village") for t in range(0, 120, 10)]
        got = scent.cues(seen(rows), "scent.main")
        self.assertEqual(got[0]["scent"], "smoke")
        self.assertEqual(got[0]["instrument"], "scent.main")
        self.assertEqual(got[0]["action"], "puff")
        self.assertIn("scene:", got[0]["source"])

    def test_a_film_of_conversations_gets_none(self):
        rows = [(t, "two people talking") for t in range(0, 600, 10)]
        self.assertEqual(scent.cues(seen(rows), "scent.main"), [])


if __name__ == "__main__":
    unittest.main()
