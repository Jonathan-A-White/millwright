# Demo: catch what he finds on his phone before he does

The last story (mw-it6qk5.6) of the epic mw-it6qk5. After the one-week Tester
trial on Lampas, the Governor reads what the trial found on his phone, says
`Looks good`, and taps keep or stop for the Tester. This page is the script for
that demo. The Governor does the numbered steps; the Mayor does the two things
marked *Mayor* before he starts.

The trial runs from 2026-10-09 21:13Z to 2026-10-16 21:13Z (the `until` of the
`[tester]` table in the home's `~/.config/mw/config.toml`; see README, *The
Tester trial*). Do not run this demo before `until` has passed.

## Before the Governor starts (Mayor)

1. Once the week is over, run `mw tester report` on the home and paste its
   whole output as a comment on this demo bead, mw-it6qk5.6. This is where the
   trial report lives: the Governor reads it in the bead's comments.
   `mw tester report` reads and writes nothing else; `--since` is a week before
   `until` by default, which is the whole trial.
2. Pick one Tester story that found something (a `Test: <title>` story under a
   Lampas epic, closed, whose landed story carries a `FINDINGS` comment with
   shots), and one [bug] story landed after mw-it6qk5.1 that has a
   `Regression test:` line and a `Class:` line in its closing comment. Name both
   by id in a comment on this bead, so the Governor can open them directly.
   Send him a Postern message that the demo is ready.

## The demo (Governor, on the phone)

1. Open the **Postern** app and open the bead **mw-it6qk5.6**, the one titled
   *DEMO (the epic's last story)*. Open its **Comments**.
   Right looks like: a comment pasted from `mw tester report`, headed
   `Tester trial since 2026-10-09 21:13 UTC, until 2026-10-16 21:13 UTC`, with a
   `lampas` section.

2. Read the report's lines for Lampas.
   Right looks like: the number of landings in the trial, the number of Tester
   runs, the findings as `N (B bug, T taste)`, the number of [bug] stories filed
   from findings, and the fuel of each Tester run in tokens. One Tester run per
   landing that had a HOW TO CHECK IT; the counts add up (a run's findings
   are bugs plus taste).

3. Open the Tester story the Mayor named in the comment under the report (a
   story titled `Test: <a Lampas story's title>`), then open the *Landed story:*
   id written on its first line.
   Right looks like: the Tester story is closed; on the landed story, the newest
   comment starts `FINDINGS`.

4. Read that `FINDINGS` comment and open the shots it names.
   Right looks like: each item is marked `[bug]` or `[taste]`, says what he would
   see, gives the steps to get there, and ends with a screenshot path (a phone
   screen, 390 by 844). The comment closes with `Tester fuel: <tokens>`. The
   shots show the screen the item describes. If the run found nothing the
   comment reads `FINDINGS: none`, which is also right.

5. Open the [bug] story the Mayor named (a story whose title starts `[bug]`,
   landed after mw-it6qk5.1) and read its closing comment.
   Right looks like: a line `Regression test: <file:line>` naming the test that
   reproduces his report and failed on main before the fix, and a line
   `Class: <the kind of bug>; sweep: <where else in the rig it can happen, fixed
   here or filed with its id>`.

6. Reply in the bead mw-it6qk5.6's channel with the words `Looks good`.
   Right looks like: the demo closes, with a comment quoting him, and the Mayor
   is mailed `Closed: mw-it6qk5.6 on his Looks good` (only that exact text,
   any case, one final `.` or `!`, closes a demo; anything else is a plain
   comment).

7. Then answer the **keep or stop** card the Mayor sends about the Tester, by
   tapping **Keep** or **Stop**.
   Right looks like: the card shows as answered, and the Mayor replies with what
   he did. **Keep** means the Tester stays on for Lampas: the Mayor moves the
   `until` of the `[tester]` table later, and Lampas landings with a HOW TO CHECK
   IT keep springing Tester stories. **Stop** means it ends: the trial's `until`
   passes by itself and nothing further is sprung; the Mayor also deletes the
   five `[tester]` lines from `~/.config/mw/config.toml` so a later `mw next`
   cannot spring one. Either way, no Tester story already filed is touched, and
   nothing the Tester did needs undoing (it commits nothing).

## Where the report is pasted

On this bead, mw-it6qk5.6, as a comment, by the Mayor, when the week ends. The
command is `mw tester report`; add `--since <day or UTC time>` to look at a
different window.

## If something is wrong

- No `lampas` section, or 0 landings: tell the Mayor; he re-runs the report with
  `--since 2026-10-09`, which is the day the trial began.
- 0 Tester runs with landings counted: no landing in the week had a HOW TO
  CHECK IT with numbered steps, or `mw next` ran on a host that did not have
  the `[tester]` table. This is a finding about the trial, not a failure of the
  demo; say `Stop` or `Keep` on the evidence you have.
- A Tester story with a commit on its branch is refused by `mw next` with the
  reason `tester-committed`, and one with no findings with `no-findings`; the
  Mayor reports either on the bead.
