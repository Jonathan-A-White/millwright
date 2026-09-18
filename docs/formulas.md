# Formulas

A formula is the step-by-step procedure a story is worked by (see
`CONTEXT.md`). This rig's formulas live in `formulas/*.formula.json`.

## Choosing a formula per story

Formula is one of the fields of a story's **path** (rig, target branch,
harness, model, effort, formula, host), set by the Mayor when it plans the
epic and written into the story bead's `formula` metadata (as `mw file`
does for every other path field). Today there are two:

- `chore` — do the change, verify it, commit, closing comment. Use for
  stories with no meaningful test-first step (docs, config, plumbing).
- `tdd-feature` — understand the story and the code it touches, write the
  failing test or feature first, implement to green, run the full build/
  test/vet, self-review the diff against the acceptance criteria, commit,
  closing comment. Use for anything that changes behavior.

Both take two variables: `story` (the story bead id) and `title` (the
story's title).

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
`bd formula --help`). For the vault's own database that directory is
`/root/millwright-vault/.beads/formulas/`. Installing a formula means
copying its `.formula.json` file there — this is the Mayor's job, not a
Builder's, and is not done by any story's own commit:

```sh
mkdir -p /root/millwright-vault/.beads/formulas
cp formulas/tdd-feature.formula.json formulas/chore.formula.json \
   /root/millwright-vault/.beads/formulas/
```

Verify with `bd -C /root/millwright-vault formula list`, which should show
both by name.

## Verifying a formula

`scripts/check-formulas.sh` (`make check-formulas`) checks every formula
under `formulas/` end to end — listed, cooked, poured with only its first
step ready — against a throwaway beads database in a temp dir. It never
touches the vault. Run it after adding or changing a formula, before
asking the Mayor to install it.
