"""What a primed candidate is given. Standard library only."""

import unittest

from sttbakeoff import candidates, testset

VOCAB = testset.load_vocab()


class Priming(unittest.TestCase):
    def test_an_unprimed_candidate_gets_nothing(self):
        p = VOCAB['1jn2']
        self.assertEqual(candidates.priming('whisper-turbo', p, p.book, p.chapter, [1]), '')
        self.assertEqual(candidates.priming('parakeet-v3', p, p.book, p.chapter, [1]), '')

    def test_the_prompt_holds_the_verses_words_as_lampas_respells_them(self):
        p = VOCAB['1jn2']
        text = candidates.priming('whisper-turbo+prompt-respelt', p, p.book, p.chapter, [1])
        self.assertTrue(text.startswith('A question about 1 John 2:1, with Greek words from it: te-KNEE-a, moo, TAF-ta'),
                        text)
        self.assertNotIn('ee-laz-MOS', text)  # verse 2's

    def test_the_greek_prompt_holds_them_in_greek_letters(self):
        p = VOCAB['rom8']
        text = candidates.priming('whisper-turbo+prompt-greek', p, p.book, p.chapter, [15])
        self.assertIn('υἱοθεσίας', text)
        self.assertIn('Romans 8:15', text)

    def test_hotwords_are_the_words_alone(self):
        p = VOCAB['1jn2']
        text = candidates.priming('whisper-turbo+hotwords-respelt', p, p.book, p.chapter, [1])
        self.assertTrue(text.startswith('te-KNEE-a moo TAF-ta '), text)

    def test_every_prompt_fits_whispers_window(self):
        # Whisper keeps the last 223 prompt tokens: the longest here (1jn2-05, two verses, 30 words) is 163
        # tokens by turbo's tokenizer, the Greek-letter one 195; a whole chapter respelt is ~1600
        for q in testset.questions():
            p = VOCAB[q['passage']]
            words = candidates.verse_words(p, q['verses'])
            self.assertLessEqual(len(words), 40, q['id'])


if __name__ == '__main__':
    unittest.main()
