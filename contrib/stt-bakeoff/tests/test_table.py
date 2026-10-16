"""The bake-off's table: totals per candidate, from per-clip results. Standard library only."""

import unittest

from sttbakeoff import table


def clip(candidate, right, total, errors, words, seconds, cpu=None):
    c = {'candidate': candidate, 'greek_right': right, 'greek_total': total,
         'english_errors': errors, 'english_words': words, 'seconds': seconds}
    if cpu is not None:
        c['cpu_seconds'] = cpu
    return c


class Percentiles(unittest.TestCase):
    def test_median_and_p90_by_nearest_rank(self):
        xs = [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0]
        self.assertEqual(table.percentile(xs, 50), 5.0)
        self.assertEqual(table.percentile(xs, 90), 9.0)
        self.assertEqual(table.percentile([4.0], 90), 4.0)


class Rows(unittest.TestCase):
    def test_totals_are_summed_over_clips_not_averaged(self):
        rows = table.rows([
            clip('a', 1, 1, 0, 5, 1.0, 6.0),
            clip('a', 0, 3, 2, 5, 3.0, 20.0),
            clip('b', 2, 2, 1, 10, 0.5),
        ], {'a': 1024.0, 'b': 512.0})
        self.assertEqual([r['candidate'] for r in rows], ['b', 'a'])  # the best Greek first
        a = rows[1]
        self.assertEqual(a['greek'], '1/4 25%')
        self.assertEqual(a['wer'], '20.0%')
        self.assertEqual(a['median_s'], '1.00')
        self.assertEqual(a['p90_s'], '3.00')
        self.assertEqual(a['peak_mb'], '1024')
        self.assertEqual(a['cpu_s'], '6.0')
        self.assertEqual(rows[0]['cpu_s'], '-')  # results saved before CPU time was kept

    def test_the_table_prints_a_header_and_one_line_a_candidate(self):
        text = table.render(table.rows([clip('a', 1, 1, 0, 5, 1.0)], {'a': 10.0}))
        lines = text.splitlines()
        self.assertIn('Greek words', lines[0])
        self.assertIn('English WER', lines[0])
        self.assertIn('median s', lines[0])
        self.assertIn('p90 s', lines[0])
        self.assertIn('CPU s', lines[0])
        self.assertIn('peak MB', lines[0])
        self.assertEqual(len(lines), 3)  # header, rule, one candidate


if __name__ == '__main__':
    unittest.main()
