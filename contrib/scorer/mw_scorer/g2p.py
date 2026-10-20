"""The expected phonemes: each word of the target through espeak-ng, by phonemizer."""

import logging
import re
import threading
from typing import NamedTuple

from . import phones

# "en" is read as American English: what a young reader here is taught. "el" is
# modern Greek (espeak-ng's own "el"), which a reader of a verse in monotonic
# letters is saying.
_LANGS = {"en": "en-us", "el": "el"}

# One espeak-ng backend for each voice, made on first use: a woven reading asks
# for several, so the cache is keyed by the voice, never by one language per reading.
_backends = {}
_lock = threading.Lock()


def _backend(lang):
    from phonemizer.backend import EspeakBackend

    name = _LANGS.get(lang, lang)
    with _lock:
        if name not in _backends:
            if not EspeakBackend.is_supported_language(name):
                raise ValueError("lang %r is not a language espeak-ng knows" % lang)
            quiet = logging.getLogger("mw-scorer.g2p")
            quiet.setLevel(logging.ERROR)
            _backends[name] = EspeakBackend(name, with_stress=False, language_switch="remove-flags", logger=quiet)
        return _backends[name]


def phonemize(words, lang):
    """The IPA of each word, one string per word, phonemized word by word."""
    from phonemizer.separator import Separator

    backend = _backend(lang)
    with _lock:
        out = backend.phonemize(list(words), separator=Separator(phone="", word=" "), strip=True)
    return [s.replace(" ", "") for s in out]


def target_words(text):
    """The words of a target as it spells them, without the punctuation around them."""
    words = [re.sub(r"^\W+|\W+$", "", tok) for tok in text.split()]
    return [w for w in words if w]


def code(lang):
    """A language's bare code: "el-GR" is "el"."""
    return (lang or "en").split("-")[0].lower()


class Word(NamedTuple):
    """A word of a woven reading: its spelling, the expected phones (each marked with
    its language, phones.speak), the language it is read in and the number of the
    segment (its place in the request, empty ones counted) it came from."""
    text: str
    phones: list
    lang: str
    segment: int


def segment_words(segments):
    """(word, language, segment number) for each word of the segments, in reading order."""
    return [(w, code(lang), k) for k, (text, lang) in enumerate(segments) for w in target_words(text)]


def expected_woven(segments, phonemize=phonemize):
    """The expected words of a reading in several languages: each segment, [(text, lang)]
    in reading order, is turned to IPA by its own espeak-ng voice, and the reference is
    the segments one after another. Every language is IPA (phones.to_ipa_phones, folded
    as its own table has it) whatever it would be alone; phonemize(words, lang) is the
    voice, a parameter for a test."""
    out = []
    for k, (text, lang) in enumerate(segments):
        words = target_words(text)
        if not words:
            continue
        lang = code(lang)
        for word, ipa in zip(words, phonemize(words, lang)):
            own = phones.with_optional_nasals(word, phones.to_ipa_phones(ipa, lang), lang)
            out.append(Word(word, [phones.speak(p, lang) for p in own], lang, k))
    return out


def version():
    """espeak-ng's version, "1.51"; refuses with a plain line when it is not installed."""
    from phonemizer.backend import EspeakBackend

    if not EspeakBackend.is_available():
        raise RuntimeError("espeak-ng is not installed: sudo apt install espeak-ng (contrib/scorer/README.md)")
    return ".".join(str(n) for n in EspeakBackend.version())
