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


if __name__ == "__main__":
    unittest.main()
