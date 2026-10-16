"""bakeoff: run every candidate over one test set and print a table. Standard library only: each candidate
runs as sttbakeoff.worker in its family's venv.

  bakeoff list                       the candidates, and whether their venv is made
  bakeoff run [options]              run candidates over the clips, print the table(s)
  bakeoff table RESULTS [--markdown] the table again from a run's saved results
"""

import argparse
import json
import os
import subprocess
import sys
import time

from . import candidates, score, table, testset

HOME = os.environ.get('STT_BAKEOFF_HOME', os.path.expanduser('~/.local/share/stt-bakeoff'))
HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def venv_python(family):
    return os.path.join(HOME, 'venv-' + family, 'bin', 'python')


SCORED = ('greek_right', 'greek_total', 'english_errors', 'english_words', 'slots')


def scored(row, vocab):
    """row (a transcript: candidate, passage, expected, text, seconds) with its scores, by today's scoring."""
    r = score.score(row['expected'], row['text'], vocab[row['passage']])
    return dict(row, greek_right=r.greek_right, greek_total=r.greek_total, english_errors=r.english_errors,
                english_words=r.english_words,
                slots=[{'expected': s.expected, 'heard': s.heard, 'named': s.named, 'right': s.right} for s in r.slots])


def run_candidate(name, clips, vocab, threads, log):
    """Run one candidate's worker over clips [(path, question)]; ([result], peak MB)."""
    c = candidates.CANDIDATES[name]
    jobs = []
    for path, q in clips:
        p = vocab[q['passage']]
        jobs.append({'id': q['id'], 'path': path,
                     'prime': candidates.priming(name, p, p.book, p.chapter, q['verses'])})
    job = {'family': c['family'], 'model': c['model'], 'options': c, 'threads': threads, 'clips': jobs}
    env = dict(os.environ, PYTHONPATH=HERE, PYTHONDONTWRITEBYTECODE='1', OMP_NUM_THREADS=str(threads))
    proc = subprocess.Popen([venv_python(c['family']), '-m', 'sttbakeoff.worker'], stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, text=True, env=env)
    proc.stdin.write(json.dumps(job))
    proc.stdin.close()
    by_path = {path: q for path, q in clips}
    out = []
    for line in proc.stdout:
        msg = json.loads(line)
        if 'loaded' in msg:
            log(f'{name}: loaded in {msg["loaded"]} s')
            continue
        q = by_path[msg['path']]
        row = scored(dict(msg, candidate=name, passage=q['passage'], expected=q['text']), vocab)
        log(f'{name}: {msg["seconds"]:.2f} s {row["greek_right"]}/{row["greek_total"]} | {msg["text"]}')
        out.append(row)
    _, status, usage = os.wait4(proc.pid, 0)
    if status != 0:
        raise SystemExit(f'bakeoff: {name} failed (exit status {status >> 8})')
    return out, usage.ru_maxrss / 1024.0  # ru_maxrss is in KiB on Linux


def cmd_run(args):
    vocab = testset.load_vocab()
    qs = testset.questions()
    sets = [('synthetic', testset.clips(args.clips, qs))]
    if args.real:
        sets.append(('real', testset.real_clips(args.real)))
    names = args.candidate or list(candidates.CANDIDATES)
    unknown = [n for n in names if n not in candidates.CANDIDATES]
    if unknown:
        raise SystemExit(f'bakeoff: no candidate {", ".join(unknown)}: bakeoff list names them')
    log = (lambda s: print(s, file=sys.stderr, flush=True)) if not args.quiet else (lambda s: None)
    saved = {'when': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'threads': args.threads, 'sets': {}}
    for set_name, clips in sets:
        if not clips:
            print(f'{set_name}: no clips', file=sys.stderr)
            continue
        rows, peak = [], {}
        for n in names:
            r, mb = run_candidate(n, clips, vocab, args.threads, log)
            rows += r
            peak[n] = mb
        saved['sets'][set_name] = {'clips': [{k: v for k, v in r.items() if k not in SCORED} for r in rows],
                                   'peak_mb': peak}
        print(f'{set_name}: {len(clips)} clips, {args.threads} threads')
        print(table.render(table.rows(rows, peak)))
    if args.out:
        with open(args.out, 'w', encoding='utf-8') as f:
            json.dump(saved, f, ensure_ascii=False, indent=1)
        print(f'results saved in {args.out}')


def cmd_table(args):
    with open(args.results, encoding='utf-8') as f:
        saved = json.load(f)
    vocab = testset.load_vocab()
    for set_name, s in saved['sets'].items():
        clips = [scored(c, vocab) for c in s['clips']]  # scored again, so a scoring change reaches old runs
        rows = table.rows(clips, s['peak_mb'])
        print(f'{set_name}: {len({c["path"] for c in s["clips"]})} clips, {saved["threads"]} threads')
        print(table.markdown(rows) if args.markdown else table.render(rows))
        if args.misses:
            for c in clips:
                for sl in c['slots']:
                    if not sl['right']:
                        print(f'  {c["candidate"]} {c["id"]}: {sl["expected"]} heard {sl["heard"]!r}'
                              f' named {sl["named"] or "-"}')


def cmd_list(_):
    for n, c in candidates.CANDIDATES.items():
        made = 'ready' if os.path.exists(venv_python(c['family'])) else 'no venv: install.sh ' + c['family']
        print(f'{n:34} {c["model"]:28} {made}')


def main():
    ap = argparse.ArgumentParser(prog='bakeoff', description=__doc__, formatter_class=argparse.RawTextHelpFormatter)
    sub = ap.add_subparsers(dest='cmd', required=True)
    r = sub.add_parser('run', help='run candidates over the clips and print the table')
    r.add_argument('--candidate', action='append', help='one candidate (repeat; default: all)')
    r.add_argument('--clips', default=os.path.join(testset.TESTSET, 'synthetic'),
                   help='the synthetic clips: <id>.<audio> anywhere below (default testset/synthetic)')
    r.add_argument('--real', help="his own holds: <name>.<audio> beside <name>.json, scored separately")
    r.add_argument('--threads', type=int, default=8, help='CPU threads each candidate may use (default 8)')
    r.add_argument('--out', help='save every clip\'s transcript and timing here (JSON); bakeoff table scores it')
    r.add_argument('--quiet', action='store_true', help='no progress on standard error')
    t = sub.add_parser('table', help='the table again from saved results')
    t.add_argument('results')
    t.add_argument('--markdown', action='store_true')
    t.add_argument('--misses', action='store_true', help='also every Greek word a candidate missed')
    sub.add_parser('list', help='the candidates')
    args = ap.parse_args()
    {'run': cmd_run, 'table': cmd_table, 'list': cmd_list}[args.cmd](args)


if __name__ == '__main__':
    main()
