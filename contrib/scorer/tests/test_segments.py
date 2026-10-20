"""A woven reading: ordered segments, each in its own language, scored in IPA.

Pure: espeak-ng is stood in for by a table, and the alignment is given the
phones a listener would hear, so these run with any python3 (test.sh --pure).
The same reading through the real voices and the model is in test_fixtures.py.
"""

import unittest

from mw_scorer import align, g2p, phones

# What espeak-ng's en-us and el voices say for the words below (their IPA as
# phonemizer returns it, stress dropped). "τὸν κόσμον" in the English voice is
# a made-up reading of Greek letters: all the test needs is that it is not Greek.
IPA = {
    ("en", "For"): "fɔːɹ", ("en", "God"): "ɡɑːd", ("en", "so"): "soʊ", ("en", "loved"): "lʌvd",
    ("el", "τὸν"): "ton", ("el", "κόσμον"): "kozmon",
    ("en", "τὸν"): "ɡɹiːk", ("en", "κόσμον"): "lɛtɚz",
}


def phonemize(words, lang):
    return [IPA[(lang.split("-")[0], w)] for w in words]


WOVEN = [("For God so loved", "en"), ("τὸν κόσμον", "el")]


def produced_from(words, drop=()):
    """The phones a listener hears when the words are read as expected, each 0.2 s long."""
    out, t = [], 0.0
    for text, expected in words:
        for p in expected:
            if text not in drop:
                out.append(align.Phone(str(p), t, t + 0.2, ""))
            t += 0.2
    return out


class Expected(unittest.TestCase):
    def test_each_segment_goes_through_its_own_voice_in_order(self):
        asked = []

        def spy(words, lang):
            asked.append((lang, list(words)))
            return phonemize(words, lang)

        g2p.expected_woven(WOVEN, spy)
        self.assertEqual(asked, [("en", ["For", "God", "so", "loved"]), ("el", ["τὸν", "κόσμον"])])

    def test_english_ipa_then_greek_ipa_and_each_word_keeps_its_segment_and_language(self):
        words = g2p.expected_woven(WOVEN, phonemize)
        self.assertEqual([(w.text, w.lang, w.segment) for w in words],
                         [("For", "en", 0), ("God", "en", 0), ("so", "en", 0), ("loved", "en", 0),
                          ("τὸν", "el", 1), ("κόσμον", "el", 1)])
        by = {w.text: list(w.phones) for w in words}
        self.assertEqual(by["loved"], ["l", "ʌ", "v", "d"])   # English, not folded
        self.assertEqual(by["τὸν"], ["t", "o", "n"])           # Greek, folded
        self.assertEqual(by["κόσμον"], ["k", "o", "z", "m", "o", "n"])
        self.assertEqual({p.lang for w in words if w.lang == "el" for p in w.phones}, {"el"})
        self.assertEqual({p.lang for w in words if w.lang == "en" for p in w.phones}, {"en"})

    def test_a_region_is_its_language_and_empty_segments_keep_their_numbers(self):
        words = g2p.expected_woven([("For", "en-US"), ("  ", "el"), ("τὸν", "el-GR")], phonemize)
        self.assertEqual([(w.text, w.lang, w.segment) for w in words], [("For", "en", 0), ("τὸν", "el", 2)])

    def test_the_words_of_the_segments_are_those_of_their_text_joined(self):
        text = " ".join(t for t, _ in WOVEN)
        self.assertEqual([(w, l) for w, l, _ in g2p.segment_words(WOVEN)],
                         [(w, "en") for w in g2p.target_words(WOVEN[0][0])] + [(w, "el") for w in g2p.target_words(WOVEN[1][0])])
        self.assertEqual([w for w, _, _ in g2p.segment_words(WOVEN)], g2p.target_words(text))


class Cost(unittest.TestCase):
    def test_an_english_phone_is_compared_as_english_and_a_greek_one_as_greek(self):
        en, el = (lambda p: phones.speak(p, "en")), (lambda p: phones.speak(p, "el"))
        cost = phones.woven_cost
        self.assertEqual(cost(en("ɪ"), "i"), 0.5)   # IH for IY: a vowel a step away
        self.assertEqual(cost(el("i"), "ɪ"), 0)      # Greek has one i
        self.assertEqual(cost(en("ʃ"), "s"), 1)      # sheep is not seep
        self.assertEqual(cost(el("s"), "ʃ"), 0)      # the hush of a Greek σ is a spelling of it
        self.assertEqual(cost(en("ɹ"), "r"), 0)      # one English r

    def test_an_optional_phone_stays_optional_and_keeps_its_language(self):
        nasals = phones.with_optional_nasals("Πάντοτε", ["p", "a", "d", "o", "t", "e"], "el")
        tagged = phones.speak(nasals[2], "el")
        self.assertIsInstance(tagged, phones.Optional)
        self.assertEqual((str(tagged), tagged.lang), ("n", "el"))


class Woven(unittest.TestCase):
    def score(self, words, produced):
        return align.score_reading([(w.text, w.phones) for w in words], produced, 5.0, cost=phones.woven_cost)

    def test_a_correct_mixed_reading_marks_no_word_missed(self):
        words = g2p.expected_woven(WOVEN, phonemize)
        result = self.score(words, produced_from([(w.text, w.phones) for w in words]))
        self.assertEqual([(w["text"], w["error"]) for w in result["words"]],
                         [(w.text, "none") for w in words])
        self.assertEqual(result["accuracy"], 100)

    def test_the_same_hearing_against_an_all_english_reference_misses_the_greek_words(self):
        words = g2p.expected_woven(WOVEN, phonemize)
        heard = produced_from([(w.text, w.phones) for w in words])
        english = g2p.expected_woven([("For God so loved τὸν κόσμον", "en")], phonemize)
        result = self.score(english, heard)
        errors = {w["text"]: w["error"] for w in result["words"]}
        self.assertEqual([errors[t] for t in ("For", "God", "so", "loved")], ["none"] * 4)
        self.assertNotEqual(errors["τὸν"], "none")
        self.assertNotEqual(errors["κόσμον"], "none")

    def test_a_greek_word_left_out_of_a_mixed_reading_is_an_omission(self):
        words = g2p.expected_woven(WOVEN, phonemize)
        heard = produced_from([(w.text, w.phones) for w in words], drop=("κόσμον",))
        result = self.score(words, heard)
        self.assertEqual({w["text"]: w["error"] for w in result["words"]}["τὸν"], "none")
        self.assertIn(result["words"][-1]["error"], {"omission", "not_reached"})

    def test_the_phones_of_the_result_serialise_as_plain_ipa(self):
        import json
        words = g2p.expected_woven(WOVEN, phonemize)
        result = self.score(words, produced_from([(w.text, w.phones) for w in words]))
        self.assertEqual(json.loads(json.dumps(result))["words"][4]["expected_phonemes"], ["t", "o", "n"])


if __name__ == "__main__":
    unittest.main()
