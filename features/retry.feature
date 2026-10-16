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

  # mw-gq6.344: a retried story has its dead-pane refunds again.

  Scenario: A retry sets the dead-pane refund count back to none
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" has had 2 dead-pane refunds
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the story "mw-gq6.1" dead-pane refund count is back at 0
    And the story "mw-gq6.1" still records 1 attempt

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

  Scenario: A story that ran on another host is retried from here without reading a worktree this host does not have
    Given the story "mw-gq6.1" ran on the host "desktop" and its worktree is there, not here
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And no worktree of "mw-gq6.1" was read or made here
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 1 attempt
    And the story "mw-gq6.1" carries a comment saying "desktop" is where its worktree is left for the next dispatch there

  Scenario: A story claimed by another host's actor has that claim given back, not refused as another actor's
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And no worktree of "mw-gq6.1" was read or made here
    And the story "mw-gq6.1" is open and unassigned

  # mw-gq6.364: where a story was worked comes from its claim, not the host key.

  Scenario: A retry from a host that does not hold the claim gives it back, guarded on the actual assignee
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the host key of the story "mw-gq6.1" is changed to "vps"
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And no worktree of "mw-gq6.1" was read or made here
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" carries a comment saying "desktop" is where its worktree is left for the next dispatch there

  Scenario: A retry on the claiming host bundles its worktree even when the host key now names another host
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the story "mw-gq6.1" is claimed by "mw@vps"
    And the host key of the story "mw-gq6.1" is changed to "laptop"
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the branch of "mw-gq6.1" was bundled into "runs/mw-gq6.1/attempt-1.bundle" in the vault, and it verifies
    And the worktree and branch of "mw-gq6.1" are both gone
    And the story "mw-gq6.1" is open and unassigned

  Scenario: A story refused at pour has no worktree to read, and its claim is given back all the same
    Given the story "mw-gq6.1" was refused at pour: claimed and blocked, with no worktree or branch ever made
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the retry says there was no worktree or branch to keep for "mw-gq6.1"
    And no worktree of "mw-gq6.1" was read or made here
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 1 attempt

  # mw-2zh33g.1: a nudge to name the cause, never a block.

  Scenario: A story with no cause recorded is retried, and the retry reminds of mw cause
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the retry printed the reminder: no cause recorded yet: mw cause mw-gq6.1 <class> "<why>"
    And the story "mw-gq6.1" is open and unassigned

  Scenario: A story whose cause is recorded is retried without the reminder
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the cause of the story "mw-gq6.1" is recorded as "tooling"
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the retry printed no cause reminder
    And the story "mw-gq6.1" is open and unassigned

  # mw-gq6.434: a session the harness stopped (a usage limit, a rate limit, an
  # overloaded or failing API) spent nothing of the story's own, so its attempt
  # is not counted, and the host claims nothing new until the time it named.

  Scenario: A usage-limit stop is retried without counting its attempt, and the host is held until the reset
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" was stopped by the harness, saying: You've hit your weekly limit · resets Oct 16, 9am (America/New_York)
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" at "2026-10-15T10:00:00Z"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned
    And the stopped attempt of "mw-gq6.1" is not counted: it records 0 attempts
    And the retry says the next dispatch tick takes "mw-gq6.1" as attempt 1
    And the retry holds new claims on "vps" until "2026-10-16T13:00:00Z"

  Scenario: A story whose last allowed attempt hit the usage limit is retried, not refused as exhausted
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the story "mw-gq6.1" has been tried 3 times in all
    And the session of "mw-gq6.1" was stopped by the harness, saying: You've hit your weekly limit · resets Oct 16, 3pm (America/New_York)
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" at "2026-10-15T20:00:00Z"
    Then the retry succeeds
    And the stopped attempt of "mw-gq6.1" is not counted: it records 2 attempts
    And the retry holds new claims on "vps" until "2026-10-16T19:00:00Z"

  Scenario: A session the work itself failed is retried as before, its attempt counted
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" ended in error, its result saying: the session ran out of fuel
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" at "2026-10-15T10:00:00Z"
    Then the retry succeeds
    And the story "mw-gq6.1" still records 1 attempt
    And the retry holds no claims on "vps"
