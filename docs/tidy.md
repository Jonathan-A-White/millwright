# mw tidy: what it may do

`mw tidy` closes what is plainly finished. Everything it does is one of the
rows below, at these ages; nothing else. The bounds are constants in
`application/tidy.go` (`TidyAnswerAge`, `TidyReadAge`), and widening one is a
change to that file and to this table together, so it shows in a diff.

| # | What it acts on | Bound | The act |
| --- | --- | --- | --- |
| 1 | A mail bead whose subject starts `Answer: ` | Older than 1 day, read or not | Close it |
| 2 | A mail bead the mailbox holds read | Older than 7 days | Close it |
| 3 | A `postern.question.<bead>` note | Its bead is closed | Clear the note |

Each act writes one line on what it touched, `Tidied by mw tidy: <why>`: the
close reason of a mail bead, a comment on the bead of a cleared note. Each act
also adds one line to the Millhand's tick log (`tidy: <kind> <id>: <line>`),
which the tick's own counts and resume rules read past.

It never:

- closes a story, an epic, a map or a hitl bead: mail is listed and closed as
  mail, and the beads adapter refuses to close any bead that is not of type
  mail;
- deletes anything: closed mail stays in the tracker;
- clears a question note whose bead is open, or a bead it cannot read.

Row 2 is the row to know about: `mw mail read` closes a message, so a message
the beads mailbox holds read is already closed and row 2 finds nothing to act
on there today. It is written against the mailbox's own word for read, so that
it acts as soon as mail has a read state that leaves it open.

The Millhand's tick runs it after the sweep, and `mw tidy` runs it by hand;
`--dry-run` lists what it would do and changes nothing. See
`features/tidy.feature`.
