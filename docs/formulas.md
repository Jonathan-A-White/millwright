# Formulas

A formula is the step-by-step procedure a story is worked by (see
`CONTEXT.md`). This rig's formulas live in `formulas/*.formula.json`.

## Choosing a formula per story

Formula is one of the fields of a story's **path** (rig, target branch,
harness, model, effort, formula, host), set by the Mayor when it plans the
epic and written into the story bead's `formula` metadata (as `mw file`
does for every other path field). Today there are three:

- `chore` — do the change, verify it, commit, closing comment. Use for
  stories with no meaningful test-first step (docs, config, plumbing).
- `tdd-feature` — understand the story and the code it touches, write the
  failing test or feature first, implement to green, run the full build/
  test/vet, self-review the diff against the acceptance criteria, commit,
  closing comment. Use for anything that changes behavior.
- `tester` — the Tester trial's, never planned by hand: mw next files the
  story after a landing. Build and serve the landed commit, drive it at
  390x844 by its HOW TO CHECK IT and five adversarial moves, write FINDINGS
  on the landed story. Commits nothing.

All take two variables: `story` (the story bead id) and `title` (the
story's title). `tdd-feature` takes a third, `kind`, which `mw next` sets to
`bug` when the story is a bug (its type is `bug` or its title starts `[bug]`)
and leaves at its default, `feature`, otherwise.

A bug story is poured with two different steps: `red-bug` and `close-bug` take
the place of `red` and `close` (bd keeps a step by its `condition` on `kind`).
Its red step has the Builder write the regression test from his report (the
screen, the words, the steps he gave), run it on the branch before any fix and
see it fail for the reason he saw; if it cannot fail because the fake hides the
bug, the Builder says so and makes the fake honest enough to fail. Its closing
step asks for two more lines ahead of HOW TO CHECK IT: `Regression test:
<file:line>` and `Class: <the kind of bug, one line>; sweep: <where else in the
rig it can happen: fixed here, or filed as a [bug] with its id>`. The
Builder reports the siblings it finds and the Mayor files them, as for any
problem a Builder discovers. A vault whose copy of `tdd-feature` predates
`kind` still pours, the plain way, until the vault's copy is installed.

A Builder's session pours its story's formula at start of work:

```sh
bd mol pour <formula> --var story=<story-id> --var title="<story title>"
```

This creates a root bead (title = `title`) plus one child bead per step,
wired with `depends_on` so only the first step is ever ready at a time
(`bd ready --parent <root-id>`). The Builder works steps in order, closing
each one as it's done — closing a step is what makes the next one ready.

## Installing formulas into the vault

Formulas are searched from `<resolved-beads-dir>/formulas/` first (see
`bd formula --help`). For the vault's own database that directory is the vault's
`.beads/formulas/`, so a change landed in `formulas/` is live only once the
vault's copy is level with it (mw-gq6.314; before that nothing installed them and
the copies sat at the 2026-09-18 originals).

The home installs them itself. When its factory checkout is level with origin's
`main` (the self-update, `application/selfupdate.go`, run by `mw millhand tick`
and `mw dispatch`), any `formulas/*.formula.json` that is missing from the vault
or differs is copied into `.beads/formulas/`, committed in the vault as
`Install formulas from millwright <short sha>` (the vault's own commit, authored
`mw@<host>`) and pushed. Nothing is committed when the copies are equal. If the
vault has uncommitted changes under `.beads/formulas`, it is left alone and the
tick's line says so; a host that is not home never installs. `mw doctor`'s
**formulas** check faults when the vault's copies are still out of step after
more than one self-update.

The way back is `git revert <that vault commit>` in the vault. The hand step
below is for when the self-update cannot run (a build that fails does not stop
the install, but a host with no `[after_landing]` command for `millwright` runs
no self-update at all):

```sh
cp formulas/*.formula.json <vault>/.beads/formulas/
git -C <vault> add .beads/formulas
git -C <vault> commit -m "Install formulas from millwright <short sha>"
```

Verify with `bd -C <vault> formula list`, which should show each by name.
`tester` is the Tester trial's (README, "The Tester trial"): a story it works
commits nothing.

## Verifying a formula

`scripts/check-formulas.sh` (`make check-formulas`) checks every formula
under `formulas/` end to end — listed, cooked, poured with only its first
step ready — against a throwaway beads database in a temp dir. It never
touches the vault. Run it after adding or changing a formula, before
landing it.
