"""Phones: espeak-ng's IPA turned into ARPAbet, and how far apart two phones are.

Both sides of a reading go through to_arpabet: the phonemes espeak-ng expects
for the target and the tokens the model hears. That folds the many IPA
spellings of one English phone (ə ɐ ʌ, ɹ r, ɑ ɑː) into one name, which is what
docs/scorers.md promises ("ARPAbet-style") and what makes the two comparable.
"""

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
_IPA_FOLD = {
    "el": {
        "ɑ": "a", "ɐ": "a", "æ": "a", "ɛ": "e", "ə": "e", "ɪ": "i", "ɨ": "i", "y": "i", "ʏ": "i",
        "ɔ": "o", "ɒ": "o", "ʊ": "u",
        "ç": "x", "ʝ": "ɣ", "c": "k", "ɟ": "ɡ", "g": "ɡ", "ŋ": "n", "ɲ": "n", "ɱ": "m",
        "ʎ": "l", "ɫ": "l", "ɾ": "r", "ɹ": "r", "ɻ": "r", "ʃ": "s", "ʒ": "z",
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


def is_english(lang):
    """Whether a language code is English, the one language compared as ARPAbet."""
    return (lang or "en").split("-")[0].lower() == "en"
