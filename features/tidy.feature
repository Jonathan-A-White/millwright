Feature: mw tidy
  mw tidy closes what is plainly finished, within fixed bounds, and nothing
  else. It may only:

    1. close an "Answer: ..." mail bead over 1 day old;
    2. close a mail bead the mailbox holds read, over 7 days old;
    3. clear a postern question note whose bead is closed.

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
