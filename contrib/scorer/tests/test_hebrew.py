"""Hebrew: a pointed target through espeak-ng's "he" voice, folded as its own IPA table has it.

The phones are those of the real voice, so these run with the venv and espeak-ng
(test.sh, not --pure) and are skipped without espeak-ng. The woven reading is the
segments path of test_segments.py, in Hebrew.
"""

import shutil
import unittest

from mw_scorer import align, g2p, phones

BERESHIT = "בְּרֵאשִׁית"
BARA = "בָּרָא"
ELOHIM = "אֱלֹהִים"
# Genesis 1:1 as the Masoretic text has it, cantillation and all
BERESHIT_CANT = "בְּרֵאשִׁ֖ית"
BARA_CANT = "בָּרָ֣א"
ELOHIM_CANT = "אֱלֹהִ֑ים"


def expected(word):
    return [str(p) for p in g2p.expected_woven([(word, "he")])[0].phones]


@unittest.skipUnless(shutil.which("espeak-ng"), "espeak-ng is not installed")
class Phones(unittest.TestCase):
    def test_the_points_of_three_words_of_genesis_1_1_are_read(self):
        self.assertEqual(expected(BERESHIT), ["b", "e", "ʁ", "e", "ʔ", "ʃ", "i", "t"])
        self.assertEqual(expected(BARA), ["b", "a", "ʁ", "a", "ʔ"])
        self.assertEqual(expected(ELOHIM), ["e", "l", "o", "h", "i", "m"])

    def test_the_dagesh_of_a_bet_makes_it_a_stop_and_its_absence_a_v(self):
        self.assertEqual(expected("בָּ")[0], "b")
        self.assertEqual(expected("בָ")[0], "v")

    def test_a_word_with_cantillation_phonemizes_as_the_same_word_without(self):
        for pointed, sung in [(BERESHIT, BERESHIT_CANT), (BARA, BARA_CANT), (ELOHIM, ELOHIM_CANT)]:
            self.assertEqual(g2p.phonemize([sung], "he"), g2p.phonemize([pointed], "he"), sung)
            self.assertEqual(expected(sung), expected(pointed), sung)

    def test_the_points_of_a_words_last_letter_and_the_sof_pasuq_after_it(self):
        self.assertEqual(g2p.target_words("הָאָֽרֶץ׃ לְךָ, בָּ"), ["הָאָֽרֶץ", "לְךָ", "בָּ"])
        self.assertEqual(g2p.target_words("“Hello,” τὸν."), ["Hello", "τὸν"])

    def test_the_shin_and_sin_dots_the_holam_and_a_he_are_not_spelled_out(self):
        self.assertEqual(g2p.phonemize(["שִׂים"], "he"), ["sim"])
        self.assertEqual(g2p.phonemize(["מֹשֶׁה"], "he")[0][:3], "moʃ")
        self.assertEqual(g2p.phonemize(["הַשָּׁמַיִם"], "he"), ["haʃamaim"])

    def test_a_maqaf_is_a_break_between_two_words(self):
        self.assertEqual(g2p.phonemize(["אֶל־יוֹנָה"], "he"), ["eljonaʔ"])
        self.assertTrue(g2p.phonemize(["יְהוָה־אֶל"], "he")[0].endswith("el"))

    def test_the_first_shewa_may_be_said_and_a_glottal_stop_may_be_left_out(self):
        word = g2p.expected_woven([(BERESHIT, "he")])[0]
        optional = [(str(p), isinstance(p, phones.Optional)) for p in word.phones]
        self.assertIn(("e", True), optional[:2])
        self.assertIn(("ʔ", True), optional)
        self.assertNotIn(("b", True), optional)


class Fold(unittest.TestCase):
    def test_the_spellings_of_one_hebrew_phone_are_folded_to_one(self):
        fold = lambda ipa: phones.to_ipa_phones(ipa, "he")
        self.assertEqual(fold("ħaχ"), ["χ", "a", "χ"])       # khet and khaf
        self.assertEqual(fold("ʕaʔ"), ["ʔ", "a", "ʔ"])        # ayin and alef
        self.assertEqual(fold("ʃɕʂ"), ["ʃ", "ʃ", "ʃ"])        # the sibilants
        self.assertEqual(fold("ɑɛəɪɔʊ"), ["a", "e", "e", "i", "o", "u"])   # five vowels, the shewa an e
        self.assertEqual(fold("ʁrɾ"), ["ʁ", "ʁ", "ʁ"])

    def test_a_vowel_a_step_away_is_a_near_miss_and_a_consonant_is_a_miss(self):
        cost = phones.ipa_cost("he")
        self.assertEqual(cost("e", "e"), 0)
        self.assertEqual(cost("e", "i"), 0.5)
        self.assertEqual(cost("o", "u"), 0.5)
        self.assertEqual(cost("a", "i"), 1)
        self.assertEqual(cost("ʃ", "s"), 1)


@unittest.skipUnless(shutil.which("espeak-ng"), "espeak-ng is not installed")
class Woven(unittest.TestCase):
    SEGMENTS = [("In the beginning", "en"), (BERESHIT + " " + BARA, "he")]

    def score(self, words, heard):
        return align.score_reading([(w.text, w.phones) for w in words], heard, 5.0, cost=phones.woven_cost)

    def heard(self, words, left_out=(), swapped=None):
        """The phones a listener hears: the words as expected, less the (word, place)s left
        out, with the phone of a (word, place) swapped for another, each 0.2 s long."""
        swapped = swapped or {}
        out, t = [], 0.0
        for w in words:
            for k, p in enumerate(w.phones):
                if (w.text, k) not in left_out:
                    out.append(align.Phone(swapped.get((w.text, k), str(p)), t, t + 0.2, ""))
                t += 0.2
        return out

    def test_each_piece_is_scored_in_its_own_language(self):
        words = g2p.expected_woven(self.SEGMENTS)
        self.assertEqual([(w.text, w.lang, w.segment) for w in words],
                         [("In", "en", 0), ("the", "en", 0), ("beginning", "en", 0), (BERESHIT, "he", 1), (BARA, "he", 1)])
        self.assertEqual({p.lang for w in words if w.lang == "he" for p in w.phones}, {"he"})
        result = self.score(words, self.heard(words))
        self.assertEqual([w["error"] for w in result["words"]], ["none"] * 5)
        self.assertEqual(result["accuracy"], 100)

    def test_the_optional_phones_left_out_cost_nothing(self):
        words = g2p.expected_woven(self.SEGMENTS)
        optional = {(w.text, k) for w in words if w.lang == "he" for k, p in enumerate(w.phones) if isinstance(p, phones.Optional)}
        self.assertEqual({(t, str(words[3 + (t == BARA)].phones[k])) for t, k in optional}, {(BERESHIT, "e"), (BERESHIT, "ʔ"), (BARA, "ʔ")})
        self.assertEqual(self.score(words, self.heard(words, left_out=optional))["accuracy"], 100)
        said = {(BERESHIT, 3)}   # the e of re, not optional
        self.assertNotEqual(self.score(words, self.heard(words, left_out=said))["words"][3]["error"], "none")

    def test_each_language_is_judged_by_its_own_table(self):
        words = g2p.expected_woven(self.SEGMENTS)
        # the Hebrew a heard as i is a miss, and the English words are not touched by it
        at = next(k for k, p in enumerate(words[4].phones) if str(p) == "a")
        errors = {w["text"]: w["error"] for w in self.score(words, self.heard(words, swapped={(BARA, at): "i"}))["words"]}
        self.assertEqual(errors["In"], "none")
        self.assertEqual(errors[BERESHIT], "none")
        self.assertNotEqual(errors[BARA], "none")


if __name__ == "__main__":
    unittest.main()
