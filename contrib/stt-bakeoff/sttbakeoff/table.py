"""The bake-off's table: one line a candidate, totals summed over its clips. Standard library only."""

import math

COLUMNS = [
    ('candidate', 'candidate'),
    ('greek', 'Greek words'),
    ('wer', 'English WER'),
    ('median_s', 'median s'),
    ('p90_s', 'p90 s'),
    ('cpu_s', 'CPU s'),
    ('peak_mb', 'peak MB'),
    ('clips', 'clips'),
]


def percentile(xs, p):
    """The nearest-rank p-th percentile of xs."""
    xs = sorted(xs)
    return xs[max(0, math.ceil(p / 100 * len(xs)) - 1)]


def rows(clips, peak_mb):
    """Per-clip results ({candidate, greek_right, greek_total, english_errors, english_words, seconds,
    cpu_seconds}) as one row a candidate, the most Greek words right first, then the lowest WER. CPU s is the
    median CPU time a clip over all threads: what a clip costs however busy the host is."""
    by = {}
    for c in clips:
        by.setdefault(c['candidate'], []).append(c)
    out = []
    for name, cs in by.items():
        right = sum(c['greek_right'] for c in cs)
        total = sum(c['greek_total'] for c in cs)
        errors = sum(c['english_errors'] for c in cs)
        words = sum(c['english_words'] for c in cs)
        secs = [c['seconds'] for c in cs]
        cpu = [c['cpu_seconds'] for c in cs if 'cpu_seconds' in c]
        acc = right / total if total else 0.0
        wer = errors / words if words else 0.0
        out.append({
            'candidate': name,
            'greek': f'{right}/{total} {acc:.0%}',
            'wer': f'{wer:.1%}',
            'median_s': f'{percentile(secs, 50):.2f}',
            'p90_s': f'{percentile(secs, 90):.2f}',
            'cpu_s': f'{percentile(cpu, 50):.1f}' if cpu else '-',
            'peak_mb': f'{peak_mb.get(name, 0):.0f}',
            'clips': str(len(cs)),
            '_key': (-acc, wer, percentile(secs, 50)),
        })
    out.sort(key=lambda r: r['_key'])
    return out


def render(rows):
    """The rows as a fixed-width text table: a header, a rule, one line a row."""
    widths = [max([len(title)] + [len(r[k]) for r in rows]) for k, title in COLUMNS]
    line = lambda cells: '  '.join(c.ljust(w) for c, w in zip(cells, widths)).rstrip()
    out = [line([t for _, t in COLUMNS]), line(['-' * w for w in widths])]
    out += [line([r[k] for k, _ in COLUMNS]) for r in rows]
    return '\n'.join(out) + '\n'


def markdown(rows):
    """The rows as a Markdown table, for docs/research."""
    out = ['| ' + ' | '.join(t for _, t in COLUMNS) + ' |', '|' + '---|' * len(COLUMNS)]
    out += ['| ' + ' | '.join(r[k] for k, _ in COLUMNS) + ' |' for r in rows]
    return '\n'.join(out) + '\n'
