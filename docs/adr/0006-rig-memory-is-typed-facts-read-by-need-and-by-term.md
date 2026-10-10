# Rig memory is typed facts, read by need and by term

ADR 0003 made a Builder's memory of a rig one file, `rigs/<rig>.md`, loaded for the rig being worked and held to a byte budget. This extends it: the file becomes a folder of facts, one file each, with an identity, a source and a status. The boot renders the current facts under the same budget; the rest is found by term rather than loaded (Part 2 of the plan, plan 0107).

A fact is `rigs/<rig>/facts/<slug>.md`: a front matter of `subject`, `kind` (gotcha or decision), `status` (current, recheck, superseded or retired), `source` and `since`, then one sentence. The slug is its identity; `supersedes`, `superseded-by`, `retired` and `reason` say what became of it. Only a current fact is rendered at boot. A rig may also have `about.md`, read before its facts. A rig with no `facts/` folder is still one file.

A Builder still proposes and the Mayor still curates. The proposal is now typed, so it arrives in the form the Mayor's tool takes: under 'For the rig memory:' a Builder writes `gotcha [subject]: ...`, `decision [subject]: ...`, `supersede <slug>: ...`, `retire <slug>: <reason>` or `recheck <slug>: <why>`, at most two, or 'nothing'. `mw next` puts each into the Landed mail as the `mw memory` command that places it, with the story as its source, and flags a line it cannot read; the landing is never refused for one. The Mayor places with `mw memory`, retires or supersedes, and never deletes.

## Why

Three things went wrong with one file of lines. Pruning by content lost the reason: when a file neared its budget a line was cut, and with it why it had been true. Nothing marked a line stale, so a fact that had stopped being true looked like one that had not, until a Builder was misled by it. And the boot layer could only grow: every addition was paid on every story, while removal cost a judgement nobody could check.

Files with a status answer each: retiring keeps the sentence and says why, a doubtful fact is flagged `recheck` and stops being read until settled, and the budget is spent on the current facts alone, with the older ones left on disk for a search.

## Consequences

`mw memory` (add, supersede, retire, recheck, list) is the only way facts change, and none of it runs git or deletes a file. `mw status` measures the render, not the folder, and says to retire or supersede when it nears 8000 bytes. A malformed fact file is skipped at boot and named in the boot file, so one bad file does not hide the rest. The Builder charter's line about 'at most two lines' is the Governor's to amend; until it is, the kickoff prompts and the formulas' close steps carry the typed forms.
