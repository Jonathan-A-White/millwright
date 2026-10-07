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
ERRORS = {"none", "omission", "insertion", "mispronunciation", "hesitation"}


def clip(name):
    with open(os.path.join(FIXTURES, name), "rb") as f:
        return f.read()


class Fixtures(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.scorer = pipeline.Scorer()

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

    def test_a_target_with_punctuation_is_scored_by_its_words(self):
        result = self.scorer.score("The cat sat on the mat.", "en", clip("clean.wav"))
        self.assertEqual([w["text"] for w in result["words"]], ["The", "cat", "sat", "on", "the", "mat"])
        self.assertEqual({w["error"] for w in result["words"]}, {"none"})

    def test_audio_that_is_not_a_wav_is_refused(self):
        with self.assertRaises(ValueError):
            self.scorer.score(TARGET, "en", b"not a wav file")


if __name__ == "__main__":
    unittest.main()
