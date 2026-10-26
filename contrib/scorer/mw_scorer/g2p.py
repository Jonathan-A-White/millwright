"""The expected phonemes: each word of the target through espeak-ng, by phonemizer."""

import logging
import re
import threading
import unicodedata
from typing import NamedTuple

from . import phones

# "en" is read as American English: what a young reader here is taught. "el" is
# modern Greek (espeak-ng's own "el"), which a reader of a verse in monotonic
# letters is saying. "he" is espeak-ng's own "he", which reads Hebrew letters and
# some of its points (see _hebrew_pieces for the ones it cannot).
_LANGS = {"en": "en-us", "el": "el", "he": "he"}

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
    words = list(words)
    pieces = None
    if code(lang) == "he":
        pieces = [_hebrew_pieces(w) for w in words]
        words = [text for ps in pieces for text, _ in ps]
    with _lock:
        out = backend.phonemize(words, separator=Separator(phone="", word=" "), strip=True)
    out = [s.replace(" ", "") for s in out]
    if pieces is None:
        return out
    ipa, at = [], 0
    for ps in pieces:
        said = []
        for (_, stop), part in zip(ps, out[at:at + len(ps)]):
            said.append(_with_stop(part, stop))
        at += len(ps)
        ipa.append("".join(said))
    return ipa


# --- Hebrew -----------------------------------------------------------------
#
# espeak-ng's Hebrew voice reads unpointed letters by its own rules and the vowel
# points segol, patah, qamats, hiriq, tsere, qubuts and shewa. A word that carries
# a dagesh, a shin or sin dot, a holam on a consonant or a cantillation mark makes
# it spell the word out, letter by letter ("vˈet ... ʁˈeʃ ..."). So the word is
# put in the form it reads (_hebrew_pieces): cantillation, meteg and the like
# dropped, a maqaf a break between two words, the sin dot read as a samekh and the
# shin dot dropped, a hataf as the short vowel it is, a holam on a consonant as that
# consonant and a vav-holam.
# The dagesh of a bet, kaf or pe makes it a stop where espeak-ng says the fricative
# (v, kh, f), and leaves out a he; so those are phonemized apart, a stop's first
# phone made the stop and a he given its h (_with_stop).

_HEB_DROP = re.compile("[\u0591-\u05AF\u05BD\u05BF\u05C0\u05C3-\u05C6]")
_MAQAF = "\u05BE"
# a hataf is the short vowel it is written with; a qamats is an a (espeak-ng says
# the qamats of an alef as o), and a qamats qatan an o
_POINT = {"\u05B1": "\u05B6", "\u05B2": "\u05B7", "\u05B3": "\u05B7", "\u05B8": "\u05B7", "\u05C7": "\u05B9"}
_SHEWA, _HOLAM, _HOLAM_HASER = "\u05B0", "\u05B9", "\u05BA"
_HIRIQ, _TSERE, _SEGOL = "\u05B4", "\u05B5", "\u05B6"
_DAGESH, _SHIN_DOT, _SIN_DOT = "\u05BC", "\u05C1", "\u05C2"
_VAV, _YOD, _SAMEKH, _HE = "\u05D5", "\u05D9", "\u05E1", "\u05D4"
# the letters a dagesh makes a stop, and the stop of each
_STOPS = {"\u05D1": "b", "\u05DB": "k", "\u05E4": "p"}


def _hebrew_clusters(word):
    """The word as [letter, marks] pairs, marks a set, cantillation and the like dropped."""
    out = []
    for c in unicodedata.normalize("NFD", _HEB_DROP.sub("", unicodedata.normalize("NFD", word))):
        if unicodedata.combining(c) and out:
            out[-1][1].add(_POINT.get(c, c))
        else:
            out.append([c, set()])
    return out


def _hebrew_pieces(word):
    """The word as [(text, stop)]: the texts espeak-ng reads, one after another, and for each
    the stop ("b", "k", "p") its first phone is when the piece is a bet, kaf or pe with a dagesh,
    or "h" when it is a he that is said (any but a he ending the word with no point of its own)."""
    cl = _hebrew_clusters(word)
    i = 0
    while i < len(cl):
        letter, marks = cl[i]
        if _SIN_DOT in marks and letter == "\u05E9":
            cl[i][0] = _SAMEKH
        marks.discard(_SHIN_DOT)
        marks.discard(_SIN_DOT)
        if _HOLAM_HASER in marks:
            marks.discard(_HOLAM_HASER)
            marks.add(_HOLAM)
        # a holam on a consonant is a vav-holam after it; a vav that is already there takes it
        if _HOLAM in marks and letter != _VAV:
            marks.discard(_HOLAM)
            if i + 1 < len(cl) and cl[i + 1][0] == _VAV and not cl[i + 1][1]:
                cl[i + 1][1].add(_HOLAM)
            else:
                cl.insert(i + 1, [_VAV, {_HOLAM}])
        # a yod after a hiriq, tsere or segol only writes the vowel
        if letter == _YOD and not marks and i and cl[i - 1][1] & {_HIRIQ, _TSERE, _SEGOL}:
            del cl[i]
            continue
        i += 1
    pieces, rest = [], ""
    i = 0
    while i < len(cl):
        letter, marks = cl[i]
        if letter == _MAQAF:
            if rest:
                pieces.append((_alone(rest), ""))
                rest = ""
        elif letter in _STOPS and _DAGESH in marks:
            if rest:
                pieces.append((_alone(rest), ""))
                rest = ""
            text = letter + "".join(sorted(marks - {_DAGESH}))
            if i + 1 < len(cl) and cl[i + 1][0] == _VAV and cl[i + 1][1] & {_HOLAM, _DAGESH}:
                text += cl[i + 1][0] + "".join(sorted(cl[i + 1][1]))
                i += 1
            pieces.append((text if marks - {_DAGESH} or len(text) > 1 else text + _SHEWA, _STOPS[letter]))
        elif letter == _HE and (marks or (i + 1 < len(cl) and cl[i + 1][0] != _MAQAF)):
            if rest:
                pieces.append((_alone(rest), ""))
                rest = ""
            marks.discard(_DAGESH)
            pieces.append((letter + "".join(sorted(marks)) if marks else letter + _SHEWA, "h"))
        else:
            if _DAGESH in marks and letter != _VAV:
                marks.discard(_DAGESH)
            rest += letter + "".join(sorted(marks))
        i += 1
    if rest:
        pieces.append((_alone(rest), ""))
    return pieces or [(word, "")]


def _alone(text):
    """A single bare consonant is spelled by its name; a shewa on it makes it a sound."""
    return text + _SHEWA if len(text) == 1 else text


def _with_stop(ipa, stop):
    """The IPA of a piece (_hebrew_pieces): the stop for a bet, kaf or pe with a dagesh,
    an h for a he, else as it is."""
    if not stop:
        return ipa
    if stop == "h":
        return "h" + ipa.lstrip("h\u0294")
    fricative = {"b": "v", "k": "\u03C7", "p": "f"}[stop]
    return stop + ipa[len(fricative):] if ipa.startswith(fricative) else ipa


def _is_mark(c):
    return c.isalnum() or unicodedata.category(c).startswith("M")


def _strip_edges(tok):
    start, end = 0, len(tok)
    while start < end and not _is_mark(tok[start]):
        start += 1
    while end > start and not _is_mark(tok[end - 1]):
        end -= 1
    return tok[start:end]


def target_words(text):
    """The words of a target as it spells them, without the punctuation around them. A
    combining mark is part of the word (the points of a Hebrew word's last letter)."""
    words = [_strip_edges(tok) for tok in text.split()]
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
