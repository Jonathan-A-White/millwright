Feature: mw note
  A seat that types a time into a bead comment gets it wrong: three times in one
  sitting the Mayor's time did not match the clock. `mw note <bead> "<text>"`
  adds the text to the bead as a comment, with every {now} in it replaced by the
  clock's UTC time at the moment of writing, to the minute, like 2026-10-13 06:30Z.
  Text with no {now} is written as it is. --file reads the text from a file. The
  time used is printed.

  Background:
    Given the note clock reads 2026-10-13T06:30:41Z
    And the note bead "mw-x" is open

  Scenario: A {now} in the text becomes the clock's time
    When mw notes on "mw-x": VERIFIED by the Mayor, {now}: ok
    Then noting succeeds
    And the note bead "mw-x" has exactly 1 comment, saying "VERIFIED by the Mayor, 2026-10-13 06:30Z: ok"
    And noting printed "2026-10-13 06:30Z"

  Scenario: Every {now} in the text is replaced
    When mw notes on "mw-x": from {now} to {now}
    Then noting succeeds
    And the note bead "mw-x" has exactly 1 comment, saying "from 2026-10-13 06:30Z to 2026-10-13 06:30Z"

  Scenario: Text with no {now} is written unchanged
    When mw notes on "mw-x": checked, nothing to add
    Then noting succeeds
    And the note bead "mw-x" has exactly 1 comment, saying "checked, nothing to add"

  Scenario: --file reads the text from a file, with the same replacement
    Given a note file holding "Landed at {now}.\nSecond line."
    When mw notes on "mw-x" with the text from that file
    Then noting succeeds
    And the note bead "mw-x" has exactly 1 comment, saying "Landed at 2026-10-13 06:30Z.\nSecond line."
    And noting printed "2026-10-13 06:30Z"

  Scenario: A file that cannot be read is refused and nothing is written
    When mw notes on "mw-x" with the text from the file "/nonexistent/note.txt"
    Then noting is refused, saying: /nonexistent/note.txt
    And nothing was written by the note command

  Scenario: An unknown bead is refused and nothing is written
    When mw notes on "mw-nope.9": hello {now}
    Then noting is refused, saying: mw-nope.9
    And nothing was written by the note command

  Scenario: An empty note is refused and nothing is written
    When mw notes on "mw-x":   
    Then noting is refused, saying: text
    And nothing was written by the note command
