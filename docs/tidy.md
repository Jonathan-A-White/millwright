# mw tidy: what it may do

`mw tidy` closes what is plainly finished. Everything it does is one of the
rows below, at these ages or on these conditions; nothing else. The bounds are constants in
`application/tidy.go` (`TidyAnswerAge`, `TidyReadAge`), and widening one is a
change to that file and to this table together, so it shows in a diff.

| # | What it acts on | Bound | The act |
| --- | --- | --- | --- |
| 1 | A mail bead whose subject starts `Answer: ` | Older than 1 day, read or not | Close it |
| 2 | A mail bead the mailbox holds read | Older than 7 days | Close it |
| 3 | A `postern.question.<bead>` note | Its bead is closed, and the note is not a decision card without an ANSWER after its QUESTION | Clear the note |
| 3a | A decision card (a note with a txid or question) nobody answered, on a closed bead | Always, once per card | Mail the Mayor `Unanswered card on closed <bead>: <question>`, comment the bead, keep the card |
| 4 | A formula's step or root left open (`mw-mol-*`) | The story its first step names (`Understand <story>: ...`, `Do <story>: ...`) is closed, and no story not closed records the formula as its own | Close it, the steps before the root |

Each act writes one line on what it touched, `Tidied by mw tidy: <why>`: the
close reason of a mail bead or a formula's bead, a comment on the bead of a
cleared note. Each act
also adds one line to the Millhand's tick log (`tidy: <kind> <id>: <line>`),
which the tick's own counts and resume rules read past.

It never:

- closes a story, an epic, a map or a hitl bead: mail is listed and closed as
  mail, and the beads adapter refuses to close any bead that is not of type
  mail;
- deletes anything: closed mail stays in the tracker;
- clears a question note whose bead is open, or a bead it cannot read, or a decision card nobody answered (row 3a: the Mayor re-asks it on an open bead or clears it with `mw postern clear`, below);
- closes a step of an open or running story's formula, or of a formula whose
  story no step names: a formula's beads are listed and closed as a formula's,
  and the beads adapter refuses to close any bead without the `-mol-` infix.

Row 2 is the row to know about: `mw mail read` closes a message, so a message
the beads mailbox holds read is already closed and row 2 finds nothing to act
on there today. It is written against the mailbox's own word for read, so that
it acts as soon as mail has a read state that leaves it open.

Row 4 is the sweep behind what `mw next` and `mw retry` do themselves
(mw-gq6.438): a close-out closes what is left open of the story's formula once
the story lands or is refused, and a retry closes it before the claim is given
back, so the next attempt is poured a fresh one. What either could not close,
and the formulas of stories closed before that, row 4 closes.

The Millhand's tick runs it after the sweep, and `mw tidy` runs it by hand;
`--dry-run` lists what it would do and changes nothing. See
`features/tidy.feature`.

## Clearing an answered card on an open bead

`mw tidy` never clears a question note on an open bead (row 3), so a card the
Governor has answered stays in his Needs you while its bead stays open (it was
approved and released, or fixed another way). The command for that is
`mw postern clear <bead> --why "<text>"`. It does three things and changes
nothing else: it clears the bead's `postern.question.<bead>` note, comments the
bead `Card cleared by mw postern clear: <why>`, and posts a short reply under
the card's txid saying it is cleared and why, so his phone thread closes the
loop. The bead's status and fields are never touched. A bead with no question
note is refused, naming it. `--dry-run` prints the three acts and does none.

A second card asked on a bead keeps the first in the note's `earlier` list rather
than overwriting it. `--txid <txid>` withdraws only the card asked under that
txid (the reply goes under that card; the bead's other cards and their note
stay; withdrawing the newest lets the one it overtook lead the note again). A
txid the note does not hold is refused, naming it.
