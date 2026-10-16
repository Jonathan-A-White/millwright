Feature: An attempt that died with its work left hands its branch to the next attempt
  An attempt whose close-out was killed (mw-gq6.533: pthread_create failed) left
  no refusal and no note, but its work is in its worktree, and redoing it costs a
  whole attempt (mw-gq6.537). mw retry commits what the worktree holds, bundles
  it, keeps the branch and leaves the note, so the dispatch that takes the story
  again cuts its worktree from that branch rebased onto the target, and the boot
  says it continues an earlier attempt.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree, committing nothing
    And the story "mw-gq6.1" was started once
    And the session of "mw-gq6.1" left these uncommitted in its worktree:
      | died-closeout.md |

  Scenario: The next attempt continues the work the dead attempt left uncommitted
    When mw retry runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" holds the committed leftover "died-closeout.md", rebased onto "main"
    And the boot of attempt 2 of "mw-gq6.1" says "You continue attempt 1's branch: read git log first"
    And the first attempt's branch of "mw-gq6.1" is kept

  Scenario: mw retry --from-main starts the next attempt from the target
    When mw retry --from-main runs for "mw-gq6.1"
    And the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" starts clean from "main"
    And the boot of attempt 2 of "mw-gq6.1" does not say "You continue attempt"
