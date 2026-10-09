"""The alignment and labelling, on phones given by hand: no model, no espeak-ng.

These run with any python3 (test.sh --pure), so the gate can run them on a
host without the scorer's venv.
"""

import unittest

from mw_scorer import align, phones


def heard(*spans):
    """Produced phones from (start_seconds, "PH PH PH") spans, 0.06 s apart."""
    out = []
    for start, text in spans:
        for i, phone in enumerate(text.split()):
            t = round(start + 0.06 * i, 2)
            out.append(align.Phone(phone, t, round(t + 0.02, 2)))
    return out


CAT = [
    ("the", ["DH", "AH"]),
    ("cat", ["K", "AE", "T"]),
    ("sat", ["S", "AE", "T"]),
]


def errors(result):
    return [(w["text"], w["error"]) for w in result["words"]]


class ScoreReading(unittest.TestCase):
    def test_a_reading_heard_exactly_is_all_none_at_100(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE T"), (0.6, "S AE T")), 1.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "none")])
        self.assertEqual([w["accuracy"] for w in result["words"]], [100, 100, 100])
        self.assertEqual(result["accuracy"], 100)
        self.assertEqual(result["seconds"], 1.0)
        self.assertEqual(result["engine"], "local")
        self.assertEqual(result["words"][1]["produced_phonemes"], ["K", "AE", "T"])
        self.assertEqual(result["words"][1]["expected_phonemes"], ["K", "AE", "T"])
        self.assertFalse(any(w["self_corrected"] for w in result["words"]))

    def test_a_consonant_swapped_is_a_mispronunciation(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE P"), (0.6, "S AE T")), 1.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "mispronunciation"), ("sat", "none")])
        cat = result["words"][1]
        self.assertEqual(cat["produced_phonemes"], ["K", "AE", "P"])
        self.assertEqual(cat["accuracy"], 67)
        self.assertEqual(result["accuracy"], 89)

    def test_a_near_phone_alone_costs_accuracy_but_is_not_called_a_mispronunciation(self):
        # The model hears vowels loosely, and voicing and nasals near enough.
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AA T"), (0.6, "S AE D")), 1.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "none")])
        self.assertEqual(result["words"][1]["accuracy"], 83)
        self.assertEqual(result["words"][2]["accuracy"], 83)

    def test_two_near_phones_in_one_word_are_a_mispronunciation(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "G AA T"), (0.6, "S AE T")), 1.0)
        self.assertEqual(result["words"][1]["error"], "mispronunciation")

    def test_on_said_with_the_cot_caught_merger_and_a_near_nasal_is_not_a_mispronunciation(self):
        words = [("on", ["AO", "N"]), ("it", ["IH", "T"])]
        result = align.score_reading(words, heard((0.1, "AA NG"), (0.4, "IH T")), 1.0)
        self.assertEqual(errors(result), [("on", "none"), ("it", "none")])

    def test_a_word_left_out_is_an_omission_with_nothing_produced(self):
        result = align.score_reading(CAT, heard((0.3, "K AE T"), (0.6, "S AE T")), 1.0)
        self.assertEqual(errors(result), [("the", "omission"), ("cat", "none"), ("sat", "none")])
        self.assertEqual(result["words"][0]["produced_phonemes"], [])
        self.assertEqual(result["words"][0]["accuracy"], 0)
        self.assertEqual(result["accuracy"], 67)

    def test_nothing_heard_is_every_word_omitted(self):
        result = align.score_reading(CAT, [], 0.5)
        self.assertEqual([w["error"] for w in result["words"]], ["omission"] * 3)
        self.assertEqual(result["accuracy"], 0)

    def test_a_word_added_is_an_insertion_in_reading_order(self):
        result = align.score_reading(
            CAT, heard((0.1, "DH AH"), (0.3, "B IH G"), (0.6, "K AE T"), (0.9, "S AE T")), 1.2)
        self.assertEqual([w["error"] for w in result["words"]], ["none", "insertion", "none", "none"])
        added = result["words"][1]
        self.assertEqual(added["expected_phonemes"], [])
        self.assertEqual(added["produced_phonemes"], ["B", "IH", "G"])
        self.assertTrue(added["text"].strip())
        self.assertEqual(added["accuracy"], 0)
        self.assertEqual(result["accuracy"], 100, "an insertion is not one of the target's words")

    def test_phones_added_at_a_word_edge_are_an_insertion_not_part_of_the_word(self):
        # What the model heard of espeak-ng saying "the big cat sat": "the big"
        # as V EH M EY G. Equally cheap: M EY inside 'the', or M EY G between words.
        result = align.score_reading(
            CAT, heard((0.06, "V EH"), (0.2, "M EY G"), (0.52, "K AE T"), (0.86, "S AE T")), 2.0)
        self.assertEqual(errors(result)[1:], [("M EY G", "insertion"), ("cat", "none"), ("sat", "none")])
        self.assertEqual(result["words"][0]["produced_phonemes"], ["V", "EH"])

    def test_one_stray_phone_between_words_is_not_an_insertion(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.25, "HH"), (0.3, "K AE T"), (0.6, "S AE T")), 1.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "none")])

    def test_a_word_got_wrong_then_right_is_self_corrected(self):
        result = align.score_reading(
            CAT, heard((0.1, "DH AH"), (0.3, "K AE P"), (0.6, "K AE T"), (0.9, "S AE T")), 1.2)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "none")])
        self.assertTrue(result["words"][1]["self_corrected"])
        self.assertEqual(result["words"][1]["produced_phonemes"], ["K", "AE", "T"])
        self.assertFalse(result["words"][0]["self_corrected"] or result["words"][2]["self_corrected"])

    def test_a_word_read_right_twice_is_not_self_corrected(self):
        result = align.score_reading(
            CAT, heard((0.1, "DH AH"), (0.3, "K AE T"), (0.6, "K AE T"), (0.9, "S AE T")), 1.2)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "none")])
        self.assertFalse(result["words"][1]["self_corrected"])

    def test_a_long_gap_before_a_word_is_a_hesitation(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE T"), (1.6, "S AE T")), 2.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "hesitation")])
        self.assertEqual(result["words"][2]["accuracy"], 100)

    def test_a_short_gap_is_not_a_hesitation_and_the_threshold_is_a_setting(self):
        spans = heard((0.1, "DH AH"), (0.3, "K AE T"), (1.0, "S AE T"))
        self.assertEqual(align.score_reading(CAT, spans, 1.5)["words"][2]["error"], "none")
        self.assertEqual(align.score_reading(CAT, spans, 1.5, hesitation_seconds=0.4)["words"][2]["error"], "hesitation")

    def test_a_mispronunciation_outranks_a_hesitation(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE T"), (1.6, "M AE P")), 2.0)
        self.assertEqual(result["words"][2]["error"], "mispronunciation")

ROMANS_8_11 = [  # the first twelve words of the target of the run that scattered (mw-gq6.304)
    ("And", ["AE", "N", "D"]),
    ("if", ["IH", "F"]),
    ("the", ["DH", "AH"]),
    ("Spirit", ["S", "P", "IH", "R", "IH", "T"]),
    ("of", ["AH", "V"]),
    ("Him", ["HH", "IH", "M"]),
    ("who", ["HH", "UW"]),
    ("raised", ["R", "EY", "Z", "D"]),
    ("Jesus", ["JH", "IY", "Z", "AH", "S"]),
    ("from", ["F", "R", "AH", "M"]),
    ("the", ["DH", "AH"]),
    ("dead", ["D", "EH", "D"]),
]


class PartialReading(unittest.TestCase):
    """A reading that stops early is aligned to the start of the target; the
    words after it are not_reached, never omissions, never scattered phones."""

    def test_a_reading_that_stops_early_leaves_the_rest_not_reached(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE T")), 0.6)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "not_reached")])
        sat = result["words"][2]
        self.assertEqual(sat["expected_phonemes"], ["S", "AE", "T"])
        self.assertEqual(sat["produced_phonemes"], [])
        self.assertEqual(sat["accuracy"], 0)
        self.assertFalse(sat["self_corrected"])

    def test_the_accuracy_is_of_the_words_reached(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE P")), 0.6)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "mispronunciation"), ("sat", "not_reached")])
        self.assertEqual(result["accuracy"], 84)

    def test_his_and_if_the_spirit_of_romans_8_11(self):
        # What the model heard of his clear 'And if the Spirit' (run direct:2789e3c9...,
        # 3.72 s): EH N D IH F OW S V EY. It scored 29 of 38 words omitted, the first
        # four among them, and the nine phones scattered over later words.
        spoken = heard((1.48, "EH"), (1.58, "N"), (1.64, "D"), (1.68, "IH"), (1.8, "F"),
                       (1.92, "OW"), (2.04, "S"), (2.18, "V"), (2.24, "EY"))
        result = align.score_reading(ROMANS_8_11, spoken, 3.72)
        words = result["words"]
        self.assertEqual([w["text"] for w in words if w["error"] != "insertion"], [w for w, _ in ROMANS_8_11])
        self.assertEqual([w["error"] for w in words[:2]], ["none", "none"])
        self.assertEqual(words[0]["produced_phonemes"], ["EH", "N", "D"])
        self.assertEqual(words[1]["produced_phonemes"], ["IH", "F"])
        first_unreached = [w["error"] for w in words].index("not_reached")
        self.assertGreaterEqual(first_unreached, 4, "And, if, the, Spirit are what he read")
        for w in words[first_unreached:]:
            self.assertEqual((w["error"], w["produced_phonemes"], w["accuracy"]), ("not_reached", [], 0))
        self.assertNotIn("omission", [w["error"] for w in words[:first_unreached]])
        self.assertEqual(sum(len(w["produced_phonemes"]) for w in words), 9, "every phone he said is in the words he read")

    def test_a_last_word_read_wrong_is_still_a_word_read(self):
        result = align.score_reading(CAT, heard((0.1, "DH AH"), (0.3, "K AE T"), (0.6, "S AE P")), 1.0)
        self.assertEqual(errors(result), [("the", "none"), ("cat", "none"), ("sat", "mispronunciation")])

    def test_a_word_left_out_in_the_middle_is_still_an_omission(self):
        words = CAT + [("on", ["AO", "N"])]
        result = align.score_reading(words, heard((0.1, "DH AH"), (0.6, "S AE T"), (0.9, "AO N")), 1.2)
        self.assertEqual([w["error"] for w in result["words"]], ["none", "omission", "none", "none"])

    def test_nothing_heard_is_every_word_omitted_not_unreached(self):
        result = align.score_reading(CAT, [], 0.5)
        self.assertEqual([w["error"] for w in result["words"]], ["omission"] * 3)


class ToArpabet(unittest.TestCase):
    def test_espeak_ipa_becomes_arpabet_without_stress_or_length(self):
        cases = {
            "ðə": ["DH", "AH"],
            "kˈæt": ["K", "AE", "T"],
            "kæɹɪktɚ": ["K", "AE", "R", "IH", "K", "T", "ER"],
            "faɪɚ": ["F", "AY", "ER"],
            "tʃæptɚ": ["CH", "AE", "P", "T", "ER"],
            "ɡɜːl": ["G", "ER", "L"],
            "doʊnt": ["D", "OW", "N", "T"],
            "bɑːɾəl": ["B", "AA", "DX", "AH", "L"],
            "dʒʌmp": ["JH", "AH", "M", "P"],
            "θɪŋ": ["TH", "IH", "NG"],
        }
        for ipa, want in cases.items():
            with self.subTest(ipa=ipa):
                self.assertEqual(phones.to_arpabet(ipa), want)

    def test_a_phone_with_no_arpabet_is_kept_as_heard(self):
        self.assertEqual(phones.to_arpabet("kχ"), ["K", "χ"])

    def test_near_phones_cost_half_and_far_ones_cost_one(self):
        self.assertEqual(phones.substitution_cost("AE", "AE"), 0)
        self.assertEqual(phones.substitution_cost("AE", "AA"), 0.5)
        self.assertEqual(phones.substitution_cost("T", "D"), 0.5)
        self.assertEqual(phones.substitution_cost("N", "NG"), 0.5)
        self.assertEqual(phones.substitution_cost("DH", "V"), 0.5)
        self.assertEqual(phones.substitution_cost("AO", "AA"), 0.25, "the cot-caught merger is an accent, not an error")
        self.assertEqual(phones.substitution_cost("T", "P"), 1)
        self.assertEqual(phones.substitution_cost("AE", "T"), 1)


class IpaForOtherLanguages(unittest.TestCase):
    """A language other than English is compared as IPA, not ARPAbet."""

    def test_stress_length_and_tone_are_dropped_and_greek_variants_fold(self):
        self.assertEqual(phones.to_ipa_phones("ˈiːðamen", "el"), ["i", "ð", "a", "m", "e", "n"])
        self.assertEqual(phones.to_ipa_phones("çɛ5", "el"), ["x", "e"])
        self.assertEqual(phones.to_ipa_phones("ʎɔɲ", "el"), ["l", "o", "n"])

    def test_a_language_with_no_table_keeps_its_phones_as_heard(self):
        self.assertEqual(phones.to_ipa_phones("ˈɛs", "fr"), ["ɛ", "s"])

    def test_english_is_the_one_language_in_arpabet(self):
        self.assertTrue(phones.is_english("en"))
        self.assertTrue(phones.is_english("en-US"))
        self.assertTrue(phones.is_english(""))
        self.assertFalse(phones.is_english("el"))

    def test_greek_costs(self):
        cost = phones.ipa_cost("el")
        self.assertEqual(cost("a", "a"), 0)
        self.assertEqual(cost("ð", "s"), 1)   # not a pair the model confuses
        self.assertEqual(cost("ð", "n"), 0.25)
        self.assertEqual(cost("p", "b"), 0.5)
        self.assertEqual(cost("e", "i"), 0.5)
        self.assertEqual(cost("a", "u"), 1)   # Greek has five vowels: two apart is a misreading
        self.assertEqual(cost("p", "k"), 1)

    def test_a_greek_word_read_with_a_far_vowel_is_a_mispronunciation_and_a_near_one_is_not(self):
        words = [("τοις", ["t", "i", "s"]), ("τον", ["t", "o", "n"])]
        cost = phones.ipa_cost("el")
        near = align.score_reading(words, heard((0.1, "t i s"), (0.4, "t u n")), 1.0, cost=cost)
        self.assertEqual(errors(near), [("τοις", "none"), ("τον", "none")])
        self.assertEqual(near["words"][1]["produced_phonemes"], ["t", "u", "n"])
        far = align.score_reading(words, heard((0.1, "t u s"), (0.4, "t o n")), 1.0, cost=cost)
        self.assertEqual(errors(far), [("τοις", "mispronunciation"), ("τον", "none")])


if __name__ == "__main__":
    unittest.main()
