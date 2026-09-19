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
