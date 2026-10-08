"""The scorer: a target text and a WAV clip in, a ReadingResult (docs/scorers.md) out.

  target words -> espeak-ng IPA (g2p) -> ARPAbet  = expected
  clip -> wav2vec2 CTC tokens with times (listen) -> ARPAbet = produced
  expected x produced -> per-word labels (align)

For English that is all. For any other language (Greek, "el") ARPAbet is not
its alphabet, so the two sides stay IPA (phones.to_ipa_phones: stress and
length dropped, the language's variants of one phone folded) and are compared
by phones.ipa_cost. The expected_phonemes and produced_phonemes of such a
reading are then IPA symbols.
"""

import re
import threading

from . import align, g2p, listen, phones


def target_words(text):
    """The words of a target as it spells them, without the punctuation around them."""
    words = [re.sub(r"^\W+|\W+$", "", tok) for tok in text.split()]
    return [w for w in words if w]


class Scorer:
    def __init__(self, threads=4, hesitation_seconds=align.HESITATION_SECONDS):
        self.espeak = g2p.version()
        self.listener = listen.Listener(threads=threads)
        self.hesitation_seconds = hesitation_seconds
        self._lock = threading.Lock()
        g2p.phonemize(["ready"], "en")
        for _ in range(2):  # the first passes through the model are slow: take them here
            self.listener.tokens_heard(listen.silence(1.0))

    def score(self, target_text, lang, wav):
        words = target_words(target_text)
        if not words:
            raise ValueError("target_text has no words")
        audio, seconds = listen.read_wav(wav)
        lang = lang or "en"
        if phones.is_english(lang):
            to_phones, cost = phones.to_arpabet, phones.substitution_cost
        else:
            def to_phones(ipa):
                return phones.to_ipa_phones(ipa, lang)
            cost = phones.ipa_cost(lang)
        with self._lock:
            expected = [to_phones(ipa) for ipa in g2p.phonemize(words, lang)]
            heard = self.listener.tokens_heard(audio)
        produced = [align.Phone(p, start, end, token if k == 0 else "")
                    for token, start, end in heard
                    for k, p in enumerate(to_phones(token))]
        return align.score_reading(list(zip(words, expected)), produced, seconds,
                                   hesitation_seconds=self.hesitation_seconds, cost=cost)

    def health(self):
        return {"model": listen.MODEL, "revision": listen.REVISION, "espeak": self.espeak,
                "hesitation_seconds": self.hesitation_seconds}
