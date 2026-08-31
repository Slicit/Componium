"""Reading a model's reply.

The labeller lives in hack/ because it is one implementation of a seam rather
than part of the composer, but its parser decides what reaches the score, and
that is worth pinning down. Loaded by path for the same reason.
"""

import importlib.util
import os
import unittest

_here = os.path.dirname(os.path.abspath(__file__))
_path = os.path.join(_here, "..", "hack", "vlm-label.py")
_spec = importlib.util.spec_from_file_location("vlm_label", _path)
vlm = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(vlm)


class TestParse(unittest.TestCase):
    def test_reads_effects_and_scene(self):
        self.assertEqual(
            vlm.parse("EFFECTS: dust, smoke\nSCENE: active"),
            ["dust", "smoke", "scene-active"])

    def test_none_produces_no_effects(self):
        self.assertEqual(vlm.parse("EFFECTS: none\nSCENE: calm"), ["scene-calm"])

    def test_water_is_a_question_of_its_own(self):
        """Water is a setting, not an effect, and asking it in the effects line
        meant it was simply never reported: a still sea on the horizon is the
        least event-like thing in a frame. Asked separately it is reliable, and
        it still reaches the composer as a plain label, because that is what
        confirms a blue scene really is water."""
        self.assertIn("water", vlm.parse("EFFECTS: none\nWATER: yes\nSCENE: calm"))

    def test_no_water_says_nothing(self):
        self.assertNotIn("water", vlm.parse("EFFECTS: dust\nWATER: no\nSCENE: active"))

    def test_water_alongside_effects(self):
        got = vlm.parse("EFFECTS: splash\nWATER: yes\nSCENE: active")
        self.assertIn("splash", got)
        self.assertIn("water", got)

    def test_rejects_a_word_that_is_not_in_the_vocabulary(self):
        # A model answering "pyroclastic flow" has produced a word that maps to
        # no instrument, which is silence that cost a GPU second.
        self.assertEqual(vlm.parse("EFFECTS: pyroclastic, smoke\nSCENE: active"),
                         ["smoke", "scene-active"])

    def test_water_is_no_longer_an_effect_word(self):
        # Only the WATER line may produce it, so a model naming it in the
        # effects list cannot smuggle it past the question.
        self.assertNotIn("water", vlm.EFFECTS)
        self.assertNotIn("water", vlm.parse("EFFECTS: water\nSCENE: calm"))

    def test_tolerates_the_shapes_models_wander_into(self):
        for reply in (
            "**EFFECTS:** dust\n**SCENE:** active",
            "- EFFECTS: dust\n- SCENE: active",
            "effects: DUST\nscene: ACTIVE",
            "EFFECTS:   dust  \n\nSCENE:  active  ",
        ):
            self.assertEqual(vlm.parse(reply), ["dust", "scene-active"], reply)

    def test_a_repeated_word_is_kept_once(self):
        self.assertEqual(vlm.parse("EFFECTS: dust, dust\nSCENE: calm"),
                         ["dust", "scene-calm"])

    def test_an_empty_reply_says_nothing(self):
        self.assertEqual(vlm.parse(""), [])
        self.assertEqual(vlm.parse("   "), [])

    def test_a_reply_with_no_recognised_lines_says_nothing(self):
        self.assertEqual(vlm.parse("I am afraid I cannot help with that."), [])


class TestDescribed(unittest.TestCase):
    def test_reads_the_sentence_off_a_comment_line(self):
        # Carried as a comment because the seam has always ignored those, so a
        # wrapper written before descriptions existed still works.
        reply = chr(10).join(["EFFECTS: dust", "SCENE: active", "SEEN: Crabs on sand."])
        self.assertEqual(vlm.described(reply), "Crabs on sand.")

    def test_says_nothing_when_there_is_no_sentence(self):
        self.assertEqual(vlm.described(chr(10).join(["EFFECTS: none", "SCENE: calm"])), "")
        self.assertEqual(vlm.described(""), "")

    def test_collapses_the_whitespace_a_wrapped_line_leaves(self):
        reply = "SEEN:  A ship   lands," + chr(10) + "   kicking up dust."
        self.assertEqual(vlm.described(reply), "A ship lands,")


class TestPrompt(unittest.TestCase):
    def test_asks_for_as_many_lines_as_it_wants(self):
        # The count is load bearing: with the prompt still saying two, the
        # water line was never emitted at all.
        self.assertIn("exactly four lines", vlm.PROMPT)
        for head in ("EFFECTS:", "WATER:", "SCENE:", "SEEN:"):
            self.assertIn(head, vlm.PROMPT)

    def test_the_sentence_is_asked_for_last(self):
        """Asked first it changed the answers: the model conditioned its labels
        on its own narration, and a crab kicking up sand came back a splash on
        water. Asked last, the labels match what it gives with no sentence at
        all."""
        self.assertLess(vlm.PROMPT.index("EFFECTS:"), vlm.PROMPT.index("SEEN:"))
        self.assertLess(vlm.PROMPT.index("SCENE:"), vlm.PROMPT.index("SEEN:"))

    def test_the_prompt_carries_no_notes_to_itself(self):
        # The explanation of why the sentence goes last belongs in a comment,
        # not in the tokens sent to the model, where it once ended up.
        self.assertNotIn("conditioned", vlm.PROMPT)
        self.assertNotIn("prompt", vlm.PROMPT.lower())

    def test_names_every_word_it_will_accept(self):
        for word in vlm.EFFECTS:
            self.assertIn(word, vlm.PROMPT, word)


if __name__ == "__main__":
    unittest.main()
