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

  Scenario: Sweeping a session whose output keeps changing makes no state changes
    Given a sweep story "mw-swp.8" filed under it
    And the sweep story "mw-swp.8" is claimed with its session running
    When the session of "mw-swp.8" prints "step 1"
    And mw sweep reads the host
    And the session of "mw-swp.8" prints "step 2"
    And mw sweep reads the host
    And the session of "mw-swp.8" prints "step 3"
    And mw sweep reads the host
    And the session of "mw-swp.8" prints "step 4"
    And mw sweep reads the host
    And the session of "mw-swp.8" prints "step 5"
    And mw sweep reads the host
    Then sweeping succeeds
    And the sweep story "mw-swp.8" is not recorded as stuck
    And no state was recorded on the sweep story "mw-swp.8" by sweeping
    And the sweep story "mw-swp.8" carries no comment

  Scenario: A session that changed for hours and then goes quiet is reported stuck
    Given a sweep story "mw-swp.9" filed under it
    And the sweep story "mw-swp.9" is claimed with its session running
    When the session of "mw-swp.9" prints "busy"
    And mw sweep reads the host
    And the clock advances 3 hours
    And the session of "mw-swp.9" prints "busier"
    And mw sweep reads the host
    And the clock advances 1 hour
    And mw sweep reads the host
    Then the sweep story "mw-swp.9" is not recorded as stuck
    When the clock advances 2 hours
    And mw sweep reads the host
    Then the sweep story "mw-swp.9" is recorded as stuck
    And the sweep story "mw-swp.9" carries a comment quoting: printed nothing new
    And no state was recorded on the sweep story "mw-swp.9" by sweeping but run=stuck

  Scenario: What sweep saw of a session is remembered in the tracker's notes, not on the story
    Given a sweep story "mw-swp.10" filed under it
    And the sweep story "mw-swp.10" is claimed with its session running
    And the session of "mw-swp.10" has printed "still working"
    When mw sweep reads the host
    Then sweeping succeeds
    And the tracker's notes hold what sweep saw of "mw-swp.10"
    And no state was recorded on the sweep story "mw-swp.10" by sweeping

  Scenario: The silence clock starts at the claim, not at sweep's first look
    Given a sweep story "mw-swp.11" filed under it
    And the sweep story "mw-swp.11" is claimed with its session running
    And the sweep story "mw-swp.11" was claimed 3 hours ago
    And the session of "mw-swp.11" has printed "still working"
    When mw sweep reads the host
    Then the sweep story "mw-swp.11" is not recorded as stuck
    When mw sweep reads the host
    Then the sweep story "mw-swp.11" is recorded as stuck
    And the sweep story "mw-swp.11" carries a comment quoting: printed nothing new

  Scenario: A story claimed recently is still owed a full threshold
    Given a sweep story "mw-swp.12" filed under it
    And the sweep story "mw-swp.12" is claimed with its session running
    And the sweep story "mw-swp.12" was claimed 1 hour ago
    And the session of "mw-swp.12" has printed "still working"
    When mw sweep reads the host
    And mw sweep reads the host
    Then the sweep story "mw-swp.12" is not recorded as stuck

  Scenario: A story sweep found stuck no longer keeps a memory in the notes
    Given a sweep story "mw-swp.13" filed under it
    And the sweep story "mw-swp.13" is claimed with its session running
    And the session of "mw-swp.13" has printed "still working"
    When mw sweep reads the host
    And the clock advances 3 hours
    And mw sweep reads the host
    Then the sweep story "mw-swp.13" is recorded as stuck
    And the tracker's notes hold nothing of "mw-swp.13"
