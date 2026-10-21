"""The alignment: what the target asked for against what the model heard, word by word.

The expected phones of every word are laid end to end and aligned with the
produced phones by a weighted Levenshtein distance (phones.substitution_cost;
a phone added or left out costs 1, except an Optional phone, which costs nothing
to leave out). Each word takes the produced phones aligned
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

Each word also says when it was heard: start and end, seconds from the start
of the recording, from its first produced phone's CTC frame to the end of its
last (an inserted word likewise; a word read twice, its last reading). They are
null for a word with nothing heard (omission, not_reached), and never run past
the clip's length.

A long reading is aligned in sections, between the stretches of eight phones or more
that the text and the reading share once each; the table of a section is kept in a
band of BAND phones each side of the best cell of the row before, and made again in a
band twice as wide when the alignment comes up against its edge (the time of a long
reading grows with its length, not its square).

accuracy per word = 100 x (1 - distance / expected phones), floored at 0; the
reading's accuracy is the mean over the words reached, insertions and the words
not reached left out (a reading that stops early is scored on what was read).
"""

import bisect
from typing import NamedTuple

from . import phones

MISPRONOUNCED_AT = 1.0
HESITATION_SECONDS = 0.8
# A run between words is an attempt at the next word when its distance to the
# word is less than this share of the word's phones.
ATTEMPT_SHARE = 0.67
# How far either side of the best cell of the row before the alignment table is kept.
BAND = 64
# A reading of more than ANCHOR_FROM phones is aligned in sections between the stretches of
# ANCHOR or more phones that expected and heard share once each (_anchors).
ANCHOR_FROM = 200
ANCHOR = 8


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
                          _step(d[i - 1][j], _gap(expected[i - 1])))
    return d


def _gap(phone):
    """What leaving an expected phone out costs: nothing for an optional one, else 1."""
    return 0 if isinstance(phone, phones.Optional) else 1


def _step(cell, cost, inside=0):
    return (cell[0] + cost, cell[1] + inside)


_OUT = (float("inf"), 0)


class _Band:
    """_table kept in a band: row i holds columns lo[i] to lo[i] + len(rows[i]) - 1, the
    half columns each side of the row before's best (least cost, then lowest) column.
    A cell outside it is _OUT, a cost nothing can win with. A table of no more rows than
    half, or no more columns, is whole.

    first is whether the table starts at the start of what was heard: a table that
    starts later is a section, whose left column leaves expected phones out at their own
    cost (an Optional one for nothing) and whose first row adds phones as inside(0) says."""

    def __init__(self, expected, produced, inside, cost, half, first=True):
        n, m = len(expected), len(produced)
        if n <= half:
            half = max(half, m)
        self.m = m
        self.whole = half >= m
        flag = inside(0)
        self.lo = [0]
        self.rows = [[(float(j), j * flag) for j in range(min(m, half) + 1)]]
        edge = 0.0
        for i in range(1, n + 1):
            e, gap, flag = expected[i - 1], _gap(expected[i - 1]), inside(i)
            edge = float(i) if first else edge + gap
            prev, plo = self.rows[-1], self.lo[-1]
            centre = plo + min(range(len(prev)), key=lambda k: prev[k][0])
            lo, hi = max(0, centre - half), min(m, centre + half)
            row = []
            for j in range(lo, hi + 1):
                if j == 0:
                    row.append((edge, 0))
                    continue
                k = j - 1 - plo
                best = _OUT
                if 0 <= k < len(prev):
                    c = prev[k]
                    best = (c[0] + cost(e, produced[j - 1]), c[1])
                if row:
                    c = row[-1]
                    best = min(best, (c[0] + 1, c[1] + flag))
                if 0 <= k + 1 < len(prev):
                    c = prev[k + 1]
                    best = min(best, (c[0] + gap, c[1]))
                row.append(best)
            self.rows.append(row)
            self.lo.append(lo)

    def at(self, i, j):
        k = j - self.lo[i]
        return self.rows[i][k] if 0 <= k < len(self.rows[i]) else _OUT

    def edge(self, i, j):
        """Whether (i, j) is at the edge of the band or outside it, where a better alignment may lie beyond."""
        lo, row = self.lo[i], self.rows[i]
        return not lo <= j < lo + len(row) or (j == lo and lo > 0) or (j == lo + len(row) - 1 and j < self.m)


def _ops(d, expected, produced, inside, cost, end):
    """The alignment of expected[:end] with all of produced, from the table d, as
    (op, i, j) from the start: op is "sub" (a match or a substitution of expected[i]
    by produced[j]), "ins" (produced[j] added before expected[i]; i may be end) or
    "del" (expected[i] left out). At a tie the later produced phones are matched, so
    a word read twice is matched on its last reading. None when the alignment comes
    up against the edge of the band."""
    i, j, out = end, len(produced), []
    while i > 0 or j > 0:
        if d.edge(i, j):
            return None
        here = d.at(i, j)
        if i > 0 and j > 0 and here == _step(d.at(i - 1, j - 1), cost(expected[i - 1], produced[j - 1])):
            i, j = i - 1, j - 1
            out.append(("sub", i, j))
        elif j > 0 and here == _step(d.at(i, j - 1), 1, inside(i)):
            j -= 1
            out.append(("ins", i, j))
        elif j == 0 or d.whole or here == _step(d.at(i - 1, j), _gap(expected[i - 1])):
            i -= 1
            out.append(("del", i, j))
        else:
            return None
    out.reverse()
    return out


def _anchors(expected, heard, cost):
    """Stretches of at least ANCHOR phones that expected and heard share, each once in
    each, in the same order in both: [(i, j, length)], the alignment passes through
    them. A stretch with a phone the reader may leave out is not one, nor is one that
    overlaps another."""
    def windows(seq):
        found = {}
        for x in range(len(seq) - ANCHOR + 1):
            found.setdefault(tuple(seq[x:x + ANCHOR]), []).append(x)
        return found

    there = windows(heard)
    pairs = []
    for window, at in windows(expected).items():
        said = there.get(window)
        if len(at) == 1 and said and len(said) == 1 and not any(isinstance(p, phones.Optional) for p in window) \
                and all(cost(expected[at[0] + k], heard[said[0] + k]) == 0 for k in range(ANCHOR)):
            pairs.append((at[0], said[0]))
    pairs.sort()
    runs = []                   # [i, j, length] of windows on one diagonal, end to end
    for i, j in pairs:
        if runs and runs[-1][0] + runs[-1][2] - ANCHOR + 1 == i and runs[-1][1] + runs[-1][2] - ANCHOR + 1 == j:
            runs[-1][2] += 1
        else:
            runs.append([i, j, ANCHOR])
    # a run that shares a phone of expected or of heard with another is not to be trusted:
    # a word read twice makes two of them, and the alignment is to choose which
    clash = set()
    for place in (0, 1):
        by = sorted(range(len(runs)), key=lambda r: runs[r][place])
        for a, b in zip(by, by[1:]):
            if runs[a][place] + runs[a][2] > runs[b][place]:
                clash.update((a, b))
    runs = [run for r, run in enumerate(runs) if r not in clash]
    # the longest chain of runs that rises in what was heard as it does in expected
    tails, tail_of, parent = [], [], []
    for r, (_, j, _) in enumerate(runs):
        k = bisect.bisect_left(tail_of, j)
        parent.append(tails[k - 1] if k else None)
        if k == len(tails):
            tails.append(r)
            tail_of.append(j)
        else:
            tails[k] = r
            tail_of[k] = j
    chain, r = [], tails[-1] if tails else None
    while r is not None:
        chain.append(runs[r])
        r = parent[r]
    chain.reverse()
    out = []
    for i, j, length in chain:
        if not out or (i >= out[-1][0] + out[-1][2] and j >= out[-1][1] + out[-1][2]):
            out.append((i, j, length))
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
    heard = [p.phone for p in produced]
    reached, ops = _align(words, heard, cost)
    result = _score_words(words[:reached], produced, seconds, hesitation_seconds, engine, cost, ops)
    for text, expected in words[reached:]:
        result["words"].append(_word(text, expected, [], 0, "not_reached"))
    _hold_to_clip(result["words"], result["seconds"])
    return result


def _hold_to_clip(words, seconds):
    """A CTC frame can end a little past the clip's length: hold the times to it."""
    for w in words:
        if w["end"] is None or w["end"] <= seconds:
            continue
        w["end"] = seconds
        if w["start"] >= seconds:
            w["start"] = round(max(0.0, seconds - 0.01), 2)


def _align(words, heard, cost):
    """How many of the target's words the reading reached, and the alignment (_ops) of
    their phones with everything heard. Reached is the cut that leaves the least
    weighted distance between the words before it and everything heard: all of the
    words when nothing was heard, or when no word has phones.

    A reading of more than ANCHOR_FROM phones is aligned in sections: the alignment
    passes through the stretches that expected and heard share (_anchors), and each
    section between them has its table made apart, in a band (_Band) made wider while
    the alignment comes up against its edge. The cut is in the section after the last
    anchor."""
    flat = [(w, p) for w, (_, expected) in enumerate(words) for p in expected]
    owner = [w for w, _ in flat]
    expected = [p for _, p in flat]

    def inside(i):
        return 1 if 0 < i < len(flat) and owner[i - 1] == owner[i] else 0

    anchors = _anchors(expected, heard, cost) if heard and len(expected) > ANCHOR_FROM else []
    ops, i0, j0 = [], 0, 0
    for i, j, length in anchors:
        ops.extend(_section(expected, heard, inside, cost, i0, i, j0, j))
        ops.extend(("sub", i + k, j + k) for k in range(length))
        i0, j0 = i + length, j + length
    reached, tail = _tail(words, expected, heard, inside, cost, i0, j0)
    return reached, ops + tail


def _section(expected, heard, inside, cost, i0, i1, j0, j1):
    """The alignment (_ops, in the places of the whole) of expected[i0:i1] with heard[j0:j1]."""
    half = BAND
    while True:
        e, h = expected[i0:i1], heard[j0:j1]

        def here(i):
            return inside(i0 + i)

        d = _Band(e, h, here, cost, half, first=j0 == 0)
        ops = _ops(d, e, h, here, cost, len(e))
        if ops is not None:
            return [(op, i + i0, j + j0) for op, i, j in ops]
        half *= 2


def _tail(words, expected, heard, inside, cost, i0, j0):
    """How many words are reached, and the alignment of their phones from (i0, j0) to
    the end of what was heard, the section after the last anchor."""
    e, h = expected[i0:], heard[j0:]

    def here(i):
        return inside(i0 + i)

    half = BAND
    while True:
        d = _Band(e, h, here, cost, half, first=j0 == 0)
        reached = _reached(words, d, i0) if heard and expected else len(words)
        if reached is not None:
            end = sum(len(w[1]) for w in words[:reached]) - i0
            ops = _ops(d, e, h, here, cost, end)
            if ops is not None:
                return reached, [(op, i + i0, j + j0) for op, i, j in ops]
        half *= 2


def _reached(words, d, start):
    """The cut that leaves the least weighted distance between the words before it and
    everything heard (of equal cuts, the later), among the cuts at or after phone start
    of the target and in the cells of d's last column; None when there is none."""
    best, reached, taken = None, None, 0
    for w, (_, expected) in enumerate(words):
        taken += len(expected)
        if taken < start:
            continue
        cost = d.at(taken - start, d.m)[0]
        if cost < float("inf") and (best is None or cost <= best):
            best, reached = cost, w + 1
    return reached


def _score_words(words, produced, seconds, hesitation_seconds, engine, cost, ops):
    """score_reading for words that were all reached, whose phones ops aligns with produced."""
    flat = [(w, p) for w, (_, expected) in enumerate(words) for p in expected]
    owner = [w for w, _ in flat]
    heard = [p.phone for p in produced]
    own = [[] for _ in words]       # indexes into produced of each word's phones
    spent = [0.0 for _ in words]
    runs = {}                       # boundary (index of the next word, or len(words)) -> produced indexes

    def inside(i):
        return 1 if 0 < i < len(flat) and owner[i - 1] == owner[i] else 0

    for op, i, j in ops:
        if op == "sub":
            own[owner[i]].append(j)
            spent[owner[i]] += cost(flat[i][1], heard[j])
        elif op == "del":
            spent[owner[i]] += _gap(flat[i][1])
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
        "start": mine[0].start if mine else None,
        "end": mine[-1].end if mine else None,
    }


def _inserted(run, produced):
    said = [produced[j] for j in run]
    text = "".join(p.heard for p in said) or " ".join(p.phone for p in said)
    return _word(text, [], said, 0, "insertion")
