# Builder's memory of each rig

A rig is kept as facts: a folder `<rig>/` with `about.md` and a `facts/` folder of one file per fact. At boot a Builder reads the about text and the current facts, rendered compactly under a budget of 8000 bytes, so every byte is paid for on every story. The rest of what the folder holds is for the Mayor and costs a Builder nothing. (A rig with no `facts/` folder is still the one file `<rig>.md`, as it always was.)

```
rigs/<rig>/about.md          what the rig is, read before its facts
rigs/<rig>/facts/<slug>.md   one fact; the slug is its identity
```

**A fact file** is a front matter, then one sentence:

```
---
subject: bd
kind: gotcha            # gotcha or decision
status: current         # current, recheck, superseded or retired
source: mw-gq6.43       # a bead, rig@sha or mayor:<date>
since: 2026-10-01
---

Point every bd call at the vault with -C, as its own Bash call.
```

A superseded fact carries `superseded-by:`, a retired one `retired:` and a `reason:`. Only a current fact is read at boot; the others stay on disk with their reasons.

**What goes in.** Facts that would have saved a Builder real time: a command that needs a flag, a test that cannot run in parallel, a path that looks right and is not, a tool whose error means something else; and choices already made, so they are not made twice. One sentence each.

**What stays out.** What the rig's own docs already say, how the code is laid out, history, incident notes, and anything private. If a fact is true of the whole factory rather than one rig, it belongs in the Builder charter's procedures, not here.

**How it changes.** A Builder never edits these files. It proposes typed facts under 'For the rig memory:' at the end of its closing comment, at most two, one per line, each in one of five forms, or the single word 'nothing':

- `gotcha [subject]: <sentence>`
- `decision [subject]: <sentence>`
- `supersede <slug>: <sentence>` (a new sentence for a fact the rig already has)
- `retire <slug>: <reason>`
- `recheck <slug>: <why>` (a fact the Builder found doubtful)

`mw next` carries each line into the Landed mail as the `mw memory` command that places it, with the story as its source, and flags a line it cannot read as `MALFORMED`. The Mayor (or the Deputy, on his mail) places what is worth keeping with `mw memory add`, retires or supersedes with `mw memory retire` and `mw memory supersede`, and never deletes: a fact's reason outlives it. When the render nears 8000 bytes, retire or supersede rather than trim.
