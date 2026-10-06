# Seats

A seat is a role the factory fills with a fresh session each time. It has a **charter** (always read at boot: who it is, what it may and must never do), and a folder of what earlier sessions in the seat learned. Only the Governor approves changes to a charter. Nobody is blamed when something goes wrong: a misstep is a fact about a seat or the machinery, written down truthfully with the smallest fix.

| Seat | What it is for | Boots from |
|---|---|---|
| Mayor | The seat the Governor talks to: records what he wants, grills it sharp, turns it into epics of stories, releases what he approved, verifies landings. Never works a story. | `mayor/charter.md`, `mayor/vision.md`, `mayor/procedures.md`, the newest file in `mayor/handoffs/` (shaped by `TEMPLATE.md`) |
| Deputy | The Mayor's hands while the Governor talks: bead writes, filings, landing checks, releases, mail. Woken on need; decides nothing. | `deputy/charter.md`, its own handoffs |
| Builder | Works one story in one rig in one fresh session, until the acceptance criteria pass when run. | `builder/charter.md`, plus `builder/rigs/<rig>.md` (see `builder/rigs/README.md`) |
| Millhand | The hands and eyes on one host: diagnoses what `mw` cannot, does hand steps when told, watches the other hosts from outside. Woken on need. | `millhand/charter.md`, its own handoffs |

`builder/ledger.md` is the Builder's append-only record, one line per story. `mayor/vision.md` is the Governor's own to write.

## Clerks are not a seat

A Clerk is a cheap helper (Sonnet at high effort) that a seat starts inside its own session for clerical work: many lookups, a bulk of comments, a long output reduced to an answer. It has no charter, no ledger and no memory of its own; it exists for one errand and its report. The seat that sent it owns the result: give the Clerk a numbered question list and a word limit, say what it may not touch, and check its report before relying on it. Anything deterministic is not a Clerk's work either: it belongs to `mw`, at no cost in tokens.

## Adapting these

These files are generic. Replace the placeholders, add what your hosts and rigs need, and keep each charter short: it is read at every boot. Put a rule you learn in `mayor/procedures.md` (or the Builder's rig memory), not in a charter, and never in a handoff.
