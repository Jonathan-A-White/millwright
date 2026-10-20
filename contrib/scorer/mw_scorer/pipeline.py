"""The scorer: a target text and a WAV clip in, a ReadingResult (docs/scorers.md) out.

  target words -> espeak-ng IPA (g2p) -> ARPAbet  = expected
  clip -> wav2vec2 CTC tokens with times (listen) -> ARPAbet = produced
  expected x produced -> per-word labels (align)

For English that is all. For any other language (Greek, "el") ARPAbet is not
its alphabet, so the two sides stay IPA (phones.to_ipa_phones: stress and
length dropped, the language's variants of one phone folded) and are compared
by phones.ipa_cost. The expected_phonemes and produced_phonemes of such a
reading are then IPA symbols.

A woven reading (segments, each in its own language) is IPA for all of its words,
English included, each compared as its own language (phones.woven_cost): see
Scorer._score_segments.
"""

import threading

from . import align, g2p, listen, phones

target_words = g2p.target_words


class Scorer:
    def __init__(self, threads=4, hesitation_seconds=align.HESITATION_SECONDS):
        self.espeak = g2p.version()
        self.listener = listen.Listener(threads=threads)
        self.hesitation_seconds = hesitation_seconds
        self._lock = threading.Lock()
        g2p.phonemize(["ready"], "en")
        for _ in range(2):  # the first passes through the model are slow: take them here
            self.listener.tokens_heard(listen.silence(1.0))

    def score(self, target_text, lang, wav, segments=None):
        """segments, [(text, lang)] in reading order, is a woven reading (server.parse_request):
        it is scored in place of target_text and lang."""
        if segments:
            return self._score_segments(segments, wav)
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
            if not phones.is_english(lang):
                expected = [phones.with_optional_nasals(w, e, lang) for w, e in zip(words, expected)]
            heard = self.listener.tokens_heard(audio)
        produced = [align.Phone(p, start, end, token if k == 0 else "")
                    for token, start, end in heard
                    for k, p in enumerate(to_phones(token))]
        return align.score_reading(list(zip(words, expected)), produced, seconds,
                                   hesitation_seconds=self.hesitation_seconds, cost=cost)

    def _score_segments(self, segments, wav):
        """A reading in the order and languages of its segments. Of one language alone it is that
        language's reading, scored as it always was; of several, every word is IPA, compared as
        its own language (phones.woven_cost), and the reference is the segments' words in order.
        Each word of the result says its segment and language."""
        words = g2p.segment_words(segments)
        if not words:
            raise ValueError("segments have no words")
        langs = {lang for _, lang, _ in words}
        if len(langs) == 1:
            result = self.score(" ".join(text for text, _ in segments), langs.pop(), wav)
        else:
            audio, seconds = listen.read_wav(wav)
            with self._lock:
                expected = g2p.expected_woven(segments)
                heard = self.listener.tokens_heard(audio)
            produced = [align.Phone(p, start, end, token if k == 0 else "")
                        for token, start, end in heard
                        for k, p in enumerate(phones.to_ipa_phones(token, ""))]
            result = align.score_reading([(w.text, w.phones) for w in expected], produced, seconds,
                                         hesitation_seconds=self.hesitation_seconds, cost=phones.woven_cost)
        # one entry for each word of the target, in order, with a word the reader added between them
        spoken = iter(words)
        for entry in result["words"]:
            if entry["error"] != "insertion":
                _, entry["lang"], entry["segment"] = next(spoken)
        return result

    def health(self):
        return {"model": listen.MODEL, "revision": listen.REVISION, "espeak": self.espeak,
                "hesitation_seconds": self.hesitation_seconds}
