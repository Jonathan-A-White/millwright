"""The scorer: a target text and a WAV clip in, a ReadingResult (docs/scorers.md) out.

  target words -> espeak-ng IPA (g2p) -> ARPAbet  = expected
  clip -> wav2vec2 CTC tokens with times (listen) -> ARPAbet = produced
  expected x produced -> per-word labels (align)
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
        with self._lock:
            expected = [phones.to_arpabet(ipa) for ipa in g2p.phonemize(words, lang or "en")]
            heard = self.listener.tokens_heard(audio)
        produced = [align.Phone(p, start, end, token if k == 0 else "")
                    for token, start, end in heard
                    for k, p in enumerate(phones.to_arpabet(token))]
        return align.score_reading(list(zip(words, expected)), produced, seconds,
                                   hesitation_seconds=self.hesitation_seconds)

    def health(self):
        return {"model": listen.MODEL, "revision": listen.REVISION, "espeak": self.espeak,
                "hesitation_seconds": self.hesitation_seconds}
