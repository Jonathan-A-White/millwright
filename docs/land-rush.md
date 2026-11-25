# Land Rush

Land Rush is a per-rig mode in which a rig's stories are queued as they pass
close-out and landed together, tested once, in a batch. The design is
`plans/0117-land-rush-design.md` in the vault; this page says what was built.
Lampas is the trial rig. Every other rig stays on `landing = "each"`, which is
what millwright always did: one story, one merge, one gate run.

Why: on lampas a story's tests ran three times on the same code (the Builder's
`mw check`, `mw next` in the worktree, `mw next` on the merged result), and one
lock per host serialised them. A batch runs the merged result's tests once, and
gives the conflicts and the "passes alone, fails together" cases one fixer
instead of a fresh Builder session each.

<!-- toc -->

- [What a rig turns on](#what-a-rig-turns-on)
- [Where a story waits](#where-a-story-waits)
- [The batch](#the-batch)
  - [A branch set aside does not hold the rest](#a-branch-set-aside-does-not-hold-the-rest)
  - [A red story does not hold the rest](#a-red-story-does-not-hold-the-rest)
  - [Rigs land side by side](#rigs-land-side-by-side)
  - [After a batch is held red](#after-a-batch-is-held-red)
  - [When a batch is held](#when-a-batch-is-held)
- [The fixer](#the-fixer)
- [Records](#records)
- [The stop rule](#the-stop-rule)
- [Rollback](#rollback)

<!-- /toc -->

## What a rig turns on

Seven keys in the rig's file, `rigs/<rig>.toml` in the vault. They are read
leniently: a value this mw cannot read becomes the default, with a warning,
and never stops a landing.

| Key | Default | Meaning |
|---|---|---|
| `landing` | `"each"` | `"batch"` turns the mode on; `"each"` is the way back |
| `batch_every` | `"30m"` | how long the oldest queued story must have waited |
| `batch_min` | `2` | stories that must be queued before a batch is due |
| `batch_max` | `6` | queued stories at which a batch is due at once, however recent; also the most a batch takes (the oldest queued; the rest wait for the next batch) |
| `batch_fixer_model` | `"sonnet"` | the model of a batch's one fixer |
| `batch_fixer_effort` | `"high"` | the fixer's effort |
| `batch_fixer_turns` | `60` | the fixer's turn cap |

```toml
landing            = "batch"
batch_every        = "30m"
batch_min          = 2
batch_max          = 6
batch_fixer_model  = "sonnet"
batch_fixer_effort = "high"
batch_fixer_turns  = 60
```

All of these are PROVISIONAL. `mw status` has a LAND RUSH section, for each
rig that lands in batches: what is queued, the oldest wait, the last batch and
the batches held in a row.

## Where a story waits

`mw next` runs today's refusals unchanged. Then, on a batch rig, it does not
merge: it pushes the story's branch to `origin/mw/<story>`, marks the run
`queued`, writes a ledger line "queued for batch landing", closes the session
and dispatches the next story. `mw status`, the sweep, dispatch and the quiet
alarm all call a queued story alive. The worktree stays until the story is
landed or held. On a batch rig nothing but `mw land` pushes the target branch.

The rig's tests are not run again for a story that was tested at the commit it is
queued at (`application/nextparts.go`, mw-6ww.119.11.1.1). A whole `mw check` that
ends "tip / passed" at that commit is trusted, and the ledger line says "tested by mw
check at <commit>". Where it did not, and the rig's gate is named as parts on the
host (`[test_parts.<rig>]`), the close-out reads the parts' records of the attempt
(`check-<n>-part-<k>.log`: a record names a tip only when the worktree was clean at it).
Every part passed at the tip: no part is run, and the changed specs are, unless their
record is a pass at the tip; the line says "tested by mw check, parts at <commit>: pass;
changed specs run at close-out". Some parts passed: the gate runs the others alone, and
the line names both ("parts 2, 3 of 3 at <commit>: pass; part 1 (lint) run at
close-out"). The batch's own gate still runs the whole suite and the specs on what is
pushed, which is why only a batch rig does this.

## The batch

The follower's `batch-land` job (home only, never on the Boost) runs
`mw land <rig>` when a batch is due: `batch_min` queued and the oldest has
waited `batch_every`, or `batch_max` queued. By hand:

```sh
mw land lampas --dry-run   # the batch it would make, oldest queued first; changes nothing
mw land lampas             # merge, test once, push main only when green
```

In a landing worktree detached at the origin's target branch, the queued
branches are merged oldest queued first, by the rig's `union_merge_files`
rules. A branch that does not merge is set aside with git's conflict list and
stays queued; so is a branch whose commits carry AI attribution. The version
is raised once, with one changelog entry per story. The rig's tests run once on
the merged result, under the host's gate lock and calm rule. Main moves only
when they pass, and as a fast-forward: a red main would refuse every Builder's
close-out and is what the VPS serves.

### A branch set aside does not hold the rest

When some branches of a batch merge and others are set aside, the ones that
merged land first: they are tested as merged, under the same gate, and pushed
without waiting for anything. The green part is never held for a set-aside
story, and nothing is pushed that has not passed the whole gate as merged. No
fixer is started for the ones set aside before the push (batch 595 of lampas
once held five clean stories about 13 hours for one fixer).

A story that conflicts only with its batch mates stays queued (`run=queued`,
open and claimed, its branch untouched and never pushed untested). A note says
why, on the story itself ("Set aside by batch N of the rig: ...", with the
conflicted files) and in the batch's report; the Mayor's "Set aside" mail names
it and its conflict. The next batch takes it with the others.

A story that conflicts with main alone cannot be cleared by waiting
(mw-gq6.1609). Once a merge into the batch conflicts, mw tries the branch on the
target without its batch mates, in a landing worktree of its own (not needed
while nothing has merged: the batch is then the target). When that conflicts too,
the story is handed back at once: its run state is cleared, its claim given back,
a comment and the mail say it "conflicts with main, not the batch", and the next
dispatch tick takes it as a new attempt to rebase it. A batch of which every
branch conflicts is first given its one fixer (see [The fixer](#the-fixer)): what
the fixer merges lands, and what it does not is handed back. When the host or the
harness kept that fixer from starting, or stopped it, the stories wait for the
next batch's fixer instead.

Whatever the cause, a story set aside by a second batch is handed back the same
way and not queued again (mw-gq6.1631, mw-gq6.1632). Only a red gate on the
merged part holds the merged part.

Green with nothing set aside lands the batch: each story is closed
`run=landed` with a ledger line naming the batch, a chain stamp and a Landed
mail; the after-landing command and the grist smoke run once. No fixer is
started and no fuel is spent.

### A red story does not hold the rest

When the rig's tests are red on the merged batch and the batch can say which
story made them red, that story is left out and the green rest lands first. It
can say in two ways: one story's branch alone changed the files of the failing
tests (read from the gate's output, as the batch's `suspect` is), or the batch's
fixer ended badly and said, in its `CAUSE` or `TOUCHED` lines, that one story
was the cause. The batch is then made again in a fresh landing worktree from the
same base without that story, the version raised again, and the tests run on it
as merged; green is pushed, so nothing is pushed that has not passed the whole
gate as merged. The story left out stays queued (`run=queued`, open and claimed)
with a note on it, "Set aside by batch N of the rig: its tests failed on the
batch (failing: ...)", and the Mayor's "Set aside" mail names it. It is not counted
as set aside for a conflict, which would hand it to a Builder to rebase: when it
is queued alone and red again it is held red alone, as before.

This is done once for a batch. A batch that cannot name one story (a failing
test that names no file, a file two branches changed, two stories' tests red)
is held as it was, and is landed in parts (below); so is one that is red again
without the story left out, which then names no suspect. A red batch whose
first run named its story is not given a fixer before the green rest lands. A
fixer that failed on a red batch whose cause it did name gives its commits to
`fixer.bundle` as always, and its word counts for nothing: the batch's line is
green, not fixed.

### Rigs land side by side

One rig has one landing at a time; different rigs do not wait for each other.
Each due rig is landed on its own, and while any landing runs, the job looks
again every minute for a rig that has fallen due and lands that one too. A slow
rig's batch (lampas, whose grist smoke can run most of an hour) does not hold
back a fast rig's: a rig that was queued and due during it is landed at once.
What keeps them from colliding is the locks, not the order: the rig's merge
slot is per rig, and the host's gate lock stays one for all rigs, so the rigs'
tests still run one after another (and under `contrib/mw-heavy`, as always).

### After a batch is held red

A batch of several stories held red after its tests ran (one that could not name
the one story whose tests failed, or that was red again without it: see above) is
landed again by the follower at once, not after `batch_every`, and in parts, not one story a batch
(28 one-story batches cost 26 gate-hours in 4.3 days, mw-6ww.119.11.1.2). The
parts are read from the batch's line in `metrics/factory.jsonl`:

- The line names a `suspect` when the batch's fixer named one story as the
  cause (its `CAUSE <story>:` lines; else the one story of its `TOUCHED`
  lines), or when one story's branch alone changed the files of the failing
  tests (read from the gate's output). The suspect is left out: the others land
  as one batch (`mw land <rig> --only <ids>`), then it lands alone.
- With no suspect the stories land as two halves, oldest first.
- A part of several held red again goes one story at a time, stopping at the
  first held red alone; that story is left out of later batches until the rig
  lands again (as `mw-gq6.1246` made it). A part held does not stop the next.

Every part is a whole gate on the batch as merged, so what is pushed has passed
as always; a wrong suspect costs one more gate, never a landing. A story the
fixer named is a guess, never recorded as the cause.

By hand: `mw land <rig> --without <story>` makes one batch of the rest of the
queue, `--only <story>[,<story>]` of just those, and `mw land <rig> --set-aside
<story> --why "..."` takes a story out of the queue and lands nothing: its
branch is kept, its claim is given back held, and a `SET-ASIDE` comment carries
the why. The next `mw land` takes the rest as one batch; `mw release <story>`
puts it back.

### When a batch is held

Every way a batch is held, who it holds and what frees them. "Held" means
nothing was pushed and every story named stays queued (`run=queued`, open and
claimed), unless it was handed back to a Builder (see above). The middle column says whether one story's trouble holds stories
that are green: a batch that sets branches aside for conflicts lands the rest
first (above), and so does a red batch that can name the one story whose tests
failed, also when its fixer fails (above); a red batch that cannot name one story
still holds the green ones for one gate and then lands in parts (below), and one
story's unreadable changelog entry holds the batch.

| Case | Who is held | What frees it |
|---|---|---|
| Some branches merged, others set aside for conflicts or AI attribution | No one: the merged ones land; one that conflicts with main alone is handed back, the others stay queued with a note | The next batch takes the queued ones; a second set-aside hands a story back |
| Every branch set aside for conflicts, and no fixer is started (host gives way, no room, harness hold) | The stories set aside, which are all of that batch | The host calm again; the next batch, after `batch_every`; one set aside a second time is handed back |
| Every branch set aside for conflicts, and the fixer fails (out of turns, error, killed, stopped by the harness) | After a harness stop, the stories set aside; after a failure, no one. The fixer's uncommitted files are kept as `fixer-uncommitted.diff` | After a failure: each story is handed back to a Builder; after a harness stop, the next batch gives them a fixer once its hold has passed |
| The rig's tests are red on the merged part, and one story's branch alone changed the failing tests' files | That story only: it is left out with a note naming its failing tests, and the green rest is tested as merged without it and lands in the same batch | The story is taken by the next batch; queued alone and red again it is held red alone |
| The rig's tests are red on the merged part, and no one story can be named | The whole merged part: one story's red holds the green ones for this one gate | The follower lands again at once, in parts: two halves, else one story at a time; the story held red alone is left out until the rig lands again |
| The tests are red and the fixer fails (out of turns, error, killed), and it or the failing tests' files name one story | That story only, as above: its fixer's commits are kept in `fixer.bundle`, the green rest lands without waiting for the drain | The story is taken by the next batch |
| The tests are red and the fixer ends badly with no one story named (fails, leaves changes uncommitted, drops a merged story's change, or its re-gate is red) | The whole merged part, as above | The same parts rule, the suspect the fixer named left out when it named one; a fixer stopped by the harness is tried again once the stop has passed |
| The tests are red again without the story left out | The whole merged part that was made again, the story left out among the stories queued | The parts rule, with no suspect: two halves |
| The tests are red and no fixer can start (host gives way, no room, harness hold) | The whole merged part | The host calm again, then the parts rule |
| Main is red: the failing tests fail on the target branch alone | The whole batch, not the stories' fault | The bead that carries main's red is fixed; the next batch |
| The rig's tests could not be run (host busy, host gives way, command did not start) | The whole batch, not the stories' fault | The next landing, when the host is calm |
| The changelog entry of one merged story could not be read, or the version could not be raised | The whole batch: one story's unreadable comments hold the rest | The next landing, when the tracker or the files read again |
| The push is refused (the target moved on) or fails | The whole batch, not the stories' fault | The next landing, made on the target as it now is |
| A landing could not be opened, or its commit could not be read | The whole batch, not the stories' fault | The next landing |
| A story is held red alone since the rig last landed | That story only: it is left out of later batches | A batch of the rig lands; or nothing else is queued |
| `mw land <rig> --without <story>`, or `--set-aside <story>` | That story only: the rest land as one batch | A `--without` story is taken by the next batch; a `--set-aside` story by `mw release <story>` |

## The fixer

A batch of which every branch was set aside for its conflicts, or whose tests
are red, is given one fixer (a batch of which some branches merged lands them
first, without a fixer, and so does a red batch whose failing tests name one
story: see above): a
Builder session (`SeatBoot.BatchFix`) in the batch's landing worktree, named
`batch-<rig>-<n>`, on the rig's fixer model, effort and turn cap. Its boot file
names every branch, which merged, git's conflict list and the tests' last
lines. When the rig raises a version (`version_files`) and keeps a changelog
(`changelog_files`), it also names, for each story, the What's new line mw writes
into those files at landing (the same reading `mw next` makes: the story's
newest `What's new:` comment, else its title), and says that the line and the
version land after the fixer exits, so a test that reads those files meets them
and a `What's new:` comment on the story changes the line. The Held mail of a
batch whose failing tests name a changelog file says mw wrote that line itself,
and prints it.

It may merge the branches that were set aside, resolve conflicts keeping both
stories' intents, fix code or tests, and run the suite.

It may not push; revert a branch; drop a story's change; or delete, skip or
loosen a test without saying so on that story. It comments on each story it
touched, naming the files.

When it exits, mw runs the tests on the batch itself, because the fixer's word
is not the gate. Green lands the batch with "resolved by the fixer" in each
affected ledger line, and the fixer's word on the cause recorded as
unbisected. Red holds the batch: every story stays queued, the Mayor is mailed,
and no second fixer is started. A fixer the harness stops for a limit holds the
batch until the reset, and a held batch is not tried again until `batch_every`
has passed.

A fixer that ends on anything but success (it ran out of turns, ended in an
error, or its session was killed) is a failed fixer, and the batch is held. What
it left uncommitted is discarded from the landing and kept as a diff,
`fixer-uncommitted.diff`, in the batch's records beside `fixer.bundle`; mw does
not test what it left. The outcome says how it ended ("the fixer ran out of
turns") and the Held mail names the story set aside as the cause.

## Records

- `runs/batch/<rig>/<n>/` in the vault keeps the batch: merge order, gate log,
  fixer result, and, for a held batch, the commits its fixer left
  (`fixer.bundle`). A fixer commit that touches one story's files only is also
  pushed to that story's branch at the origin, so the next batch merges it; the
  Held mail says where. One that spans stories stays in the bundle alone.
- `metrics/factory.jsonl` gets one batch line for each batch: stories, gate
  runs and minutes, the fixer and its tokens, the outcome, red minutes.
- A ledger line for each story, naming the batch.

## The stop rule

The trial is judged per week against the baseline of lampas 10-11..15, after
its big files were split. Flip the rig to `each` and keep the code if:

- two batches are held in a row; or
- more than 25% of batches need a fixer; or
- tokens per landing rise more than 20%, fixer fuel included; or
- bugs per landing from the nightly suite rise more than 50%: the canary for
  a fixer that weakens tests; or
- anyone finds that a story's intent was dropped (at once, with no threshold).

The go/no-go is after 7 days of the trial.

## Rollback

Set `landing = "each"` in the rig's file. The queue is then drained singly: the
follower runs `mw land <rig> --one` for the oldest queued story until none
remains, and new close-outs land as before. Flip it only once both hosts run a
rebuilt mw; an older `mw next` would still merge directly.
