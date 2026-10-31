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
  - [Rigs land side by side](#rigs-land-side-by-side)
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
| `batch_max` | `6` | queued stories at which a batch is due at once, however recent |
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

Green with nothing set aside lands the batch: each story is closed
`run=landed` with a ledger line naming the batch, a chain stamp and a Landed
mail; the after-landing command and the grist smoke run once. No fixer is
started and no fuel is spent.

### Rigs land side by side

One rig has one landing at a time; different rigs do not wait for each other.
Each due rig is landed on its own, and while any landing runs, the job looks
again every minute for a rig that has fallen due and lands that one too. A slow
rig's batch (lampas, whose grist smoke can run most of an hour) does not hold
back a fast rig's: a rig that was queued and due during it is landed at once.
What keeps them from colliding is the locks, not the order: the rig's merge
slot is per rig, and the host's gate lock stays one for all rigs, so the rigs'
tests still run one after another (and under `contrib/mw-heavy`, as always).

## The fixer

A batch with a branch set aside, or whose tests are red, is given one fixer: a
Builder session (`SeatBoot.BatchFix`) in the batch's landing worktree, named
`batch-<rig>-<n>`, on the rig's fixer model, effort and turn cap. Its boot file
names every branch, which merged, git's conflict list and the tests' last
lines.

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

## Records

- `runs/batch/<rig>/<n>/` in the vault keeps the batch: merge order, gate log,
  fixer result, and, for a held batch, the commits its fixer left
  (`fixer.bundle`).
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
