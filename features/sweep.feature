Feature: mw sweep
  mw sweep finds, for this host, the claimed stories whose lease has run out
  with no heartbeat since — StaleClaims's own definition of a stuck claim, and
  the only one — and marks each one run=stuck, once. It never kills or
  restarts a session, never gives a claim back, and never touches a worktree,
  git or the ledger.

  Background:
    Given the sweep epic "mw-swp" on the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A claimed story whose lease has run out with no heartbeat is reported stuck
    Given a sweep story "mw-swp.1" filed under it
    And the sweep story "mw-swp.1" is claimed
    When the clock advances 6 minutes
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.1" is recorded as stuck
    And the sweep story "mw-swp.1" carries a comment quoting: its lease had expired

  Scenario: A claimed story whose lease is still current is left alone
    Given a sweep story "mw-swp.2" filed under it
    And the sweep story "mw-swp.2" is claimed
    When mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.2" is not recorded as stuck
    And the sweep story "mw-swp.2" carries no comment

  Scenario: A heartbeated claim's lease never runs out, however long it runs
    Given a sweep story "mw-swp.3" filed under it
    And the sweep story "mw-swp.3" is claimed
    When the clock advances 4 minutes
    And the claim on "mw-swp.3" is heartbeaten
    And the clock advances 4 minutes
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.3" is not recorded as stuck

  Scenario: Sweeping twice comments once
    Given a sweep story "mw-swp.4" filed under it
    And the sweep story "mw-swp.4" is claimed
    When the clock advances 6 minutes
    And mw sweep reads the host
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.4" carries exactly 1 comment

  Scenario: A story already marked run=stopped by mw next is not commented on again
    Given a sweep story "mw-swp.5" filed under it
    And the sweep story "mw-swp.5" is claimed
    And the sweep story "mw-swp.5" is marked run=stopped
    When the clock advances 6 minutes
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.5" carries no comment
    And the sweep story "mw-swp.5" is still recorded run=stopped, not stuck

  Scenario: A story filed with path overrides only is reported with the epic's defaults filled in
    Given a sweep story "mw-swp.6" filed under it, overriding "model" with "sonnet"
    And the sweep story "mw-swp.6" is claimed
    When the clock advances 6 minutes
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep report shows "mw-swp.6" on the rig "millwright"

  Scenario: Sweep changes nothing but state and comments
    Given a sweep story "mw-swp.7" filed under it
    And the sweep story "mw-swp.7" is claimed
    When the clock advances 6 minutes
    And mw sweep reads the host
    Then sweeping succeeds
    And nothing was written through the sweep tracker but state and comments
