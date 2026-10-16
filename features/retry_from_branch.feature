Feature: A refused or capped attempt's branch is handed to the next attempt
  An attempt refused at close-out, by the gate or the merge, or stopped at its
  turn cap, has done most of its story: re-running the whole of it costs a whole
  attempt again (mw-1ljpo9.7). mw next records that its next attempt continues
  its branch, mw retry keeps the branch, and the dispatch that takes the story
  again cuts its worktree from that branch rebased onto the target, so the fix
  is a delta. A rebase that conflicts falls back to the target, and the boot
  says so. mw retry --from-main, or next.retry_from_branch = false in the
  config file, starts the next attempt from the target, as before.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" was started once
    And the session of "mw-gq6.1" reported a plain success

  Scenario: An attempt refused at the gate is continued by the next, rebased onto the target
    Given the rig's tests fail, saying "undefined: Ledger"
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "tests-fail"
    Given the other host landed its own work on "main" while "mw-gq6.1" was worked
    When mw retry runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the story "mw-gq6.1" records attempt 2
    And the worktree of "mw-gq6.1" holds its first attempt's work, rebased onto "main"
    And the boot of attempt 2 of "mw-gq6.1" says "You continue attempt 1's branch: read git log first"
    And the first attempt's branch of "mw-gq6.1" is kept

  Scenario: An attempt stopped at its turn cap is continued by the next
    Given the session of "mw-gq6.1" reported this result:
      """
      {"type": "result", "subtype": "error_max_turns", "is_error": true, "num_turns": 121, "session_id": "s-cap"}
      """
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "turn-cap"
    When the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" holds its first attempt's work, rebased onto "main"
    And the boot of attempt 2 of "mw-gq6.1" says "You continue attempt 1's branch: read git log first"

  Scenario: A branch that does not rebase onto the target falls back to the target, and the boot says so
    Given the rig's tests fail, saying "undefined: Ledger"
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "tests-fail"
    Given the other host landed its own version of the work of "mw-gq6.1" on "main"
    When mw retry runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" starts clean from "main"
    And the boot of attempt 2 of "mw-gq6.1" says "did not rebase onto origin/main without conflicts, so your worktree starts from origin/main"
    And the first attempt's branch of "mw-gq6.1" is kept

  Scenario: mw retry --from-main starts the next attempt from the target
    Given the rig's tests fail, saying "undefined: Ledger"
    When mw closes out "mw-gq6.1"
    And mw retry --from-main runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" starts clean from "main"
    And the boot of attempt 2 of "mw-gq6.1" does not say "You continue attempt"

  Scenario: With next.retry_from_branch = false the next attempt starts from the target
    Given the config file says next.retry_from_branch is false
    And the rig's tests fail, saying "undefined: Ledger"
    When mw closes out "mw-gq6.1"
    And mw retry runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" starts clean from "main"
    And the boot of attempt 2 of "mw-gq6.1" does not say "You continue attempt"
