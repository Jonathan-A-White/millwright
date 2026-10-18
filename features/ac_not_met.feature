Feature: A landing whose closing comment says an acceptance criterion is not met
  A Builder's closing comment can say that an acceptance criterion is NOT met,
  or NOT done, because the work fell short or its harness refused a step. The
  landing stands, since the rest of the work is good, but mw next does not let
  it pass as whole: it marks the Landed mail with the criterion, and files one
  held bug for it under the story's epic, for the Mayor to word and release.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" has the acceptance:
      """
      1. The first thing works.
      2. The second thing works, shown by running it end to end against the real rig.
      3. The third thing works, and its long explanation runs on and on past the eightieth character of the line.
      4. The fourth thing works.
      """
    And the session of "mw-gq6.1" reported a plain success

  Scenario: A landing whose closing comment says AC 3 is not met is marked and files a held bug
    Given the story "mw-gq6.1" carries the closing comment "Done.\nAC 3 NOT met: the harness refused the step.\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" is closed
    And that mail's subject is "Landed, AC 3 NOT met: The story mw-gq6.1"
    And exactly 1 bug was filed under "mw-gq6"
    And the filed bug is titled "[bug] mw-gq6.1 landed without its AC 3: The third thing works, and its long explanation runs on and on past the eightiet"
    And the filed bug is held and labelled "bug" and "friction"
    And the filed bug's description quotes: AC 3 NOT met: the harness refused the step.
    And the filed bug's description quotes: 3. The third thing works, and its long explanation
    And the filed bug has the path of "mw-gq6.1"
    And the filed bug's acceptance has a line beginning "Sweep:"

  Scenario: Only the criterion that is not met gets a bug
    Given the story "mw-gq6.1" carries the closing comment "AC 1, 3 and 4 done; AC 2 NOT done (the harness refused the step).\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then that mail's subject is "Landed, AC 2 NOT met: The story mw-gq6.1"
    And exactly 1 bug was filed under "mw-gq6"
    And the filed bug is titled "[bug] mw-gq6.1 landed without its AC 2: The second thing works, shown by running it end to end against the real rig."

  Scenario: Two criteria not met give one bug naming both
    Given the story "mw-gq6.1" carries the closing comment "AC 2 and 4 NOT met.\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then that mail's subject is "Landed, AC 2 and 4 NOT met: The story mw-gq6.1"
    And exactly 1 bug was filed under "mw-gq6"
    And the filed bug is titled "[bug] mw-gq6.1 landed without its AC 2 and 4: The second thing works, shown by running it end to end against the real rig."
    And the filed bug's description quotes: 4. The fourth thing works.

  Scenario: A comment with no NOT met line lands as it always did
    Given the story "mw-gq6.1" carries the closing comment "Done. AC 3 was not met by the first try, so the code was changed until it was.\nAC 1 to 4 met.\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And that mail's subject is "Landed: The story mw-gq6.1"
    And exactly 0 bugs were filed under "mw-gq6"

  Scenario: Closing out the same landing twice files the bug once
    Given the story "mw-gq6.1" carries the closing comment "Done.\nAC 3 NOT met.\nFor the rig memory: nothing"
    And the tracker cannot record the run state, saying: the database is locked
    And the tracker refuses to close "mw-gq6.1", saying: assignee is root, actor is mw@vps; reclaim or use --force
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is not closed
    And exactly 1 bug was filed under "mw-gq6"
    Given the tracker can record the run state again
    And the tracker will take a close of "mw-gq6.1" again
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And exactly 1 bug was filed under "mw-gq6"
