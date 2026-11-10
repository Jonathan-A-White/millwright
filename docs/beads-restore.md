# Restoring the beads from a dump

The beads have a copy off the Laptop's disk: a `bd export` of the whole store,
gzipped (the Mayor's export of 2026-11-10 15:23Z, `beads-20261110.jsonl.gz`: 64,878
beads, 26 MB). `scripts/beads-restore-drill.sh` reads such a file back into a
scratch store and counts what came back against the file. This page is what the
drill found on 2026-11-10 on the desktop (bd 1.3.0, embedded Dolt, 12 GB of memory,
other sessions running), and how to repeat it:

```sh
cd /tmp            # any directory with no .beads at or above it
/path/to/millwright/scripts/beads-restore-drill.sh <dump.jsonl.gz> [--lines N]
```

It works only in a directory it makes with `mktemp -d` and removes on exit; every
`bd` runs as `env -i HOME=<scratch>/home PATH=... nice -n 19 bd` from
`<scratch>/store`, so none of a seat's `BEADS_*` variables reach it and the live
server is never touched. It steps over the `bd` shim (`exec mw bd`) that leads a
seat's PATH, never calls `mw`, and never runs `mw bd export`. `scripts/check-beads-restore-drill.sh`
(in `make lint`) holds all of that against a stand-in `bd`.

## What a restore costs (measured)

| run | lines | `bd import` | `.beads` on disk | came back |
| --- | --- | --- | --- | --- |
| trial, `--lines 2000` (with the 3 beads the checks name) | 2,002 | 105 s (an earlier trial of the same lines: 70 s, 182 s on a busier host) | 557 MB | all 2,002 |
| whole file | 64,878 | **stopped by `timeout 90m` after 5,228 s (87.1 min)** | 7.4 GB | 40,000 of 64,878 |

The trial projected 57 min and 17.6 GB for the whole file, under the stop rule of 90
minutes and 40 GB, so the whole file was run. It did not finish: the import slows as
the store grows (about 19 beads/s in the trial, about 7.6 beads/s over the 40,000
that landed), so cost is not linear in beads. **A restore of today's store takes
more than 87 minutes; how much more is not measured.** Disk was 7.4 GB at 40,000
beads (about 185 KB a bead, against 280 KB a bead in the trial), so the whole file
needs roughly 12 GB or more; keep 40 GB free. Memory: the import held 16-23% of a
12 GB host. The drill refuses with under 8 GB available.

Exactly 40,000 of the 64,878 beads were in the store when the import was stopped, a
round number that this drill did not explain (a batch of the import is a guess); the
24,878 that were not are not known to be the file's last lines. The drill names the
first ten absent ids when a count is short (the first was `mw-06ggew.10.1`, an
`event`).

## What came back

From the whole-file run (partial) and the trial (complete):

- Every bead in the trial, and the 40,000 that landed: `bd count --include-infra`
  and `--by-type` agree with the file on the types that finished (`chore` 9,
  `mail` 14,340, `molecule` 4,445) and on every type in the trial.
- `mw-6ww.9`: parent `mw-6ww`, labels `hitl` and `wayfinder:grilling`, and all 281
  comments, as the file has them (ok in both runs).
- A new `bd create --parent mw-gq6` took `mw-gq6.1059` in the whole run (the file's
  highest is `mw-gq6.1058`, and 1059 was never a bead in the file) and `mw-gq6.1024`
  in the trial (the trial's highest is 1023): the new child follows the file's
  highest number and reuses none of the file's, in both runs.
- Dependencies: the whole-file import skipped none (every target lies in the file);
  the trial skipped 967 whose targets were outside its 2,000 lines, as expected.

### The 8 lines marked MISS in the whole-file run

Each is a count that came back short in a store whose import was stopped at 90
minutes. Whether the import would have brought back every bead had it been let run
is not known: it was not run past the stop.

1. `import: bd import was stopped by timeout 90m after 5228 s`
2. `count --include-infra against the file's issue lines: file 64878, store 40000`
3. `by-type bug: file 1329, store 1158`
4. `by-type epic: file 225, store 224`
5. `by-type event: file 10311, store 4749`
6. `by-type feature: file 215, store 129`
7. `by-type story: file 32, store 21`
8. `by-type task: file 33972, store 14925`

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
  `--include-infra` (40,000 of 40,000). Whether `bd export` leaves wisps out or the
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
   measurement above: more than 87 minutes and about 12 GB for the 2026-11-10 file.
5. Count: `bd count --include-infra` and `--by-type` against the file's lines by
   `issue_type`; read one bead with parent, labels and comments.

The drill does NOT prove that the restored store can be served to the factory and its clones: that a Dolt server over it takes the place of the live one, and that the Laptop, the desktop and the VPS reach it (mw-6ww.9).
