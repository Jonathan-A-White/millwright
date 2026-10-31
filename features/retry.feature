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
    And mw next refused the landing of "mw-gq6.1" for a signed commit
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the branch of "mw-gq6.1" was bundled into "runs/mw-gq6.1/attempt-1.bundle" in the vault, and it verifies
    And the vault committed and pushed the bundle of "mw-gq6.1"
    And the worktree and branch of "mw-gq6.1" are both gone
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 1 attempt
    And the story "mw-gq6.1" carries a comment naming the branch commit, the bundle path and the vault commit

  # mw-gq6.537: an attempt that left work and was not refused for its commits
  # keeps its branch, so the next attempt continues it.

  Scenario: An attempt that left commits and was not refused keeps its branch
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the branch of "mw-gq6.1" was bundled into "runs/mw-gq6.1/attempt-1.bundle" in the vault, and it verifies
    And the worktree of "mw-gq6.1" is gone but its branch is kept
    And the story "mw-gq6.1" is open and unassigned

  # mw-gq6.823: a story carries a run state (run:blocked, run:stopped, ...) that
  # a retry that left on gave the claim back with, and the next attempt never
  # started; run:queued is the one a retry no longer gives back at all (below).

  Scenario: A retried story carries no run state, so dispatch does not pass it as queued
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the story "mw-gq6.1" was refused by mw next and left blocked
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" carries no run state label
    And the next dispatch tick does not pass "mw-gq6.1" as queued for its batch

  # mw-gq6.862: a story queued for its rig's batch has its work done and is
  # landed by that batch; giving it back would run a second attempt beside it.

  Scenario: A story queued for its rig's batch is not retried, with --force or without
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the story "mw-gq6.1" is queued for its rig's batch
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: queued for batch landing
    And the worktree of "mw-gq6.1" was left untouched
    And the story "mw-gq6.1" is still claimed
    And the story "mw-gq6.1" carries no new comment

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

  # mw-gq6.774: a retry off the story's host asks that host before it gives the claim back.

  Scenario: A retry off the story's host refuses when its worktree there holds uncommitted work
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the worktree of "mw-gq6.1" on the host "desktop" holds uncommitted work in "application/retry.go"
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: run `mw retry mw-gq6.1` on desktop
    And the host "desktop" was asked what its worktree holds
    And the story "mw-gq6.1" is still claimed
    And the story "mw-gq6.1" carries no new comment

  Scenario: A retry off the story's host refuses when that host cannot be asked
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the ssh line to the host "desktop" is down
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: desktop could not be asked
    And the story "mw-gq6.1" is still claimed

  Scenario: A forced retry off the story's host says the work is left unsaved there, on stderr and on the bead
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the worktree of "mw-gq6.1" on the host "desktop" holds uncommitted work in "application/retry.go"
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1" with --force
    Then the retry succeeds
    And the retry says "work left unsaved on desktop: /home/jonathan/.mw-worktrees/mw-gq6.1" on stderr and on the bead "mw-gq6.1"
    And the story "mw-gq6.1" is open and unassigned

  Scenario: A forced retry off the story's host with a clean worktree there says nothing of unsaved work
    Given the story "mw-gq6.1" ran on the host "desktop" and is claimed there by "mw@desktop"
    And the story "mw-gq6.1" has been tried 1 time in all
    When mw retries "mw-gq6.1" with --force
    Then the retry succeeds
    And the retry says nothing of unsaved work on stderr or on the bead "mw-gq6.1"

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
    And mw next refused the landing of "mw-gq6.1" for a signed commit
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

  # mw-gq6.597: a story stopped before mw next gave its claim back (mw-gq6.594's
  # give-back did not reach it) is still claimed and nobody can close it; --hold
  # gives the claim back and leaves the story held, never open, so no dispatch
  # takes it and a plain bd close can finish it.

  Scenario: A story stopped before its claim was given back is released and held, never open
    Given the story "mw-gq6.1" was refused at pour: claimed and blocked, with no worktree or branch ever made
    When mw retries "mw-gq6.1" with --hold
    Then the retry succeeds
    And the story "mw-gq6.1" is held and unassigned
    And the claim of "mw-gq6.1" went from claimed to held in one write, never through open
    And the story "mw-gq6.1" carries a comment saying it is held, not open
    And the retry says "mw-gq6.1" is held and no dispatch tick takes it

  Scenario: A held retry of a story with commits bundles its branch and holds it too
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" with --hold
    Then the retry succeeds
    And the branch of "mw-gq6.1" was bundled into "runs/mw-gq6.1/attempt-1.bundle" in the vault, and it verifies
    And the story "mw-gq6.1" is held and unassigned
    And the story "mw-gq6.1" carries a comment saying it is held, not open

  Scenario: A held retry of a story worked on another host holds it without reading a worktree
    Given the story "mw-gq6.1" ran on the host "laptop" and is claimed there by "mw@laptop"
    When mw retries "mw-gq6.1" with --hold
    Then the retry succeeds
    And the story "mw-gq6.1" is held and unassigned
    And no worktree of "mw-gq6.1" was read or made here

  Scenario: A retry without --hold still opens the story
    Given the story "mw-gq6.1" was refused at pour: claimed and blocked, with no worktree or branch ever made
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned

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

  Scenario: A usage-limit stop that left no result JSON is retried without counting its attempt (mw-gq6.471)
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" was stopped by the harness before it wrote a result, printing: Claude AI usage limit reached|1792155600
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" at "2026-10-15T10:00:00Z"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned
    And the stopped attempt of "mw-gq6.1" is not counted: it records 0 attempts
    And the retry holds new claims on "vps" until "2026-10-16T13:00:00Z"

  Scenario: A session the work itself failed is retried as before, its attempt counted
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" ended in error, its result saying: the session ran out of fuel
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1" at "2026-10-15T10:00:00Z"
    Then the retry succeeds
    And the story "mw-gq6.1" still records 1 attempt
    And the retry holds no claims on "vps"

  # mw-gq6.438: the next attempt starts on a fresh branch, so it works a fresh
  # formula: the retry closes what the last attempt left open of its own.
  Scenario: A retried story's formula is closed, the steps its session left open with it
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the retried story "mw-gq6.1" worked a formula and closed 1 of its 3 steps
    And the session of "mw-gq6.1" has ended
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned
    And nothing of the retried story's formula "mw-gq6.1" is left open, its root and 2 steps closed saying "mw-gq6.1 was retried"
    And the printed retry report says "formula closed"

  Scenario: A story that ran on another host has its formula closed by the retry from here
    Given the story "mw-gq6.1" ran on the host "desktop" and its worktree is there, not here
    And the retried story "mw-gq6.1" worked a formula and closed 3 of its 3 steps
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And nothing of the retried story's formula "mw-gq6.1" is left open, its root and 0 steps closed saying "mw-gq6.1 was retried"

  Scenario: A retry refused for a live session leaves its formula as the session has it
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the retried story "mw-gq6.1" worked a formula and closed 1 of its 3 steps
    And the session of "mw-gq6.1" is still running
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: still running
    And the retried story's formula "mw-gq6.1" is as its session left it, 2 steps still open

  # mw-gq6.450: under A40 an attempt that failed for what is not the story's own
  # code (a host down, a flaky step) is retried by the Mayor and is not the
  # story's: only the attempts mw cause named code, or named nothing, count
  # toward max_attempts.

  Scenario: Attempts whose cause was not the story's own code do not count toward max_attempts
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" has been tried 3 times, mw cause naming its attempts process, test, code
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the story "mw-gq6.1" is open and unassigned
    And the story "mw-gq6.1" still records 3 attempts
    And the printed retry report says "takes it as attempt 4, its own attempt 2 of 3 (not counted: attempt 1 (process), attempt 2 (test))"
    And the retry comment on "mw-gq6.1" says "takes it as attempt 4, its own attempt 2 of 3 (not counted: attempt 1 (process), attempt 2 (test))"

  Scenario: An attempt with no cause named counts as the story's own
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" has been tried 3 times, mw cause naming its attempts code, -, code
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: attempts exhausted
    And the worktree of "mw-gq6.1" was left untouched

  Scenario: An exhausted story's refusal says how many attempts counted and which did not, with their causes
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" has been tried 5 times, mw cause naming its attempts tooling, code, process, code, code
    When mw retries "mw-gq6.1"
    Then the retry refuses, saying: attempts exhausted
    And the retry refuses, saying: started 5 times and 3 count
    And the retry refuses, saying: not counted: attempt 1 (tooling), attempt 3 (process)
    And the worktree of "mw-gq6.1" was left untouched
    And the story "mw-gq6.1" still records 5 attempts

  # mw-gq6.854: a story the Mayor closed (superseded) while its claim was held
  # and its retry ran was given back open, and the retry told its thread the
  # story was open again and would run as attempt 2. A closed story stays
  # closed: the retry cleans up and says so.

  Scenario: A story closed while its retry runs stays closed, and the retry says so
    Given the story "mw-gq6.1" was dispatched and worked in its own worktree
    And the session of "mw-gq6.1" has ended
    And the story "mw-gq6.1" is closed while its retry runs
    When mw retries "mw-gq6.1"
    Then the retry succeeds
    And the worktree and branch of "mw-gq6.1" are both gone
    And the story "mw-gq6.1" is still closed
    And the retry comment on "mw-gq6.1" says "the story stays closed"
    And the retry comment on "mw-gq6.1" says "no dispatch tick takes it"
    And the retry comment on "mw-gq6.1" does not say "open again"
    And the retry comment on "mw-gq6.1" does not say "attempt 2"
    And the printed retry report says "closed"
    And the printed retry report does not say "open    the claim was given back"
    And the printed retry report does not say "takes it as"
