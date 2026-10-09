"""The alignment: what the target asked for against what the model heard, word by word.

The expected phones of every word are laid end to end and aligned with the
produced phones by a weighted Levenshtein distance (phones.substitution_cost;
a phone added or left out costs 1). Each word takes the produced phones aligned
to its own; the words are anchored in time by those phones' CTC frame times.
Of two alignments that cost the same, the one with fewer phones added inside a
word wins (extra phones belong between words), then the one matching the later
produced phones (a word read twice is matched on its last reading).

A reading may stop before the text does. The expected phones are not all
owed: the words after the one the reading reached (the cut) are free, so the
alignment is to the start of the text and never scatters the phones heard over
the words after it. The cut is where the weighted distance to the produced
phones is least (of equal cuts, the later one: a word attempted is a word
reached); at least the first word is always reached, and with nothing heard
there is no cut.

Labels, in this order:
  not_reached       a word after the cut: the reader stopped before it
  omission          nothing produced for the word
  mispronunciation  the word's distance is MISPRONOUNCED_AT or more: one far
                    phone, or two near ones
  hesitation        a gap of more than hesitation_seconds before the word,
                    after the word before it (never on the first word)
  none              otherwise
A run of two or more produced phones between words is an attempt at the next
word when it is close to it (the word is then self_corrected if its final
reading is closer still), and otherwise a word the reader added: an insertion.
One stray phone between words is ignored.

accuracy per word = 100 x (1 - distance / expected phones), floored at 0; the
reading's accuracy is the mean over the words reached, insertions and the words
not reached left out (a reading that stops early is scored on what was read).
"""

from typing import NamedTuple

from . import phones

MISPRONOUNCED_AT = 1.0
HESITATION_SECONDS = 0.8
# A run between words is an attempt at the next word when its distance to the
# word is less than this share of the word's phones.
ATTEMPT_SHARE = 0.67


class Phone(NamedTuple):
    """One produced phone: its ARPAbet name, when it was heard, and the model's
    own token for it (on the first phone of a token only)."""
    phone: str
    start: float
    end: float
    heard: str = ""


def _round(x):
    return int(x + 0.5)


def distance(expected, produced, cost=phones.substitution_cost):
    """The weighted edit distance between two lists of phones, by the cost of
    substituting one for another (English ARPAbet unless a language says otherwise)."""
    return _table(expected, produced, cost=cost)[len(expected)][len(produced)][0]


def _table(expected, produced, inside=lambda i: 0, cost=phones.substitution_cost):
    """d[i][j] is (cost, phones added inside a word) of the best alignment of
    expected[:i] with produced[:j], least cost first: of two alignments that cost
    the same, the one that puts added phones between words rather than inside
    one wins. inside(i) is 1 when position i (before expected[i]) is inside a word."""
    n, m = len(expected), len(produced)
    d = [[(0.0, 0)] * (m + 1) for _ in range(n + 1)]
    for i in range(1, n + 1):
        d[i][0] = (float(i), 0)
    for j in range(1, m + 1):
        d[0][j] = (float(j), 0)
    for i in range(1, n + 1):
        for j in range(1, m + 1):
            d[i][j] = min(_step(d[i - 1][j - 1], cost(expected[i - 1], produced[j - 1])),
                          _step(d[i][j - 1], 1, inside(i)),
                          _step(d[i - 1][j], 1))
    return d


def _step(cell, cost, inside=0):
    return (cell[0] + cost, cell[1] + inside)


def _ops(expected, produced, inside, cost=phones.substitution_cost):
    """The alignment as (op, i, j) from the start: op is "sub" (a match or a
    substitution of expected[i] by produced[j]), "ins" (produced[j] added
    before expected[i]; i may be len(expected)) or "del" (expected[i] left out).
    At a tie the later produced phones are matched, so a word read twice is
    matched on its last reading."""
    d = _table(expected, produced, inside, cost)
    i, j, out = len(expected), len(produced), []
    while i > 0 or j > 0:
        if i > 0 and j > 0 and d[i][j] == _step(d[i - 1][j - 1], cost(expected[i - 1], produced[j - 1])):
            i, j = i - 1, j - 1
            out.append(("sub", i, j))
        elif j > 0 and d[i][j] == _step(d[i][j - 1], 1, inside(i)):
            j -= 1
            out.append(("ins", i, j))
        else:
            i -= 1
            out.append(("del", i, j))
    out.reverse()
    return out


def score_reading(words, produced, seconds, hesitation_seconds=HESITATION_SECONDS, engine="local",
                  cost=phones.substitution_cost):
    """A ReadingResult (docs/scorers.md) as a dict.

    words: the target's words in order, as (text, expected phones).
    produced: the Phones heard, in time order.
    seconds: how long the clip is.
    cost: the cost of one phone for another (phones.substitution_cost for ARPAbet,
          phones.ipa_cost(lang) for a reading compared as IPA).
    """
    reached = _reached(words, [p.phone for p in produced], cost)
    result = _score_words(words[:reached], produced, seconds, hesitation_seconds, engine, cost)
    for text, expected in words[reached:]:
        result["words"].append(_word(text, expected, [], 0, "not_reached"))
    return result


def _reached(words, heard, cost):
    """How many of the target's words the reading reached: the cut that leaves
    the least weighted distance between the words before it and everything heard.
    All of them when nothing was heard, or when no word has phones."""
    flat = [p for _, expected in words for p in expected]
    if not heard or not flat:
        return len(words)
    d = _table(flat, heard, cost=cost)
    best, reached, taken = None, len(words), 0
    for w, (_, expected) in enumerate(words):
        taken += len(expected)
        if best is None or d[taken][len(heard)][0] <= best:
            best, reached = d[taken][len(heard)][0], w + 1
    return reached


def _score_words(words, produced, seconds, hesitation_seconds, engine, cost):
    """score_reading for words that were all reached."""
    flat = [(w, p) for w, (_, expected) in enumerate(words) for p in expected]
    owner = [w for w, _ in flat]
    heard = [p.phone for p in produced]
    own = [[] for _ in words]       # indexes into produced of each word's phones
    spent = [0.0 for _ in words]
    runs = {}                       # boundary (index of the next word, or len(words)) -> produced indexes

    def inside(i):
        return 1 if 0 < i < len(flat) and owner[i - 1] == owner[i] else 0

    for op, i, j in _ops([p for _, p in flat], heard, inside, cost):
        if op == "sub":
            own[owner[i]].append(j)
            spent[owner[i]] += cost(flat[i][1], heard[j])
        elif op == "del":
            spent[owner[i]] += 1
        elif inside(i):
            own[owner[i]].append(j)   # added inside a word: part of the word
            spent[owner[i]] += 1
        else:
            runs.setdefault(owner[i] if i < len(flat) else len(words), []).append(j)

    attempt = [None for _ in words]
    added = {}
    for boundary, run in runs.items():
        if len(run) < 2:
            continue
        if boundary < len(words) and words[boundary][1]:
            expected = words[boundary][1]
            gone = distance(expected, [heard[j] for j in run], cost)
            if gone < ATTEMPT_SHARE * len(expected):
                attempt[boundary] = (gone, run)
                continue
        added[boundary] = run

    out, accuracies, last_end = [], [], None
    for w, (text, expected) in enumerate(words):
        if w in added:
            run = added[w]
            out.append(_inserted(run, produced))
            last_end = produced[run[-1]].end
        mine = [produced[j] for j in own[w]]
        if not expected:
            entry = _word(text, expected, mine, 100, "none")
        elif not mine:
            entry = _word(text, expected, mine, 0, "omission")
        else:
            accuracy = _round(100 * max(0.0, 1 - spent[w] / len(expected)))
            error = "mispronunciation" if spent[w] >= MISPRONOUNCED_AT else "none"
            first = min(own[w] + (attempt[w][1] if attempt[w] else []))
            if error == "none" and last_end is not None and produced[first].start - last_end > hesitation_seconds:
                error = "hesitation"
            entry = _word(text, expected, mine, accuracy, error)
            entry["self_corrected"] = attempt[w] is not None and attempt[w][0] > spent[w]
            last_end = mine[-1].end
        accuracies.append(entry["accuracy"])
        out.append(entry)
    if len(words) in added:
        out.append(_inserted(added[len(words)], produced))

    return {
        "engine": engine,
        "words": out,
        "accuracy": _round(sum(accuracies) / len(accuracies)) if accuracies else 0,
        "seconds": round(max(0.0, seconds), 2),
    }


def _word(text, expected, mine, accuracy, error):
    return {
        "text": text,
        "expected_phonemes": list(expected),
        "produced_phonemes": [p.phone for p in mine],
        "error": error,
        "accuracy": accuracy,
        "self_corrected": False,
    }


def _inserted(run, produced):
    said = [produced[j] for j in run]
    text = "".join(p.heard for p in said) or " ".join(p.phone for p in said)
    return _word(text, [], said, 0, "insertion")
