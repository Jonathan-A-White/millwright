# Restoring the beads from a dump

The beads have a copy off the Laptop's disk: a `bd export` of the whole store,
gzipped (the Mayor's export of 2026-11-10 15:23Z, `beads-20261110.jsonl.gz`: 64,878
beads, 26 MB). `scripts/beads-restore-drill.sh` reads such a file back into a
scratch store and counts what came back against the file. This page is what the
drill found on 2026-11-10 on the desktop (bd 1.3.0, embedded Dolt, 12 GB of memory,
other sessions running; the whole-file run below was made again on 2026-11-10/11 with a 360-minute cap), and how to repeat it:

```sh
cd /tmp            # any directory with no .beads at or above it
/path/to/millwright/scripts/beads-restore-drill.sh <dump.jsonl.gz> [--lines N] [--timeout MINUTES] [--keep | --resume DIR]
```

`--timeout MINUTES` (default 360) caps `bd import` over all its passes. A cap that
cuts the import is said, with the count the store had reached. An import that exits
non-zero before the cap is run again on the same store (`bd import --help`: committed
chunks stay durable, a failed import is safe to re-run), up to 3 passes, and each
pass prints its minutes and the store's count. A whole file takes hours: start it
detached and read its log, for example
`systemd-run --user --unit=beads-drill -p StandardOutput=file:/tmp/drill.log -p StandardError=file:/tmp/drill.log -E PATH=$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin /path/to/millwright/scripts/beads-restore-drill.sh <dump.jsonl.gz>`
(the user manager's PATH holds no `bd`, so name one).

**Progress.** During each pass the drill prints an `info import progress` line every 10
minutes: the pass, the minutes into it, and the count `bd import` prints on its own
stderr (`bd import: 5000/64878 issues committed`, one line every 250 beads in a 1,500-line probe of bd 1.3.0). The drill
never asks the store for its count while the import runs (a second `bd` on the store
could contend with the import), so the count is bd's own; until bd has printed one the
line says the store's count is not read during the import, and why. A pass still prints
its own `info import pass` line, with the store's count, when it ends.

**Keep and resume.** A restore that the cap cuts used to be lost with its scratch
directory. `--keep` leaves the scratch directory in place when the import did not
finish (cut by `--timeout`, or failed on every pass) and prints its path and the
command to continue it; a pass that finished removes it as ever, and a drill without
the flag removes it on every exit. `--resume DIR` imports again into that kept store
(`bd import` is safe to re-run on it), counts it against the file and removes it once
the import has finished; give it the same dump and the same `--lines`, or it refuses
(it checks the import file against the one the store was made for) and leaves `DIR`
alone. `--resume` keeps the store again if the import is cut again, with a new
`--timeout`. A kept store costs disk: about 220 KB a bead, 12 GB at 55,750 beads, 14 GB
or more for the whole file. The drill makes no `bd create` in a store it keeps, since a
new bead would change its count.

It works only in a directory it makes with `mktemp -d` and removes on exit; every
`bd` runs as `env -i HOME=<scratch>/home PATH=... nice -n 19 bd` from
`<scratch>/store`, so none of a seat's `BEADS_*` variables reach it and the live
server is never touched. It steps over the `bd` shim (`exec mw bd`) that leads a
seat's PATH, never calls `mw`, and never runs `mw bd export`. `scripts/check-beads-restore-drill.sh`
(in `make lint`) holds all of that against a stand-in `bd`.

<!-- toc -->

- [What a restore costs (measured)](#what-a-restore-costs-measured)
- [What came back](#what-came-back)
  - [The 7 lines marked MISS in the second whole-file run](#the-7-lines-marked-miss-in-the-second-whole-file-run)
- [What a restore does not bring back](#what-a-restore-does-not-bring-back)
- [What was not known, and is now](#what-was-not-known-and-is-now)
- [The steps of a real restore, as far as the drill proves them](#the-steps-of-a-real-restore-as-far-as-the-drill-proves-them)

<!-- /toc -->

## What a restore costs (measured)

| run | lines | `bd import` | `.beads` on disk | came back |
| --- | --- | --- | --- | --- |
| trial, `--lines 2000` (with the 3 beads the checks name) | 2,002 | 105 s (an earlier trial of the same lines: 70 s, 182 s on a busier host) | 557 MB | all 2,002 |
| whole file, first run (90-minute cap) | 64,878 | stopped by `timeout 90m` after 5,228 s (87.1 min) | 7.4 GB | 40,000 of 64,878 |
| whole file, second run (`--timeout 360`, one pass) | 64,878 | **stopped by the 360-minute cap (`timeout 21600s`); the drill's own `date` clock read 19,695 s (328.2 min) when it stopped** | 12 GB | 55,750 of 64,878 |

**A restore of the 2026-11-10 store did not finish inside 360 minutes.** (The two clocks
disagree: `ps` showed `bd import` at 5 h 34 min elapsed while the drill's `date` had
counted less, so the drill's 328.2 min is the lower reading and the 360-minute cap, which
`timeout` counts, is the real stop; why they differ on this WSL host is not known. Rates
below use the drill's `date` clock.) The import
exited by the cap (exit 124), not by an error, so there was no second pass. 40,000 was
therefore not a limit of `bd import`: the second run went past it, to 55,750. It is
slow and slows as the store grows: 40,000 beads in the first 87 minutes (about 7.6
beads/s), then 15,750 more in the next 241 minutes (about 1.1 beads/s). The 9,128
beads left at the cut would take hours more at that rate; how many is not measured.
**How long a whole restore takes is still not known; it is more than 5 hours.** Disk
was 12 GB at 55,750 beads (about 220 KB a bead), so the whole file needs about 14 GB or
more; keep 40 GB free. The drill refuses with under 8 GB available.

The trial projected 57 min and 17.6 GB for the whole file, so the trial's linear
projection was wrong for time (it was right-ish for disk). **The projection is now made
from this measurement, not linearly**: the minutes for the whole file are read off the
curve of the second whole-file run (0 beads at 0 min, 40,000 at 87.1 min, 55,750 at
328.2 min, and past 55,750 the last stretch's rate, 1.1 beads/s, which was still
falling, so past it the figure is a floor), and scaled by how this trial's seconds a
line compare with the trial that measurement began with (2,002 lines in 105 s). For the
64,878 lines of the 2026-11-10 file a trial at that rate projects about 468 min, not 57,
and so is told to stop; disk stays linear. The line says it is a projection from the
measurement of mw-t5shinh.38; a trial on a host much quicker than the desktop projects
fewer minutes, and the measurement is of that one host and file. To finish a restore,
give the drill `--timeout` of more than 360 (say 720) and `--keep`, read its log, and
`--resume` the kept store if it is cut.

The 9,128 beads not in the store at the cut are not known to be the file's last lines
(the first absent id was `mw-06ggew.11`, a closed `task`; the first run's was
`mw-06ggew.10.1`).

## What came back

From the whole-file run (partial) and the trial (complete):

- Every bead in the trial, and the 55,750 that landed in the second whole run: `bd count --include-infra`
  and `--by-type` agree with the file on the types that finished (`chore` 9, `epic` 225,
  `mail` 14,340, `molecule` 4,445) and on every type in the trial.
- `mw-6ww.9`: parent `mw-6ww`, labels `hitl` and `wayfinder:grilling`, and all 281
  comments, as the file has them (ok in both runs).
- A new `bd create --parent mw-gq6` took `mw-gq6.1059` in both whole runs (the file's
  highest is `mw-gq6.1058`, and 1059 was never a bead in the file) and `mw-gq6.1024`
  in the trial (the trial's highest is 1023): the new child follows the file's
  highest number and reuses none of the file's, in both runs.
- Dependencies: the whole-file import skipped none (every target lies in the file);
  the trial skipped 967 whose targets were outside its 2,000 lines, as expected.

### The 7 lines marked MISS in the second whole-file run

Each is a count that came back short in a store whose import was cut at 360 minutes.
Whether the import would have brought back every bead had it been let run is not
known: it was not run past the cap.

1. `import: bd import was stopped by the --timeout cap of 360 min after 19695 s, with 55750 of 64878 issue lines in the store`
2. `count --include-infra against the file's issue lines: file 64878, store 55750`
3. `by-type bug: file 1329, store 1277`
4. `by-type event: file 10311, store 8458`
5. `by-type feature: file 215, store 192`
6. `by-type story: file 32, store 31`
7. `by-type task: file 33972, store 26773`

`chore` (9), `epic` (225), `mail` (14,340) and `molecule` (4,445) agree with the file.
In the first run (90-minute cap) `epic` was also short (224 of 225) and there were 8
lines; at 55,750 every `epic` is back.

## What a restore does not bring back

The dump is a list of beads, so a restore does not bring back:

- Dolt history: every commit before the import; the restored store has one history,
  made by the import.
- bd's audit table (who changed what, when).
- The config table: `bd config` values come back only as `bd init` and the drill set
  them (`types.custom mail`); a store restored without that line may refuse the mail
  rows.
- Every `kv.*` note mw keeps in the config table (`infrastructure/beads/keptread.go`,
  `noteOverConnection` and `notesOverConnection`): the notes live beside the beads,
  not in them.

## What was not known, and is now

- Wisp-tier beads: none of the file's 64,878 issue lines carries an `ephemeral` or
  `wisp` flag, and the store's `bd count` is the same with and without
  `--include-infra` (55,750 of 55,750). Whether `bd export` leaves wisps out or the
  store holds none was not told apart by this drill.
- Memory records (`"_type":"memory"`): none in this file (0 lines).
- The new child's number: see above, `mw-gq6.1059`.
- `bd import` was run here on a file path of decompressed lines; the drill unzips
  the dump first, which the time above does not include (seconds).

## The steps of a real restore, as far as the drill proves them

1. Make an empty directory outside any rig, with no `.beads` at or above it; run
   `git init` in it.
2. `env -i HOME=<empty home> PATH=... bd init -p mw --non-interactive --role maintainer --skip-agents --skip-hooks -q`
   (`env -i` keeps `BEADS_DOLT_*` of the shell out of it).
3. `bd config set types.custom mail`.
4. Unzip the dump and `bd import <file>.jsonl`, under no timeout shorter than the
   measurement above: more than 5 hours (not finished at 360 minutes) and about 14 GB
   or more for the 2026-11-10 file. Re-run `bd import` on the same store if it exits
   non-zero or is cut (the drill's `--keep` and `--resume` do that).
5. Count: `bd count --include-infra` and `--by-type` against the file's lines by
   `issue_type`; read one bead with parent, labels and comments.

The drill does NOT prove that the restored store can be served to the factory and its clones: that a Dolt server over it takes the place of the live one, and that the Laptop, the desktop and the VPS reach it (mw-6ww.9).
