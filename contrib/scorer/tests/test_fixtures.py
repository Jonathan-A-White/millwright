"""The whole pipeline (espeak-ng, the wav2vec2 model, the alignment) on the
three fixture clips of "the cat sat on the mat" that fixtures/make.sh makes.

Needs the scorer's venv, espeak-ng and the model in the cache: test.sh runs
these, and refuses with a plain line when one is missing.
"""

import os
import unittest

from mw_scorer import pipeline

HERE = os.path.dirname(os.path.abspath(__file__))
FIXTURES = os.path.join(HERE, "..", "fixtures")
TARGET = "the cat sat on the mat"
ERRORS = {"none", "omission", "insertion", "mispronunciation", "hesitation", "not_reached"}


def clip(name):
    with open(os.path.join(FIXTURES, name), "rb") as f:
        return f.read()


_scorer = None


def scorer():
    """One Scorer for the whole run: loading the model is the slow part."""
    global _scorer
    if _scorer is None:
        _scorer = pipeline.Scorer()
    return _scorer


class Fixtures(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.scorer = scorer()

    def score(self, name):
        result = self.scorer.score(TARGET, "en", clip(name))
        self.assertContract(result)
        return result

    def assertContract(self, result):
        """The checks of docs/scorers.md, as application.ReadingResult.Validate makes them."""
        self.assertEqual(result["engine"], "local")
        self.assertTrue(result["words"])
        self.assertTrue(0 <= result["accuracy"] <= 100)
        self.assertIsInstance(result["accuracy"], int)
        self.assertGreater(result["seconds"], 0)
        for w in result["words"]:
            self.assertTrue(w["text"].strip())
            self.assertIn(w["error"], ERRORS)
            self.assertIsInstance(w["accuracy"], int)
            self.assertTrue(0 <= w["accuracy"] <= 100)
            self.assertIsInstance(w["expected_phonemes"], list)
            self.assertIsInstance(w["produced_phonemes"], list)
            self.assertIsInstance(w["self_corrected"], bool)
        Fixtures.assertWordTimes(self, result)

    def assertWordTimes(self, result):
        """Every word that was heard has 0 <= start < end <= the clip's length, and
        the words are in order; the others (omitted, not reached) have null times."""
        last = 0.0
        for w in result["words"]:
            if w["produced_phonemes"]:
                self.assertTrue(0 <= w["start"] < w["end"] <= result["seconds"], w)
                self.assertGreaterEqual(w["start"], last, w)
                last = w["end"]
            else:
                self.assertEqual((w["start"], w["end"]), (None, None), w)

    def errors(self, result):
        return [(w["text"], w["error"]) for w in result["words"]]

    def test_the_clean_reading_is_all_none(self):
        result = self.score("clean.wav")
        self.assertEqual(self.errors(result), [(w, "none") for w in TARGET.split()])
        self.assertEqual(result["words"][1]["expected_phonemes"], ["K", "AE", "T"])
        self.assertEqual(result["words"][1]["produced_phonemes"], ["K", "AE", "T"])
        self.assertGreaterEqual(result["accuracy"], 90)

    def test_cap_for_cat_is_a_mispronunciation_on_cat_alone(self):
        result = self.score("cap.wav")
        want = [(w, "mispronunciation" if w == "cat" else "none") for w in TARGET.split()]
        self.assertEqual(self.errors(result), want)
        self.assertEqual(result["words"][1]["produced_phonemes"][-1], "P")
        self.assertLess(result["words"][1]["accuracy"], 80)

    def test_the_first_the_dropped_is_an_omission(self):
        result = self.score("no-the.wav")
        want = [(w, "omission" if i == 0 else "none") for i, w in enumerate(TARGET.split())]
        self.assertEqual(self.errors(result), want)
        self.assertEqual(result["words"][0]["produced_phonemes"], [])

    def test_a_reading_that_stops_early_leaves_the_rest_not_reached(self):
        result = self.score("partial.wav")
        want = [(w, "none" if i < 4 else "not_reached") for i, w in enumerate(TARGET.split())]
        self.assertEqual(self.errors(result), want)
        for w in result["words"][4:]:
            self.assertEqual((w["produced_phonemes"], w["accuracy"]), ([], 0))
        self.assertGreaterEqual(result["accuracy"], 90)

    def test_the_words_are_timed_in_the_recording(self):
        clean = self.score("clean.wav")
        self.assertTrue(all(w["start"] is not None for w in clean["words"]))
        nothe = self.score("no-the.wav")
        self.assertEqual((nothe["words"][0]["start"], nothe["words"][0]["end"]), (None, None))
        partial = self.score("partial.wav")
        for w in partial["words"][4:]:
            self.assertEqual((w["start"], w["end"]), (None, None))

    def test_a_target_with_punctuation_is_scored_by_its_words(self):
        result = self.scorer.score("The cat sat on the mat.", "en", clip("clean.wav"))
        self.assertEqual([w["text"] for w in result["words"]], ["The", "cat", "sat", "on", "the", "mat"])
        self.assertEqual({w["error"] for w in result["words"]}, {"none"})

    def test_audio_that_is_not_a_wav_is_refused(self):
        with self.assertRaises(ValueError):
            self.scorer.score(TARGET, "en", b"not a wav file")


GREEK = "Οίδαμεν δε ότι τοις αγαπώσι τον Θεόν πάντα συνεργεί εις αγαθόν"


class GreekFixtures(unittest.TestCase):
    """Romans 8:28's first clause in modern Greek, read by espeak-ng's Greek voice.

    The model had no Greek in its training, so a reading is compared with a
    little more tolerance (phones.ipa_cost) and the clean clip is held to the
    reading's accuracy, as the English one is, not to every word."""

    @classmethod
    def setUpClass(cls):
        cls.scorer = scorer()

    def assertContract(self, result):
        Fixtures.assertContract(self, result)

    def score_el(self, name, target=GREEK):
        result = self.scorer.score(target, "el", clip(name))
        self.assertContract(result)
        return result

    def test_the_clean_greek_reading_is_read(self):
        result = self.score_el("el-clean.wav")
        self.assertEqual([w["text"] for w in result["words"]], GREEK.split())
        self.assertGreaterEqual(result["accuracy"], 90)
        read = [w for w in result["words"] if w["error"] == "none"]
        self.assertGreaterEqual(len(read), len(result["words"]) - 1)
        self.assertEqual(result["words"][3]["expected_phonemes"], ["t", "i", "s"])  # IPA, not ARPAbet
        self.assertEqual(result["words"][3]["produced_phonemes"], ["t", "i", "s"])

    def test_the_misread_word_is_marked(self):
        clean, misread = self.score_el("el-clean.wav"), self.score_el("el-misread.wav")
        word = misread["words"][3]  # τοις, read τους
        self.assertEqual((word["text"], word["error"]), ("τοις", "mispronunciation"))
        self.assertEqual(word["produced_phonemes"][-1], "s")
        self.assertIn("u", word["produced_phonemes"])
        self.assertLess(word["accuracy"], clean["words"][3]["accuracy"])

    def test_a_target_in_greek_with_punctuation_is_scored_by_its_words(self):
        result = self.scorer.score("Οίδαμεν δε, ότι τοις αγαπώσι τον Θεόν· πάντα συνεργεί εις αγαθόν.", "el", clip("el-clean.wav"))
        self.assertEqual([w["text"] for w in result["words"]], GREEK.split())

    def test_a_language_espeak_does_not_know_is_refused(self):
        with self.assertRaises(ValueError):
            self.scorer.score(TARGET, "xx-nope", clip("clean.wav"))


if __name__ == "__main__":
    unittest.main()
