# Does bd's `--dolt-auto-commit batch` cut the beads history at the source?

Research for mw-gq6.508 (item 3 of mw-gq6.455's closing comment). Measured 2026-10-16 on cloud2 with
bd 1.3.0 (f45b249ce) and the Dolt 2.2.0 release binary (the version the Laptop runs), on throwaway
copies. Nothing live was touched.

**Answer: yes, by about 4x on a store the size of ours, and a flush every 15 minutes should cut the
growth to under a tenth of today's. No acknowledged write was lost in any mode when the Dolt server
was killed with `kill -9`. Recommendation: adopt with conditions (end of file).**

## What was touched, and what was not

- Scratch directory: `/tmp/gq6508` (one subdirectory per run, each holding its own `data/` for the
  Dolt server and `ws/` for the bd workspace). `HOME` was `/tmp/gq6508/home`.
- Scratch ports: `127.0.0.1:33508` to `33600`, one throwaway `dolt sql-server` per run, started from
  the Dolt 2.2.0 binary copied to `/tmp/gq6508/bin`. The seeded runs all used `33600`, one after the
  other.
- Every `BEADS_*` variable was unset in the scratch shell (this session's environment points at the
  Laptop's server, `10.88.0.2:3307`), and each workspace was made with `bd init --server --server-host
  127.0.0.1 --server-port <port>`, so no scratch `bd` could reach it.
- **Not touched: the live beads server on port 3307, and `~/millwright-vault/.beads`.** No scratch
  `bd` was pointed at either. The only commands run against the vault were this story's own
  bookkeeping (`bd -C <vault> show mw-gq6.455`, the formula-step closes and the closing comment).

## What bd 1.3.0 offers

`bd --help`: `--dolt-auto-commit string   Dolt auto-commit policy (off|on|batch)`. Default `on`;
override with the config key `dolt.auto-commit`. So there are exactly three modes:

- `on`: a Dolt commit after every write (today's behaviour).
- `batch`: writes accumulate in the Dolt working set; `bd dolt commit` commits them all together.
  The help also says a live batch-mode bd process flushes on SIGTERM and SIGHUP.
- `off`: never commits by itself. Writes sit in the working set until someone commits.

Batch needs somebody to flush, so two flush cadences were also measured: `bd dolt commit` once at the
end, every 50 writes and every 10 writes.

## Method

The same 500 writes each time, one `bd` process per write, against a `dolt sql-server` (the way the
Laptop serves the store): 167 creates, 167 updates (`--notes`) and 166 comments, in the order
create, update, comment, with each update and comment landing on the bead just created. Per run: the
number of Dolt commits added (`dolt_log`), store bytes (`du -sb` of the server's data directory) before GC,
after `bd gc --skip-decay --force` (what `mw sync` runs) and after the same with `--full`, wall time of
the 500 writes, and a check that every acknowledged write (bd exited 0) is present in the tables
afterwards.

Two stores:

- **Empty**: a fresh `bd init` (about 2 MB, 76 setup commits that are left out of the commit counts).
- **Seeded**: the same plus 30,006 closed issues with 640-byte descriptions and 12,000 comments,
  17.9 MB after a full GC. The live store holds 29,623 beads (a copy of its data was 66 MB, mw-gq6.455), so this
  is the same row count with less text. The default-mode growth measured here is 35 KB per commit,
  the figure mw-gq6.455 measured on a copy of the live data, so the seed behaves like the real store.

Kill test: in the middle of write 250, a background `kill -9` of the `dolt sql-server` process
(0.12 s into the write), then a restart 1 s later; the writes that were in flight or sent while the
server was down failed and were not counted as acknowledged. (The first attempt at this test killed a wrapper shell instead of the
server, so it proved nothing; every kill result here is from the rerun, after checking the pid was
`dolt`.) A second test, with no flush at all before the end, killed the server four times at random moments during 240 writes and checked every
acknowledged write both before and after the flush.

## Results

Seeded store, 500 writes, no kill (the table closest to our store). "Growth" is store bytes after
`bd gc --skip-decay --force`, minus the seed's 17.95 MB.

| mode | commits | bytes after gc | growth | wall time | data lost on a mid-run kill |
|---|---|---|---|---|---|
| `on` (default) | 500 | 35,423,318 | +17.5 MB (35 KB a commit) | 114 s | 0 of 497 acknowledged |
| `batch`, flushed once at the end | 1 | 21,880,373 | +3.9 MB | 122 s | 0 of 497 acknowledged |
| `batch`, flushed every 50 writes | 10 | 22,687,206 | +4.7 MB | 99 s | 0 of 497 acknowledged |
| `batch`, flushed every 10 writes | 50 | 25,863,089 | +7.9 MB | 107 s | not run (see below) |
| `off` | 0 | 21,586,828 | +3.6 MB | 102 s | 0 of 497 acknowledged |

Empty store, 500 writes. The kill column is from the separate runs with the mid-write `kill -9`
(497 writes acknowledged, 3 failed in the dead-server window); the other columns from the runs
without a kill. A full GC after the default GC changed no row by more than 57 bytes.

| mode | commits | bytes after gc | wall time | data lost on a mid-run kill |
|---|---|---|---|---|
| `on` | 500 | 6,923,689 | 110 s | 0 of 497 |
| `batch`, flushed once at the end | 1 | 1,541,863 | 104 s | 0 of 497 |
| `batch`, flushed every 50 writes | 10 | 2,154,767 | 129 s | 0 of 497 |
| `off` | 0 | 1,519,267 | 107 s | 0 of 497 (all 497 still uncommitted) |

Four random `kill -9` of the server during 240 writes, nothing flushed until the end:

| mode | server kills | writes acknowledged | acknowledged writes missing after restart, before any flush | after the flush |
|---|---|---|---|---|
| `batch` | 4 | 240 | 0 (working set still dirty in 2 tables) | 0 (77 commits = 76 setup + 1 flush) |
| `on` | 4 | 207 (33 failed in the dead windows) | 0 | 0 |

What this says:

- **Commits and bytes.** Batch makes as many commits as it is flushed. Each default-mode commit costs
  35 KB of reachable history here, exactly what mw-gq6.455 measured. The marginal cost of a flush
  was 0.11 MB at 50 writes a flush and 0.09 MB at 10 writes a flush, so a flush costs about as much as
  three single commits and then stops growing with the number of writes it carries. The first 3.6 MB
  of the batch and `off` rows is a one-off, not a rate: it is the changed index pages that the head
  commit and the working set both hold.
- **Wall time.** No difference: 99 to 129 s for 500 writes in every mode, which is the spread between
  repeats of the same mode on this shared box (about 0.2 s of bd start-up a write dominates). Batch
  does not make writes faster or slower.
- **Crash.** A Dolt working set is on disk, not in memory: after a `kill -9` of the server in the
  middle of a write, and after four random kills in a row, every write bd had acknowledged was still
  there, in batch and `off` mode too, committed or not. The only writes lost were the ones bd had not
  acknowledged (3 per kill here). Not measured: a power cut or a dead disk (Dolt's own fsync
  behaviour), and embedded mode (the live store is served, and no scratch store was run embedded).
- **What a crash does cost in batch**: an uncommitted write is in no Dolt commit, so it is in no
  backup or push of the store either, until the flush. The window between flushes is also the most a
  lost disk on the serving host can take.

Not measured, from the help text and the commit log only: `bd history <id>` and `dolt_diff` see
one version per flush instead of one per write. The commit message of a flush is bd's summary of the
last change (it read `bd: create issue` after a flush of three creates), so the kind-of-write
breakdown mw-gq6.455 made from commit messages stops being possible.

## Things that decide how to adopt it

1. **The setting is per client, and any client left on `on` defeats it.** One `bd` with `on` commits
   the whole shared working set at its next write (tested: a create with `BD_DOLT_AUTO_COMMIT=on`
   committed two pending batch creates in the same commit). Every host that writes to the server, the
   Mayor's interactive shell included, has to be in batch.
2. **Where the setting lives.** `bd config set dolt.auto-commit batch` writes
   `dolt.auto-commit: "batch"` into `.beads/config.yaml`, which the vault tracks in git, so one commit
   reaches every host (the same file `application/homemove.go` and `application/init.go` already
   look after). The flag `--dolt-auto-commit` and the environment variable `BD_DOLT_AUTO_COMMIT` both
   override the file (tested), which is how a single command can go back to `on`.
3. **Someone must flush.** `bd dolt commit` from any process commits every client's pending writes
   (tested: the flushing process was never the one that wrote). It must also run before anything
   that wants a clean working set or a commit to carry: `bd dolt push` and `pull`, the backup, and
   `mw sync`'s squash (`SquashHistory` soft-resets onto the first commit and commits). The commit tally in
   mw-gq6.455 shows 8,043 "auto-commit before pull" commits, so something already commits ahead of pulls.
4. **It stops the growth; it does not shrink the 4.4 GB.** The existing history still needs
   mw-gq6.455's squash. With batch the history grows by about 100 commits a day, so the squash's
   50,000-commit trigger will rarely fire again, which is the point.
5. **`off` is not an option for us.** It never commits, so the backup and every push see nothing.

## Estimate for the live store

The Laptop writes 3,300 to 8,600 commits a day, about 175 MB a day (mw-gq6.455). At 35 KB a commit
that is the whole of it. With a flush every 15 minutes (96 flushes a day, 34 to 90 writes in each) and 0.09 to 0.11 MB a flush, the growth is about **9 to 11 MB a day**, a tenth
of today's. A flush every hour would be about a quarter of that again, at the price of a longer
unflushed window. This is an extrapolation from 500-write runs; the first week on the Laptop checks
it (`mw status` BEADS line).

## Recommendation: adopt with conditions

Adopt batch, on these conditions:

1. Every host that writes to the beads server sets it (the vault's `.beads/config.yaml` is the one
   place that reaches them all), and `mw doctor` flags a host whose bd is not in batch mode, since
   one stray client commits everyone's writes at its next write.
2. mw runs `bd dolt commit` on a cadence of 15 minutes (from the Laptop, where the store lives; this
   is also the longest the backup can be behind), and before every push, pull, backup and squash.
3. A flush that fails is a doctor fault, not a silent one: an unflushed working set is the one thing
   batch adds to the failure modes.
4. Roll out on the Laptop first and compare its beads growth over a week with the 175 MB a day of
   today; the squash of mw-gq6.455 stays as it is.

The exact config line mw would set, once, in the vault:

```
bd config set dolt.auto-commit batch
```

which leaves `dolt.auto-commit: "batch"` in `.beads/config.yaml`, and the flush it pairs with:

```
bd dolt commit
```
