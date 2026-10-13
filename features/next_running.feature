Feature: A by-hand mw next leaves a story whose session is still running alone
  mw next is what a story's session ends with, but a person also runs it by
  hand to recover a story. Run while the session still works, it finds no
  result yet and would stop the story no-result, write a ledger line, label it
  blocked and mail the Mayor: all false of an attempt still running. So it
  refuses and changes nothing, naming the session, as mw retry does.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the session of "mw-gq6.1" left no result at all

  Scenario: A by-hand mw next on a story whose session is running changes nothing
    Given the session of "mw-gq6.1" is still running, its heartbeat alive as process 4242
    When mw closes out "mw-gq6.1"
    Then the close-out refuses, naming the running session of "mw-gq6.1"
    And the ledger holds only the lines it held before
    And the story "mw-gq6.1" is still running and carries no comment
    And no mail was sent
    And the story "mw-gq6.1" is not closed
    And the worktree of "mw-gq6.1" is still there

  Scenario: A by-hand mw next after the session ended behaves as it always has
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is held blocked
    And the report says it stopped for "no-result"
    And the last ledger line holds:
      | not landed (no-result): |
