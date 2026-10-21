"""The alignment of a long reading: the same words as the whole-table alignment gave,
in time that grows with the reading, not its square (mw-gq6.667).

tests/reference_align.py is the alignment as it was, the whole expected-by-produced
table filled twice. These run with any python3 (test.sh --pure): no model, no espeak-ng.
"""

import random
import unittest

from mw_scorer import align, phones

from . import reference_align

CONSONANTS = "B CH D DH F G HH JH K L M N NG P R S SH T TH V W Y Z ZH".split()
VOWELS = "AA AE AH AO AW AY EH ER EY IH IY OW OY UH UW".split()
SENTENCE = [
    ("the", ["DH", "AH"]), ("cat", ["K", "AE", "T"]), ("sat", ["S", "AE", "T"]),
    ("on", ["AA", "N"]), ("the", ["DH", "AH"]), ("mat", ["M", "AE", "T"]),
]
SECONDS_PER_PHONE = 0.108      # 300 s of speech is about 2800 phones


def vocabulary(rng, size=400):
    """Words of 1 to 6 phones, a vowel somewhere in each."""
    out = []
    for k in range(size):
        ph = [rng.choice(CONSONANTS + VOWELS) for _ in range(rng.randint(1, 6))]
        ph[rng.randrange(len(ph))] = rng.choice(VOWELS)
        out.append(("w%d" % k, ph))
    return out


def random_text(rng, words):
    book = vocabulary(rng)
    return [rng.choice(book) for _ in range(words)]


def periodic_text(words):
    return [SENTENCE[k % len(SENTENCE)] for k in range(words)]


def read(rng, text, error=0.05, stop_at=None, skip=None, again=None):
    """The phones a reader of the text produced: each phone left out, replaced or followed
    by a stray one with the chance `error` / 3; the reading stops after the word
    stop_at; the words skip = (from, to) are not read; the word `again` is read twice."""
    out, t = [], 0.0
    for w, (_, expected) in enumerate(text):
        if stop_at is not None and w >= stop_at:
            break
        if skip and skip[0] <= w < skip[1]:
            continue
        for _ in range(2 if w == again else 1):
            for phone in expected:
                roll = rng.random()
                if roll < error / 3:
                    continue
                if roll < 2 * error / 3:
                    phone = rng.choice(CONSONANTS + VOWELS)
                out.append(align.Phone(str(phone), round(t, 2), round(t + 0.02, 2)))
                t += SECONDS_PER_PHONE
                if roll > 1 - error / 3:
                    out.append(align.Phone(rng.choice(CONSONANTS), round(t, 2), round(t + 0.02, 2)))
                    t += SECONDS_PER_PHONE
        t += 0.1
    return out, t + 0.5


def as_reference(produced):
    return [reference_align.Phone(*p) for p in produced]


class SameWordsAsTheWholeTable(unittest.TestCase):
    def assertSame(self, text, produced, seconds):
        got = align.score_reading(text, produced, seconds)
        want = reference_align.score_reading(text, as_reference(produced), seconds)
        self.assertEqual(got, want)

    def test_a_reading_of_random_words_with_errors(self):
        for seed, error in [(1, 0.0), (2, 0.05), (3, 0.15), (4, 0.3)]:
            with self.subTest(seed=seed, error=error):
                rng = random.Random(seed)
                text = random_text(rng, 150)
                self.assertSame(text, *read(rng, text, error))

    def test_a_reading_that_stops_early(self):
        for seed, stop in [(5, 40), (6, 120), (7, 220)]:
            with self.subTest(seed=seed, stop=stop):
                rng = random.Random(seed)
                text = random_text(rng, 150)
                self.assertSame(text, *read(rng, text, 0.08, stop_at=stop * 150 // 250))

    def test_a_reading_that_skips_a_passage_and_repeats_a_word(self):
        # The phones heard either side of the passage may be given to the skipped words in
        # more than one way at the same cost, and the two alignments choose differently:
        # the words either side of it, and the word read twice, are the same.
        for seed in (8, 9, 10):
            with self.subTest(seed=seed):
                rng = random.Random(seed)
                text = random_text(rng, 150)
                produced, seconds = read(rng, text, 0.08, skip=(40, 80), again=120)
                got = align.score_reading(text, produced, seconds)["words"]
                want = reference_align.score_reading(text, as_reference(produced), seconds)["words"]
                self.assertEqual(len(got), len(want))
                self.assertEqual(got[:35], want[:35])
                self.assertEqual(got[-70:], want[-70:])

    def test_a_reading_of_the_same_sentence_over_and_over(self):
        for seed, error in [(11, 0.05), (12, 0.2)]:
            with self.subTest(seed=seed, error=error):
                rng = random.Random(seed)
                text = periodic_text(180)
                self.assertSame(text, *read(rng, text, error, stop_at=None if seed == 11 else 150))

    def test_nothing_heard_and_nothing_expected(self):
        text = periodic_text(30)
        self.assertSame(text, [], 1.0)
        self.assertSame([("a", []), ("b", [])], read(random.Random(1), text)[0], 1.0)

    def test_300_seconds_of_one_sentence_read_over_and_over(self):
        rng = random.Random(13)
        text = periodic_text(1100)
        produced, seconds = read(rng, text, 0.08)
        self.assertGreater(len(produced), 2500)
        self.assertSame(text, produced, seconds)


class TheTableIsNotWholeOnALongReading(unittest.TestCase):
    """The cost of one phone for another is asked once for a cell of the table: a
    banded table asks for a number of them that grows with the reading, the whole
    table with its square."""

    def asked(self, words):
        rng = random.Random(21)
        text = periodic_text(words)
        produced, seconds = read(rng, text, 0.08)
        calls = [0]

        def cost(expected, heard):
            calls[0] += 1
            return phones.substitution_cost(expected, heard)

        align.score_reading(text, produced, seconds, cost=cost)
        return calls[0], sum(len(p) for _, p in text)

    def test_the_table_asked_of_a_long_reading_is_a_band_of_it(self):
        asked, expected = self.asked(1100)
        self.assertLess(asked, 200 * expected)    # the whole table is about 2500 x expected, twice

    def test_twice_the_reading_is_about_twice_the_cells(self):
        short, _ = self.asked(550)
        long, _ = self.asked(1100)
        self.assertLess(long, 2.2 * short)


if __name__ == "__main__":
    unittest.main()
