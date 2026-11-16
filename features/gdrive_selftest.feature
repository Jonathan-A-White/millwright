Feature: mw gdrive selftest proves a write and cleans up after itself
  mw gdrive selftest creates a spreadsheet named "millwright write test" in his
  Drive, writes the UTC time to Sheet1!A1, reads it back and prints both, then
  moves that spreadsheet to the Drive trash by the id Google gave it and prints
  the id and "trashed". It trashes only the file it made in the same run, even
  when the write failed, and asks for no scope beyond the ones already granted.

  Scenario: The selftest writes one cell, reads it back and trashes its own sheet
    Given a signed-in Drive holding a file also named "millwright write test"
    When the Drive selftest runs
    Then the Drive selftest succeeds
    And the Drive selftest wrote and read back the same time
    And the Drive selftest trashed only the sheet it created

  Scenario: A refused write still trashes the sheet the selftest created
    Given a signed-in Drive holding a file also named "millwright write test"
    And the Drive refuses writes to cells
    When the Drive selftest runs
    Then the Drive selftest fails saying: write refused
    And the Drive selftest trashed only the sheet it created
