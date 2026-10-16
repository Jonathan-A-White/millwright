"""The test set holds what the story asks for, and every clip says its question. Standard library only."""

import unittest

from sttbakeoff import testset

VOCAB = testset.load_vocab()
QUESTIONS = testset.questions()


class Questions(unittest.TestCase):
    def test_at_least_twenty_from_1_john_2_and_romans_8(self):
        self.assertGreaterEqual(len(QUESTIONS), 20)
        self.assertEqual({q['passage'] for q in QUESTIONS}, {'1jn2', 'rom8'})

    def test_each_has_one_to_three_greek_words_of_its_passage(self):
        for q in QUESTIONS:
            words = testset.greek_words(q['text'])
            self.assertTrue(1 <= len(words) <= 3, q['id'])
            for w in words:
                self.assertTrue(set(q['verses']) & set(VOCAB[q['passage']].word(w).verses), (q['id'], w))

    def test_a_perfect_respelt_transcript_scores_every_greek_word_right(self):
        from sttbakeoff import score
        for q in QUESTIONS:
            p = VOCAB[q['passage']]
            r = score.score(q['text'], testset.spoken(q['text'], p), p)
            self.assertEqual(r.greek_right, r.greek_total, (q['id'], [s.__dict__ for s in r.slots]))
            self.assertEqual(r.english_errors, 0, q['id'])


class Speakable(unittest.TestCase):
    def test_capitals_go_and_digraphs_read_as_english(self):
        self.assertEqual(testset.speakable('khree-STO'), 'kree-sto')
        self.assertEqual(testset.speakable('DHEE-ke-on'), 'thee-ke-on')
        self.assertEqual(testset.speakable('GHRA-fo'), 'gra-fo')

    def test_the_clip_says_the_greek_word_respelt(self):
        p = VOCAB['1jn2']
        self.assertEqual(testset.spoken('What does {ταῦτα} mean?', p), 'What does taf-ta mean?')


if __name__ == '__main__':
    unittest.main()
