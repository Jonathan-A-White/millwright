# Where the time goes between a Builder finishing and the landing

Story mw-gq6.1433, written 2026-11-17 on the desktop. A study: it changes no code. It
measures the seven days from 2026-11-10 23:00Z to 2026-11-17 23:00Z.

It answers the Governor's words in Postern general on 2026-11-17: at 14:32Z "some stories
have been in working for a long time do you know what's going on and is there anything
underlying I guess in the factory that could help", and at 14:37Z "I wonder if we could make
it so that one of the cloud machines could run these tests ... do they take up a slot if
they haven't fully landed yet".

Hours here are story-hours unless a line says otherwise: one story waiting one hour.
A "pipeline hour" is an hour of a rig's one landing line (`mw land <rig>`), which every
queued story of that rig waits behind.

## Short answers

1. **Most of what Postern called Working was finished and waiting to land.** At 14:32Z, 25
   stories were in progress; 18 of them had no session and were queued for lampas's batch
   landing, the oldest since 05:17Z (table T5). Postern's Working filter is the bead's
   status alone, so it cannot tell the two apart (section 6).
2. **A queued story takes no slot.** The code leaves it out of the cap in `mw status`, in
   `mw dispatch` and in the cloud count, and three tests hold that (section 2). The
   Laptop's "5 of cap 5" was five live Builder sessions, four of them lampas. What held the
   slots is how long a lampas session lasts: 1.8 hours at the median, of which the model
   works under 4 minutes; the rest is the rig's tests and the wait for a turn to run them
   (tables T2 and T6).
3. **A cloud host cannot take lampas's landing gate today, and nothing in `mw land` itself
   forbids it.** Only the home's follower begins a landing, and a busy home waits; it never
   looks for another host. Cloud hosts already run the same 11 parts for every lampas story
   they build, 40 of them in the week. The fix is drafted as a story of size L (section 3).
4. **Lampas is where the time is.** 645 stories landed in the week. Between the Builder
   finishing and the landing they waited 1,152 hours in all, 969 of them in lampas: 5.8
   hours a story at the median, 12.7 at the 90th percentile. Every other rig is at 1.1 hours or
   less at the median (table T1).
5. **Of lampas's 969 hours, 60% is the wait for its batch's gate to begin, 30% is waits
   inside the gate and 10% is tests.** The landing line was busy 16.7 hours of every 24,
   landed 2 stories a batch at the median, held 8 batches red (18.2 pipeline hours) and
   gave 21 batches to a fixer (13.3 hours) (tables T3 and T7).
6. **The Laptop is the one loaded host.** Close-outs of the rigs that land each story
   waited 61.1 hours beyond their own tests on the Laptop, and 6.8 hours or less on every
   other host (table T8). The Laptop is the home, runs five Builders, and gates every
   lampas batch.
7. **Three fixes for exactly this landed on 2026-11-17** (mw-gq6.1420, mw-gq6.1422,
   mw-gq6.1450). The week measured here is the week before them. Section 8 ranks what is
   left; the first thing to do is to run Appendix A again on 2026-11-21 and read table T7.

## 1. What exists already

Linked, not redone:

- **mw-2zh33g** (open): the factory health report epic, `mw report`. This study moves none
  of its metrics; the fixes in section 8 would.
- **mw-skwocd** (open): Land Rush, batch landing, trialled on lampas. Its design is
  plans/0117 in the vault and `docs/land-rush.md`.
- **mw-gq6.1422** (landed 2026-11-17): the wait for a calm host no longer counts the load
  of the factory's own `--no-slot` part workers (`application/hostload.go:54`).
- **mw-gq6.1423** (open): the changed specs' second run on a merged result waits for a calm
  host while holding the gate lock.
- **mw-gq6.1425** (open): `mw stuck` calls a story that is done and queued for a batch
  landing "claimed, no session for over 6 hours" (`application/stuck.go:202`). Section 6
  confirms it from the code.
- **mw-gq6.1420** (landed 2026-11-17): the gate lock is given back for every wait for a
  calm host, and a gate's turns are bounded at 30 minutes (`application/gateturns.go:163`).
- **mw-gq6.1450** (landed 2026-11-17 22:20Z): a batch no longer gives the gate lock
  back after each part; it goes ahead of single stories' checks and close-outs
  (`application/gateturns.go:25-31`, `application/batchland.go:272`).
- Open gate-lock and landing friction besides: **mw-gq6.1424** (a close-out holds its rig's
  merge slot across the merged result's wait for a calm host), **mw-gq6.1438** (`waitForRoom`
  still counts the part workers), **mw-gq6.1457** (a batch gives the lock back when its
  merge-slot wait ends), **mw-gq6.1458** (a give-way's frozen time is spent from a check's
  wait for the gate lock), **mw-gq6.1263**, **mw-gq6.1079**, **mw-gq6.1080** (deferred),
  and the epic **mw-t0z3fu** (caps that adjust themselves to load and memory).
- `docs/research/stranded-stories.md` (2026-10-03, mw-6ww.79): stories on a host that has
  gone away. Its option B is still not built; section 6 measures what that cost this week.

Source: `bd -C ~/millwright-vault list --all --flat --brief --json --id mw-2zh33g,mw-skwocd,mw-t0z3fu,mw-gq6.1424,mw-gq6.1438,mw-gq6.1457,mw-gq6.1458,mw-gq6.1263`,
and the titles in the file Appendix A reads, on 2026-11-17 23:15Z.

## 2. Does a story queued for batch landing count against its host's cap?

**No.** Read from the code at 010b6096:

- `mw status`: `application/status.go:989-993` adds to `InUse` only `if !rs.Refused() &&
  !rs.Queued()`, with the comment "A refused story waits on the Mayor and a queued one for
  its batch: neither holds a session, so the cap does not count it, as in mw dispatch".
  `Queued()` is `application/status.go:492`; the line itself, "host: %d of cap %d", is
  `application/status_render.go:185`.
- `mw dispatch`: `application/dispatch.go:1013-1017` passes over a claimed story that is
  `QueuedForBatch` before it is counted as running ("It holds no session, so the cap does
  not count it"), and `application/dispatch.go:1220` never claims one again. The cap gate
  is `application/dispatch.go:1448`.
- A claim that is open, not in progress, counts only while `LiveClaim` holds, and that
  ends with `!QueuedForBatch(detail)` (`application/worktracker.go:395-397`).
- Cloud hosts: `application/cloud.go:1640-1648` counts a host's sessions without the
  queued ones, so a cloud host whose only claims are queued goes idle and is given up.
- What "queued" is: `application/nextqueue.go:16-18`, the label `run:queued`. `mw next`
  sets it in `queue` (`application/nextqueue.go:138`): the branch is pushed to the origin
  as `mw/<id>` (line 155), the label is written (lines 171-172) and the time is kept as
  `queued_at` (line 176). The story stays in progress and claimed, its worktree kept
  (comment at lines 130-137).
- Tests that hold it: `application/queued_test.go:69`
  (TestDispatchNeitherCountsNorGivesBackAQueuedStory), `:107`, and `:156`
  (TestStatusShowsAQueuedStoryAsQueuedAndNotAsASession), and
  `application/cloud_test.go:338`.
- The cap itself is each host's own `cap`, `~/.config/mw/config.toml:4` on both: 5 on the
  Laptop, 4 on the desktop on 2026-11-17.

So no fix is owed for the question as asked. What the Laptop showed is in the table: at
14:32Z it had nine stories queued and five sessions, and the five sessions are the "5 of
cap 5".

| story | rig | host | claimed | queued | landed | at 14:32Z |
|-|-|-|-|-|-|-|
| mw-5r3p30.1166 | lampas | laptop | 17 01:38 | 17 08:00 | 17 20:42 | queued for landing |
| mw-qtu3zvz.15 | lampas | laptop | 17 02:43 | 17 05:23 | 17 20:42 | queued for landing |
| mw-rl10piw.24 | lampas | cloud2 | 17 03:02 | 17 04:27 | 17 17:11 | queued for landing |
| mw-fnvjof.17.149 | lampas | laptop | 17 04:01 | 17 07:29 | 17 20:42 | queued for landing |
| mw-zbgo4ep.49 | lampas | cloud2 | 17 04:29 | 17 05:17 | 17 20:41 | queued for landing |
| mw-indx1x6.77 | lampas | laptop | 17 06:06 | 17 08:00 | 17 20:42 | queued for landing |
| mw-indx1x6.79 | lampas | laptop | 17 06:21 | 17 08:01 | still open | queued for landing |
| mw-indx1x6.80 | lampas | cloud2 | 17 06:22 | 17 07:01 | 17 20:42 | queued for landing |
| mw-fnvjof.17.151 | lampas | laptop | 17 07:30 | 17 09:03 | still open | queued for landing |
| mw-indx1x6.81 | lampas | cloud2 | 17 07:32 | 17 08:05 | still open | queued for landing |
| mw-u2w5klk.349.5 | lampas | laptop | 17 08:02 | 17 11:46 | still open | queued for landing |
| mw-5r3p30.1172 | lampas | cloud2 | 17 08:37 | 17 09:43 | still open | queued for landing |
| mw-indx1x6.60 | lampas | laptop | 17 09:06 | 17 13:03 | still open | queued for landing |
| mw-5r3p30.1174 | lampas | cloud2 | 17 09:45 | 17 10:39 | still open | queued for landing |
| mw-5r3p30.1175 | lampas | cloud2 | 17 10:51 | 17 11:24 | still open | queued for landing |
| mw-5r3p30.1176 | lampas | cloud2 | 17 11:26 | 17 12:20 | still open | queued for landing |
| mw-ar5k42b.22 | lampas | laptop | 17 11:49 | 17 13:09 | still open | queued for landing |
| mw-qtu3zvz.190 | lampas | cloud1 | 17 12:27 | 17 13:59 | still open | queued for landing |
| mw-qtu3zvz.194 | lampas | laptop | 17 13:03 | 17 15:20 | still open | Builder session or close-out |
| mw-gq6.1422 | millwright | laptop | 17 13:10 | - | 17 15:16 | Builder session or close-out |
| mw-qtu3zvz.196 | lampas | laptop | 17 13:18 | 17 16:59 | still open | Builder session or close-out |
| mw-indx1x6.83 | lampas | laptop | 17 13:26 | 17 15:39 | still open | Builder session or close-out |
| mw-fnvjof.17.153 | lampas | laptop | 17 13:33 | 17 15:38 | still open | Builder session or close-out |
| mw-gq6.1394 | millwright | cloud2 | 17 14:27 | - | 17 14:44 | Builder session or close-out |
| mw-ar5k42b.26 | lampas | cloud1 | 17 14:28 | 17 20:12 | still open | Builder session or close-out |

By host: the Laptop 5 sessions and 9 queued; cloud2 1 session and 8 queued; cloud1 1 and 1.
In all 7 sessions and 18 queued. Times are the day of November 2026 and the UTC time.

Source: Appendix A with `--at 2026-11-17T14:32:00Z` (table T5): each bead's `claimed_at`,
`queued_at` and `closed_at`. His screenshot showed 24 rows; this read gives 25, two of
them claimed in the five minutes before.

What does hold a slot for a long time is a session waiting to test. The four lampas
sessions on the Laptop in that table ran 2.1 to 3.7 hours from claim to queued. A lampas
Builder's own `mw check` runs the rig's whole gate, 11 parts, on the host it works on,
and waits there for the gate lock, a test-runner slot and a calm host; the session lives,
and holds its slot, the whole time. Story draft 5 in section 8 makes `mw status` say so.

| rig | sessions | session wall median min | model (API) median min | tool time median min | tool share of wall |
|-|-|-|-|-|-|
| millwright | 306 | 26.8 | 2.1 | 24.6 | 92% |
| lampas | 162 | 101.0 | 3.8 | 97.5 | 96% |
| sermon-index | 42 | 25.4 | 3.5 | 21.6 | 87% |
| trade-tracker | 35 | 19.8 | 2.7 | 15.3 | 86% |
| postern | 32 | 41.6 | 1.9 | 39.3 | 95% |
| voice-kit | 19 | 22.0 | 2.3 | 18.8 | 87% |
| legend | 12 | 27.1 | 2.2 | 23.0 | 92% |
| spell-forge | 9 | 21.4 | 1.3 | 19.5 | 93% |
| whisper-hid | 6 | 20.0 | 1.9 | 18.1 | 93% |
| nac-service-media | 4 | 29.7 | 2.8 | 27.1 | 86% |
| cairn | 3 | 18.2 | 0.9 | 17.4 | 94% |
| pwa-kit | 1 | 6.8 | 0.9 | 5.9 | 87% |
| night-stack | 1 | 24.7 | 1.0 | 23.7 | 96% |

Source: Appendix A, table T2: `duration_ms` and `duration_api_ms` of each landed story's
`runs/<story>/result.json` in the vault, as the harness reported them. "Tool time" is
the difference: the rig's tests, builds, and the waits inside `mw check`.

| rig | Builder host | stories | claim to queued median h | p90 h | session wall median h | model (API) median min |
|-|-|-|-|-|-|-|
| lampas | auto | 1 | 6.9 | 6.9 | 5.6 | 4.6 |
| lampas | cloud1 | 3 | 2.7 | 2.7 | 2.0 | 3.9 |
| lampas | cloud2 | 15 | 1.1 | 2.5 | 1.0 | 6.3 |
| lampas | cloud3 | 16 | 1.8 | 4.3 | 1.8 | 3.8 |
| lampas | cloud4 | 5 | 1.4 | 1.6 | 1.4 | 4.6 |
| lampas | cloud8 | 1 | 1.8 | 1.8 | 1.7 | 4.0 |
| lampas | desktop | 10 | 1.4 | 5.6 | 1.5 | 4.1 |
| lampas | laptop | 117 | 1.9 | 4.6 | 1.8 | 3.5 |
| spell-forge | cloud2 | 1 | 0.1 | 0.1 | 0.1 | 1.2 |
| spell-forge | cloud3 | 3 | 0.4 | 0.7 | 0.4 | 1.2 |
| spell-forge | cloud9 | 1 | 0.5 | 0.5 | 0.4 | 2.0 |
| spell-forge | desktop | 1 | 0.3 | 0.3 | 0.3 | 1.1 |
| spell-forge | laptop | 3 | 0.5 | 0.6 | 0.5 | 1.9 |

Source: Appendix A, table T6: `claimed_at` and `queued_at` of each bead, and the same
result files. The host is the story's path host.

## 3. Can a rig's landing gate run on a cloud host or the desktop when the home is over its load?

**Not today, by itself. By hand it could, and it would race the home.** What stops it:

1. **Only the home's follower begins a landing.** `BatchLandings.due` returns nothing on
   a host that is not the home: `application/batchland.go:1613`, "if at, err :=
   b.Home(ctx); err != nil || !at". The same test guards `pending` (line 1813), the drain
   in `landRig` (line 1846) and `landAlone` (line 1870). It is wired in
   `cmd/mw/batchland.go:47-49` to `application.IsHome` (`application/home.go:75`), which
   reads the vault's `home` file: "laptop 2026-09-29T12:57:09Z mayor". A Boost has no
   follower at all (`contrib/boost-bootstrap.sh:44`, `:1061`).
2. **A busy home waits and then holds the batch; it never looks elsewhere.** The wait
   for a calm host reads this host's own load only. After `GateLoadBound`, 15 minutes
   (`application/gate.go:21-22`), a batch gets `HostStillBusyError`
   (`application/gate.go:228-231`: "host still at load %.1f of %d: the batch waits for
   the next landing", lines 103-105), and `landIn` holds it with every story still queued
   (`application/batchland.go:652-656`). "Busy" is the 1-minute load, less the part
   workers' share, at or over the core count, or twice the cores whatever the share
   (`application/hostload.go:54-56`, `:112-116`); there is no config key for it.
3. **The locks and the history are each host's own.** The gate lock is a file under
   `~/.local/state/mw` (`infrastructure/hostlock/gate.go:18`, taken by `mw land`
   at `cmd/mw/land.go:161`, by `mw next` at `cmd/mw/next.go:303` and by `mw check` at
   `cmd/mw/check.go:157`). The merge slot is "this host's slot, not the factory's"
   (`infrastructure/rig/slot.go:59-61`). The landings in flight are in
   `~/.local/state/mw/land` on the host that began them
   (`infrastructure/apartproc/apartproc.go:75`). And the batch history the follower
   reads to decide the next landing, `metrics/factory.jsonl`, is on the host that landed
   and is not committed to the vault (`application/batchmetrics.go:9-13`, and the rig's
   fact of mw-gq6.1246). So a landing made on another host today leaves the home's
   follower blind to it.

What does not stop it:

- `mw land` has no test for the home. `BatchLand.Run` asks only for a host name and for
  the rig to be checked out here (`application/batchland.go:280-288`).
- The queued branches are at the origin, not on the Builder's disk
  (`application/nextqueue.go:155`; `application/batchland.go:140-143`: "a story queued on
  the other host has no branch here of its own").
- A landing closes stories another host claimed: "this host takes the claim over and
  closes it under its own" (`application/next.go:2853-2857`).
- The after-landing command of a rig the landing host names none for is "handed to the
  home" (`application/afterlanding.go:629`), so the deploy stays on the Laptop.
- A cloud host has what lampas's gate needs. The Boost's config holds lampas's 11 parts
  (`hosts/boost-config.toml:38` in the vault), and in the week cloud hosts built 40
  lampas stories, each tested there by `mw check` with those parts before it queued
  (table T6). On cloud2 a whole lampas session, tests included, took 1.0 hour at the
  median.
- Tests are already run on another host over ssh in one place: `hostGap`
  (`application/mainred.go:628`) asks the home to run named tests through
  `Remote.RunNamedOn` (`infrastructure/peekremote/remote.go:68`, the ssh itself at line
  81). It goes toward the home only, runs a few tests by name, and takes no gate lock
  there. It is the piece to build on.

**The fix, Story draft 2 in section 8:** the home's follower, when the home is busy and a
host it is told of is calm, runs `mw land <rig>` there over ssh and waits for it. Its
cost: one story of size L, and the cloud host's time, $0.132 an hour
(`hosts/vultr/cloud-table.toml:9` in the vault). Lampas's gates spanned 12.5 hours a day
in the week (table T7), so a cloud host kept for landing alone would be about $1.65 a
day, and nothing more when the landing shares a host that is already up.

Running `mw land lampas` by hand on another host is not advised today: the home's
follower would land the same queue beside it, the origin would refuse the slower push,
and the home would hold no line of what the other host landed.

## 4. The measurements

### T1. Per rig: from the Builder finishing to the landing

"Builder done" is the story's last claim plus its session's wall time. "Queue wait" is
from `queued_at` to the first test of the batch that landed it. "Test parts" is the gate's
own run: the vitest runs of that batch, or for a rig that lands each story the
`gate_seconds` of its close-out. "Gate-lock and other waits" is the rest: for a batch,
everything in its gate's span that is not a vitest run, which holds the waits for the
gate lock, for a test-runner slot and for a calm host, and also the typecheck, lint,
build and changed specs, which the gate log does not time; for a single story, the
close-out less its gate. "Not split" are stories whose result names no gate time.
A retry is a second attempt, or for a batch rig a second batch that tried the story; a
red gate is a held batch, or a merged result that sent the story back.

| rig | landed | done to landed median h | p90 h | sum h | queue wait sum h | gate-lock and other waits sum h | test parts sum h | not split sum h | landed after a retry | were in a red gate |
|-|-|-|-|-|-|-|-|-|-|-|
| millwright | 312 | 0.3 | 0.8 | 131.5 | 0.0 | 61.2 | 46.7 | 23.6 | 24 | 7 |
| lampas | 168 | 5.8 | 12.7 | 968.7 | 580.2 | 289.1 | 99.4 | 0.0 | 25 | 25 |
| sermon-index | 42 | 0.1 | 0.4 | 5.1 | 0.0 | 3.7 | 1.0 | 0.4 | 4 | 0 |
| trade-tracker | 35 | 0.1 | 0.5 | 6.7 | 0.0 | 5.3 | 1.4 | 0.0 | 1 | 0 |
| postern | 32 | 0.7 | 1.1 | 22.2 | 0.0 | 10.5 | 11.3 | 0.3 | 4 | 0 |
| voice-kit | 19 | 0.1 | 0.4 | 2.8 | 0.0 | 1.8 | 0.5 | 0.4 | 3 | 0 |
| legend | 13 | 0.2 | 0.4 | 2.7 | 0.0 | 2.1 | 0.7 | 0.0 | 1 | 0 |
| spell-forge | 9 | 1.1 | 1.8 | 10.0 | 9.1 | 0.6 | 0.4 | 0.0 | 0 | 0 |
| whisper-hid | 6 | 0.1 | 0.2 | 0.5 | 0.0 | 0.4 | 0.1 | 0.0 | 1 | 0 |
| nac-service-media | 4 | 0.2 | 0.7 | 1.2 | 0.0 | 1.0 | 0.2 | 0.0 | 0 | 0 |
| cairn | 3 | 0.1 | 0.2 | 0.3 | 0.0 | 0.2 | 0.0 | 0.0 | 0 | 0 |
| pwa-kit | 1 | 0.1 | 0.1 | 0.1 | 0.0 | 0.1 | 0.0 | 0.0 | 0 | 0 |
| night-stack | 1 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 | 0 | 0 |

All rigs: 645 landed, 1152 story-hours between Builder done and landed.

Source: Appendix A, table T1: `bd list` (the command at the top of the script),
`runs/<story>/result.json`, `runs/batch/<rig>/<n>/merge-order.txt`, `outcome.txt` and
`gate.log`, and `git log -- runs/batch`, all in the vault.

### T3 and T7. The batch landings

| rig | batches | landed | held | stories landed | stories per landed batch median | with a fixer | fixer h sum | landing to end of mw land (after-landing and smoke) median min | sum h | vitest runs span median min | of it between parts median min |
|-|-|-|-|-|-|-|-|-|-|-|-|
| lampas | 71 | 63 | 8 | 184 | 2.0 | 21 | 13.3 | 6.2 | 16.2 | 88.6 | 44.9 |
| spell-forge | 8 | 8 | 0 | 9 | 1.0 | 0 | 0.0 | 3.5 | 0.6 | - | - |

Source: Appendix A, table T3. A batch's end is the vault commit that records it, which
`mw land` makes when it ends; its landing is the latest `closed_at` of its stories; the
fixer's time is the "spent:" line of `fixer.txt`; the vitest span is from the first
"Start at" of `gate.log` to the end of the last, and "between parts" is that span less
the vitest "Duration" lines.

| rig | story-hours queued (queued to landed) | mean stories waiting | batches per day | batches of 1 story | of 2 to 5 | of 6 or more | queue at gate start median | pipeline busy h per day (fixer + gate + after-landing) | gate span h per day | of it not in a vitest run h per day | fixer h per day | after-landing h per day | held batches |
|-|-|-|-|-|-|-|-|-|-|-|-|-|-|
| lampas | 938 | 5.6 | 10.1 | 26 | 23 | 14 | 3.0 | 16.7 | 12.5 | 7.6 | 1.9 | 2.3 | 8 |
| spell-forge | 10 | 0.1 | 1.1 | 7 | 1 | 0 | 1.0 | 0.2 | 0.1 | 0.0 | 0.0 | 0.1 | 0 |

- Lampas: its held batches took 18.2 h of the pipeline (each from the batch before it ending to its own end).
- Lampas: 16 landings ended over 15 min after the landing itself, 12.3 h in all; 25 landed stories had been in a held batch and waited 138 story-hours between that batch ending and their landing.

Source: Appendix A, table T7 and the lines under it. "Mean stories waiting" is the
story-hours queued over the window's 168 hours. "Queue at gate start" counts the rig's
stories queued and not yet landed when the batch's first test began.

Reading T7 for lampas:

- The line was busy 16.7 hours of 24, so any batch that goes red, or any fixer, pushes
  the queue out by hours: there is little slack to catch up in.
- 7.6 of the 12.5 gate hours a day were not a vitest run. The one landing log that keeps
  the figure, batch 555's, says "it ran 1h41m38s, and waited 31m58s of it for other
  gates": 31% of that gate was turns given to other gates, the thing mw-gq6.1450 ended
  on 2026-11-17.
- 26 of 63 landed batches carried one story. Each cost a whole gate.
- Held batches took 18.2 pipeline hours, and the 25 stories that had been in one waited
  138 hours between that batch ending and their own landing. Where a fixer named the
  cause (batches 520, 524, 526, and 555 for the stories 552 had held) it was a fault
  between stories that each passed alone: a layer rule one story added and another broke,
  a count gone stale, a size limit two stories reached together. Batch 507 was a push to
  GitHub that timed out. None is recorded as the host's load.

Source: `runs/batch/lampas/<n>/outcome.txt` and `fixer.txt` for batches 507, 520, 524,
526, 533, 540, 542, 552 and 555 in the vault; the Laptop's `~/.local/state/mw/land/lampas/log.1`,
read over `ssh laptop` on 2026-11-17 23:08Z, for batch 555.

### T8. The rigs that land each story: what a close-out waits for

| host | landed with a gate time | close-out median min | gate run median min | wait beyond the gate median min | p90 min | sum h | close-outs waiting over 30 min |
|-|-|-|-|-|-|-|-|
| cloud1 | 19 | 11.7 | 8.3 | 1.8 | 10.0 | 1.0 | 0 |
| cloud2 | 63 | 12.4 | 8.9 | 2.2 | 12.3 | 4.6 | 0 |
| cloud3 | 55 | 13.7 | 8.4 | 3.5 | 14.7 | 6.2 | 1 |
| cloud4 | 12 | 12.8 | 10.0 | 3.1 | 6.4 | 0.7 | 0 |
| cloud6 | 6 | 3.8 | 3.0 | 0.8 | 2.7 | 0.1 | 0 |
| cloud8 | 19 | 22.1 | 13.0 | 5.2 | 28.1 | 3.1 | 1 |
| cloud9 | 18 | 21.6 | 12.9 | 6.2 | 23.6 | 2.7 | 1 |
| desktop | 49 | 16.6 | 11.6 | 5.4 | 24.0 | 6.8 | 2 |
| laptop | 190 | 18.0 | 5.7 | 11.5 | 48.9 | 61.1 | 37 |

Source: Appendix A, table T8: `closed_at` less Builder done, against `gate_seconds`, by
the `host` of each `result.json`. A story whose result names no gate time is left out
(the "not split" hours of table T1).

On the Laptop a close-out waited 11.5 minutes beyond its own tests at the median and 49
at the 90th percentile; 37 close-outs waited over half an hour. On no other host did
more than two. The Laptop's gate lock is shared by lampas's batch gate, every Builder's
whole check there, and every close-out (section 3, point 3).

### The day he asked: lampas on 2026-11-17

| batch | stories | what happened | first test | landed | `mw land` ended |
|-|-|-|-|-|-|
| 551 | 6 | landed | 05:06Z | 07:50Z | 07:58Z |
| 552 | 6 tried | 3 set aside for conflicts, a fixer for 1.5 h, then red: held | 11:52Z | never | 13:24Z |
| 553 | 1 | mw-rl10piw.9 alone, after the held batch | 13:31Z | about 14:30Z | 15:24Z |
| 554 | 1 | mw-rl10piw.24 alone, a fixer for 0.7 h | 16:23Z | 17:11Z | 17:15Z |
| 555 | 6 | 2 set aside for conflicts, a fixer for 1h37m, a gate of 1h41m38s | 19:05Z | 20:41Z | 21:33Z |
| 556 | (in flight) | 3 set aside for conflicts, a fixer; in its gate at 23:05Z | | | |

Source: Appendix A with `--batches` (the lines under T3b) for batches 551 to 555;
`runs/batch/lampas/<n>/fixer.txt` and `merge-order.txt`; the Laptop's
`~/.local/state/mw/land/lampas/log` and `log.1`; the Mayor's handoffs 945 and 946
(`seats/mayor/handoffs/2026-11-17-945.md`, `-946.md`) for when batch 553 landed.

From 07:58Z to 20:41Z, 12.7 hours, the line landed two stories. In that time 25 more
queued. Batch 552 took 5.4 hours and landed nothing. Batch 555's landing was followed by
52 minutes of grist smoke, inside the same `mw land`, before batch 556 could begin. That
is what his screenshot at 14:32Z was a picture of.

The Mayor's answer to him (946) holds up, with two corrections: the 11-part gate was not
the whole of the wait (a held batch and two fixers were most of it), and mw-gq6.1422's
check was one of several holders of the gate lock that day, not the cause.

## 5. Every story in progress for over three hours

One row for every story whose last claim lasted over three hours inside the window, to
its landing or to 2026-11-17 23:00Z, with its cause. "Red gate" is a story that was in a
held batch. A story is under "queued for landing" when the queue is half or more of its
time. The first three rows are the desktop's, from its own files (section 6): their bead
times were written again when they were taken again, so the script cannot see them.

| story | rig | host | hours in progress | of them queued | now | cause |
|-|-|-|-|-|-|-|
| mw-gq6.1341 | millwright | desktop | 32.3 | - | landed | dead host: the desktop was down from 11-16 07:20Z to 11-17 14:53Z |
| mw-gq6.1343 | millwright | desktop | 32.4 | - | landed | dead host: the same outage |
| mw-gq6.1350 | millwright | desktop | 32.8 | - | landed | dead host: the same outage |
| mw-82lb97i.27 | lampas | laptop | 19.9 | 15.7 | landed | red gate, then queued for landing |
| mw-indx1x6.8 | lampas | laptop | 19.9 | 15.3 | landed | red gate, then queued for landing |
| mw-5r3p30.1166 | lampas | laptop | 19.1 | 12.7 | landed | queued for landing |
| mw-5r3p30.1164 | lampas | laptop | 18.9 | 14.2 | landed | red gate, then queued for landing |
| mw-qtu3zvz.15 | lampas | laptop | 18.0 | 15.3 | landed | red gate, then queued for landing |
| mw-u2w5klk.115 | lampas | laptop | 17.9 | 14.0 | landed | red gate, then queued for landing |
| mw-fnvjof.17.149 | lampas | laptop | 16.7 | 13.2 | landed | red gate, then queued for landing |
| mw-indx1x6.79 | lampas | laptop | 16.6 | 15.0 | in progress | queued for landing |
| mw-qtu3zvz.149 | lampas | laptop | 16.4 | 13.5 | landed | queued for landing |
| mw-zbgo4ep.49 | lampas | cloud2 | 16.2 | 15.4 | landed | red gate, then queued for landing |
| mw-qtu3zvz.150 | lampas | laptop | 15.7 | 12.4 | landed | queued for landing |
| mw-u2w5klk.117 | lampas | laptop | 15.6 | 13.6 | landed | red gate, then queued for landing |
| mw-qtu3zvz.92 | lampas | laptop | 15.6 | 13.3 | landed | red gate, then queued for landing |
| mw-fnvjof.17.151 | lampas | laptop | 15.5 | 13.9 | in progress | queued for landing |
| mw-indx1x6.81 | lampas | cloud2 | 15.5 | 14.9 | in progress | queued for landing |
| mw-indx1x6.30 | lampas | cloud3 | 15.4 | 13.6 | landed | red gate, then queued for landing |
| mw-zbgo4ep.1 | lampas | laptop | 15.3 | 10.6 | landed | queued for landing |
| mw-u2w5klk.372 | lampas | laptop | 15.0 | 13.5 | landed | queued for landing |
| mw-u2w5klk.349.5 | lampas | laptop | 15.0 | 11.2 | in progress | queued for landing |
| mw-indx1x6.77 | lampas | laptop | 14.6 | 12.7 | landed | queued for landing |
| mw-6ww.175.12 | lampas | cloud8 | 14.5 | 12.7 | landed | red gate, then queued for landing |
| mw-5r3p30.1172 | lampas | cloud2 | 14.4 | 13.3 | in progress | queued for landing |
| mw-indx1x6.80 | lampas | cloud2 | 14.3 | 13.7 | landed | red gate, then queued for landing |
| mw-fnvjof.17.104 | lampas | laptop | 14.2 | 7.2 | landed | red gate, then queued for landing |
| mw-qtu3zvz.151 | lampas | laptop | 14.2 | 13.1 | landed | queued for landing |
| mw-rl10piw.24 | lampas | cloud2 | 14.2 | 12.7 | landed | red gate, then queued for landing |
| mw-indx1x6.29 | lampas | desktop | 14.1 | 8.5 | landed | queued for landing |
| mw-indx1x6.60 | lampas | laptop | 13.9 | 9.9 | in progress | queued for landing |
| mw-6ww.175.68 | lampas | laptop | 13.4 | 12.2 | landed | queued for landing |
| mw-indx1x6.21 | lampas | laptop | 13.4 | 11.2 | landed | red gate, then queued for landing |
| mw-indx1x6.22 | lampas | laptop | 13.3 | 8.6 | landed | red gate, then queued for landing |
| mw-5r3p30.1174 | lampas | cloud2 | 13.2 | 12.3 | in progress | queued for landing |
| mw-qtu3zvz.106 | lampas | laptop | 13.2 | 9.6 | landed | queued for landing |
| mw-qtu3zvz.153 | lampas | laptop | 13.0 | 11.2 | landed | queued for landing |
| mw-qtu3zvz.143 | lampas | auto | 13.0 | 6.1 | landed | queued for landing |
| mw-indx1x6.39 | lampas | laptop | 12.8 | 11.2 | landed | red gate, then queued for landing |
| mw-indx1x6.44 | lampas | desktop | 12.6 | 8.7 | landed | queued for landing |
| mw-5r3p30.1175 | lampas | cloud2 | 12.1 | 11.6 | in progress | queued for landing |
| mw-indx1x6.23 | lampas | cloud3 | 11.7 | 10.1 | landed | red gate, then queued for landing |
| mw-5r3p30.1176 | lampas | cloud2 | 11.6 | 10.7 | in progress | queued for landing |
| mw-fnvjof.17.114 | lampas | laptop | 11.5 | 9.9 | landed | queued for landing |
| mw-6ww.175.15 | lampas | laptop | 11.5 | 9.3 | landed | queued for landing |
| mw-zbgo4ep.3 | lampas | laptop | 11.4 | 5.6 | landed | queued for landing |
| mw-qtu3zvz.125 | lampas | laptop | 11.3 | 6.6 | landed | red gate, then queued for landing |
| mw-indx1x6.41 | lampas | laptop | 11.2 | 7.4 | landed | queued for landing |
| mw-ar5k42b.22 | lampas | laptop | 11.2 | 9.8 | in progress | queued for landing |
| mw-fnvjof.17.115 | lampas | laptop | 11.1 | 9.3 | landed | queued for landing |
| mw-qtu3zvz.175 | lampas | laptop | 11.1 | 6.4 | landed | queued for landing |
| mw-qtu3zvz.147 | lampas | laptop | 11.0 | 8.6 | landed | queued for landing |
| mw-fnvjof.17.116 | lampas | laptop | 10.9 | 8.0 | landed | queued for landing |
| mw-qtu3zvz.94 | lampas | cloud3 | 10.9 | 8.5 | landed | queued for landing |
| mw-qtu3zvz.190 | lampas | cloud1 | 10.5 | 9.0 | in progress | queued for landing |
| mw-qtu3zvz.161 | lampas | laptop | 10.5 | 8.9 | landed | queued for landing |
| mw-qtu3zvz.126 | lampas | desktop | 10.4 | 9.6 | landed | queued for landing |
| mw-ar5k42b.5 | lampas | laptop | 10.4 | 7.7 | landed | queued for landing |
| mw-indx1x6.78 | lampas | cloud1 | 10.3 | 7.6 | landed | queued for landing |
| mw-5r3p30.1165 | lampas | laptop | 10.3 | 5.2 | landed | queued for landing |
| mw-indx1x6.9 | lampas | cloud2 | 10.2 | 5.9 | landed | queued for landing |
| mw-5r3p30.1167 | lampas | desktop | 10.2 | 9.1 | landed | queued for landing |
| mw-fnvjof.17.142 | lampas | laptop | 10.0 | 6.2 | landed | queued for landing |
| mw-qtu3zvz.95 | lampas | laptop | 10.0 | 7.6 | landed | queued for landing |
| mw-qtu3zvz.194 | lampas | laptop | 9.9 | 7.7 | in progress | queued for landing |
| mw-fnvjof.17.118 | lampas | laptop | 9.9 | 8.5 | landed | queued for landing |
| mw-82lb97i.28 | lampas | laptop | 9.9 | 5.1 | landed | queued for landing |
| mw-qtu3zvz.169 | lampas | laptop | 9.8 | 8.6 | landed | queued for landing |
| mw-indx1x6.7 | lampas | desktop | 9.8 | 8.0 | landed | queued for landing |
| mw-fnvjof.17.121 | lampas | laptop | 9.7 | 6.3 | landed | queued for landing |
| mw-indx1x6.10 | lampas | desktop | 9.7 | 8.5 | landed | queued for landing |
| mw-qtu3zvz.196 | lampas | laptop | 9.7 | 6.0 | in progress | queued for landing |
| mw-6ww.175.18 | lampas | desktop | 9.7 | 9.0 | landed | queued for landing |
| mw-indx1x6.83 | lampas | laptop | 9.6 | 7.3 | in progress | queued for landing |
| mw-fnvjof.17.153 | lampas | laptop | 9.4 | 7.4 | in progress | queued for landing |
| mw-indx1x6.14 | lampas | laptop | 9.4 | 7.2 | landed | queued for landing |
| mw-qtu3zvz.167 | lampas | laptop | 9.4 | 7.9 | landed | queued for landing |
| mw-zbgo4ep.38 | lampas | laptop | 9.3 | 7.8 | landed | queued for landing |
| mw-fnvjof.17.113 | lampas | laptop | 9.2 | 6.7 | landed | queued for landing |
| mw-u2w5klk.116 | lampas | laptop | 9.2 | 6.9 | landed | queued for landing |
| mw-indx1x6.56 | lampas | laptop | 9.1 | 6.3 | landed | queued for landing |
| mw-82lb97i.40 | lampas | laptop | 9.0 | 6.8 | landed | queued for landing |
| mw-indx1x6.6 | lampas | cloud3 | 8.8 | 7.0 | landed | queued for landing |
| mw-lexuwrk | lampas | laptop | 8.8 | 6.2 | landed | queued for landing |
| mw-indx1x6.13 | lampas | laptop | 8.6 | 6.5 | landed | queued for landing |
| mw-5r3p30.1163 | lampas | laptop | 8.6 | 6.4 | landed | queued for landing |
| mw-82lb97i.19 | lampas | laptop | 8.6 | 4.9 | landed | queued for landing |
| mw-ar5k42b.26 | lampas | cloud1 | 8.5 | 2.8 | in progress | Builder running (its session ran 5.7 h) |
| mw-82lb97i.11 | lampas | cloud3 | 8.5 | 6.7 | landed | queued for landing |
| mw-qtu3zvz.184 | lampas | laptop | 8.4 | 6.5 | landed | queued for landing |
| mw-6ww.175.79 | lampas | laptop | 8.4 | 6.5 | landed | queued for landing |
| mw-qtu3zvz.174 | lampas | laptop | 8.2 | 6.8 | landed | queued for landing |
| mw-qtu3zvz.160 | lampas | laptop | 8.1 | 5.6 | landed | queued for landing |
| mw-indx1x6.25 | lampas | cloud2 | 8.1 | 6.0 | landed | queued for landing |
| mw-qtu3zvz.158 | lampas | laptop | 8.1 | 6.9 | landed | queued for landing |
| mw-zbgo4ep.4 | lampas | laptop | 8.1 | 3.5 | landed | queued for landing |
| mw-indx1x6.12 | lampas | desktop | 7.9 | 6.4 | landed | queued for landing |
| mw-zbgo4ep.33 | lampas | laptop | 7.8 | 3.2 | landed | queued for landing |
| mw-qtu3zvz.104 | lampas | laptop | 7.8 | 3.8 | landed | queued for landing |
| mw-qtu3zvz.108 | lampas | laptop | 7.8 | 2.7 | landed | queued for landing |
| mw-indx1x6.57 | lampas | laptop | 7.8 | 4.3 | landed | queued for landing |
| mw-82lb97i.44 | lampas | laptop | 7.7 | 5.6 | landed | queued for landing |
| mw-ar5k42b.20 | lampas | laptop | 7.7 | 5.8 | landed | queued for landing |
| mw-fnvjof.17.107 | lampas | laptop | 7.7 | 6.0 | landed | queued for landing |
| mw-njqj1b0 | lampas | laptop | 7.5 | 4.1 | landed | queued for landing |
| mw-ar5k42b.28 | lampas | laptop | 7.4 | 5.6 | landed | queued for landing |
| mw-5r3p30.1154 | lampas | laptop | 7.4 | 5.0 | landed | queued for landing |
| mw-qtu3zvz.103 | lampas | laptop | 7.2 | 6.5 | landed | queued for landing |
| mw-qtu3zvz.110 | lampas | cloud3 | 7.2 | 5.1 | landed | queued for landing |
| mw-qtu3zvz.181 | lampas | cloud2 | 7.0 | 6.4 | landed | queued for landing |
| mw-5r3p30.1177 | lampas | cloud2 | 7.0 | 6.4 | in progress | queued for landing |
| mw-qtu3zvz.186 | lampas | laptop | 6.9 | 5.2 | landed | queued for landing |
| mw-indx1x6.16 | lampas | laptop | 6.9 | 5.6 | landed | queued for landing |
| mw-6ww.175.52 | lampas | desktop | 6.8 | 5.8 | landed | queued for landing |
| mw-zbgo4ep.2 | lampas | laptop | 6.7 | 2.3 | landed | queued for landing |
| mw-6ww.175.54 | lampas | laptop | 6.7 | 4.9 | landed | queued for landing |
| mw-rl10piw.11 | lampas | cloud2 | 6.6 | 5.8 | landed | queued for landing |
| mw-u2w5klk.349.4 | lampas | laptop | 6.6 | 4.0 | landed | queued for landing |
| mw-u2w5klk.349.3 | lampas | laptop | 6.5 | 4.5 | landed | queued for landing |
| mw-indx1x6.38 | lampas | laptop | 6.4 | 4.8 | landed | red gate, then queued for landing |
| mw-fnvjof.17.158 | lampas | laptop | 6.3 | 4.1 | in progress | queued for landing |
| mw-fnvjof.17.159 | lampas | laptop | 6.3 | 2.0 | in progress | Builder running (its session ran 4.3 h) |
| mw-ar5k42b.6 | lampas | cloud2 | 6.0 | 5.1 | landed | queued for landing |
| mw-qtu3zvz.73 | lampas | laptop | 6.0 | 1.7 | landed | Builder running (its session ran 4.3 h) |
| mw-5r3p30.1162 | lampas | laptop | 6.0 | 4.6 | landed | queued for landing |
| mw-u2w5klk.238 | lampas | cloud2 | 6.0 | 3.5 | landed | queued for landing |
| mw-indx1x6.74 | lampas | laptop | 6.0 | 4.5 | landed | queued for landing |
| mw-u2w5klk.345 | lampas | cloud3 | 5.9 | 1.6 | landed | Builder running (its session ran 4.3 h) |
| mw-u2w5klk.241 | lampas | cloud2 | 5.8 | 4.7 | landed | queued for landing |
| mw-fnvjof.17.101 | lampas | cloud3 | 5.8 | 1.4 | landed | queued for landing |
| mw-rl10piw.31 | lampas | desktop | 5.7 | 3.7 | in progress | queued for landing |
| mw-qtu3zvz.193 | lampas | cloud2 | 5.6 | 4.9 | in progress | queued for landing |
| mw-u2w5klk.246 | lampas | cloud3 | 5.4 | 4.6 | landed | queued for landing |
| mw-u2w5klk.243 | lampas | cloud3 | 5.3 | 4.3 | landed | queued for landing |
| mw-qtu3zvz.155 | lampas | laptop | 5.3 | 4.0 | landed | queued for landing |
| mw-indx1x6.34 | lampas | laptop | 5.3 | 3.5 | landed | red gate, then queued for landing |
| mw-indx1x6.72 | lampas | laptop | 5.1 | 2.8 | landed | queued for landing |
| mw-qtu3zvz.183 | lampas | cloud2 | 5.0 | 4.6 | landed | queued for landing |
| mw-indx1x6.28 | lampas | laptop | 5.0 | 3.8 | landed | queued for landing |
| mw-fnvjof.17.147 | lampas | laptop | 4.9 | 3.1 | landed | queued for landing |
| mw-qtu3zvz.144 | lampas | laptop | 4.9 | 2.2 | landed | queued for landing |
| mw-rl10piw.32 | lampas | laptop | 4.8 | 1.5 | in progress | queued for landing |
| mw-fnvjof.17.148 | lampas | laptop | 4.5 | 2.6 | landed | queued for landing |
| mw-zbgo4ep.37 | lampas | laptop | 4.5 | 2.8 | landed | red gate, then queued for landing |
| mw-qtu3zvz.206 | lampas | cloud2 | 4.3 | 3.4 | in progress | queued for landing |
| mw-ar5k42b.34 | lampas | laptop | 4.3 | 2.7 | in progress | queued for landing |
| mw-rl10piw.18 | lampas | laptop | 4.2 | 2.6 | landed | queued for landing |
| mw-5r3p30.1153 | lampas | laptop | 4.1 | 1.9 | landed | queued for landing |
| mw-u2w5klk.250 | lampas | cloud3 | 4.0 | 3.2 | landed | queued for landing |
| mw-ar5k42b.35 | lampas | laptop | 3.9 | 2.0 | in progress | queued for landing |
| mw-82lb97i.10 | lampas | laptop | 3.9 | 1.2 | landed | Builder running (its session ran 2.6 h) |
| mw-u2w5klk.248 | lampas | laptop | 3.8 | 1.4 | landed | queued for landing |
| mw-indx1x6.18 | lampas | laptop | 3.7 | 3.0 | landed | queued for landing |
| mw-fnvjof.17.69 | lampas | cloud1 | 3.6 | 0.9 | landed | Builder running (its session ran 2.0 h) |
| mw-fnvjof.17.70 | lampas | cloud4 | 3.6 | 2.2 | landed | queued for landing |
| mw-qtu3zvz.59.2 | lampas | desktop | 3.5 | 0.8 | landed | Builder running (its session ran 2.8 h) |
| mw-82lb97i.18 | lampas | laptop | 3.4 | 1.7 | landed | Builder running (its session ran 3.1 h) |
| mw-fnvjof.17.176 | lampas | cloud2 | 3.4 | 2.8 | in progress | queued for landing |
| mw-qtu3zvz.124 | lampas | cloud2 | 3.4 | 2.3 | landed | red gate, then queued for landing |
| mw-indx1x6.11 | lampas | cloud2 | 3.2 | 2.0 | landed | queued for landing |
| mw-5r3p30.1156 | lampas | laptop | 3.2 | 1.0 | landed | Builder running (its session ran 1.6 h) |
| mw-qtu3zvz.72 | lampas | laptop | 3.1 | 1.5 | landed | queued for landing |
| mw-qtu3zvz.65 | lampas | laptop | 3.0 | 1.0 | landed | Builder running (its session ran 1.5 h) |
| mw-rz2dak.8 | millwright | laptop | 776.2 | - | closed | other: closed by hand on a demo or verify word, not by a landing |
| mw-gq6.1213 | millwright | laptop | 8.8 | - | landed | other: close-out waited 6.5 h for the gate (lock, calm host, red merged result) |
| mw-gq6.1189 | millwright | laptop | 5.9 | - | landed | Builder running (its session ran 4.0 h) |
| mw-t5shinh.38 | millwright | desktop | 5.9 | - | landed | Builder running (its session ran 6.3 h) |
| mw-6ww.175.4 | millwright | laptop | 4.8 | - | landed | Builder running (its session ran 3.5 h) |
| mw-gq6.1301 | millwright | laptop | 4.5 | - | landed | other: close-out waited 3.1 h for the gate (lock, calm host, red merged result) |
| mw-gq6.1166 | millwright | laptop | 4.0 | - | landed | other: close-out waited 2.2 h for the gate (lock, calm host, red merged result) |
| mw-gq6.1221 | millwright | laptop | 3.9 | - | landed | other: close-out waited 2.5 h for the gate (lock, calm host, red merged result) |
| mw-t5shinh.33 | millwright | laptop | 3.8 | - | landed | Builder running (its session ran 2.2 h) |
| mw-gq6.1226 | millwright | laptop | 3.8 | - | landed | other: close-out waited 2.1 h for the gate (lock, calm host, red merged result) |
| mw-gq6.1339 | millwright | laptop | 3.8 | - | landed | Builder running (its session ran 3.1 h) |
| mw-gq6.1316 | millwright | laptop | 3.5 | - | landed | Builder running (its session ran 2.0 h) |
| mw-gq6.1286 | millwright | laptop | 3.4 | - | landed | other: close-out waited 1.8 h for the gate (lock, calm host, red merged result) |
| mw-gq6.1172 | millwright | laptop | 3.3 | - | landed | Builder running (its session ran 2.5 h) |
| mw-6ww.175.10 | millwright | laptop | 3.0 | - | landed | other: close-out waited 2.3 h for the gate (lock, calm host, red merged result) |
| mw-ht6gsw.6 | postern | auto | 619.8 | - | closed | other: closed by hand on a demo or verify word, not by a landing |
| mw-rdpwdp1.12 | voice-kit | laptop | 4.5 | - | landed | Builder running (its session ran 4.2 h) |

Source: Appendix A, table T4, for all but the first three rows; for those, the times of
`runs/mw-gq6.1341/heartbeat-2.log`, `runs/mw-gq6.1343/heartbeat-1.log` and
`runs/mw-gq6.1350/heartbeat-1.log` against each story's `boot.md` or `boot-2.md` on the
desktop (`TZ=UTC ls -la --time-style='+%m-%dT%H:%MZ' ~/millwright-vault/runs/mw-gq6.1341`).

180 rows. By cause: queued for landing 127 stories (1129 hours in progress between them); red gate, then
queued for landing 23 (313 hours); the Builder running, its session over half the time, 18 (83 hours);
dead host 3 (97 hours); other 9: 7 close-outs that waited for the gate on the Laptop, and 2 stories
landed weeks before and closed by hand in the window on a demo or verify word, whose long hours are
that wait and not a landing's.

## 6. Stories left working on a dead host, and Postern's Working filter

### A dead host

The desktop stopped at about 07:20Z on 2026-11-16 (its root went read-only when D:
filled, then WSL stopped) and was back at 14:53Z on 2026-11-17. Three millwright stories
had sessions on it: mw-gq6.1341, mw-gq6.1343 and mw-gq6.1350. Each sat in progress,
claimed by mw@desktop, with no session, for 32 hours, 97 hours between them, and each
landed within two hours of the desktop's return. Their attempts had run 7 to 33 minutes
when the host died, so little work was at stake.

Source: the Mayor's handoff 924 (`seats/mayor/handoffs/2026-11-16-924.md`: "Desktop DOWN
since ~07:20Z"); the file times named under section 5; each bead's `closed_at`.

Nothing in the factory moves such a story, by design so far:

- The lease is bd's five minutes (`application/nudge.go:70`), renewed every two by `mw
  next --heartbeat` (`application/next.go:211`). When the host dies the lease lapses and
  the story stays in progress.
- The host's own sweep and dispatch are what give a dead session's claim back, and a dead
  host runs neither.
- Another host takes back only a claim whose path names no host or `auto`
  (`application/nosessionclaim.go:33-44`); these three were pathed to the desktop.
- Re-pathing moves open stories only: "never ... a claimed one"
  (`application/repath.go:124-129`).
- `bd reclaim` is never given `--any-replica`: "who may reclaim the other host's claims
  is not settled" (`infrastructure/beads/beads.go:1086-1088`).
- `mw status` names the silence after two hours (`application/status.go:28`) and prints
  the hint (`application/status.go:94`); a person then runs `mw retry <id> --force` and
  sets the host (`application/retry.go:1088`, `:1171`).

`docs/research/stranded-stories.md` set this out on 2026-10-03 and asked the Governor six
questions; its option B, the home handing back after a timeout, was not built. Story
draft 4 in section 8 is that option, off by default.

On 2026-11-17 at 23:10Z no story was stranded: of 40 in progress, 29 were queued for
landing (their leases lapsed, as they should) and 11 had a lease under ten minutes old.

Source: `bd -C ~/millwright-vault list --flat --brief --json --limit 0 --status in_progress`
at 23:10Z, the `lease_expires_at` and labels of each row.

### Postern's Working filter

Postern's Map puts a bead in Working when `bead.status === 'in_progress'` and nothing
else (`src/model/tree.ts:103` in the postern rig, at 8edbc7bd). The age on the row is
`bead.updated || bead.closed` (`src/cockpit/BeadCards.tsx:138`). The view millwright
publishes carries the bead's status and its labels (`application/posternview.go:215`,
`:218`), so `run:queued` reaches Postern; nothing reads it.

A story queued for its batch stays in progress (section 2), so it is listed as Working
from the minute it queues until its batch lands, and the age on its row is how long it
has been queued. At 14:32Z that was 18 of 25 rows; at 23:10Z it would have been 29 of 40.

`mw stuck` has the same blind spot (mw-gq6.1425): it lists claims whose lease ran out
over six hours ago (`application/stuck.go:40`, `:200-202`) and never asks whether the
story is queued for its batch.

Story draft 3 in section 8 gives Postern a Landing bucket.

## 7. Limits of this study

- The home's event log (`~/.local/state/mw/events` on the Laptop) holds every bead
  transition. Copying a week of it to the desktop was refused by this session's
  permission check, so the times here are the beads' own (`claimed_at`, `queued_at`,
  `closed_at`). A story taken twice shows only its last claim; the three desktop stories
  are the known case, added by hand.
- `~/.local/state/mw/land/<rig>/log` keeps the current landing and the one before it, so
  the split of a gate into "ran" and "waited for other gates" exists for batch 555 alone.
  The week's figure (7.6 of 12.5 hours) comes from the gate logs, which time vitest only.
- `metrics/factory.jsonl`, with each batch's gate minutes and red minutes, is on the
  Laptop and not in the vault; the script rebuilds what it can from `runs/batch/`.
- A batch the home held for its own load without testing it leaves no batch directory,
  so such holds are not counted.
- "Hours saved" in section 8 are estimates from one week. The model is stated there.

## 8. Ranked fixes

How the hours are estimated: a rig's landing line serves its whole queue, so each
pipeline hour freed moves every waiting story forward an hour. Lampas had 5.6 stories
waiting on average (table T7), so one pipeline hour a day is worth about 5.6 story-hours
a day. Sizes are the factory's (S, M, L). What a millwright story of each size took in
the week:

| size | stories | session cost median $ | session wall median min |
|-|-|-|-|
| XS | 23 | 0.27 | 19.9 |
| S | 139 | 0.42 | 23.9 |
| M | 110 | 0.66 | 32.3 |
| L | 20 | 1.36 | 39.1 |
| XL | 14 | 0.61 | 23.4 |

Source: `total_cost_usd` and `duration_ms` of `runs/<story>/result.json` for the
millwright stories landed in the window, by the `size` of each bead's path (the last
lines Appendix A prints).

**0. Already landed: measure it before building more.** mw-gq6.1420, mw-gq6.1422 and
mw-gq6.1450 landed on 2026-11-17, the last at 22:20Z; batch 556 was still taking
turns at 23:05Z on the Laptop's older `mw`. If batch 555's ratio holds, they free about
3.9 pipeline hours a day (31% of 12.5), about 22 story-hours a day. Run Appendix A again
on 2026-11-21 over 2026-11-18 to 2026-11-21 and read T7: "of it not in a vitest run"
should fall from 7.6 hours a day to under 3, and "mean stories waiting" from 5.6. Cost:
ten minutes.

**1. The next batch begins while the last landing's grist smoke runs.** Expected: 1.8
pipeline hours a day, about 10 story-hours a day. 16 lampas landings ended over 15
minutes after the landing itself, 12.3 hours in all; the smoke runs inside `mw land`
(`application/batchland.go:854-857`), and the follower begins no landing of a rig while
one runs (`application/batchland.go:1906`, `infrastructure/apartproc/apartproc.go:75`).
Cost: one story of size M. The surest of the estimates: it is measured, not modelled.

Story draft 1:
- title: `mw land` ends when the batch is landed and its smoke goes on apart, so the rig's next landing begins while the grist smoke runs
- rig: millwright
- files: application/batchland.go, application/landedtail.go, infrastructure/apartproc/apartproc.go, cmd/mw/land.go, features/batch_land.feature, docs/land-rush.md
- acceptance:
  1. `mw land <rig>` exits once the batch is landed, its stories closed, its after-landing command run and its mail sent; the grist smoke runs in a process of its own that the landing begins, with its log at `~/.local/state/mw/land/<rig>/smoke.log`.
  2. features/batch_land.feature has a scenario in which a landing whose batch changed a grind ends before its smoke, and the follower begins the rig's next landing while the smoke still runs: `MW_FEATURE=batch_land.feature go test ./features/ -run '^TestFeatures$'` passes.
  3. Two smokes of one rig never run at once: the second waits for the first (a unit test in application/).
  4. A smoke that fails still comments on the story, raises the alarm event, holds the rig's open stories and mails the Mayor naming the batch.
  5. `make test` and `make lint` pass.
  Sweep: the smoke of a rig that lands each story, in `mw next` (application/afterlanding.go:718). Expect: lampas's "after-landing h per day" in table T7 of docs/research/landing-throughput.md falls from 2.3 to under 1.
- size: M

**2. A batch rig's landing runs on a calm host when the home is over its load.**
Expected: 10 to 15 story-hours a day, the least certain figure here. It has two parts.
The Laptop's gate lock loses its largest holder, so close-outs there wait less: half of
the 61.1 hours of table T8 would be 4 story-hours a day. And the batch's own gate no
longer waits on the Laptop's load, test-runner slots and other gates: after fix 0, one
to two pipeline hours a day, 6 to 11 story-hours. Cost: one story of size L, and cloud
time at $0.132 an hour, at most about $1.65 a day (section 3). Do it after fix 0 is
measured: if the Laptop's waits are gone, this is worth less.

Story draft 2:
- title: The home's follower runs a batch rig's landing on a calm host it is told of when the home is over its load, and waits for it
- rig: millwright
- files: application/batchland.go, application/landelsewhere.go, application/batchmetrics.go, infrastructure/peekremote/remote.go, infrastructure/apartproc/apartproc.go, infrastructure/config/config.go, cmd/mw/batchland.go, cmd/mw/land.go, features/batch_land.feature, docs/land-rush.md, docs/codemap-detail.md
- acceptance:
  1. With `land_hosts = ["<host>", ...]` in the home's config, when a rig's batch is due, the home's load reading is Busy (application/hostload.go:54) and a named host answers and is not busy, the follower runs `mw land <rig>` on that host over ssh and waits for it; no part of the gate runs on the home. A scenario in features/batch_land.feature holds it: `MW_FEATURE=batch_land.feature go test ./features/ -run '^TestFeatures$'`.
  2. With no host named, none reachable or none calm, the home lands as it does today (a scenario).
  3. While the far landing runs, the home's `Pending` names the rig, so no second landing of it begins, and a follower started again finds it and waits (a unit test).
  4. The far landing's line reaches the home's `metrics/factory.jsonl` and its `runs/batch/<rig>/<n>/` reaches the vault before the next landing is decided: a unit test in which a batch held red on the far host makes the home land its stories alone next.
  5. The rig's after-landing command still runs on the home (application/afterlanding.go:629).
  6. The closing comment quotes the `mw land` log of one lampas batch landed from the desktop or a cloud host, with the gate's span.
  7. `make test` and `make lint` pass.
  Sweep: `mw suite` and the nightly timing job, which take the same gate lock on the home (cmd/mw/suite.go, cmd/mw/timingnightly.go).
- size: L

**3. Postern shows a story queued for landing as Landing, not Working.** Expected: no
machine hours. It removes the false alarm itself: 18 of 25 Working rows at 14:32Z, 29 of
40 at 23:10Z. Cost: one story of size S in the postern rig.

Story draft 3:
- title: Map: a story that is done and queued for its batch landing is in a Landing bucket, and Working holds only stories a session works
- rig: postern
- files: src/model/tree.ts, src/cockpit/MapScreen.tsx, the unit tests of `bucketOf`
- acceptance:
  1. `bucketOf` returns `landing` for a bead whose status is `in_progress` and whose labels hold `run:queued`, and `working` for one without the label (a unit test).
  2. The filter bar has a "Landing" toggle beside "Working", and with only Working on, no bead labelled `run:queued` is listed (a component test).
  3. The counts on an epic's row count such a bead under landing, not working.
  4. `npm test`, `npm run typecheck` and `npm run lint` pass.
- size: S

**4. A host silent for over two hours gives its claimed stories back.** Expected: 97
story-hours in a week with an outage like this one's, 14 a day averaged over this week,
none in a week without. Cost: one story of size M, and the Governor's word first, since
it lets the home take another host's claim (the six questions of
`docs/research/stranded-stories.md`). The risk is fuel: a story taken elsewhere is
worked again from its start.

Story draft 4:
- title: With `take_from_silent` on, the home's dispatch hands back a story claimed on a host silent for over two hours and re-paths it to auto
- rig: millwright
- files: application/dispatch.go, application/dispatchnosession.go, application/nosessionclaim.go, application/repath.go, infrastructure/config/config.go, features/dispatch.feature, docs/research/stranded-stories.md
- acceptance:
  1. With `[dispatch] take_from_silent = true` on the home (default false), a story in progress whose path names a host that has recorded no sync for over `DefaultHostSilence` (application/status.go:28), whose lease has run out, is handed back by the home's dispatch: its claim released, its status open, its host set to `auto`, one attempt refunded, and a `RE-PATHED:` comment naming the silent host and since when. A scenario in features/dispatch.feature holds it: `MW_FEATURE=dispatch.feature go test ./features/ -run '^TestFeatures$'`.
  2. It is not done for a story queued for its batch, a hitl story, a landed one, or a host that gives way, nor with the key off: one scenario each.
  3. When the silent host is back, its dispatch does not start the story again, and bundles what its worktree holds before removing it (a unit test).
  4. `make test` and `make lint` pass.
- size: M

**5. `mw status` says how many of a host's sessions wait for the gate.** Expected: no
machine hours. It answers "do they take up a slot" on the screen: "host: 5 of cap 5, 4
waiting for the gate". Cost: one story of size S.

Story draft 5:
- title: `mw status`'s host line says how many of the sessions counted against the cap are waiting for the gate lock or a test-runner slot
- rig: millwright
- files: application/status.go, application/status_render.go, cmd/mw/status.go, features/status.feature
- acceptance:
  1. With cap 5 and five sessions, four of whose stories hold a ticket in the host's gate queue (`~/.local/state/mw/gate.queue`) or its test-runner queue, `mw status` prints `host: 5 of cap 5, 4 waiting for the gate`, within the 60 runes `clip` allows (application/status_render.go:185).
  2. With none waiting the line is as today.
  3. A scenario in features/status.feature holds both: `MW_FEATURE=status.feature go test ./features/ -run '^TestFeatures$'`.
  4. `make test` and `make lint` pass.
- size: S

**A decision, not a story: fewer lampas Builders on the home.** In the week 117 of 168
lampas stories were built on the Laptop, where claim to queued took 1.9 hours at the
median against 1.1 on cloud2 (table T6), and where every other rig's close-out queued
behind them (table T8). Moving them is config: the Laptop's `cap` (5), and `max_boxes`
(4) and `box_cap` (1) in `hosts/vultr/cloud-table.toml`. At cloud2's pace it would save
about 13 story-hours a day inside sessions (117 stories, 0.8 hours each, over 7 days)
for about $17 of cloud time a week (117 stories at 1.1 hours and $0.132 an hour). It spends money, so it is the
Governor's to decide; mw-t0z3fu is the epic that would make it automatic.

**Not ranked, and why.** The fixers (13.3 hours in the week, 21 of 71 batches, most
"set aside for conflicts") and the held batches (18.2 pipeline hours) are the largest
single costs on the landing line, and both come from stories that pass alone and fail
together. That is what batch landing exists to catch, and mw-gq6.1246 already lands a
held batch's stories alone. One thing seen and not traced: after batch 552 held six
stories, two were landed alone and batch 555 took six together again;
`HeldRedTogether` reads only the rig's last batch line
(`application/batchmetrics.go:121-127`), so once one story has landed alone a follower
that starts again finds nothing to go on with (`application/batchland.go:1646`). It is
in this story's closing comment as a finding.

## Appendix A. The script

Everything in tables T1 to T8 comes from this script. It reads the vault and one file of
bead rows, and writes nothing.

```sh
bd -C ~/millwright-vault list --all --flat --brief --json --limit 0 \
   --updated-after 2026-11-10 --has-metadata-key rig > beads.json
python3 -I analyze.py ~/millwright-vault beads.json                # T1 to T4, T6 to T8
python3 -I analyze.py ~/millwright-vault beads.json --batches      # and every batch (T3b)
python3 -I analyze.py ~/millwright-vault beads.json --at 2026-11-17T14:32:00Z   # and T5
python3 -I analyze.py ~/millwright-vault beads.json 2026-11-18T00:00:00Z 2026-11-21T00:00:00Z   # another window
```

Run it on a host whose vault is synced (`mw sync`). The batch records and result files
are in the vault for every host; the check and close-out logs of a story are only on the
host that worked it and are not read.

```python
#!/usr/bin/env python3
# Landing-throughput measurements (mw-gq6.1433). Reads only; writes nothing.
# Run:  python3 -I analyze.py <vault> <beads.json> [window-start] [window-end]
#   beads.json:  bd -C <vault> list --all --flat --brief --json --limit 0 \
#                   --updated-after 2026-11-10 --has-metadata-key rig > beads.json
import json, os, re, subprocess, sys, statistics as st
from datetime import datetime, timedelta, timezone
AT = sys.argv[sys.argv.index('--at') + 1] if '--at' in sys.argv else None
POS = [a for a in sys.argv[1:] if not a.startswith('--') and a != AT]
V, BEADS = POS[0], POS[1]
T0 = POS[2] if len(POS) > 2 else '2026-11-10T23:00:00Z'
T1 = POS[3] if len(POS) > 3 else '2026-11-17T23:00:00Z'
def ts(s):
    return datetime.fromisoformat(s.replace('Z', '+00:00')).astimezone(timezone.utc) if s else None
W0, W1 = ts(T0), ts(T1)
def h(td): return td.total_seconds() / 3600 if td is not None else None
def med(xs):
    xs = [x for x in xs if x is not None]
    return st.median(xs) if xs else float('nan')
def p90(xs):
    xs = sorted(x for x in xs if x is not None)
    return xs[min(len(xs) - 1, int(0.9 * len(xs)))] if xs else float('nan')
def f(x): return '%.1f' % x if x == x else '-'

beads = {b['id']: b for b in json.load(open(BEADS))}
def result(sid):
    try: return json.load(open(os.path.join(V, 'runs', sid, 'result.json')))
    except Exception: return {}

# ---- batches: runs/batch/<rig>/<n>/ and the vault's commit of each -------------
commits = {}
out = subprocess.run(['git', '-C', V, 'log', '--since=2026-11-01', '--format=%cI\t%s', '--', 'runs/batch'],
                     capture_output=True, text=True).stdout
for line in out.splitlines():
    when, subj = line.split('\t', 1)
    m = re.search(r'batch (\d+) of ([\w-]+)', subj)
    if m: commits.setdefault((m.group(2), int(m.group(1))), (ts(when), subj))
batches = {}
for rig in sorted(os.listdir(os.path.join(V, 'runs', 'batch'))):
    for n in os.listdir(os.path.join(V, 'runs', 'batch', rig)):
        d = os.path.join(V, 'runs', 'batch', rig, n); n = int(n)
        if (rig, n) not in commits: continue
        end, subj = commits[(rig, n)]
        b = dict(rig=rig, n=n, end=end, ids=[], queued={}, held=False, fixer=None, starts=[], durs=[], why='')
        try:
            for line in open(os.path.join(d, 'merge-order.txt')):
                m = re.match(r'\d+\. (\S+) \S+ queued (\S+)', line)
                if m: b['ids'].append(m.group(1)); b['queued'][m.group(1)] = ts(m.group(2))
        except OSError: pass
        try:
            o = open(os.path.join(d, 'outcome.txt')).read()
            b['held'] = not o.startswith('landed'); b['why'] = o[:150].replace('\n', ' ')
            if not b['held']:
                b['landed'] = re.findall(r'mw-[\w.]+', o.split('stories:')[-1])
        except OSError: b['held'] = True
        try:
            m = re.search(r'(?:(\d+)h)?(\d+)m\s*$', [l for l in open(os.path.join(d, 'fixer.txt')) if l.startswith('spent:')][0])
            b['fixer'] = int(m.group(1) or 0) + int(m.group(2)) / 60
        except Exception: pass
        try:
            txt = open(os.path.join(d, 'gate.log'), errors='replace').read()
            for m in re.finditer(r'Start at \S*\s*(\d\d):(\d\d):(\d\d)\s*\n\S*\s*Duration \S*\s*([\d.]+)s', txt):
                t = end.replace(hour=int(m.group(1)), minute=int(m.group(2)), second=int(m.group(3)))
                if t > end: t -= timedelta(days=1)
                b['starts'].append(t); b['durs'].append(float(m.group(4)))
        except OSError: pass
        batches[(rig, n)] = b
tried = {}                      # story -> the batches that tried it, in order
for k in sorted(batches):
    for sid in batches[k]['ids']: tried.setdefault(sid, []).append(batches[k])

# ---- per story ---------------------------------------------------------------
rows = []
for sid, b in beads.items():
    md = b.get('metadata') or {}
    if b.get('issue_type') == 'epic' or not md.get('claimed_at'): continue
    r = result(sid)
    claim, queued, closed = ts(md.get('claimed_at')), ts(md.get('queued_at')), ts(b.get('closed_at'))
    landed = closed is not None and (b.get('close_reason') or '').startswith('landed')
    dur = r.get('duration_ms'); api = r.get('duration_api_ms')
    done = claim + timedelta(milliseconds=dur) if dur else None
    for lim in (queued, closed):
        if done and lim and done > lim: done = lim
    rows.append(dict(id=sid, rig=md.get('rig'), host=md.get('host'), claim=claim, queued=queued, closed=closed,
                     landed=landed, done=done, dur=dur, api=api, gate=r.get('gate_seconds'), rhost=r.get('host'),
                     attempts=md.get('attempts'), status=b.get('status'), labels=b.get('labels') or [],
                     lease=ts(b.get('lease_expires_at')), size=md.get('size'), usd=r.get('total_cost_usd')))
inwin = [r for r in rows if r['landed'] and W0 <= r['closed'] < W1]

print('## T1 per rig: Builder done to landed, stories landed %s .. %s (hours are story-hours)' % (T0, T1))
print('| rig | landed | done to landed median h | p90 h | sum h | queue wait sum h | gate-lock and other waits sum h | test parts sum h | not split sum h | landed after a retry | were in a red gate |')
print('|-|-|-|-|-|-|-|-|-|-|-|')
tot = {}
for rig in sorted({r['rig'] for r in inwin}, key=lambda g: -len([r for r in inwin if r['rig'] == g])):
    rs = [r for r in inwin if r['rig'] == rig]
    d2l = []; qw = wt = tp = un = 0.0; retry = red = 0
    for r in rs:
        bs = tried.get(r['id'], [])
        retry += (len(bs) > 1) if r['queued'] else ((r['attempts'] or 1) > 1)
        red += any(x['held'] for x in bs) or 'mergedtests:sent-back' in r['labels']
        if not r['done']: continue
        total = h(r['closed'] - r['done']); d2l.append(total)
        if r['queued']:
            last = bs[-1] if bs else None
            if last and last['starts'] and not last['held']:
                g0 = max(min(last['starts']), r['queued'])
                tests = min(sum(last['durs']) / 3600, h(r['closed'] - g0))
                qw += h(g0 - r['queued']); tp += tests; wt += h(r['queued'] - r['done']) + h(r['closed'] - g0) - tests
            else: un += total
        elif r['gate'] is not None:
            tests = min(r['gate'] / 3600, total); tp += tests; wt += total - tests
        else: un += total
    tot[rig] = sum(d2l)
    print('| %s | %d | %s | %s | %.1f | %.1f | %.1f | %.1f | %.1f | %d | %d |' % (rig, len(rs), f(med(d2l)), f(p90(d2l)), sum(d2l), qw, wt, tp, un, retry, red))
print('all rigs: %d landed, %.0f story-hours between Builder done and landed' % (len(inwin), sum(tot.values())))

print('\n## T2 in-session time: wall time of the Builder session against the model\'s own time')
print('| rig | sessions | session wall median min | model (API) median min | tool time median min | tool share of wall |')
print('|-|-|-|-|-|-|')
for rig in sorted({r['rig'] for r in inwin}, key=lambda g: -len([r for r in inwin if r['rig'] == g])):
    rs = [r for r in inwin if r['rig'] == rig and r['dur'] and r['api']]
    if not rs: continue
    w = sum(r['dur'] for r in rs); a = sum(r['api'] for r in rs)
    print('| %s | %d | %s | %s | %s | %d%% |' % (rig, len(rs), f(med([r['dur'] / 60000 for r in rs])), f(med([r['api'] / 60000 for r in rs])),
                                                f(med([(r['dur'] - r['api']) / 60000 for r in rs])), round(100 * (w - a) / w)))

print('\n## T3 batches ended in the window')
print('| rig | batches | landed | held | stories landed | stories per landed batch median | with a fixer | fixer h sum | landing to end of mw land (after-landing and smoke) median min | sum h | vitest runs span median min | of it between parts median min |')
print('|-|-|-|-|-|-|-|-|-|-|-|-|')
for rig in sorted({k[0] for k in batches}):
    bs = [b for b in batches.values() if b['rig'] == rig and W0 <= b['end'] < W1]
    if not bs: continue
    tails, spans, gaps = [], [], []
    for b in bs:
        cl = [ts(beads[s]['closed_at']) for s in b.get('landed', []) if s in beads and beads[s].get('closed_at')]
        if cl and not b['held']: tails.append((b['end'] - max(cl)).total_seconds() / 60)
        if len(b['starts']) >= 2:
            first = min(b['starts']); last = max(t + timedelta(seconds=d) for t, d in zip(b['starts'], b['durs']))
            span = (last - first).total_seconds() / 60; spans.append(span); gaps.append(span - sum(b['durs']) / 60)
    L = [b for b in bs if not b['held']]
    print('| %s | %d | %d | %d | %d | %s | %d | %s | %s | %s | %s | %s |' % (rig, len(bs), len(L), len(bs) - len(L), sum(len(b.get('landed', [])) for b in L),
          f(med([len(b.get('landed', [])) for b in L])), sum(b['fixer'] is not None for b in bs), f(sum(b['fixer'] or 0 for b in bs)),
          f(med(tails)), f(sum(tails) / 60), f(med(spans)), f(med(gaps))))

if '--batches' in sys.argv:
    print('\n## T3b each batch')
    for k in sorted(batches):
        b = batches[k]
        if not (W0 <= b['end'] < W1): continue
        cl = [ts(beads[s]['closed_at']) for s in b.get('landed', []) if s in beads and beads[s].get('closed_at')]
        print(b['rig'], b['n'], 'end', b['end'].strftime('%d %H:%M'), 'landed' if not b['held'] else 'HELD', len(b['ids']), 'tried',
              'landing', max(cl).strftime('%d %H:%M') if cl else '-', 'oldest queued', min(b['queued'].values()).strftime('%d %H:%M') if b['queued'] else '-',
              'fixer %.1fh' % b['fixer'] if b['fixer'] else '', 'gate first', min(b['starts']).strftime('%H:%M') if b['starts'] else '-', b['why'][:70] if b['held'] else '')

print('\n## T6 batch rigs by the host the Builder ran on: claim to queued')
print('| rig | Builder host | stories | claim to queued median h | p90 h | session wall median h | model (API) median min |')
print('|-|-|-|-|-|-|-|')
for rig in ('lampas', 'spell-forge'):
    for host in sorted({r['host'] for r in inwin if r['rig'] == rig and r['host']}):
        rs = [r for r in inwin if r['rig'] == rig and r['host'] == host and r['queued']]
        if not rs: continue
        print('| %s | %s | %d | %s | %s | %s | %s |' % (rig, host, len(rs), f(med([h(r['queued'] - r['claim']) for r in rs])), f(p90([h(r['queued'] - r['claim']) for r in rs])),
              f(med([r['dur'] / 3.6e6 for r in rs if r['dur']])), f(med([r['api'] / 60000 for r in rs if r['api']]))))

print('\n## T7 the landing pipeline of each batch rig, per day of the window')
days = (W1 - W0).total_seconds() / 86400
print('| rig | story-hours queued (queued to landed) | mean stories waiting | batches per day | batches of 1 story | of 2 to 5 | of 6 or more | queue at gate start median | pipeline busy h per day (fixer + gate + after-landing) | gate span h per day | of it not in a vitest run h per day | fixer h per day | after-landing h per day | held batches |')
print('|-|-|-|-|-|-|-|-|-|-|-|-|-|-|')
notes = []
for rig in ('lampas', 'spell-forge'):
    rs = [r for r in inwin if r['rig'] == rig and r['queued']]
    qh = sum(h(r['closed'] - r['queued']) for r in rs)
    bs = [b for b in batches.values() if b['rig'] == rig and W0 <= b['end'] < W1]
    L = [b for b in bs if not b['held']]
    span = gap = tail = 0.0; qlens = []
    for b in bs:
        cl = [ts(beads[s]['closed_at']) for s in b.get('landed', []) if s in beads and beads[s].get('closed_at')]
        if b['starts']:
            g0 = min(b['starts']); lastv = max(t + timedelta(seconds=d) for t, d in zip(b['starts'], b['durs']))
            g1 = max(cl) if cl and not b['held'] else lastv
            if g1 < lastv: g1 = lastv
            span += h(g1 - g0); gap += h(g1 - g0) - sum(b['durs']) / 3600
            qlens.append(sum(1 for r in rows if r['rig'] == rig and r['queued'] and r['queued'] <= g0 and (r['closed'] is None or r['closed'] >= g0)))
        if cl and not b['held']: tail += h(b['end'] - max(cl))
    fx = sum(b['fixer'] or 0 for b in bs)
    sz = [len(b.get('landed', [])) for b in L]
    longtail = [h(b['end'] - max(ts(beads[s]['closed_at']) for s in b['landed'] if s in beads and beads[s].get('closed_at'))) for b in L
                if any(s in beads and beads[s].get('closed_at') for s in b.get('landed', []))]
    redh = 0.0; redn = 0
    for r in rs:
        held = [x for x in tried.get(r['id'], []) if x['held']]
        if held: redn += 1; redh += h(r['closed'] - min(x['end'] for x in held))
    allb = sorted((b for b in batches.values() if b['rig'] == rig), key=lambda b: b['end'])
    heldh = sum(min(h(b['end'] - allb[i - 1]['end']), 12) for i, b in enumerate(allb) if i and b['held'] and W0 <= b['end'] < W1)
    notes.append('%s: its held batches took %.1f h of the pipeline (each from the batch before it ending to its own end)' % (rig, heldh))
    notes.append('%s: %d landings ended over 15 min after the landing itself, %.1f h in all; %d landed stories had been in a held batch and waited %.0f story-hours between that batch ending and their landing'
                 % (rig, sum(t > 0.25 for t in longtail), sum(t for t in longtail if t > 0.25), redn, redh))
    print('| %s | %.0f | %.1f | %.1f | %d | %d | %d | %s | %.1f | %.1f | %.1f | %.1f | %.1f | %d |' % (rig, qh, qh / (days * 24), len(bs) / days, sum(x == 1 for x in sz), sum(2 <= x <= 5 for x in sz), sum(x >= 6 for x in sz),
          f(med(qlens)), (span + fx + tail) / days, span / days, gap / days, fx / days, tail / days, len(bs) - len(L)))

for n_ in notes: print(n_)
print('\n## T8 rigs that land each story: the close-out beyond its own gate run, by landing host')
print('| host | landed with a gate time | close-out median min | gate run median min | wait beyond the gate median min | p90 min | sum h | close-outs waiting over 30 min |')
print('|-|-|-|-|-|-|-|-|')
for host in sorted({r['rhost'] or r['host'] or '?' for r in inwin if not r['queued']}):
    every = [r for r in inwin if not r['queued'] and (r['rhost'] or r['host'] or '?') == host and r['done']]
    rs = [r for r in every if r['gate'] is not None]      # a result with no gate time cannot be split
    if not rs: continue
    co = [(r['closed'] - r['done']).total_seconds() / 60 for r in rs]
    gt = [(r['gate'] or 0) / 60 for r in rs]
    wt = [max(0, c - g) for c, g in zip(co, gt)]
    print('| %s | %d | %s | %s | %s | %s | %.1f | %d |' % (host, len(rs),
          f(med(co)), f(med(gt)), f(med(wt)), f(p90(wt)), sum(wt) / 60, sum(w > 30 for w in wt)))

# ---- every story 'working' (in_progress) over 3 hours inside the window -------------
print('\n## T4 stories in progress for over 3 hours (last claim to landing, or to %s)' % T1)
def cause(r, end):
    work = h(end - r['claim'])
    q = h(end - r['queued']) if r['queued'] else 0
    sess = (r['dur'] or 0) / 3.6e6
    bs = tried.get(r['id'], [])
    if any(x['held'] for x in bs) and r['queued'] and q >= work / 2: return 'red gate, then queued for landing', q
    if r['queued'] and q >= work / 2: return 'queued for landing', q
    if not r['closed'] and 'run:queued' not in r['labels']:
        if r['lease'] and r['lease'] > W1 - timedelta(minutes=10): return 'Builder still running', q
        return 'dead host or no session (lease ran out %s)' % (r['lease'].strftime('%d %H:%MZ') if r['lease'] else '?'), q
    if sess >= work / 2: return 'Builder running (its session ran %.1f h)' % sess, q
    if r['queued']: return 'queued for landing', q
    return ('other: closed by hand on a demo or verify word, not by a landing' if r['closed'] and not r['landed'] else 'other: close-out waited %.1f h for the gate (lock, calm host, red merged result)' % (work - sess)), q
t4 = []
for r in rows:
    if r['status'] not in ('closed', 'in_progress'): continue
    end = r['closed'] or W1
    if end < W0 or r['claim'] > W1: continue
    work = h(end - max(r['claim'], r['claim']))
    if work <= 3: continue
    c, q = cause(r, end)
    t4.append((r['rig'], r['id'], r['host'], work, q, 'landed' if r['landed'] else ('closed' if r['closed'] else 'in progress'), c))
print('| story | rig | host | hours in progress | of them queued | now | cause |')
print('|-|-|-|-|-|-|-|')
for x in sorted(t4, key=lambda x: (x[0], -x[3])): print('| %s | %s | %s | %.1f | %s | %s | %s |' % (x[1], x[0], x[2], x[3], f(x[4]) if x[4] else '-', x[5], x[6]))
import collections
print('by cause:', collections.Counter(re.sub(r' \(.*', '', x[6]) for x in t4).most_common())
print('hours by cause:', {k: round(sum(x[3] for x in t4 if re.sub(r' \(.*', '', x[6]) == k)) for k in {re.sub(r' \(.*', '', x[6]) for x in t4}})

# ---- a moment in time: what Postern's Working filter held -------------------------
if AT:
    at = ts(AT)
    print('\n## T5 in progress at %s' % at.isoformat())
    snap = collections.Counter()
    for r in sorted(rows, key=lambda r: r['claim']):
        if r['status'] not in ('closed', 'in_progress'): continue
        if not (r['claim'] <= at and (r['closed'] is None or r['closed'] > at)): continue
        state = 'queued for landing' if r['queued'] and r['queued'] <= at else ('Builder session or close-out' if r['done'] is None or r['done'] > at - timedelta(minutes=30) or not r['queued'] else 'close-out')
        snap[(r['host'], state)] += 1
        print('|', r['id'], '|', r['rig'], '|', r['host'], '|', r['claim'].strftime('%d %H:%M'), '|', r['queued'].strftime('%d %H:%M') if r['queued'] else '-', '|', r['closed'].strftime('%d %H:%M') if r['closed'] else 'still open', '|', state, '|')
    print(sorted(snap.items()))

print('\n## T9 what a millwright story of each size took (stories landed in the window)')
print('| size | stories | session cost median $ | session wall median min |')
print('|-|-|-|-|')
for size in ('XS', 'S', 'M', 'L', 'XL'):
    rs = [r for r in inwin if r['rig'] == 'millwright' and r['size'] == size and r['usd'] is not None and r['dur']]
    if rs: print('| %s | %d | %.2f | %s |' % (size, len(rs), med([r['usd'] for r in rs]), f(med([r['dur'] / 60000 for r in rs]))))
```
