"""Listener.tokens_heard over a long clip, with a stub model that hears a click in a frame.

Needs numpy (the scorer's venv), not the model: test.sh runs it with the full tests.
The stub reports a token "a" for every 20 ms frame of its input that holds a click, so
where the real model would hear a phone, this hears exactly what the test put there,
whatever windows the Listener cuts the clip into.
"""

import contextlib
import unittest

import numpy as np

from mw_scorer import listen

RATE = listen.RATE
FRAME = 320


class Out:
    def __init__(self, ids):
        self.logits = [Argmax(ids)]


class Argmax:
    def __init__(self, ids):
        self.ids = ids

    def argmax(self, axis):
        return self


    def tolist(self):
        return self.ids


class Stub:
    """A Listener with no torch and no model: the features are the audio, the model hears clicks."""

    def __init__(self):
        self.heard = []   # the length in samples of each input the model was given
        listener = listen.Listener.__new__(listen.Listener)
        listener._torch = self
        listener.features = lambda audio, sampling_rate, return_tensors: Features(audio)
        listener.model = self.model
        listener.tokens = {1: "a"}
        listener.skip = set()
        listener.blank = 0
        listener.frame = FRAME / RATE
        self.listener = listener

    def inference_mode(self):
        return contextlib.nullcontext()

    def model(self, x):
        self.heard.append(len(x))
        frames = len(x) // FRAME
        blocks = np.abs(x[: frames * FRAME]).reshape(frames, FRAME).max(axis=1)
        return Out([1 if b >= 0.9 else 0 for b in blocks])


class Features:
    def __init__(self, audio):
        self.input_values = audio


def clip(seconds, clicks, loud=0.0, gaps=()):
    """Silence, or noise of amplitude loud outside the (from, to) gaps, with a click at each of clicks (seconds)."""
    n = int(seconds * RATE)
    audio = np.zeros(n, dtype=np.float32)
    if loud:
        audio[:] = np.random.RandomState(1).uniform(-loud, loud, n)
        for a, b in gaps:
            audio[int(a * RATE): int(b * RATE)] = 0
    for t in clicks:
        audio[int(t * RATE) // FRAME * FRAME + 5] = 1.0
    return audio


def expected(clicks):
    return [("a", round(int(t * RATE) // FRAME * FRAME / RATE, 2), round((int(t * RATE) // FRAME + 1) * FRAME / RATE, 2))
            for t in clicks]


class TokensHeard(unittest.TestCase):
    def test_a_clip_of_one_window_goes_through_the_model_in_one_pass(self):
        stub = Stub()
        clicks = [1.0, 12.5, 29.0]
        self.assertEqual(stub.listener.tokens_heard(clip(30, clicks)), expected(clicks))
        self.assertEqual(stub.heard, [30 * RATE])

    def test_a_long_clip_goes_through_in_windows_none_longer_than_the_limit(self):
        stub = Stub()
        stub.listener.tokens_heard(clip(100, [5.0]))
        self.assertGreater(len(stub.heard), 2)
        self.assertLessEqual(max(stub.heard), 36 * RATE)
        self.assertGreater(sum(stub.heard), 100 * RATE)   # the windows overlap

    def test_the_tokens_of_the_windows_come_back_on_the_whole_clips_clock_each_once(self):
        stub = Stub()
        # a click every 0.7 s, so some fall in every overlap and some on each cut
        clicks = [0.5 + 0.7 * k for k in range(140)]
        self.assertEqual(stub.listener.tokens_heard(clip(100, clicks)), expected(clicks))

    def test_a_click_just_either_side_of_a_cut_is_heard_once(self):
        stub = Stub()
        audio = clip(100, [])
        cuts = [w.keep_to for w in stub.listener.windows(audio)[:-1]]
        self.assertTrue(cuts)
        clicks = sorted(t for c in cuts for t in (c / RATE - 0.05, c / RATE + 0.05))
        self.assertEqual(stub.listener.tokens_heard(clip(100, clicks)), expected(clicks))

    def test_a_ten_minute_clip_is_all_heard_in_order(self):
        stub = Stub()
        clicks = [0.9 + 1.3 * k for k in range(450)]
        self.assertEqual(stub.listener.tokens_heard(clip(600, clicks)), expected(clicks))

    def test_the_cut_falls_in_the_quietest_place_near_the_window_length(self):
        stub = Stub()
        audio = clip(100, [], loud=0.3, gaps=[(28.0, 28.5), (57.0, 57.4)])
        cuts = [w.keep_to / RATE for w in stub.listener.windows(audio)[:-1]]
        self.assertTrue(28.0 <= cuts[0] <= 28.5, cuts)
        self.assertTrue(len(cuts) >= 2)

    def test_audio_under_a_tenth_of_a_second_hears_nothing(self):
        self.assertEqual(Stub().listener.tokens_heard(np.zeros(100, dtype=np.float32)), [])


if __name__ == "__main__":
    unittest.main()
