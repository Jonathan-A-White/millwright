"""Where a long clip is cut into windows for the model.

wav2vec2's self-attention grows with the square of the frames it sees at once, so
one pass over a ten-minute reading costs far more than ten one-minute passes
(0.25 s per audio second on a short clip, 0.9 on a 600 s one). A clip longer than
WINDOW seconds is cut into regions of about that length, each cut at the quietest
place within SEARCH seconds of the window length, so a cut falls in a pause rather
than inside a word. Each region is listened to with PAD seconds of its neighbours
on both sides, for context, and keeps only the tokens whose middle is in the region
itself: the padding is heard twice and kept once.

Pure (no numpy), so the plan is tested with any python3 (test.sh --pure).
"""

from typing import NamedTuple

RATE = 16000
FRAME = 320      # samples in one model frame (20 ms): cuts and starts land on a frame, as in one pass
WINDOW = 30      # seconds; a clip this long or shorter is one pass, exactly as it always was
SEARCH = 3       # seconds either side of a window's end to look for the quietest place
PAD = 1          # seconds of the neighbouring regions heard for context
MIN_TAIL = 5     # seconds: the last region is never shorter


class Window(NamedTuple):
    """Samples [start, end) go through the model; the tokens whose middle is in
    [keep_from, keep_to) are the region's."""
    start: int
    end: int
    keep_from: int
    keep_to: int

    def owns(self, start, end):
        """Whether a token heard from start to end seconds (on the whole clip's clock) is this region's."""
        middle = (start + end) / 2
        return self.keep_from / RATE <= middle < self.keep_to / RATE


def plan(samples, quiet):
    """The windows of a clip of that many samples, in order. quiet(lo, hi) is the sample
    in [lo, hi) where the clip is quietest."""
    if samples <= WINDOW * RATE:
        return [Window(0, samples, 0, samples)]
    cuts, pos = [], 0
    while samples - pos > (WINDOW + SEARCH) * RATE:
        target = pos + WINDOW * RATE
        lo, hi = target - SEARCH * RATE, min(target + SEARCH * RATE, samples - MIN_TAIL * RATE)
        pos = quiet(lo, hi) // FRAME * FRAME
        cuts.append(pos)
    edges = [0] + cuts + [samples]
    pad = PAD * RATE
    return [Window(max(0, a - pad) // FRAME * FRAME, min(samples, b + pad), a, b)
            for a, b in zip(edges, edges[1:])]
