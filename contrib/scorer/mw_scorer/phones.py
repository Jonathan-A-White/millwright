"""Phones: espeak-ng's IPA turned into ARPAbet, and how far apart two phones are.

Both sides of a reading go through to_arpabet: the phonemes espeak-ng expects
for the target and the tokens the model hears. That folds the many IPA
spellings of one English phone (ə ɐ ʌ, ɹ r, ɑ ɑː) into one name, which is what
docs/scorers.md promises ("ARPAbet-style") and what makes the two comparable.
"""

import re
import unicodedata

# Which phones, spellings and distances a reading is compared in depends on the
# language. English goes through ARPAbet (to_arpabet, substitution_cost): the
# names docs/scorers.md promises. Every other language is compared as IPA
# (to_ipa_phones, ipa_cost): ARPAbet is English's alphabet, and the model and
# espeak-ng both speak IPA already.

# Longest first: the two-letter IPA phones are matched before their letters.
_IPA = {
    "tʃ": ["CH"], "dʒ": ["JH"],
    "aɪ": ["AY"], "aʊ": ["AW"], "ɔɪ": ["OY"], "oʊ": ["OW"], "əʊ": ["OW"], "eɪ": ["EY"],
    "p": ["P"], "b": ["B"], "t": ["T"], "d": ["D"], "k": ["K"], "ɡ": ["G"], "g": ["G"],
    "f": ["F"], "v": ["V"], "θ": ["TH"], "ð": ["DH"], "s": ["S"], "z": ["Z"],
    "ʃ": ["SH"], "ʒ": ["ZH"], "h": ["HH"], "m": ["M"], "n": ["N"], "ŋ": ["NG"],
    "l": ["L"], "ɫ": ["L"], "ɹ": ["R"], "r": ["R"], "ɻ": ["R"], "w": ["W"], "ʍ": ["W"],
    "j": ["Y"], "ɾ": ["DX"], "ʔ": ["Q"],
    "i": ["IY"], "ɪ": ["IH"], "ᵻ": ["IH"], "ɨ": ["IH"], "e": ["EY"], "ɛ": ["EH"],
    "æ": ["AE"], "a": ["AE"], "ɑ": ["AA"], "ɒ": ["AA"], "ɔ": ["AO"], "o": ["OW"],
    "ʊ": ["UH"], "u": ["UW"], "ʌ": ["AH"], "ə": ["AH"], "ɐ": ["AH"],
    "ɚ": ["ER"], "ɝ": ["ER"], "ɜ": ["ER"],
}
_LONGEST = max(len(k) for k in _IPA)

# Marks that change a phone's stress, length, tone or colour but not which
# English phone it is.
_DROP = set("ˈˌːˑ.^[]\"':0123456789ʰʲˤˠʷ")

VOWELS = {"IY", "IH", "EY", "EH", "AE", "AA", "AO", "OW", "UH", "UW", "AH", "ER", "AY", "AW", "OY"}

# Consonants near enough that the model, or a young reader's speech rather than
# their decoding, swaps them: voicing, the nasals, the flap and glottal stop,
# th-fronting and stopping, r-gliding.
_NEAR = {frozenset(p) for p in [
    ("P", "B"), ("T", "D"), ("K", "G"), ("F", "V"), ("TH", "DH"), ("S", "Z"), ("SH", "ZH"), ("CH", "JH"),
    ("M", "N"), ("N", "NG"), ("M", "NG"),
    ("T", "DX"), ("D", "DX"), ("T", "Q"),
    ("TH", "F"), ("DH", "V"), ("DH", "D"), ("TH", "T"),
    ("R", "W"), ("ER", "R"),
]}

# Vowels that a whole accent says alike: the cot-caught merger of most American
# speech. espeak-ng's en-us says "on" with AO; a reader saying AA is not wrong.
_MERGED = {frozenset(("AO", "AA"))}


def to_arpabet(ipa):
    """The ARPAbet phones of an IPA string; a phone with no ARPAbet name is kept as heard."""
    clean = "".join(c for c in unicodedata.normalize("NFD", ipa)
                    if c not in _DROP and not unicodedata.combining(c) and not c.isspace())
    out, i = [], 0
    while i < len(clean):
        for n in range(min(_LONGEST, len(clean) - i), 0, -1):
            if clean[i:i + n] in _IPA:
                out.extend(_IPA[clean[i:i + n]])
                i += n
                break
        else:
            out.append(clean[i])
            i += 1
    return out


def substitution_cost(expected, produced):
    """0 for the same phone, 0.25 for a merged pair, 0.5 for a near one (vowel for
    vowel, or a pair in _NEAR), else 1."""
    if expected == produced:
        return 0
    if frozenset((expected, produced)) in _MERGED:
        return 0.25
    if (expected in VOWELS and produced in VOWELS) or frozenset((expected, produced)) in _NEAR:
        return 0.5
    return 1


# --- Languages other than English: compared as IPA -------------------------

# Spellings the model and espeak-ng each give one phone of the language, folded
# to the one espeak-ng's own transcription of the language uses. This is the
# tolerance table: add a row only for a variant of the same phone, never for a
# phone a reader could actually misread (that would hide the misreading).
#
# Modern Greek (monotonic), after Holton, Mackridge and Philippaki-Warburton:
#   five vowels, so every other vowel quality is a spelling of the nearest one,
#   whatever length or stress it carries;
#   /x/ is [x] or [ç] and /ɣ/ is [ɣ] or [ʝ] by the vowel after them, /k/ and
#   /ɡ/ are [c] and [ɟ] before front vowels, /n/ is [ŋ] or [ɲ] and /m/ is [ɱ] by
#   the consonant after them, /l/ is [ʎ] before [j], and /r/ is a tap or a trill;
#   the model writes the Greek voiced stop as g or ɡ, and may hear σ and ζ with a
#   hush ([ʃ], [ʒ]) which a Greek reader does not distinguish.
#
# Modern Hebrew (the Israeli pronunciation, which espeak-ng's "he" speaks):
#   five vowels, as Greek, and the shewa is an e, so a schwa is a spelling of it;
#   the guttural letters are two sounds, [χ] for khet and khaf (the model may write
#   [x] or [ħ]) and [ʔ] for alef and ayin (it may write [ʕ] or [ʡ]); the resh is
#   a uvular [ʁ] to espeak-ng and the model may write any rhotic; the sibilants
#   [ɕ] [ʂ] are a shin and [ʑ] [ʐ] a zayin-with-a-hush; vav is [v], never [w].
_IPA_FOLD = {
    "el": {
        "ɑ": "a", "ɐ": "a", "æ": "a", "ɛ": "e", "ə": "e", "ɪ": "i", "ɨ": "i", "y": "i", "ʏ": "i",
        "ɔ": "o", "ɒ": "o", "ʊ": "u",
        "ç": "x", "ʝ": "ɣ", "c": "k", "ɟ": "ɡ", "g": "ɡ", "ŋ": "n", "ɲ": "n", "ɱ": "m",
        "ʎ": "l", "ɫ": "l", "ɾ": "r", "ɹ": "r", "ɻ": "r", "ʃ": "s", "ʒ": "z",
    },
    "he": {
        "ɑ": "a", "ɐ": "a", "æ": "a", "ɛ": "e", "ə": "e", "ɘ": "e", "ɜ": "e", "ɪ": "i", "ɨ": "i", "ɔ": "o", "ɒ": "o", "ɵ": "o",
        "ʊ": "u", "ɯ": "u", "y": "u", "ʏ": "u",
        "x": "χ", "ħ": "χ", "ʕ": "ʔ", "ʡ": "ʔ", "ʢ": "ʔ", "ɦ": "h",
        "ɕ": "ʃ", "ʂ": "ʃ", "ʑ": "ʒ", "ʐ": "ʒ",
        "r": "ʁ", "ɾ": "ʁ", "ɹ": "ʁ", "ɻ": "ʁ", "ʀ": "ʁ", "ɫ": "l",
        "g": "ɡ", "c": "k", "ɟ": "ɡ", "w": "v", "ʋ": "v",
    },
}

# IPA phones that are close enough to be a near miss and not a plain error:
# the voiced and voiceless of one place, the same for every language.
_IPA_NEAR = {frozenset(p) for p in [
    ("p", "b"), ("t", "d"), ("k", "ɡ"), ("f", "v"), ("θ", "ð"), ("s", "z"), ("x", "ɣ"),
    ("l", "r"), ("θ", "f"), ("θ", "t"),
]}

# Phones the model, not the reader, confuses. Its training had no Greek, and the
# voiced fricatives of Greek come out of it as a stop, a nasal or a liquid of the
# same place, and it hears a nasal or a hissed θ for another, so a pair here counts a quarter of a phone, not half: a clean
# reading of /ð/ that it hears as [n] must not be a misreading. A pair is added
# here only when a clean synthetic reading shows the model doing it.
_IPA_MODEL = {frozenset(p) for p in [
    ("ð", "d"), ("ð", "l"), ("ð", "n"), ("ð", "m"), ("ð", "z"), ("ð", "v"),
    ("ɣ", "ɡ"), ("ɣ", "k"), ("ɣ", "x"), ("ɣ", "j"),
    ("m", "n"), ("θ", "s"),
]}

# Vowels by language that stand a step apart (a near miss, half a phone).
_IPA_NEAR_VOWELS = {
    "el": {frozenset(p) for p in [("e", "i"), ("o", "u"), ("a", "e"), ("a", "o")]},
    "he": {frozenset(p) for p in [("e", "i"), ("o", "u"), ("a", "e"), ("a", "o"), ("e", "o")]},
}

_IPA_VOWELS = set("aeiouyɑɐæɛəɪɨʏɔɒʊøœɜɵ")


def _fold_for(lang):
    return _IPA_FOLD.get(lang.split("-")[0].lower(), {})


def to_ipa_phones(ipa, lang):
    """The phones of an IPA string for a language other than English: one per base
    symbol, stress, length, tone and diacritics dropped, then the language's
    variants folded together (_IPA_FOLD). A phone the table does not know is kept as heard."""
    fold = _fold_for(lang)
    out = []
    for c in unicodedata.normalize("NFC", ipa):
        if c in _DROP or c.isspace() or unicodedata.combining(c):
            continue
        if c in fold:
            out.append(fold[c])
            continue
        # a composed letter (ç) was folded above; any other loses its marks
        out.extend(fold.get(b, b) for b in unicodedata.normalize("NFD", c) if not unicodedata.combining(b))
    return out


class Spoken(str):
    """An expected phone that knows the language its word is read in (a woven reading,
    woven_cost). It is a str, so it compares, folds and serialises as the plain phone."""

    lang = ""


class Optional(Spoken):
    """A phone the target expects but a reader may leave out: it is deleted for nothing
    (align._gap). It is a str, so it compares, folds and serialises as the plain phone."""


def speak(phone, lang):
    """The phone marked as one of lang (a bare code, "en"), still Optional when it was."""
    out = type(phone)(phone) if isinstance(phone, Spoken) else Spoken(phone)
    out.lang = lang
    return out


# A Greek voiced stop is spelled with two letters, and espeak-ng says only the stop
# ("Πάντοτε" is pˈadote). Lampas teaches the nasal inside a word ("PAN-do-te"), at the
# start of a word the stop alone; so inside a word the nasal is expected, and optional,
# because a Greek reader says it either way. letters -> (nasal, stop), after _IPA_FOLD.
_OPTIONAL_NASALS = {
    "el": {"ντ": ("n", "d"), "μπ": ("m", "b"), "γκ": ("n", "ɡ"), "γγ": ("n", "ɡ")},
}


def with_optional_nasals(word, phs, lang):
    """The phones of a word (to_ipa_phones) with the nasal before the voiced stop of each
    ντ, μπ, γκ, γγ inside the word marked Optional, put in where espeak-ng left it out. A
    cluster at the start of the word stays the stop alone. When the clusters in the letters
    and the stops in the phones do not pair up one to one, the phones are left as they are."""
    if lang.split("-")[0].lower() == "he":
        return _optional_hebrew(word, phs)
    table = _OPTIONAL_NASALS.get(lang.split("-")[0].lower())
    if not table:
        return list(phs)
    letters = "".join(c for c in unicodedata.normalize("NFD", word.lower()) if not unicodedata.combining(c))
    clusters = [(m.start(), table[m.group()]) for m in re.finditer("|".join(table), letters)]
    stops = [i for i, p in enumerate(phs) if p in {stop for _, stop in table.values()}]
    if len(clusters) != len(stops) or any(phs[at] != stop for at, (_, (_, stop)) in zip(stops, clusters)):
        return list(phs)
    out = list(phs)
    for at, (start, (nasal, _)) in reversed(list(zip(stops, clusters))):
        if start == 0:
            continue
        if at > 0 and out[at - 1] == nasal:
            out[at - 1] = Optional(nasal)
        else:
            out.insert(at, Optional(nasal))
    return out


def _optional_hebrew(word, phs):
    """The phones of a Hebrew word with the ones a reader may leave out marked Optional: the
    glottal stop of an alef, ayin or final he, which a speaker of Israeli Hebrew says rarely
    and the model seldom hears, and the e of a shewa on the first letter, which is said or
    not (bereshit, be-reshit)."""
    out = [Optional(p) if p == "ʔ" else p for p in phs]
    letters = unicodedata.normalize("NFD", word)
    first = re.match(r"[\u05D0-\u05EA][\u0591-\u05AF\u05B1-\u05BD\u05BF-\u05C7]*\u05B0", letters)
    if first and len(out) > 1 and out[1] not in _IPA_VOWELS:
        out.insert(1, Optional("e"))
    return out


def ipa_cost(lang):
    """The substitution_cost for a language compared as IPA: 0 for the same phone, 0.25 for
    a pair the model confuses (_IPA_MODEL), 0.5 for a near one (_IPA_NEAR; a vowel a step
    away, _IPA_NEAR_VOWELS), else 1."""
    vowels = _IPA_NEAR_VOWELS.get(lang.split("-")[0].lower())

    def cost(expected, produced):
        if expected == produced:
            return 0
        pair = frozenset((expected, produced))
        if pair in _IPA_MODEL:
            return 0.25
        if pair in _IPA_NEAR:
            return 0.5
        if vowels is not None:
            return 0.5 if pair in vowels else 1
        if expected in _IPA_VOWELS and produced in _IPA_VOWELS:
            return 0.5
        return 1

    return cost


def woven_cost(expected, produced):
    """The substitution_cost of a woven reading, whose words are read in different
    languages but all compared as IPA phones (to_ipa_phones(ipa, "")). What was heard
    is the model's raw IPA with no language, so it is compared as the language of the
    expected phone's word (speak): English through the ARPAbet classes (an English
    reader's ɪ is not i), any other language folded as its own table has it and costed
    by ipa_cost. English has a table of its own and Greek's never reaches an English word."""
    lang = getattr(expected, "lang", "")
    if is_english(lang):
        return substitution_cost((to_arpabet(expected) or [expected])[0], (to_arpabet(produced) or [produced])[0])
    fold = _fold_for(lang)
    return _woven_ipa_cost(lang)(fold.get(expected, expected), fold.get(produced, produced))


_woven_costs = {}


def _woven_ipa_cost(lang):
    code = lang.split("-")[0].lower()
    if code not in _woven_costs:
        _woven_costs[code] = ipa_cost(code)
    return _woven_costs[code]


def is_english(lang):
    """Whether a language code is English, the one language compared as ARPAbet."""
    return (lang or "en").split("-")[0].lower() == "en"
