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
- clears a question note whose bead is open, or a bead it cannot read, or a decision card nobody answered (row 3a: the Mayor re-asks it on an open bead or clears it by hand);
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
