Feature: mw retry
  mw retry runs on a story's own host once its session has ended, typically
  after mw next has refused its landing: it proves the session is dead, keeps
  whatever the session left uncommitted, bundles the branch's work into the
  vault before the branch itself is removed, takes the worktree and branch
  away without forcing either, and gives the story back open so the next
  dispatch tick tries it again as a fresh attempt. It never resets a story's
  attempts.

  Background:
    Given a vault that is a real git clone
    And the rig "millwright" is checked out here, from a bare origin of its own
    And an epic "mw-gq6" whose stories are worked on "vps" and target "main"

  Scenario: A refused landing is retried: bundled, cleaned up, handed back open
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the branch of "mw-gq6.1" was bundled into "runs/mw-gq6.1/attempt-1.bundle" in the vault, and it verifies
    And the vault committed and pushed the bundle of "mw-gq6.1"
    And the worktree and branch of "mw-gq6.1" are both gone
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 1 attempt
    And the story "mw-gq6.1" carries a comment naming the branch commit, the bundle path and the vault commit

  Scenario: A live session refuses the retry and changes nothing
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" is still running
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: still running
    And the worktree of "mw-gq6.1" was left untouched
    And the story "mw-gq6.1" is still claimed
    And the story "mw-gq6.1" carries no new comment

  Scenario: A dirty worktree is committed onto the branch before it is bundled
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the worktree of "mw-gq6.1" also holds uncommitted work
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the leftover work was committed onto the branch of "mw-gq6.1"

  Scenario: A story already tried the most times it may be is refused, not retried
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" has been tried 3 times in all
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: attempts exhausted
    And the worktree of "mw-gq6.1" was left untouched
    And the story "mw-gq6.1" still records 3 attempts

  Scenario: A zero-commit attempt is retried: nothing to bundle, cleaned up, handed back open
    Given the story "mw-gq6.1" was dispatched but the session made no commits
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the retry found nothing to keep for "mw-gq6.1"
    And the worktree and branch of "mw-gq6.1" are both gone
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 1 attempt

  Scenario: A retry that fails partway describes only what it actually did
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the vault cannot reach its origin
    When mw retries "mw-gq6.1"
    Then the retry fails, saying: could not be pushed
    And the printed report names the bundle but nothing after it
    And the worktree of "mw-gq6.1" was left untouched
    And the story "mw-gq6.1" is still claimed
