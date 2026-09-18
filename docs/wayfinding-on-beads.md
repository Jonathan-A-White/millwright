# Wayfinding operations on beads

How the [wayfinder](https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md) skill's tracker operations are expressed in this factory. Beads is the only tracker; GitHub Issues and Linear are not used.

| Wayfinder concept | Beads expression |
| --- | --- |
| The map | An `epic` bead labelled `wayfinder:map`. Its description is the map body. |
| A ticket | A child bead of the map (`--parent <map>`), labelled `wayfinder:research`, `wayfinder:prototype`, `wayfinder:grilling` or `wayfinder:task`. Its description is `## Question`. |
| HITL / AFK | Label `hitl` or `afk`. |
| Blocking | `bd dep add <blocked> <blocker>`. Create all tickets first, wire in a second pass. |
| Frontier | `bd ready --parent <map> --unassigned` |
| Claim | `bd update <id> --claim`, first, before any work. |
| Resolution | `bd comment <id> "<answer>"`, then `bd close <id> --reason "<one-line gist>"`, then add the gist line to the map's **Decisions so far**. |
| Assets | Files in the rig (`docs/research/`, `docs/adr/`, prototypes), named in the resolution comment. Never pasted into the bead. |
| Ruled out of scope | `bd close <id> --reason "out of scope: <why>"` plus one line in the map's **Out of scope**. |
| Load the map | `bd show <map>`, then `bd children <map>` for titles only. Zoom with `bd show <id>` on demand. |

Refer to beads by title in anything the Governor reads; the id rides along in parentheses, never alone.

One ticket per session, research tickets excepted.
