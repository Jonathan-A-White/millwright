# Research: what the desktop should run, and at what cap by day and by night

Answers mw-6ww.185.11 (D11 of the desktop plan, `plans/desktop-reliability-2026-11-21.md` in the vault,
sections 2 and 3). Written 2026-11-21 on cloud2 by a Builder. It reads the vault's own records of the
desktop from 2026-10-28 to 2026-11-18 (22 UTC days, both included) and **sets nothing on any host**: the
lines at the end are the Mayor's to run, each with its way back, and the `.wslconfig` change rides the
Governor's next Windows paste.

The Governor's words (2026-11-21), the question this answers: "asking it to do the things that should do
and not asking it to do the things that it can't anymore".

<!-- toc -->

- [The answer in short](#the-answer-in-short)
- [1. What was read](#1-what-was-read)
- [2. The command](#2-the-command)
- [2a. Gate seconds, load, memory and swap by how many stories ran at once](#2a-gate-seconds-load-memory-and-swap-by-how-many-stories-ran-at-once)
- [2b. Landings a day, refusals by cause, gate waits](#2b-landings-a-day-refusals-by-cause-gate-waits)
- [3. The caps, with the reasoning](#3-the-caps-with-the-reasoning)
- [4. What it could not measure](#4-what-it-could-not-measure)
- [5. What the desktop takes, and what it never takes](#5-what-the-desktop-takes-and-what-it-never-takes)
- [6. The lines (the Mayor runs them; nothing was run)](#6-the-lines-the-mayor-runs-them-nothing-was-run)

<!-- /toc -->

## The answer in short

| Setting | Now (read, not set by this note) | Recommended | The figures beside it |
|-|-|-|-|
| `cap` by day | 4 since 2026-11-14 20:24Z, his "Keep cap 4 across the board. No need for a night window" (handoff 907) | **2** | R9: with 1 or 2 stories running, 2 of 63 gates started at load over 8 and 1 of 63 saw swap-in; with 3, 7 of 21 and 6 of 21; with 4, 29 of 48 and 12 of 48. R1: gates took 255 s alone and 602 s with 2, then 731 s with 3, 611 s with 4: no steady gain from more than 2. R7: all 10 `timeout_under_load` refusals the desktop ever had ran with 3 to 5 running |
| `cap` by night (23:00 to 06:00 America/New_York) | no window since 2026-11-14 20:38Z | **3**, kept as the plan's provisional value, on a stop rule (below) | R4: night gates with 3 running took 667 s at load 6.8 (6 gates), with 2 274 s (7 gates). R9 does not tell 3 from 4 on swap (6 of 21, 12 of 48). Nothing in the records shows a third session landing more: the landings of a day follow what was waiting (R10) |
| `builder_jobs` | 4 since 2026-10-13 (`docs/fuel-knobs.md`; not read from the host today) | **4**, unchanged, all hours | It held for the whole window, so no record separates 4 from another number. 2 sessions x 4 workers is 8 threads of 16 by day, 3 x 4 is 12 by night |
| `.wslconfig` `memory` | 12GB (`.wslconfig` read 2026-11-06 in handoff 793: `vmIdleTimeout=-1`, `memory=12GB`, `swap=8GB`, `autoMemoryReclaim=gradual`; the 12 GB read back in R6 and at 20:48Z on 11-14) | **12GB**, unchanged | R9: the most used at a gate start on a 12 GB day was 8,688 MB (4 running), the 90th percentile 5,594 MB. The one out-of-memory on record (2026-11-07 06:06Z) was at 7.8 GB with the file set aside, not at 12 |
| `.wslconfig` `processors` | not set: `cores` is 16 in all 179 gate records (R1 header) | **8** (the one change) | R9: with 1 or 2 running 61 of 63 gates started at load 8 or under, so a ceiling of 8 threads touches almost nothing the caps now allow, and always leaves Grace 8 |
| `.wslconfig` `swap` | 8GB | **8GB**, unchanged | Swap-out was seen at 5 of 179 gates (R5), at most 5,700 pages a second, never with 1 or 2 running except one gate at 60 |

One more setting this note found out of line: the vault's copy of the home's table, `hosts/vultr/cloud-table.toml`, says `[cloud.caps] desktop = 6`, which tells the home's cloud check that the desktop has 6 sessions' room. See "The lines".

His own cap of 2026-11-14 ("Keep cap 4 across the board") is **older than these figures and these figures disagree with it**: the Mayor asks him before the lines are run.

## 1. What was read

All paths are in the vault `~/millwright-vault`, read on cloud2 (the vault as synced there). Nothing was read on the desktop: it was not asked, and WSL is sealed from Windows (`hosts/desktop.md`).

- `runs/*/result.json` and `runs/*/result-<n>.json` (a later attempt's): one record per close-out's gate. The fields used: `host` (only `desktop`), `benchmark_at` (the UTC day), `rig`, `gate_seconds` (how long the rig's tests ran; the waits for the gate lock and for the host to calm are not in it, `application/benchmark.go`), `running_count` (stories claimed on the host and not hitl when the first gate began, itself among them, `application/benchmarkrecord.go`: it counts claims, so it passes the cap when a finished session's claim is still held), `load_at_gate`, `cores`, `mem_free_mb` (available memory) and `swap_in_per_s`, `swap_out_per_s` (taken **when the tests start**, not their peak), `landed`, `actual_s`, `timeout_under_load` (set only on a refusal for tests that timed out while the load was at or over the cores).
- `seats/builder/ledger.md`: the lines saying `dispatched by mw on desktop` (374 in the window). The outcome is read from the line (`landed on <branch>`, `queued for batch landing`, `not landed (<cause>)`, `tested ... nothing merged`) and the waits from `gate waited <time> for ...` in its note.
- The part timings, `runs/*/check-<n>-part-<n>.took`: 44 files in the vault, **all `host cloud2`, all 2026-11-21**. There is none from the desktop in the window, so no figure below is made from them.
- The cap and `.wslconfig` history is from the Mayor's handoffs (`seats/mayor/handoffs/`, numbers given where used), `docs/fuel-knobs.md`, `docs/research/desktop-shared-pc.md`, `hosts/desktop.md`.

## 2. The command

One script made every figure marked R or L below. Its sections are named R1 to R10 and L1 to L3 in its output; each table here is that section, copied. To run it, from any host with the vault:

```sh
cd ~/millwright-vault && awk '/^```python$/{f=1;next} /^```$/{f=0} f' ~/millwright/docs/research/desktop-right-size.md | python3 -I -
```

(`~/millwright` is any checkout holding this file; it reads only `runs/*/result*.json` and `seats/builder/ledger.md`, and writes nothing.) The script follows.

```python
import json, glob, re, collections, statistics as st, datetime as dt
from zoneinfo import ZoneInfo

FROM, TO = '2026-10-28', '2026-11-18'          # UTC days, both included
NY = ZoneInfo('America/New_York')


def med(x): return round(st.median(x)) if x else None
def p90(x):
    if not x: return None
    x = sorted(x); return round(x[min(len(x) - 1, int(0.9 * len(x)))])
def grp(n): return str(n) if n <= 4 else '5+'
def night(at):
    t = dt.datetime.strptime(at, '%Y-%m-%dT%H:%M:%SZ').replace(tzinfo=dt.timezone.utc).astimezone(NY)
    return t.hour >= 23 or t.hour < 6


rows = []
for f in sorted(glob.glob('runs/*/result.json') + glob.glob('runs/*/result-[0-9]*.json')):
    try: d = json.load(open(f))
    except Exception: continue
    if d.get('host') == 'desktop' and 'gate_seconds' in d and FROM <= d['benchmark_at'][:10] <= TO:
        d['_f'] = f; rows.append(d)
print('result.json files, host desktop, gate_seconds present, %s..%s: %d' % (FROM, TO, len(rows)))
print('cores seen:', dict(collections.Counter(d['cores'] for d in rows)))
print()

print('## R1. by sessions running at once (running_count), all rigs')
print('| running | gates | median gate s | p90 gate s | median load | median free MB | min free MB | swap-out>0 | timeout_under_load | landed |')
print('|-|-|-|-|-|-|-|-|-|-|')
by = collections.defaultdict(list)
for d in rows: by[grp(d['running_count'])].append(d)
for k in sorted(by):
    r = by[k]; g = [d['gate_seconds'] for d in r]
    print('| %s | %d | %s | %s | %.1f | %s | %s | %d | %d | %d |' % (
        k, len(r), med(g), p90(g), st.median(d['load_at_gate'] for d in r),
        med([d['mem_free_mb'] for d in r]), min(d['mem_free_mb'] for d in r),
        sum(d['swap_out_per_s'] > 0 for d in r), sum(bool(d['timeout_under_load']) for d in r),
        sum(bool(d['landed']) for d in r)))
print()

print('## R2. by rig and sessions at once: gates, median gate s')
print('| rig | 1 | 2 | 3 | 4 | 5+ | all |')
print('|-|-|-|-|-|-|-|')
rigs = collections.Counter(d['rig'] for d in rows)
for rig, n in rigs.most_common():
    cells = []
    for k in ['1', '2', '3', '4', '5+']:
        g = [d['gate_seconds'] for d in rows if d['rig'] == rig and grp(d['running_count']) == k]
        cells.append('%d: %s s' % (len(g), med(g)) if g else '-')
    allg = [d['gate_seconds'] for d in rows if d['rig'] == rig]
    print('| %s | %s | %d: %s s |' % (rig, ' | '.join(cells), n, med(allg)))
print()

print('## R3. by day: gates, landed, most sessions at once, median gate s, load, free MB')
print('| day (UTC) | gates | landed | most at once | median gate s | median load | min free MB | swap-out>0 |')
print('|-|-|-|-|-|-|-|-|')
days = collections.defaultdict(list)
for d in rows: days[d['benchmark_at'][:10]].append(d)
for day in sorted(days):
    r = days[day]
    print('| %s | %d | %d | %d | %s | %.1f | %d | %d |' % (
        day, len(r), sum(bool(d['landed']) for d in r), max(d['running_count'] for d in r),
        med([d['gate_seconds'] for d in r]), st.median(d['load_at_gate'] for d in r),
        min(d['mem_free_mb'] for d in r), sum(d['swap_out_per_s'] > 0 for d in r)))
print()

print('## R4. night (23:00 to 06:00 New York) against day, by sessions at once')
print('| running | day gates | day median s | day median load | night gates | night median s | night median load |')
print('|-|-|-|-|-|-|-|')
for k in ['1', '2', '3', '4', '5+']:
    a = [d for d in rows if grp(d['running_count']) == k and not night(d['benchmark_at'])]
    b = [d for d in rows if grp(d['running_count']) == k and night(d['benchmark_at'])]
    f = lambda r: (len(r), med([d['gate_seconds'] for d in r]), round(st.median(d['load_at_gate'] for d in r), 1) if r else None)
    print('| %s | %d | %s | %s | %d | %s | %s |' % ((k,) + f(a) + f(b)))
print()

print('## R5. by sessions at once: session length (actual_s) and swap')
print('| running | sessions | median session min | swap_in>0 | swap_out>0 | max swap_out_per_s |')
print('|-|-|-|-|-|-|')
for k in sorted(by):
    r = by[k]
    print('| %s | %d | %s | %d | %d | %s |' % (k, len(r), round(st.median(d['actual_s'] for d in r) / 60),
          sum(d['swap_in_per_s'] > 0 for d in r), sum(d['swap_out_per_s'] > 0 for d in r),
          max(d['swap_out_per_s'] for d in r)))
print()

print('## R6. free memory by day (largest free MB read that day shows how much memory WSL had)')
print('| day (UTC) | max free MB | min free MB |')
print('|-|-|-|')
for day in sorted(days):
    m = [d['mem_free_mb'] for d in days[day]]
    print('| %s | %d | %d |' % (day, max(m), min(m)))
print()

# the ledger
L = [l for l in open('seats/builder/ledger.md') if 'on desktop' in l]
led = []
for l in L:
    c = [x.strip() for x in l.split('|')]
    if len(c) > 6 and FROM <= c[1] <= TO: led.append((c[1], l, ''))
print('## L1. ledger lines "on desktop", %s..%s: %d' % (FROM, TO, len(led)))
OUT = re.compile(r'\| (landed on \w+|not landed \(([a-z-]+)\)|queued for batch landing|tested [^|]*nothing merged)')
def cause(o):
    m = OUT.search(o)
    if not m: return 'other'
    t = m.group(1)
    if t.startswith('landed'): return 'landed'
    if t.startswith('queued'): return 'queued for the batch'
    if t.startswith('tested'): return 'tester (nothing merged)'
    return 'refused: ' + m.group(2)
print('| outcome | lines | of which the text says "did not end in" (a timeout) |')
print('|-|-|-|')
cc = collections.Counter(); tt = collections.Counter()
for day, o, note in led:
    k = cause(o); cc[k] += 1
    if 'did not end in' in o: tt[k] += 1
for k, n in cc.most_common(): print('| %s | %d | %d |' % (k, n, tt[k]))
print()

print('## L2. landings and refusals a day (ledger lines on desktop), with gate wait')
print('| day (UTC) | landed or queued | refused | tester | gate waited, total min | longest wait min |')
print('|-|-|-|-|-|-|')
wd = collections.defaultdict(lambda: [0, 0, 0, 0.0, 0.0])
for l in L:
    c = [x.strip() for x in l.split('|')]
    if len(c) < 7 or not (FROM <= c[1] <= TO): continue
    w = wd[c[1]]; k = cause(l)
    if k in ('landed', 'queued for the batch'): w[0] += 1
    elif k.startswith('refused'): w[1] += 1
    elif k.startswith('tester'): w[2] += 1
    for m in re.finditer(r'gate waited (?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)? for', l):
        s = int(m.group(1) or 0) * 3600 + int(m.group(2) or 0) * 60 + int(m.group(3) or 0)
        w[3] += s / 60; w[4] = max(w[4], s / 60)
for day in sorted(wd):
    w = wd[day]; print('| %s | %d | %d | %d | %.0f | %.0f |' % (day, w[0], w[1], w[2], w[3], w[4]))
print()

print('## L3. gate wait on the ledger lines, by whether the line landed')
tot = collections.defaultdict(list)
for l in L:
    c = [x.strip() for x in l.split('|')]
    if len(c) < 7 or not (FROM <= c[1] <= TO): continue
    ws = 0
    for m in re.finditer(r'gate waited (?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)? for', l):
        ws += int(m.group(1) or 0) * 3600 + int(m.group(2) or 0) * 60 + int(m.group(3) or 0)
    tot[cause(l).split(':')[0]].append(ws / 60)
print('| outcome | lines | with a wait | median wait min (those with) | total wait min |')
print('|-|-|-|-|-|')
for k, v in tot.items():
    w = [x for x in v if x > 0]
    print('| %s | %d | %d | %s | %.0f |' % (k, len(v), len(w), round(st.median(w), 1) if w else '-', sum(v)))

print()
print('## R7. load at the gate: how often it was at or over the 16 threads, and the timeout flag, by host, whole history')
print('| host | gate records | load_at_gate >= cores | timeout_under_load true |')
print('|-|-|-|-|')
allr = collections.defaultdict(list)
for f in glob.glob('runs/*/result.json') + glob.glob('runs/*/result-[0-9]*.json'):
    try: d = json.load(open(f))
    except Exception: continue
    if 'gate_seconds' in d: allr[d.get('host')].append(d)
for h in ['desktop', 'laptop', 'cloud1', 'cloud2', 'cloud3']:
    r = allr[h]
    print('| %s | %d | %d | %d |' % (h, len(r), sum(d['load_at_gate'] >= d['cores'] > 0 for d in r), sum(bool(d['timeout_under_load']) for d in r)))
print('desktop, in the window, highest load_at_gate: %.1f' % max(d['load_at_gate'] for d in rows))
print()
print('## R8. landings by rig (result.json landed=true), window of 22 days')
print('| rig | landed | days with a landing | most in a day |')
print('|-|-|-|-|')
lr = collections.defaultdict(collections.Counter)
for d in rows:
    if d['landed']: lr[d['rig']][d['benchmark_at'][:10]] += 1
for rig in sorted(lr, key=lambda r: -sum(lr[r].values())):
    print('| %s | %d | %d | %d |' % (rig, sum(lr[rig].values()), len(lr[rig]), max(lr[rig].values())))

print()
print('## R9. memory and load by sessions at once, on the days WSL held its 12 GB')
print('(a day counts as a 12 GB day when its largest mem_free_mb reading is over 10,000; used = 12,200 - mem_free_mb, MemTotal as read on 2026-10-13)')
dm = collections.defaultdict(int)
for d in rows: dm[d['benchmark_at'][:10]] = max(dm[d['benchmark_at'][:10]], d['mem_free_mb'])
big = [d for d in rows if dm[d['benchmark_at'][:10]] > 10000]
print('12 GB days: %d gates; other days: %d gates (%s)' % (len(big), len(rows) - len(big), ', '.join(sorted(set(d['benchmark_at'][:10] for d in rows if dm[d['benchmark_at'][:10]] <= 10000)))))
print('| running | gates | used MB, median | used MB, p90 | used MB, max | load > 8 | load >= 12 | swap-in > 0 | swap-out > 0 |')
print('|-|-|-|-|-|-|-|-|-|')
for k in ['1', '2', '3', '4', '5+']:
    r = [d for d in big if grp(d['running_count']) == k]
    if not r: continue
    u = [12200 - d['mem_free_mb'] for d in r]
    print('| %s | %d | %d | %d | %d | %d | %d | %d | %d |' % (k, len(r), med(u), p90(u), max(u),
          sum(d['load_at_gate'] > 8 for d in r), sum(d['load_at_gate'] >= 12 for d in r),
          sum(d['swap_in_per_s'] > 0 for d in r), sum(d['swap_out_per_s'] > 0 for d in r)))
print()
print('## R10. days grouped by the most sessions at once that day (landed or queued: the ledger; gate wait: the ledger)')
print('| most at once | days | landed or queued a day, mean | refused a day, mean | gate wait min a day, mean |')
print('|-|-|-|-|-|')
bucket = collections.defaultdict(list)
for day, r in days.items():
    m = max(d['running_count'] for d in r)
    w = wd.get(day, [0, 0, 0, 0.0, 0.0])
    bucket['1-2' if m <= 2 else '3-4' if m <= 4 else '5-6' if m <= 6 else '7+'].append(w)
for k in ['1-2', '3-4', '5-6', '7+']:
    v = bucket.get(k, [])
    if v: print('| %s | %d | %.1f | %.1f | %.0f |' % (k, len(v), st.mean(w[0] for w in v), st.mean(w[1] for w in v), st.mean(w[3] for w in v)))
```

## 2a. Gate seconds, load, memory and swap by how many stories ran at once

Made from `runs/*/result.json`, `runs/*/result-<n>.json` (command above, section R1). Header of the output: 179 gate records, `cores` 16 in every one.

**R1**, all rigs:

| running | gates | median gate s | p90 gate s | median load | median free MB | min free MB | swap-out>0 | timeout_under_load | landed |
|-|-|-|-|-|-|-|-|-|-|
| 1 | 24 | 255 | 809 | 2.4 | 11072 | 6987 | 1 | 0 | 21 |
| 2 | 44 | 602 | 889 | 4.0 | 10924 | 5305 | 0 | 0 | 40 |
| 3 | 25 | 731 | 1703 | 5.5 | 10219 | 3031 | 1 | 0 | 21 |
| 4 | 49 | 611 | 1500 | 8.7 | 9399 | 3512 | 3 | 0 | 41 |
| 5+ | 37 | 657 | 1103 | 6.0 | 10299 | 5005 | 0 | 0 | 37 |

Reading: a gate alone took 255 s; with a second story claimed it took 602 s, and it did not get quicker or slower in any steady way with more (731, 611, 657). Free memory fell with the count (median 11,072 MB with 1, 9,399 MB with 4). `timeout_under_load` is 0 in the whole window (see R7 for the whole history).

**R2**, the same by rig (`n: median gate seconds`):

| rig | 1 | 2 | 3 | 4 | 5+ | all |
|-|-|-|-|-|-|-|
| millwright | 18: 302 s | 27: 679 s | 14: 722 s | 28: 658 s | 29: 733 s | 116: 664 s |
| trade-tracker | 4: 51 s | 5: 68 s | 2: 61 s | 7: 153 s | 2: 136 s | 20: 70 s |
| postern | - | 5: 733 s | 5: 734 s | 6: 1181 s | 2: 532 s | 18: 759 s |
| lampas | 1: 1345 s | 1: 3400 s | 3: 2641 s | 3: 1896 s | - | 8: 2269 s |
| pwa-kit | - | 1: 41 s | 1: 49 s | 3: 69 s | - | 5: 49 s |
| whisper-hid | - | 3: 37 s | - | - | 1: 72 s | 4: 54 s |
| nac-service-media | - | - | - | - | 2: 283 s | 2: 283 s |
| voice-kit | - | - | - | 2: 100 s | - | 2: 100 s |
| bsv-kit | 1: 22 s | - | - | - | - | 1: 22 s |
| night-stack | - | 1: 85 s | - | - | - | 1: 85 s |
| cairn | - | 1: 51 s | - | - | - | 1: 51 s |
| learn-kit | - | - | - | - | 1: 16 s | 1: 16 s |

Reading: millwright is 116 of the 179 gates and went 302 s, 679 s, 722 s, 658 s, 733 s: the step is from 1 to 2, as for all rigs. Lampas, the rig whose gates are long, had 8 gates on the desktop (1,345 s to 3,400 s).

**R9**, memory and load by count, on the 12 GB days only (see its note on how a day is told):

(a day counts as a 12 GB day when its largest mem_free_mb reading is over 10,000; used = 12,200 - mem_free_mb, MemTotal as read on 2026-10-13)
12 GB days: 169 gates; other days: 10 gates (2026-11-01, 2026-11-07, 2026-11-08, 2026-11-11, 2026-11-15)
| running | gates | used MB, median | used MB, p90 | used MB, max | load > 8 | load >= 12 | swap-in > 0 | swap-out > 0 |
|-|-|-|-|-|-|-|-|-|
| 1 | 22 | 1124 | 1164 | 1223 | 0 | 0 | 0 | 1 |
| 2 | 41 | 1269 | 2216 | 5915 | 2 | 1 | 1 | 0 |
| 3 | 21 | 1801 | 4724 | 5044 | 7 | 4 | 6 | 1 |
| 4 | 48 | 2793 | 5594 | 8688 | 29 | 13 | 12 | 3 |
| 5+ | 37 | 1901 | 5224 | 7195 | 13 | 8 | 3 | 0 |

Reading: the cliff is between 2 and 3. At 1 or 2 running, 2 of 63 gates started at load over 8 and 1 saw swap-in. At 3 it is 7 of 21 and 6 of 21, at 4 it is 29 of 48 and 12 of 48. The most used at a gate start on a 12 GB day was 8,688 MB (4 running): memory was never out at 12 GB in this window.

**R5**, session length and swap:

| running | sessions | median session min | swap_in>0 | swap_out>0 | max swap_out_per_s |
|-|-|-|-|-|-|
| 1 | 24 | 22 | 0 | 1 | 60.25980551611306 |
| 2 | 44 | 35 | 1 | 0 | 0 |
| 3 | 25 | 39 | 7 | 1 | 5699.570374890877 |
| 4 | 49 | 41 | 12 | 3 | 3120.4050340002045 |
| 5+ | 37 | 52 | 3 | 0 | 0 |

**R4**, night (23:00 to 06:00 New York) against day, same counts:

| running | day gates | day median s | day median load | night gates | night median s | night median load |
|-|-|-|-|-|-|-|
| 1 | 18 | 247 | 1.8 | 6 | 398 | 3.3 |
| 2 | 37 | 617 | 4.2 | 7 | 274 | 2.6 |
| 3 | 19 | 733 | 4.8 | 6 | 667 | 6.8 |
| 4 | 39 | 539 | 8.0 | 10 | 720 | 10.7 |
| 5+ | 22 | 584 | 5.9 | 15 | 873 | 6.2 |

Reading: the night's samples are small (6 to 15 gates a row). At night the gate with 2 running was quickest (274 s, 7 gates); with 4 it was 720 s at load 10.7 and with 5 or more 873 s at load 6.2.

## 2b. Landings a day, refusals by cause, gate waits

**R3**, by day (the most at once is the highest `running_count` of that day's gates; `landed` is the gates whose story landed):

| day (UTC) | gates | landed | most at once | median gate s | median load | min free MB | swap-out>0 |
|-|-|-|-|-|-|-|-|
| 2026-10-28 | 5 | 5 | 3 | 210 | 2.8 | 7795 | 0 |
| 2026-10-29 | 8 | 5 | 7 | 250 | 9.7 | 3512 | 0 |
| 2026-10-30 | 11 | 11 | 4 | 66 | 7.6 | 5460 | 1 |
| 2026-10-31 | 10 | 6 | 4 | 282 | 4.2 | 8367 | 0 |
| 2026-11-01 | 1 | 1 | 2 | 100 | 9.8 | 8811 | 0 |
| 2026-11-02 | 5 | 5 | 4 | 68 | 4.2 | 8821 | 0 |
| 2026-11-03 | 29 | 25 | 5 | 779 | 6.8 | 8240 | 0 |
| 2026-11-04 | 18 | 15 | 4 | 255 | 2.9 | 7204 | 2 |
| 2026-11-05 | 5 | 2 | 4 | 780 | 14.3 | 6193 | 2 |
| 2026-11-07 | 6 | 6 | 3 | 370 | 4.7 | 3031 | 0 |
| 2026-11-08 | 1 | 0 | 4 | 1896 | 6.2 | 7725 | 0 |
| 2026-11-09 | 2 | 2 | 5 | 351 | 1.1 | 10967 | 0 |
| 2026-11-10 | 11 | 11 | 5 | 682 | 2.6 | 8705 | 0 |
| 2026-11-11 | 1 | 1 | 1 | 575 | 0.3 | 8876 | 0 |
| 2026-11-13 | 23 | 23 | 2 | 693 | 3.4 | 6285 | 0 |
| 2026-11-14 | 11 | 11 | 6 | 723 | 5.0 | 7772 | 0 |
| 2026-11-15 | 1 | 1 | 3 | 731 | 7.4 | 4307 | 0 |
| 2026-11-16 | 4 | 3 | 8 | 707 | 8.3 | 8938 | 0 |
| 2026-11-17 | 17 | 17 | 6 | 604 | 9.2 | 5727 | 0 |
| 2026-11-18 | 10 | 10 | 10 | 796 | 5.9 | 9080 | 0 |

**L2**, from the ledger's lines on desktop (a line is `landed` or `queued for batch landing` for a story whose work is done; the gate wait is the sum of its `gate waited` notes):

| day (UTC) | landed or queued | refused | tester | gate waited, total min | longest wait min |
|-|-|-|-|-|-|
| 2026-10-28 | 10 | 0 | 0 | 0 | 0 |
| 2026-10-29 | 17 | 5 | 0 | 69 | 32 |
| 2026-10-30 | 19 | 7 | 0 | 20 | 6 |
| 2026-10-31 | 6 | 0 | 0 | 0 | 0 |
| 2026-11-01 | 8 | 0 | 0 | 0 | 0 |
| 2026-11-02 | 20 | 5 | 0 | 163 | 48 |
| 2026-11-03 | 28 | 12 | 0 | 415 | 34 |
| 2026-11-04 | 18 | 7 | 1 | 7 | 3 |
| 2026-11-05 | 0 | 5 | 0 | 9 | 9 |
| 2026-11-06 | 3 | 0 | 3 | 0 | 0 |
| 2026-11-07 | 14 | 3 | 0 | 0 | 0 |
| 2026-11-08 | 8 | 0 | 0 | 0 | 0 |
| 2026-11-09 | 13 | 0 | 8 | 0 | 0 |
| 2026-11-10 | 21 | 2 | 9 | 46 | 15 |
| 2026-11-11 | 1 | 0 | 0 | 0 | 0 |
| 2026-11-13 | 31 | 2 | 0 | 43 | 5 |
| 2026-11-14 | 10 | 8 | 5 | 22 | 10 |
| 2026-11-15 | 3 | 6 | 0 | 0 | 0 |
| 2026-11-16 | 4 | 5 | 1 | 48 | 12 |
| 2026-11-17 | 31 | 6 | 5 | 184 | 37 |
| 2026-11-18 | 1 | 2 | 0 | 19 | 8 |

**R10**, the days grouped by the most at once:

| most at once | days | landed or queued a day, mean | refused a day, mean | gate wait min a day, mean |
|-|-|-|-|-|
| 1-2 | 3 | 13.3 | 0.7 | 14 |
| 3-4 | 9 | 10.9 | 3.7 | 22 |
| 5-6 | 5 | 20.6 | 5.6 | 134 |
| 7+ | 3 | 7.3 | 4.0 | 46 |

Reading: there are 3 days in the lowest group and 3 in the highest, so this table is a sketch. Its one steady figure: the day with 5 or 6 at once that landed most (11-03, 28 landed or queued) also waited 415 minutes for the gate lock in total; 11-13, at most 2 at once, landed 31 and waited 43. Landings follow what was waiting, not the cap; more at once bought waiting at the gate.

**L1**, the outcomes of the 374 lines, with the refusals by cause:

| outcome | lines | of which the text says "did not end in" (a timeout) |
|-|-|-|
| landed | 159 | 0 |
| queued for the batch | 107 | 0 |
| tester (nothing merged) | 32 | 0 |
| refused: open-steps | 23 | 0 |
| refused: tests-fail | 21 | 2 |
| refused: merged-tests-fail | 15 | 0 |
| refused: unmet-criterion | 5 | 0 |
| refused: turn-cap | 3 | 0 |
| refused: merge-conflict | 2 | 0 |
| refused: no-commits | 2 | 0 |
| refused: toc-stale | 1 | 0 |
| other | 1 | 0 |
| refused: no-closing-comment | 1 | 0 |
| refused: acceptance-changed | 1 | 0 |
| refused: host-gap | 1 | 0 |

Reading: of 75 refusals, 36 are failing tests (`tests-fail`, `merged-tests-fail`); 23 are `open-steps` (the Builder stopped with formula steps open: not the host); 5 `unmet-criterion`; 3 `turn-cap`. **Timeouts, apart:** 2 of the `tests-fail` lines say the run "did not end in 10m0s" (2026-11-05 and 2026-11-16); `timeout_under_load` is set on none of the 179 records in the window.

**R7**, the timeout flag and load at or over the cores, by host, whole history:

| host | gate records | load_at_gate >= cores | timeout_under_load true |
|-|-|-|-|
| desktop | 526 | 1 | 10 |
| laptop | 1908 | 67 | 6 |
| cloud1 | 199 | 1 | 4 |
| cloud2 | 188 | 0 | 1 |
| cloud3 | 112 | 0 | 0 |
desktop, in the window, highest load_at_gate: 17.0

Reading: the desktop's 10 flagged refusals are all before the window (2026-10-10 to 2026-10-27), all with 3 to 5 running (lampas 5, postern 5), at loads of 6.1 to 15.9. The window had none.

**L3**, the waits for the gate lock on the ledger lines:

| outcome | lines | with a wait | median wait min (those with) | total wait min |
|-|-|-|-|-|
| queued for the batch | 107 | 6 | 29.1 | 197 |
| landed | 159 | 62 | 6.5 | 553 |
| refused | 75 | 23 | 10.7 | 296 |
| tester (nothing merged) | 32 | 0 | - | 0 |
| other | 1 | 0 | - | 0 |

**R8**, landings by rig (`landed` true), the 22 days:

| rig | landed | days with a landing | most in a day |
|-|-|-|-|
| millwright | 108 | 18 | 20 |
| trade-tracker | 20 | 10 | 6 |
| postern | 16 | 8 | 4 |
| pwa-kit | 5 | 3 | 3 |
| whisper-hid | 4 | 2 | 3 |
| nac-service-media | 2 | 1 | 2 |
| bsv-kit | 1 | 1 | 1 |
| night-stack | 1 | 1 | 1 |
| cairn | 1 | 1 | 1 |
| learn-kit | 1 | 1 | 1 |
| voice-kit | 1 | 1 | 1 |

**R6**, the memory a day shows (the largest `mem_free_mb` of the day is how much WSL had; ten gates fall on the five days that never read over 10,000 MB):

| day (UTC) | max free MB | min free MB |
|-|-|-|
| 2026-10-28 | 11126 | 7795 |
| 2026-10-29 | 10821 | 3512 |
| 2026-10-30 | 11108 | 5460 |
| 2026-10-31 | 11114 | 8367 |
| 2026-11-01 | 8811 | 8811 |
| 2026-11-02 | 11104 | 8821 |
| 2026-11-03 | 11084 | 8240 |
| 2026-11-04 | 11080 | 7204 |
| 2026-11-05 | 10399 | 6193 |
| 2026-11-07 | 9163 | 3031 |
| 2026-11-08 | 7725 | 7725 |
| 2026-11-09 | 11057 | 10967 |
| 2026-11-10 | 11061 | 8705 |
| 2026-11-11 | 8876 | 8876 |
| 2026-11-13 | 11243 | 6285 |
| 2026-11-14 | 11184 | 7772 |
| 2026-11-15 | 4307 | 4307 |
| 2026-11-16 | 10982 | 8938 |
| 2026-11-17 | 11083 | 5727 |
| 2026-11-18 | 10871 | 9080 |

Reading: from 2026-11-09 the day's largest reading is back over 10,000 MB (11-11 and 11-15 have one gate each), so the `.wslconfig` of 12GB/8GB was in force again after its set-aside of 11-06 to 11-07 (handoff 805 said the rename back was still owed on 11-07). On 11-14 at 20:48Z the Mayor read 10.8 GB free of 12 (handoff 907). This is read from the memory figures; the file itself was not read.

## 3. The caps, with the reasoning

**Cap by day: 2.** Grace uses the machine (her hours are not in the vault); give way (mw-2891blf) freezes the factory when she is at the screen, but it is the factory's own state when she is not that decides whether a gate starts in a swamp. With 2 claimed, 2 of 63 gates started over load 8 and none was refused for a timeout under load; with 3 a third of them started over load 8; with 4, 60%. A story's gate takes about 600 s with 2 running and no less with more. The records give no reason to run a third by day.

**Cap by night: 3, on a stop rule.** At night nobody is at the screen, so the cost of a third session falls on the factory alone: gates with 3 running at night took 667 s at load 6.8 (6 gates), under the 16 threads, and no gate in the window was refused for a timeout under load. But the same records show no gain: landings follow what was waiting (R10), and 3 is past the cliff of R9. So 3 is the most the figures allow, not a figure they support by a gain. **Stop rule for the proof week:** if over the seven nights any night's gate is refused for a timeout under load, or sees swap-out, or more than a third of the night's gates start at load over 8 (R9's figure for 3 running by day was a third), set the window's cap to 2 or remove the window.

**Where the plan's night window sat.** The Governor removed it on 2026-11-14 ("Keep cap 4 across the board. No need for a night window"). The window's recorded night was cap 4 in the 2026-10-15 table of `docs/fuel-knobs.md`; R4 says 4 and above at night took 720 s and more at load 10.7 or more. The 11-05 night sat at load 14 to 30 of 16 (the plan, class C).

**builder_jobs: 4, all hours, no window key.** It has been 4 since 2026-10-13 and no record has another value, so the records can say only how gates ran at 4, as R1 shows. It bounds the workers inside one run, so 2 sessions give 8 threads' worth of tests by day and 3 give 12 by night (a window that leaves `builder_jobs` out keeps the root value). `builder_test_runs` (README) is not set on the desktop (`docs/fuel-knobs.md`: "none recorded"); gates already take the host's gate lock one at a time, which is why the gate waits of L2 grow with the count; no record here shows a second gate running at once on the desktop, so none is proposed.

**`.wslconfig`.** `memory=12GB` stays: used memory at a gate start peaked at 8,688 MB (R9), a snapshot not a peak, and the one out-of-memory was at 7.8 GB. `swap=8GB` stays: it was in use at 5 of 179 gates. `processors=8` is the one change: `cores` is 16 in every record, so the 8-thread ceiling of `docs/research/desktop-shared-pc.md` (2026-10-13) was never in force in the window; at cap 2 the tests ask for 8 threads (R9 shows 61 of 63 of those gates under load 8), at night's 3 for 12, and the other 8 threads are then the PC's other user's whatever the factory asks. The cost is slower gates at night when the load goes over 8 (R9: 7 of 21 at 3 running).

## 4. What it could not measure

- **Windows memory and the other user's use.** Interop and automount are off (`hosts/desktop.md`), so Windows' memory, her PIN bounce at 78 to 80% (2026-10-08, mw-6ww.95) and what she did and when are not in the vault. The plan's windows-state (D2) is what will read them; until then `memory=12GB` cannot be tested against her 16 GB. Give way's own log is on the desktop, not in the vault.
- **Peaks.** `mem_free_mb`, the swap rates and the load are one reading at the start of a gate, not the highest of the gate or of the night. The out-of-memory of 2026-11-07 06:06Z and the load of 14 to 30 on the 11-05 night are not in a gate record: the nearest records are the 5 gates of 11-05 (median load 14.3, R3) and one gate at 07:22Z on 11-07, an hour after the wedge: 3 running, 3,031 MB free of 7.8 GB.
- **The cap in force at each gate.** No record names it. The caps above are from handoffs and `docs/fuel-knobs.md`: 4 to 2026-11-07, then 2 (handoff 805); retired from 11-11 (handoff 848); un-retired at 2 on 11-13 (887), 0 on 11-14 07:45Z (898), 4 from 11-14 20:24Z (907). `running_count` is claims, so the "at once" of every table is stories the host held, which passed the cap on 11-16 (8) and 11-18 (10).
- **A count's own effect on landings.** There are 22 days and the queue of ready stories changed day to day (days with one gate only: 11-01, 11-08, 11-11, 11-15); the figures can show what a count costs (load, swap, gate wait) and that landings did not rise with it, not what a count would land with a full queue. Only the seven nights at the proof story (D13) can.
- **A part's time on the desktop.** No `.took` file is from the desktop (section 1), so the time of a gate by part is unknown there.
- **builder_jobs** at any other value; **`processors`** at any value but 16 (never in force in the window); `grist_cap` (the cap less 2 and never under 1, `application/gristroom.go`: 1 at cap 2 while a tutor is in use; whether a tutor is ever used on the desktop is not read).
- **Whether the desktop's units, config lines and `.wslconfig` are as the vault says today.** Nothing was read from it; each figure of "Now" is from a dated record.

## 5. What the desktop takes, and what it never takes

**It takes** (the role of the plan: a free cloud box, PROVISIONAL): story sessions and their checks, `host=auto` or pinned to it by name (11-13: night-stack, spell-forge, cairn were pathed `host=desktop`, handoff 887); Tester sessions (32 of the 374 lines); the landing of the rigs that land each by itself (159 `landed on` lines in the window); and stories worked and checked there then queued for a rig's batch (107 lines `queued for batch landing`, the way Lampas lands). `testers_per_rig` and the shared-files gate hold as on every host.

**It never takes**, with what keeps it away today:

| Never | What keeps it away today | If nothing: the smallest change |
|-|-|-|
| A home role: the served beads database (`dolt-beads`) | The vault's `home` file names the laptop (`laptop 2026-09-29T12:57:09Z mayor`), and `mw home move`, the Governor's tap, is the only writer (`home_move_bead`). That says where hosts read; it does not stop a unit on the desktop. Whether the desktop's `dolt-beads.service` is enabled is not read here | **Nothing keeps it away** at the unit level: nothing stops a unit running there. A doctor check `host-roles` run on a host that is not home, warning for `dolt-beads`, the Postern backend and the mirror active (`systemctl --user is-active`): one file of `infrastructure/doctor/` and a line in `cmd/mw/doctor.go`, beside `beads-server` |
| A seat (Mayor, Deputy, a Builder seat on the host) | `mw dispatch` starts only story sessions; the `hitl` gate keeps stories that need the Governor off every host (`docs/dispatch-gates.md`). A seat is started by hand | **Nothing keeps it away** from a hand starting one. It is the Governor's and the Mayor's habit; the `host-roles` check above can name a seat's tmux session |
| The Postern backend or its mirror | `retired_hosts` makes the mirror skip a host (README); the plan says both are off since 11-11. The desktop was un-retired on 11-13, so the key may no longer list it | **Nothing keeps it away** once the desktop is not retired: the same `host-roles` check |
| The nightly suite | `suite_host` in a rig's file (`rigs/*.toml`) names the host that runs a rig's suite at any hour; none sets it (`grep suite_host rigs/*.toml` prints nothing), so the home's follower runs it (`nightly_suite = true`, lampas) | Nothing needed today; keep it so: never write `suite_host = "desktop"` |
| A deploy | A host with no `after_landing.<rig>` line says "handed to the home (laptop)" and the home deploys (`application/afterlanding.go`, `noCommandLine`). The desktop's own `after_landing` is millwright's build | Read `mw config get after_landing.<rig>` on the desktop for each deploying rig (postern, trade-tracker, lampas) and expect it unset; **nothing keeps it away** but the absent line, and nothing checks it: a `host-roles` line for `after_landing` of a non-home rig |
| A story that needs the home: the age key, his sheets (gdrive, YNAB, `mw budget`) | `mw path set` and a release print `WARN: ... Pin it: mw path set <id> host=<home host>` for a criterion naming them on a host that is not a home host (`application/pathhomesecret.go`); the pin, once set, is the `other-host` gate | A warning only, and `domain.HomeHosts` counts the desktop as a home host, so a story pinned `host=desktop` raises none. **nothing keeps it away** when it is unpinned: make the warning a refusal at release for a `host=auto` story |
| A story that needs the served beads (127.0.0.1) | the pin by `host=laptop` with its `host_why` | **nothing keeps it away** when `host=auto`; the same refusal at release, with the words of `homeSecretWords` widened to `127.0.0.1:3307` and `the live vault` |
| The Septuagint real-data stories (the landing study's A4) | Only `[data_parts.lampas]` in the Boost template (`hosts/boost-config.toml`, 2026-11-21), which gives a story that changes only data files its own check; it does not keep a code story away. A4 is a proposal of the landing study; whether it was placed is not recorded here | **Nothing keeps it away** (`skip_rigs = ["lampas"]` in the desktop's `[dispatch]` would keep all of lampas away, which the role does not want). The smallest change is A4, a pathing by the Mayor: a story whose files key names `scripts/lxx/`, `data/lxx/` or `tests/unit/lxx-*` gets `mw path set <id> host=laptop host_why="its real-data tests read the Laptop's Morpheus cache"` |
| To be the fallback home | `mw home move` is the Governor's tap; the plan parks it until seven steady days (mw-43v9x, mw-6ww.149) | Nothing needed |
| Timing-sensitive stories while its clock steps | Nothing (D12, `mw doctor clock-steps`, is not landed) | Pin each such story `host=laptop`, or wait for D12 |

## 6. The lines (the Mayor runs them; nothing was run)

Run on the desktop unless a line says the home. Each `mw config set` copies the file to `config.toml.bak-<UTC time>` first and prints the way back as one command (`cp <backup> <config>`); the lines name theirs too. Asked of the Governor first: the first line reverses his "Keep cap 4 across the board" of 2026-11-14.

**1. Cap by day** (`~/.config/mw/config.toml`, root key; takes the next dispatch tick, a story running is not stopped):

```sh
mw config set cap 2
```

Way back: `mw config set cap 4`, or the `cp <backup> ~/.config/mw/config.toml` the command printed.

**2. Cap by night** (`mw config set` cannot write a `[[dispatch.windows]]` table; paste it, backup first; the first command must print nothing):

```sh
grep -n 'dispatch.windows' ~/.config/mw/config.toml
B=~/.config/mw/config.toml.bak-d11-$(date -u +%Y-%m-%dT%H%M%SZ); cp ~/.config/mw/config.toml "$B"; echo "way back: cp $B ~/.config/mw/config.toml"
cat >> ~/.config/mw/config.toml <<'EOF'

[[dispatch.windows]]
name = "night"
from = "23:00"
to   = "06:00"
tz   = "America/New_York"
cap  = 3
EOF
mw doctor dispatch-windows
```

Right looks like: the doctor check says the window is well formed, and `mw status` says `cap 3: night window to 06:00 America/New_York` after 23:00 New York and `cap 2` at noon. Way back: the `cp` line it printed; or delete the block.

**3. builder_jobs**, unchanged, and read to be sure (the window above leaves it out, so the root value holds at night):

```sh
mw config get builder_jobs
```

It should print `4`. If it does not: `mw config set builder_jobs 4`; way back, the old value it prints, set the same way (`mw config set builder_jobs <old>`, or the key's absence by `cp` of the backup).

**4. The home's count of the desktop's sessions** (on the home, the laptop; the vault's copy `hosts/vultr/cloud-table.toml:15` says 6; read the live one first):

```sh
mw config get cloud.caps.desktop
mw config set cloud.caps.desktop 2
```

Way back: `mw config set cloud.caps.desktop <the value the get printed>` (the vault's copy says 6). Side effect: the home counts fewer free sessions, so it makes a box sooner when stories wait; the month's `monthly_cap_usd` still bounds it (it stood at $62.57 of $75 on 11-19, handoff 970). The Mayor's call whether to place it.

**5. The one `.wslconfig` change: `processors=8`.** It is the paste of `docs/research/desktop-shared-pc.md` section 1, for a **new Windows PowerShell in the account WSL runs under** (named in `docs/research/windows-hands.md`), to be run when no story is running there (`mw status`) and the Mayor knows: it ends with `wsl --shutdown`, which takes the desktop off the network for about a minute. On a file that already has a `memory=` line (it has: 12GB) the paste adds `processors=8` under `[wsl2]` and nothing else, and keeps every other line (`swap=8GB`, `vmIdleTimeout=-1`, `autoMemoryReclaim=gradual`). **First look** at the file: `Get-Content $env:USERPROFILE\.wslconfig` must show `memory=12GB` and `swap=8GB`; if the file is missing (a `.wslconfig.off` left from 11-06: `Get-ChildItem $env:USERPROFILE -Filter '.wslconfig*'`), do not run the paste: it would write a new file with `memory=10GB`; rename the `.off` file back first. Right looks like: the paste prints the file with `processors=8` added and its last line prints `8`; then `nproc` on the desktop says 8 and the next gate record has `cores` 8. Way back: the paste prints `WAY BACK: Copy-Item '<backup>' '...\.wslconfig' -Force; wsl --shutdown`; run that line in a Windows PowerShell, then start WSL again with the second block of the same document's "Way back".
