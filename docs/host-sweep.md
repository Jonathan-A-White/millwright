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
| 3 | A directory of the vault's `runs/` | Its newest file is 14 days old or more (`host_sweep_runs_days`, `MW_HOST_SWEEP_RUNS_DAYS`), and its name is not a story that is still open on this host | Delete the directory, whole |
| 4 | `node_modules` and `ms-playwright` (the Playwright browsers) inside a worktree | The story is closed, and the worktree has no uncommitted change and no process in it. Row 1 takes them with the worktree; this row is for a worktree row 1 kept because of a commit not on main | Delete those directories only |
| 5 | A `beads-*-aside-*` directory in the home directory (what `mw home move` sets aside) | Its newest file is 7 days old or more (`host_sweep_aside_days`, `MW_HOST_SWEEP_ASIDE_DAYS`) | Delete the directory, whole |

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
runs `mw host sweep --timer` every Sunday at 04:40 UTC and 15 minutes after the
host boots, as `mw-doctor.timer` is installed:

```
sh scripts/install-units.sh --enable mw-host-sweep
```

With `--timer` the **first run on a host only lists**, and keeps
`~/.local/state/mw/host-sweep-listed`; every later run removes. Delete that file
to have the timer list once more.

## The runs

`runs/` is tracked in the vault's git, and a directory's age is read from its
files' times on this host, so a freshly cloned vault has none old. A removed run
shows as a deletion in the vault's git; the vault's sync keeps or commits it as
it does any change. A run's story is not asked of the tracker: one with no file
written for 14 days is over.
