Feature: mw sweep
  mw sweep finds, for this host, the claimed stories whose Runner session is
  gone or has gone quiet past the stale threshold, and marks each one
  run=stuck — once. It never kills or restarts a session, never gives a claim
  back, and never touches a worktree, git or the ledger.

  Background:
    Given the sweep epic "mw-swp" on the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A claimed story with no live session is reported stuck
    Given a sweep story "mw-swp.1" filed under it
    And the sweep story "mw-swp.1" is claimed with no session behind it
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.1" is recorded as stuck
    And the sweep story "mw-swp.1" carries a comment quoting: no session behind it

  Scenario: A claimed story with a live, active session is left alone
    Given a sweep story "mw-swp.2" filed under it
    And the sweep story "mw-swp.2" is claimed with its session running
    And the session of "mw-swp.2" has printed "still working"
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.2" is not recorded as stuck
    And the sweep story "mw-swp.2" carries no comment

  Scenario: A claimed story older than the threshold whose output has not changed is reported stuck
    Given a sweep story "mw-swp.3" filed under it
    And the sweep story "mw-swp.3" is claimed with its session running
    And the session of "mw-swp.3" has printed "still working"
    When mw sweep reads the host
    And the clock advances 3 hours
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.3" is recorded as stuck
    And the sweep story "mw-swp.3" carries a comment quoting: printed nothing new

  Scenario: Sweeping twice comments once
    Given a sweep story "mw-swp.4" filed under it
    And the sweep story "mw-swp.4" is claimed with no session behind it
    When mw sweep reads the host
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.4" carries exactly 1 comment

  Scenario: A story already marked run=stopped by mw next is not commented on again
    Given a sweep story "mw-swp.5" filed under it
    And the sweep story "mw-swp.5" is claimed with no session behind it
    And the sweep story "mw-swp.5" is marked run=stopped
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.5" carries no comment
    And the sweep story "mw-swp.5" is still recorded run=stopped, not stuck

  Scenario: A story filed with path overrides only is reported with the epic's defaults filled in
    Given a sweep story "mw-swp.6" filed under it, overriding "model" with "sonnet"
    And the sweep story "mw-swp.6" is claimed with no session behind it
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep report shows "mw-swp.6" on the rig "millwright"

  Scenario: Sweep changes nothing but state and comments
    Given a sweep story "mw-swp.7" filed under it
    And the sweep story "mw-swp.7" is claimed with no session behind it
    When mw sweep reads the host
    Then sweeping succeeds
    And nothing was written through the sweep tracker but state and comments
    And nothing was started, sent to or closed through the sweep runner
