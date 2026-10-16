"""Scoring one transcript against what was said. Standard library only.

A reference is the question as he asked it, each Greek word in braces:
'What does {ταῦτα} mean in verse one?'. The transcript is aligned to it word by
word; a Greek word may be heard as up to MAX_SPAN words ('taf ta'), in Greek
letters ('ταύτα'), in Lampas's respelling ('tafta') or a classical spelling
('pneuma').

A Greek word counts as right when some run of the words aligned to it names it:
the passage word whose sound is nearest is the expected one, at least NAME_AT
alike. That is the mapping the record-and-transcribe build would make: snap
what was heard to the passage's words.

English word error rate counts substitutions, deletions and insertions against
the English words only; the Greek words are scored on their own.
"""

import json
import re
import unicodedata
from dataclasses import dataclass, field

NAME_AT = 0.7
MAX_SPAN = 8

# ---- sounds -------------------------------------------------------------

GREEK_VOICELESS = set('θκξπσςτφχψ')
GREEK_FRONT = ('ε', 'ι', 'η', 'υ')
GREEK_VOICED_AFTER_S = set('βγδμνρ')
CLASSICAL = {
    'α': 'a', 'β': 'b', 'γ': 'g', 'δ': 'd', 'ε': 'e', 'ζ': 'z', 'η': 'e', 'θ': 'th', 'ι': 'i',
    'κ': 'k', 'λ': 'l', 'μ': 'm', 'ν': 'n', 'ξ': 'x', 'ο': 'o', 'π': 'p', 'ρ': 'r', 'σ': 's',
    'ς': 's', 'τ': 't', 'υ': 'u', 'φ': 'ph', 'χ': 'ch', 'ψ': 'ps', 'ω': 'o',
}
MODERN = {
    'α': 'a', 'β': 'v', 'γ': 'gh', 'δ': 'dh', 'ε': 'e', 'ζ': 'z', 'η': 'i', 'θ': 'th', 'ι': 'i',
    'κ': 'k', 'λ': 'l', 'μ': 'm', 'ν': 'n', 'ξ': 'ks', 'ο': 'o', 'π': 'p', 'ρ': 'r', 'σ': 's',
    'ς': 's', 'τ': 't', 'υ': 'i', 'φ': 'f', 'χ': 'kh', 'ψ': 'ps', 'ω': 'o',
}
NUMBERS = {w: i for i, w in enumerate(
    'zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen '
    'fifteen sixteen seventeen eighteen nineteen'.split())}
TENS = {w: 10 * (i + 2) for i, w in enumerate('twenty thirty forty fifty sixty seventy eighty ninety'.split())}


def is_greek(s):
    return any('\u0370' <= c <= '\u03ff' or '\u1f00' <= c <= '\u1fff' for c in s)


def _letters(word):
    """The Greek word as (base letter, has diaeresis) pairs, lower case, marks dropped."""
    out = []
    for c in unicodedata.normalize('NFD', word.lower()):
        if unicodedata.combining(c):
            if c == '\u0308' and out:  # a diaeresis
                out[-1] = (out[-1][0], True)
            continue
        if is_greek(c):
            out.append((c, False))
    return out


def fold_greek(s):
    """Greek without marks, lower case, final sigma as σ: 'Ἰησοῦς' is 'ιησουσ'."""
    return ''.join(c for c, _ in _letters(s)).replace('ς', 'σ')


def modern(word):
    """The Greek word in Latin letters as Modern Greek says it, near Lampas's respelling."""
    ls = _letters(word)
    out, i = [], 0
    while i < len(ls):
        c = ls[i][0]
        nxt = ls[i + 1][0] if i + 1 < len(ls) and not ls[i + 1][1] else ''
        after = ls[i + 2][0] if i + 2 < len(ls) else ''
        pair = c + nxt
        if pair == 'αι':
            out.append('e'); i += 2; continue
        if pair in ('ει', 'οι', 'υι'):
            out.append('i'); i += 2; continue
        if pair == 'ου':
            out.append('u'); i += 2; continue
        if pair in ('αυ', 'ευ', 'ηυ'):
            vowel = {'α': 'a', 'ε': 'e', 'η': 'i'}[c]
            out.append(vowel + ('f' if not after or after in GREEK_VOICELESS else 'v')); i += 2; continue
        if pair == 'μπ':
            out.append('mb'); i += 2; continue
        if pair == 'ντ':
            out.append('nd'); i += 2; continue
        if pair in ('γκ', 'γγ'):
            out.append('ng'); i += 2; continue
        if c == 'γ' and nxt in GREEK_FRONT:
            out.append('y')
        elif c == 'χ' and nxt in GREEK_FRONT:
            out.append('hy')
        elif c in 'σς' and nxt in GREEK_VOICED_AFTER_S:
            out.append('z')
        else:
            out.append(MODERN.get(c, ''))
        i += 1
    return ''.join(out)


def classical(word):
    """The Greek word in Latin letters as English books spell it: 'πνεῦμα' is 'pneuma'."""
    s = ''.join(c for c, _ in _letters(word))
    s = s.replace('γγ', 'ng').replace('γκ', 'nk').replace('ου', 'ou')
    return ''.join(CLASSICAL.get(c, c) for c in s)


def fold_latin(s):
    """Latin letters as rough sound: spellings an English speaker or a model might use fold together."""
    s = ''.join(c for c in unicodedata.normalize('NFD', s.lower()) if 'a' <= c <= 'z')
    s = s.replace('x', 'ks').replace('q', 'k').replace('ch', 'k').replace('ph', 'f').replace('c', 'k')
    s = s.replace('h', '').replace('y', 'i').replace('w', 'u')
    s = s.replace('ee', 'i').replace('oo', 'u').replace('ou', 'u')
    return re.sub(r'(.)\1+', r'\1', s)


def sounds(s):
    """Every key s may be heard as: Greek letters as Modern Greek and as a classical spelling."""
    if is_greek(s):
        return {fold_latin(modern(s)), fold_latin(classical(s))} - {''}
    return {fold_latin(s)} - {''}


def sound(s):
    """The main key: Greek letters as Modern Greek says them, Latin letters folded."""
    return fold_latin(modern(s)) if is_greek(s) else fold_latin(s)


def alike(a, b):
    """1 for the same sound, toward 0 by edit distance."""
    if not a or not b:
        return 0.0
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return 1.0 - prev[-1] / max(len(a), len(b))


# ---- the passage --------------------------------------------------------

@dataclass
class Word:
    greek: str
    respelling: str
    keys: set
    verses: list = field(default_factory=list)


@dataclass
class Passage:
    name: str
    words: list
    by_fold: dict = field(default_factory=dict)
    book: str = ''
    chapter: int = 0

    def word(self, greek):
        try:
            return self.by_fold[fold_greek(greek)]
        except KeyError:
            raise ValueError(f'{greek} is not a word of {self.name}') from None


def passage_of(name, words, book='', chapter=0):
    out = Passage(name, [], book=book, chapter=chapter)
    for w in words:
        keys = sounds(w['greek']) | {fold_latin(w['respelling'])}
        word = Word(w['greek'], w['respelling'], keys - {''}, w.get('verses', []))
        out.words.append(word)
        out.by_fold.setdefault(fold_greek(w['greek']), word)
    return out


def load_vocab(path):
    """testset/vocab.json as {passage key: Passage}."""
    with open(path, encoding='utf-8') as f:
        data = json.load(f)
    return {k: passage_of(k, p['words'], p.get('book', ''), p.get('chapter', 0)) for k, p in data['passages'].items()}


def nearest(heard, passage):
    """The passage words nearest the heard text's sound, and how alike: ([Word], alike)."""
    keys = sounds(heard)
    best, at = [], 0.0
    for w in passage.words:
        a = max((alike(k, wk) for k in keys for wk in w.keys), default=0.0)
        if a > at + 1e-9:
            best, at = [w], a
        elif abs(a - at) <= 1e-9 and a > 0:
            best.append(w)
    return best, at


# ---- words --------------------------------------------------------------

def words_of(text):
    """Lower-case words, punctuation and apostrophes dropped, numbers as digits."""
    text = unicodedata.normalize('NFC', text.lower()).replace("'", '').replace('\u2019', '')
    kept = ''.join(c if unicodedata.category(c)[0] in 'LNM' else ' ' for c in text)
    toks = kept.split()
    out, i = [], 0
    while i < len(toks):
        t = toks[i]
        if t in TENS:
            n = TENS[t]
            if i + 1 < len(toks) and toks[i + 1] in NUMBERS and NUMBERS[toks[i + 1]] < 10:
                n += NUMBERS[toks[i + 1]]
                i += 1
            out.append(str(n))
        elif t in NUMBERS:
            out.append(str(NUMBERS[t]))
        else:
            out.append(t)
        i += 1
    return out


@dataclass
class Slot:
    expected: str
    heard: str = ''
    named: str = ''
    alike: float = 0.0
    right: bool = False


@dataclass
class Result:
    slots: list
    english_words: int
    english_errors: int

    @property
    def greek_total(self):
        return len(self.slots)

    @property
    def greek_right(self):
        return sum(s.right for s in self.slots)


def reference_items(reference):
    """The reference as English words (str) and Greek words (Slot), in order."""
    items = []
    for i, part in enumerate(re.split(r'\{([^}]*)\}', reference)):
        if i % 2:
            items.append(Slot(part.strip()))
        else:
            items.extend(words_of(part))
    return items


def _runs(toks):
    for a in range(len(toks)):
        for b in range(a + 1, len(toks) + 1):
            yield ''.join(toks[a:b])


def _judge(slot, toks, passage):
    """Whether some run of toks names the slot's word; fills the slot in."""
    expected = passage.word(slot.expected)
    slot.heard = ' '.join(toks)
    slot.right, slot.named, slot.alike = False, '', 0.0
    for run in _runs(toks):
        best, at = nearest(run, passage)
        if at < NAME_AT or not best:
            continue
        named = expected if expected in best else best[0]
        if named is expected and not slot.right:
            slot.right, slot.named, slot.alike = True, expected.greek, at
        elif not slot.right and at > slot.alike:
            slot.named, slot.alike = named.greek, at


ERR = 100  # one English error, or one Greek word with nothing heard for it


def score(reference, hypothesis, passage):
    """Score hypothesis against reference with passage's words: a Result."""
    ref = reference_items(reference)
    hyp = words_of(hypothesis)
    n, m = len(ref), len(hyp)
    expected = {i: passage.word(r.expected) for i, r in enumerate(ref) if isinstance(r, Slot)}

    def slot_cost(i, a, b):
        if a == b:
            return ERR
        w = expected[i]
        best = max((alike(k, wk) for run in _runs(hyp[a:b]) for k in sounds(run) for wk in w.keys), default=0.0)
        return int(round(ERR * (1 - best))) + (b - a)

    inf = float('inf')
    cost = [[inf] * (m + 1) for _ in range(n + 1)]
    back = [[None] * (m + 1) for _ in range(n + 1)]
    cost[0][0] = 0
    for i in range(n + 1):
        for j in range(m + 1):
            c = cost[i][j]
            if c == inf:
                continue
            if j < m and c + ERR < cost[i][j + 1]:  # an extra word heard
                cost[i][j + 1], back[i][j + 1] = c + ERR, ('ins', i, j)
            if i == n:
                continue
            r = ref[i]
            if isinstance(r, Slot):
                for b in range(j, min(m, j + MAX_SPAN) + 1):
                    v = c + slot_cost(i, j, b)
                    if v < cost[i + 1][b]:
                        cost[i + 1][b], back[i + 1][b] = v, ('slot', i, j)
                continue
            if c + ERR < cost[i + 1][j]:  # an English word not heard
                cost[i + 1][j], back[i + 1][j] = c + ERR, ('del', i, j)
            if j < m:
                v = c + (0 if hyp[j] == r else ERR)
                if v < cost[i + 1][j + 1]:
                    cost[i + 1][j + 1], back[i + 1][j + 1] = v, ('sub' if hyp[j] != r else 'ok', i, j)

    errors, slots = 0, []
    i, j = n, m
    while (i, j) != (0, 0):
        kind, pi, pj = back[i][j]
        if kind in ('ins', 'del', 'sub'):
            errors += 1
        elif kind == 'slot':
            slot = ref[pi]
            _judge(slot, hyp[pj:j], passage)
            slots.append(slot)
        i, j = pi, pj
    slots.reverse()
    return Result(slots, sum(isinstance(r, str) for r in ref), errors)
