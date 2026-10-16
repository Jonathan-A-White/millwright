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

## Filing a millwright story

A story on the `millwright` rig changes the factory, so it names the metric it should move. Its acceptance criteria carry one line:

```
Expect: <metric> <up|down>
```

The metric is one of the health report's: `refusals` (the share of ledger rows that were refusals), `attempts/landing`, `bugs/landing`, `escaped bugs` (the bugs filed that a landing let through), `dispatch-to-landing`, `tokens/landing` or `$/landing`. Write `Expect: none, because <reason>` for a change that moves none of them (a doc, a rename). A story with no Expect line is measured against nothing and counted in one line of the report.

`mw report` lists every millwright landing of the last 30 days under FACTORY CHANGES, with the metric for the whole factory in the 7 days before the landing day and the 7 days after it (the landing day is in neither; "too soon" until the 7 days after are over). A change that moved the wrong way by more than a 10% band of the week before is marked WORSE. Each change also shows its escaped bugs: the `[bug]` stories of its rig filed on its landing day or in the 14 days after that name its story id, "too soon" until the 14 days are over. `mw report send` mails the Mayor about each WORSE change once, and appends the day's key metrics to `metrics/factory.jsonl` in the vault, one snapshot a day, so any past week can be compared with another. `mw land` appends a line of its own to the same file for every batch of a rig that lands in batches (`kind` is `batch`, with `stories`, `gate_runs`, `gate_minutes`, `fixer`, `fixer_tokens`, `outcome` of green, fixed or held, and `red_minutes`), the numbers the Land Rush stop rule is read from; `mw status` shows the newest of them under LAND RUSH.
