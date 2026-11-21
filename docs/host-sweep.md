# mw host sweep: what a host's disk keeps, and for how long

A host's disk fills with what a story left behind: its worktree and branch, its
`node_modules` and Playwright browsers, a landing a close-out could not take
away, runs of the vault, directories a home move set aside. The desktop's D:
reached 0 GB free on three days of November 2026 and WSL would not start. `mw
host sweep` frees what no live story can need, on every host, on a timer.
Its ages are PROVISIONAL, for the Governor to confirm (mw-gq6.1427).

| # | What it acts on | Bound | The act |
| --- | --- | --- | --- |
| 1 | A story's worktree under `.mw-worktrees/<story>` beside a rig, and its branch `mw/<story>` (a branch whose worktree is already gone too) | The story is **closed** (landed, superseded or abandoned) for over an hour, the worktree has no uncommitted change and no process in it, and every commit of the branch is on main or pushed to the branch's own remote ref | `git worktree remove` (never `--force`), then delete the branch |
| 2 | A `.mw-landing-*` directory in the home directory or beside a rig | Over a day old, and the doctor's rule for a spare landing holds: clean, HEAD on main, no process in it | Delete the directory (the doctor's `stale-landings` check does the same for these; the sweep is the rolling one) |
| 3 | A directory of the vault's `runs/` | Its newest file is 14 days old or more (`host_sweep_runs_days`, `MW_HOST_SWEEP_RUNS_DAYS`), and its name is not a story with a worktree here that is not closed | Delete the directory, whole |
| 4 | `node_modules` and `ms-playwright` (the Playwright browsers) inside a worktree | The story is closed, and the worktree has no uncommitted change and no process in it. Row 1 takes them with the worktree; this row is for a worktree row 1 kept because of a commit not on main | Delete those directories only |
| 5 | A `beads-*-aside-*` directory in the home directory (what `mw home move` sets aside) | Its newest file is 7 days old or more (`host_sweep_aside_days`, `MW_HOST_SWEEP_ASIDE_DAYS`) | Delete the directory, whole |

<!-- toc -->

- [What it never touches](#what-it-never-touches)
- [Running it](#running-it)
- [Whether it has run: `mw doctor host-upkeep`](#whether-it-has-run-mw-doctor-host-upkeep)
- [The runs](#the-runs)

<!-- /toc -->

## What it never touches

* A worktree with an uncommitted change (an untracked file counts), or one git
  cannot read: listed as `kept: uncommitted changes`.
* A branch with a commit that is neither on main nor pushed: `kept: 2 commits on
  mw/<story> are neither on main nor pushed`. Its `node_modules` still go (row 4).
* A story that is not closed (claimed, retried, waiting, blocked): not listed at
  all. A worktree whose story the tracker does not know: `kept: the tracker has
  no story <id>`. A story closed under an hour ago is kept: its close-out is
  still taking the worktree away.
* A worktree a process is running in, and a landing with one.
* The tracker unread: with no answer from beads no story is known closed, so no
  worktree is taken, and the run says so.

## Running it

`mw host sweep --dry-run` lists each path, its size and the reason, and changes
nothing. `mw host sweep` removes them and writes one line a path to the
Millhand's tick log (`tidy: host: removed ...`, under mw tidy's prefix so the
tick's counts read past it); a second run lists nothing. `mw-host-sweep.timer`
runs `mw host sweep --timer` every day at 04:40 UTC and 15 minutes after the
host boots (daily, not weekly: the sweep spends no fuel, and the desktop churned
about 60 GB in a day; the cadence is PROVISIONAL, for the Governor to confirm).
Every host runs it, with the tidy, the doctor and the dispatch, from one name:

```
sh scripts/install-units.sh --boost            # the four units, and which this host lacks
sh scripts/install-units.sh --boost --enable   # link and enable them (a Boost's bootstrap does)
```

With `--timer` the **first run on a host only lists**, and keeps
`~/.local/state/mw/host-sweep-listed`; every later run removes. Delete that file
to have the timer list once more.

## Whether it has run: `mw doctor host-upkeep`

A real run of the sweep, and of `mw host tidy go-cache`, leaves a mark under
`~/.local/state/mw` (`host-sweep-ran`, `host-tidy-go-cache-ran`); a listing run
leaves only `host-sweep-listed`. The `host-upkeep` check of `mw doctor` is faulty
when:

* the last real sweep is over 2 days old, or there has only ever been a listing
  run, or none at all;
* the last go-cache tidy is over 2 days old, or none has run;
* over 20 worktrees of stories closed for over a day are still in the worktrees
  directory beside a rig (a story built here and landed from another host leaves
  its worktree here: the close-out that takes it away runs on the other host);
* `mw-host-sweep.timer` or `mw-host-tidy.timer` is not enabled: the finding names
  which, and the one command, `sh scripts/install-units.sh --boost --enable`, run
  from the rig's checkout (a person runs it: no cure does).

Its cure runs `mw host tidy tmp`, `mw host tidy go-cache` and `mw host sweep`
itself, in that order (a sweep the doctor's time limit cuts short goes on at the
next run, each act standing alone), and its way back says what each removed.
Nothing it removes is put back: a closed story's worktree is gone with its
merged or pushed commits, a cache is rebuilt by its tool.

## The runs

`runs/` is tracked in the vault's git, and a directory's age is read from its
files' times on this host, so a freshly cloned vault has none old. A removed run
shows as a deletion in the vault's git; the vault's sync keeps or commits it as
it does any change. A run is kept only when its name is a story with a worktree here
that is not closed; no other story is asked of the tracker for a run.
