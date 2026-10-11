# Demo: the factory's health, measured and posted daily

The last story (mw-2zh33g.6) of the epic mw-2zh33g. A cause is recorded on a real
refused story, `mw report` runs on the laptop, and the daily post is read on the
Governor's phone in the **Factory** channel; he says `Looks good`. This page is the
script. The Governor does the numbered steps; the Mayor did the things marked *Mayor*.

## Before the Governor starts (Mayor)

1. Name a real refused story of the week and its class. This run used mw-gq6.325
   (refused `merge-conflict` on 2026-10-10, because docs/codemap.md conflicted):

   ```
   mw cause mw-gq6.325 tooling "docs/codemap.md conflicted on merge (no merge driver; every millwright story edits it at its byte cap), so the landing was sent back to rebase"
   ```

2. Run `mw report` and `mw report send --dry-run`, and keep the texts as
   `runs/mw-2zh33g.6/report.txt` and `runs/mw-2zh33g.6/summary.txt` in the vault. Both
   fit 60 columns. The summary, as the phone gets it, is below.

```
Factory, 7 days to 2026-10-11
QUALITY landed 623 ↑ better, refused 164 (21% ↑ worse)
  attempts 1.22/landing ↑ worse, bugs 0.40/landing ↓ better
SPEED par x1.0 median, x2.3 p90
  dispatch to landing 23m51s
FUEL tokens/landing 3,676,383 ↑ worse
  $/landing 1.39 ↑ worse
CHEAP WINS
1. 157 refusals have no cause: mw cause them
2. merge-conflict is 75% of legend refusals: look for what …
3. mw-bsf54t.4 +22 more used 3+ attempts: split them
```

## The demo (Governor, on the phone)

1. Open the Factory channel at or after 12:00Z (the post comes from the home, once
   a UTC day; `mw-report.timer` sends it).
   Right looks like: a new message headed `Factory, 7 days to <today>`.
2. Read the four sections in order: **QUALITY**, **SPEED**, **FUEL**, **CHEAP WINS**.
   Right looks like: QUALITY first and CHEAP WINS last; QUALITY gives landed and refused
   with the refusal rate, attempts per landing and bugs per landing, each with an arrow
   (↑ or ↓) and the word `better` or `worse`; SPEED never comes first; CHEAP WINS holds
   up to three numbered lines, or `none seen`.
3. Check the width.
   Right looks like: every line fits the screen without scrolling sideways.
4. On the laptop, run `mw report --rig lampas`.
   Right looks like: the longer report for one rig, QUALITY then SPEED then FUEL then
   CHEAP WINS, with lampas's refusal reasons and its causes counted by class.
5. Say `Looks good`.
