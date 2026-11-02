Feature: mw doctor's stale-landings check
  A landing is a throwaway worktree made and taken away inside one close-out.
  One left behind for more than a day that is clean, whose HEAD is already on
  the rig's main and that has no process running in it holds nothing that is
  not in the rig, so the doctor's cure removes it and prunes git's record,
  saying each path (mw-gq6.882). Anything else is someone's work and is left in
  place, flagged for a person as before.

  Scenario: A day-old landing that is clean and on main is removed, by path
    Given a rig with a landing worktree left 30 hours ago, clean and level with main
    When mw doctor runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "stale-landings cured"
    And the doctor log and the report both name the landing worktree's path
    And the landing worktree is gone and git no longer lists it
    When mw doctor runs
    Then the doctor log holds "stale-landings ok"

  Scenario: A day-old landing holding an uncommitted file is left in place and flagged
    Given a rig with a landing worktree left 30 hours ago, clean and level with main
    And the landing worktree holds an uncommitted file
    When mw doctor runs
    Then the doctor log holds "stale-landings cannot-tell"
    And the doctor log holds "uncommitted changes"
    And the landing worktree is still there
