"""The expected phonemes: each word of the target through espeak-ng, by phonemizer."""

import logging
import threading

# "en" is read as American English: what a young reader here is taught. "el" is
# modern Greek (espeak-ng's own "el"), which a reader of a verse in monotonic
# letters is saying.
_LANGS = {"en": "en-us", "el": "el"}

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


def version():
    """espeak-ng's version, "1.51"; refuses with a plain line when it is not installed."""
    from phonemizer.backend import EspeakBackend

    if not EspeakBackend.is_available():
        raise RuntimeError("espeak-ng is not installed: sudo apt install espeak-ng (contrib/scorer/README.md)")
    return ".".join(str(n) for n in EspeakBackend.version())
