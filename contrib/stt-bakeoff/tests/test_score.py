"""The bake-off's scoring: known outputs score as expected. Standard library only."""

import os
import unittest

from sttbakeoff import score

HERE = os.path.dirname(os.path.abspath(__file__))
VOCAB = score.load_vocab(os.path.join(HERE, '..', 'testset', 'vocab.json'))


def scored(reference, hypothesis, passage='1jn2'):
    return score.score(reference, hypothesis, VOCAB[passage])


class GreekWords(unittest.TestCase):
    def test_the_respelling_as_written_names_the_word(self):
        r = scored('What does {ταῦτα} mean in verse one?', 'What does tafta mean in verse one?')
        self.assertEqual((r.greek_right, r.greek_total), (1, 1))
        self.assertEqual(r.slots[0].named, 'ταῦτα')

    def test_the_word_in_greek_letters_names_it(self):
        r = scored('What does {ταῦτα} mean in verse one?', 'What does ταύτα mean in verse 1?')
        self.assertEqual((r.greek_right, r.greek_total), (1, 1))
        self.assertEqual(r.english_errors, 0)

    def test_the_classical_spelling_names_it(self):
        r = scored('Is {πνεῦμα} here the Holy Spirit?', 'Is pneuma here the Holy Spirit?', 'rom8')
        self.assertEqual(r.greek_right, 1)

    def test_a_near_spelling_still_names_it(self):
        r = scored('Can you explain {σκάνδαλον} in verse ten?', 'Can you explain Scandalon in verse 10?')
        self.assertEqual(r.greek_right, 1)

    def test_a_respelling_broken_into_pieces_names_it(self):
        r = scored('What does {τεκνία} mean?', 'What does tek nia mean?')
        self.assertEqual(r.greek_right, 1)
        self.assertEqual(r.english_errors, 0)

    def test_another_passage_word_is_wrong(self):
        r = scored('How is {ψεύστης} different from {ψεῦδος}?', 'How is psevdos different from psevdos?')
        self.assertEqual((r.greek_right, r.greek_total), (1, 2))
        self.assertEqual(r.slots[0].named, 'ψεῦδος')
        self.assertFalse(r.slots[0].right)
        self.assertTrue(r.slots[1].right)

    def test_an_english_word_in_its_place_is_wrong(self):
        r = scored('What does {τεκνία} mean in verse one?', 'What does technique mean in verse one?')
        self.assertEqual(r.greek_right, 0)

    def test_a_dropped_greek_word_is_wrong_and_no_english_error(self):
        r = scored('What does {τεκνία} mean in verse one?', 'What does mean in verse one?')
        self.assertEqual((r.greek_right, r.greek_total), (0, 1))
        self.assertEqual(r.english_errors, 0)
        self.assertEqual(r.slots[0].heard, '')

    def test_side_by_side_words_each_find_their_own(self):
        r = scored('Does {ἀλαζονεία} {τοῦ} {βίου} mean pride?', 'Does alazonia too mean pride?')
        self.assertEqual([s.right for s in r.slots], [True, True, False])


class EnglishWords(unittest.TestCase):
    def test_substitutions_count_against_the_english_words_only(self):
        r = scored('What does {ταῦτα} mean in verse one?', 'What did tafta mean in first one?')
        self.assertEqual(r.english_words, 6)
        self.assertEqual(r.english_errors, 2)
        self.assertEqual(r.greek_right, 1)

    def test_an_extra_word_is_an_error(self):
        r = scored('What does {ταῦτα} mean?', 'Um what does tafta mean?')
        self.assertEqual(r.english_errors, 1)

    def test_numbers_as_words_or_digits_are_the_same(self):
        r = scored('Who are they in verse twenty eight?', 'Who are they in verse 28?', 'rom8')
        self.assertEqual(r.english_errors, 0)

    def test_case_and_punctuation_do_not_count(self):
        r = scored("What's the {χρίσμα} that John says we have?", 'whats the krisma, that john says we have')
        self.assertEqual(r.english_errors, 0)
        self.assertEqual(r.greek_right, 1)


class Folding(unittest.TestCase):
    def test_greek_folds_without_marks(self):
        self.assertEqual(score.fold_greek('Ἰησοῦς'), 'ιησουσ')

    def test_respelling_and_greek_letters_sound_alike(self):
        self.assertEqual(score.sound('TAF-ta'), score.sound('ταύτα'))


if __name__ == '__main__':
    unittest.main()
