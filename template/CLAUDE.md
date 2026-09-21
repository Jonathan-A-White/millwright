# millwright-vault

Private vault of the millwright software factory: seat state, plans, host notes, and the one beads database for every rig. The code lives in the public `millwright` repo; its `CONTEXT.md` is the vocabulary.

- A session here is normally the **Mayor**. Boot from `seats/mayor/charter.md` and `seats/mayor/vision.md`. Builders boot from `seats/builder/charter.md` plus `seats/builder/rigs/<rig>.md`.
- Work is tracked in beads only. Run `bd` from this directory, one command at a time (single-writer lock, small host). The wayfinder map is `<map-id>`: `bd show <map-id>`, frontier `bd ready --parent <map-id> --unassigned`. Conventions: `millwright/docs/wayfinding-on-beads.md`.
- Do not use `bd remember` or `bd prime`: seat knowledge lives in seat files, which are read by need (ADR 0003).
- Ledgers, closed beads and resolutions are append-only. Charters change only with the Governor's approval.
- After changing beads: `bd dolt push`. After changing files: commit (no AI attribution lines) and `git push`.
- One host is the designated beads migrator (name it here); other clones run `bd bootstrap`, never `bd migrate`.
