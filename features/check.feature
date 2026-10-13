Feature: Checking a branch before its session ends
  A Builder learns that mw next will refuse its branch only after its session
  has ended and the fuel is spent. mw check runs the checks mw next runs before
  it lands a story — the branch has commits, none is signed by a machine, every
  formula step is closed and the rig's tests pass in the worktree — on the
  story's own branch, and prints what mw next would print for each refusal.
  It is read-only: it writes nothing to the tracker, the ledger or git, and it
  fails when any check fails, so a session can fix its branch while that is
  still cheap.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: A branch that mw next would land passes, and nothing is written anywhere
    When a session checks "mw-gq6.1"
    Then the check passes
    And the check says the branch would be landed
    And the rig's tests were run 1 times
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: Failing tests fail the check with the text mw next would write
    Given the rig's tests fail, saying "undefined: Ledger"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: the rig's tests fail in the worktree
    And the check prints: undefined: Ledger
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: Tests that could not be run are not reported as failing
    Given the rig's tests cannot be run, saying "make: go: No such file or directory"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: could not be run
    And the check prints: make: go: No such file or directory
    And the check does not print: tests fail

  Scenario: A branch with no commits fails the check without running the tests
    Given the story "mw-gq6.9" has been worked in its own worktree, committing nothing
    When a session checks "mw-gq6.9"
    Then the check fails
    And the check prints: the session committed nothing to mw/mw-gq6.9
    And the rig's tests were run 0 times
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: Work left uncommitted is named by the check
    Given the story "mw-gq6.9" has been worked in its own worktree, committing nothing
    And the session of "mw-gq6.9" left these uncommitted in its worktree:
      | README.md          |
      | notes/half-done.md |
    When a session checks "mw-gq6.9"
    Then the check fails
    And the check prints: left uncommitted work in its worktree
    And the check prints: notes/half-done.md
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: A commit signed by an AI fails the check and is named
    Given a commit on the branch of "mw-gq6.1" carries "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: is signed as a machine's work
    And the check names the signed commit
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: A formula step still open fails the check and is named
    Given a formula was poured for "mw-gq6.1" and one of its steps is still open
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: formula step(s)
    And the check prints: Implement until green
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: Every check that fails is printed at once, not only the first
    Given a commit on the branch of "mw-gq6.1" carries "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
    And a formula was poured for "mw-gq6.1" and one of its steps is still open
    And the rig's tests fail, saying "still red here"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: is signed as a machine's work
    And the check prints: formula step(s)
    And the check prints: still red here
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: A story that is closed already cannot be checked
    Given the story "mw-gq6.1" has already been closed
    When a session checks "mw-gq6.1"
    Then the check fails, saying: closed already

  Scenario: A check waits for the merge slot of its rig and runs the tests once it is free
    Given another close-out is holding the merge slot of the rig
    When a session checks "mw-gq6.1" while the other close-out finishes after 400 milliseconds
    Then the check waited at least 300 milliseconds for the merge slot
    And the check passes
    And the rig's tests were run 1 times
    And the check said it was waiting for the merge slot, held by the other close-out

  Scenario: A check told to skip the merge slot does not wait for it
    Given another close-out is holding the merge slot of the rig
    When a session checks "mw-gq6.1" without the merge slot
    Then the check passes
    And the check did not wait for the merge slot
    And the rig's tests were run 1 times

  Scenario: The check runs the rig's changed-specs command on the specs the branch added
    Given the rig has a changed-specs command that logs the specs it is given
    And the story "mw-gq6.1" also added the spec "tests/e2e/talk-fit.spec.ts"
    When a session checks "mw-gq6.1"
    Then the check passes
    And the changed-specs command was run once, given: tests/e2e/talk-fit.spec.ts

  Scenario: A failing changed spec fails the check, naming the spec
    Given the rig has a changed-specs command that fails, saying "1 failed: talk-fit.spec.ts:26"
    And the story "mw-gq6.1" also added the spec "tests/e2e/talk-fit.spec.ts"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: tests/e2e/talk-fit.spec.ts
    And the check prints: 1 failed: talk-fit.spec.ts:26

  Scenario: The check runs no changed-specs command when the branch changed no spec
    Given the rig has a changed-specs command that logs the specs it is given
    When a session checks "mw-gq6.1"
    Then the check passes
    And the changed-specs command was not run

  Scenario: Tests that fail on a busy host are run once more when it calms, and pass the check
    Given the host's load reads in turn: calm, busy, calm
    And the rig's tests fail the first time and pass the second, saying "Test Files 3 failed"
    When a session checks "mw-gq6.1"
    Then the check passes
    And the rig's tests were run 2 times
    And the check prints: ran twice
    And the check prints: the first run failed while vps was at load 39.0 of 20 cores
    And the check prints: the second run passed
    And the check wrote nothing to the tracker, the ledger, the vault or git

  Scenario: Tests that fail on a calm host fail the check at once, with no second run
    Given the host's load reads in turn: calm
    And the rig's tests fail, saying "undefined: Ledger"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the rig's tests were run 1 times
    And the check prints: the rig's tests fail in the worktree
    And the check prints: undefined: Ledger
    And the check does not print: ran twice

  Scenario: Tests that fail on a busy host and again once it calms fail the check, reported once
    Given the host's load reads in turn: calm, busy, calm
    And the rig's tests fail, saying "undefined: Ledger"
    When a session checks "mw-gq6.1"
    Then the check fails
    And the rig's tests were run 2 times
    And the check prints: ran twice
    And the check prints: the second run failed
    And the check prints "the rig's tests fail in the worktree" 1 time(s)

  Scenario: Checks of two stories started in the background write two logs, each named for its story
    Given the story "mw-gq6.9" has been worked in its own worktree, committing nothing
    When a session starts checks of "mw-gq6.1" and "mw-gq6.9" in the background
    And the background checks finish
    Then the log "check-1.log" in the run of "mw-gq6.1" ends with the verdict that mw next would land it
    And the log "check-1.log" in the run of "mw-gq6.9" ends with the verdict that mw next would not land it
    And the log of "mw-gq6.1" never names "mw-gq6.9"

  Scenario: A check started in the background prints its log's path and returns before the tests finish
    Given the rig's tests wait until they are told to finish
    When a session starts a check of "mw-gq6.1" in the background
    Then the command printed the path of the log "check-1.log" in the run of "mw-gq6.1"
    And the command returned before the rig's tests finished
    And the log of "mw-gq6.1" has no verdict yet
    When the rig's tests are told to finish
    And the background checks finish
    Then the log "check-1.log" in the run of "mw-gq6.1" ends with the verdict that mw next would land it

  Scenario: A check run in the foreground keeps the same verdict in its story's log as it prints
    When a session checks "mw-gq6.1"
    Then the check passes
    And the log "check-1.log" in the run of "mw-gq6.1" holds what the check printed
    And the check wrote nothing to the tracker, the ledger, the vault or git
