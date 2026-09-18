# Seats are folders in the vault, read by need

A seat is a folder of markdown in the private vault, split by *when* it is read: `charter.md` is always loaded at boot; `rigs/<rig>.md` only for the rig being worked; `ledger.md` and `postmortems/` never at boot. We chose files over beads memories (`bd remember`) and over one all-knowing seat document because every fresh session pays about 23K tokens to prime before it does any work, and each beads memory adds to every future session forever; what is always loaded must stay tiny, and what is history must cost nothing until someone asks. Files in the vault are also what the Governor reads in Obsidian, so seat state and human-readable record are the same thing.

The ledger is append-only and never edited: one falsified record makes every record suspect, and distrust is paid for in tokens. Charter edits require the Governor's approval; a session may only append to its ledger and tend its own rig memories.

## Consequences

Booting a session is `mw`'s job: charter + rig memory + the story's bead, passed as an appended system prompt so it sits in the shared cached prefix. `bd prime` is not used at boot, and the instructions file beads generates is replaced with a minimal one. Beads holds only work; `bd kv` holds `mw`'s machine config.
