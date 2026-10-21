"""Where a long clip is cut into windows for the model (windows.plan): pure, no numpy.

The same cuts through a stub model, with real audio, are in test_listen.py.
"""

import unittest

from mw_scorer import windows

RATE = 16000
FRAME = 320   # one wav2vec2 frame, in samples


def centre(lo, hi):
    return (lo + hi) // 2


class Plan(unittest.TestCase):
    def test_a_clip_of_one_window_or_less_is_one_pass_over_all_of_it(self):
        n = windows.WINDOW * RATE
        self.assertEqual(windows.plan(n, centre), [windows.Window(0, n, 0, n)])
        self.assertEqual(windows.plan(1000, centre), [windows.Window(0, 1000, 0, 1000)])

    def test_a_longer_clip_is_cut_near_every_window_length(self):
        n = 100 * RATE
        plan = windows.plan(n, centre)
        self.assertGreater(len(plan), 2)
        for w in plan:
            self.assertLessEqual(w.end - w.start, (windows.WINDOW + windows.SEARCH + 2 * windows.PAD) * RATE)
        for w in plan[:-1]:
            self.assertGreaterEqual(w.keep_to - w.keep_from, (windows.WINDOW - windows.SEARCH) * RATE)

    def test_the_kept_parts_tile_the_clip_so_no_sample_is_kept_twice_or_never(self):
        for seconds in (31, 40, 63.3, 100, 300, 600):
            n = int(seconds * RATE)
            plan = windows.plan(n, centre)
            self.assertEqual(plan[0].keep_from, 0)
            self.assertEqual(plan[-1].keep_to, n)
            for a, b in zip(plan, plan[1:]):
                self.assertEqual(a.keep_to, b.keep_from, seconds)

    def test_each_window_reaches_past_its_kept_part_into_its_neighbours_for_context(self):
        plan = windows.plan(100 * RATE, centre)
        for a, b in zip(plan, plan[1:]):
            self.assertGreater(a.end, a.keep_to)
            self.assertLess(b.start, b.keep_from)
            self.assertGreater(a.end, b.start)   # the overlap
        self.assertEqual(plan[0].start, 0)
        self.assertEqual(plan[-1].end, 100 * RATE)

    def test_every_cut_and_window_start_is_on_a_frame_so_times_match_a_one_pass_run(self):
        plan = windows.plan(int(77.7 * RATE), lambda lo, hi: lo + 123)
        for w in plan:
            self.assertEqual(w.start % FRAME, 0)
            self.assertEqual(w.keep_from % FRAME, 0)

    def test_the_cut_is_where_quiet_says_within_the_search_around_the_window_length(self):
        asked = []

        def quiet(lo, hi):
            asked.append((lo, hi))
            return lo + 1000

        plan = windows.plan(100 * RATE, quiet)
        self.assertEqual(asked[0], ((windows.WINDOW - windows.SEARCH) * RATE, (windows.WINDOW + windows.SEARCH) * RATE))
        self.assertEqual(plan[0].keep_to, (windows.WINDOW - windows.SEARCH) * RATE // FRAME * FRAME + 1000 // FRAME * FRAME)

    def test_the_last_window_is_never_a_sliver(self):
        for tenths in range(300, 700):
            n = tenths * RATE // 10
            plan = windows.plan(n, lambda lo, hi: hi - 1)   # the worst case: always the latest cut
            self.assertGreaterEqual(plan[-1].keep_to - plan[-1].keep_from, windows.MIN_TAIL * RATE - FRAME, tenths)

    def test_a_plan_of_ten_minutes_is_linear_in_the_windows(self):
        plan = windows.plan(600 * RATE, centre)
        self.assertTrue(18 <= len(plan) <= 24, len(plan))

    def test_a_token_belongs_to_the_window_that_keeps_its_middle(self):
        plan = windows.plan(100 * RATE, centre)
        cut = plan[0].keep_to / RATE
        self.assertTrue(plan[0].owns(cut - 0.03, cut - 0.01))
        self.assertFalse(plan[0].owns(cut + 0.01, cut + 0.03))
        self.assertTrue(plan[1].owns(cut + 0.01, cut + 0.03))
        self.assertFalse(plan[1].owns(cut - 0.03, cut - 0.01))
        # a token that straddles the cut goes to exactly one of the two
        self.assertEqual(plan[0].owns(cut - 0.01, cut + 0.03) + plan[1].owns(cut - 0.01, cut + 0.03), 1)
        self.assertTrue(plan[-1].owns(99.9, 100.0))   # the last window keeps the end


if __name__ == "__main__":
    unittest.main()
