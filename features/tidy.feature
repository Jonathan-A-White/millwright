Feature: mw tidy
  mw tidy closes what is plainly finished, within fixed bounds, and nothing
  else. It may only:

    1. close an "Answer: ..." mail bead over 1 day old;
    2. close a mail bead the mailbox holds read, over 7 days old;
    3. clear a postern question note whose bead is closed;
    4. close the beads left open of a formula poured for a story that is closed.

  Each act writes one line on what it touched, "Tidied by mw tidy: <why>", and
  one line to the tick log. It never closes a story, an epic, a map or a hitl
  bead, and never deletes. --dry-run says what it would do and changes nothing.
  The bounds are the table in docs/tidy.md.

  Background:
    Given a tidy world with the clock at "2026-09-29T12:00:00Z"

  Scenario: An Answer mail 2 days old is closed and one 2 hours old is not
    Given the mail "Answer: mw-a.1: Release" was sent 2 days ago
    And the mail "Answer: mw-a.2: Hold" was sent 2 hours ago
    When mw tidy runs
    Then tidying succeeds
    And the tidy mail "Answer: mw-a.1: Release" is closed, saying "Tidied by mw tidy: an Answer mail over 1 day old"
    And the tidy mail "Answer: mw-a.2: Hold" is still open

  Scenario: A read mail 8 days old is closed and an unread one is not
    Given the mail "Landed: an old one" was sent 8 days ago and read
    And the mail "Landed: another old one" was sent 8 days ago
    And the mail "Landed: a recent one" was sent 6 days ago and read
    When mw tidy runs
    Then tidying succeeds
    And the tidy mail "Landed: an old one" is closed, saying "Tidied by mw tidy: a mail read and over 7 days old"
    And the tidy mail "Landed: another old one" is still open
    And the tidy mail "Landed: a recent one" is still open

  Scenario: A question note on a closed bead is cleared and on an open bead it is not
    Given a tidy story "mw-q.1" that is closed
    And a tidy story "mw-q.2" that is open
    And a postern question note for "mw-q.1" and for "mw-q.2"
    When mw tidy runs
    Then tidying succeeds
    And the question note for "mw-q.1" is cleared
    And the question note for "mw-q.2" is kept
    And the tidy story "mw-q.1" carries the comment "Tidied by mw tidy: its question note outlived the closed bead"
    And the tidy story "mw-q.1" is still closed
    And the tidy story "mw-q.2" carries no comment

  Scenario: A story, an epic and a hitl bead of any age are never touched
    Given a tidy story "mw-s.1" that is open and 400 days old
    And a tidy epic "mw-s" that is open and 400 days old
    And a tidy story "mw-s.2" labelled "hitl" that is open and 400 days old
    When mw tidy runs
    Then tidying succeeds
    And the tidy story "mw-s.1" is still open
    And the tidy story "mw-s" is still open
    And the tidy story "mw-s.2" is still open
    And nothing was written to the tidy tracker

  Scenario: Each act writes a line to the tick log
    Given the mail "Answer: mw-a.1: Release" was sent 2 days ago
    And a tidy story "mw-q.1" that is closed
    And a postern question note for "mw-q.1" and for "mw-q.1"
    When mw tidy runs
    Then the tick log has 2 tidy lines
    And a tidy line says "answer mail mail-1: Tidied by mw tidy: an Answer mail over 1 day old"
    And a tidy line says "question note mw-q.1: Tidied by mw tidy: its question note outlived the closed bead"

  Scenario: A dry run says what it would do and changes nothing
    Given the mail "Answer: mw-a.1: Release" was sent 2 days ago
    And a tidy story "mw-q.1" that is closed
    And a postern question note for "mw-q.1" and for "mw-q.1"
    When mw tidy runs with --dry-run
    Then tidying succeeds
    And the tidy report says "would close answer mail mail-1"
    And the tidy report says "would clear question note mw-q.1"
    And the tidy mail "Answer: mw-a.1: Release" is still open
    And the question note for "mw-q.1" is kept
    And nothing was written to the tidy tracker
    And the tick log has 0 tidy lines

  # mw-gq6.438: a formula's steps left open by an ended session padded every
  # open count. A formula's story is the one its first step names, "Understand
  # <story>: <title>".
  Scenario: The formula of a closed story is closed, one line each, and the formulas of an open and a running story are not
    Given a tidy story "mw-f.1" that is closed, with a formula poured whose session closed 1 of its 3 steps
    And a tidy story "mw-f.2" that is open, with a formula poured whose session closed 1 of its 3 steps
    And a tidy story "mw-f.3" that is running, with a formula poured whose session closed 1 of its 3 steps
    When mw tidy runs
    Then tidying succeeds
    And nothing of the tidy formula of "mw-f.1" is left open, its root and 2 steps closed saying "Tidied by mw tidy: its story mw-f.1 is closed"
    And the tidy report says "closed formula step f-mol-1.2"
    And the tidy report says "closed formula step f-mol-1.3"
    And the tidy report says "closed formula f-mol-1"
    And the tick log has 3 tidy lines
    And a tidy line says "formula step f-mol-1.2: Tidied by mw tidy: its story mw-f.1 is closed"
    And a tidy line says "formula step f-mol-1.3: Tidied by mw tidy: its story mw-f.1 is closed"
    And a tidy line says "formula f-mol-1: Tidied by mw tidy: its story mw-f.1 is closed"
    And the tidy formula of "mw-f.2" is as its session left it, 2 steps still open
    And the tidy formula of "mw-f.3" is as its session left it, 2 steps still open
    And the tidy story "mw-f.1" is still closed
    And the tidy story "mw-f.2" is still open

  Scenario: The steps left open under a root closed already are closed too
    Given a tidy story "mw-f.1" that is closed, with a formula poured whose session closed 1 of its 3 steps
    And the root of the tidy formula of "mw-f.1" was closed already
    When mw tidy runs
    Then tidying succeeds
    And the tidy report says "closed formula step f-mol-1.2"
    And the tidy report says "closed formula step f-mol-1.3"
    And the tick log has 2 tidy lines

  Scenario: A dry run lists the formula beads it would close and closes none
    Given a tidy story "mw-f.1" that is closed, with a formula poured whose session closed 1 of its 3 steps
    When mw tidy runs with --dry-run
    Then tidying succeeds
    And the tidy report says "would close formula step f-mol-1.2"
    And the tidy report says "would close formula step f-mol-1.3"
    And the tidy report says "would close formula f-mol-1"
    And the tidy formula of "mw-f.1" is as its session left it, 2 steps still open
    And nothing was written to the tidy tracker
