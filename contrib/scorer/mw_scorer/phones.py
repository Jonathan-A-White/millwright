"""Phones: espeak-ng's IPA turned into ARPAbet, and how far apart two phones are.

Both sides of a reading go through to_arpabet: the phonemes espeak-ng expects
for the target and the tokens the model hears. That folds the many IPA
spellings of one English phone (ə ɐ ʌ, ɹ r, ɑ ɑː) into one name, which is what
docs/scorers.md promises ("ARPAbet-style") and what makes the two comparable.
"""

import unicodedata

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
