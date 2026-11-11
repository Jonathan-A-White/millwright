Feature: A host gives way to the person at its screen
  When Grace unlocks the desktop, mw giveway on stops every Builder process
  there within a second and the host takes nothing new; mw giveway off lets
  them go on. Every Builder session's own `mw next --heartbeat` keeps beating,
  so the story's lease holds while the rest is stopped. The host's marker is a
  file of its own, written first; its log times each move. The home never gives
  way.

  Background:
    Given a host "desktop" with the Builder sessions "mw-a_1" and "mw-b_2", each with a heartbeat, and a landing in an apart scope

  Scenario: on stops every process of two sessions and of an apart scope but not a heartbeat
    When mw giveway on runs
    Then every process of both sessions and of the apart scope is stopped
    And no heartbeat process and no tmux server is stopped
    And the give-way marker stands
    And the give-way log has 1 line, of event "on"

  Scenario: a second on renews and stops only what is new
    Given mw giveway on has run
    And a new process has started in the session "mw-a_1"
    When mw giveway on runs
    Then only the new process was sent a stop
    And the give-way log has 2 lines, the last of event "renew"

  Scenario: dispatch with a marker claims nothing and says it gives way
    Given mw giveway on has run
    When dispatch runs on the host with a story ready
    Then dispatch says "gives way since"
    And dispatch started nothing and claimed nothing

  Scenario: off continues them, removes the marker and logs how long
    Given mw giveway on has run
    And 7 minutes have passed
    When mw giveway off runs
    Then no process is stopped
    And the give-way marker is gone
    And the give-way log has 2 lines, the last of event "off", after 7 minutes of giving way

  Scenario: off with a dead pane retries that story
    Given mw giveway on has run
    And the session "mw-b_2" died while the host gave way
    When mw giveway off runs
    Then the story "mw-b.2" was retried
    And the story "mw-a.1" was not retried

  Scenario: off with no marker continues a stopped Builder process
    Given a Builder process was left stopped and no marker stands
    When mw giveway off runs
    Then no process is stopped
    And mw giveway says there was no marker

  Scenario: a stale marker is taken off by the next dispatch tick
    Given mw giveway on has run
    And 20 minutes have passed with no renewal
    When dispatch runs on the host with nothing ready
    Then no process is stopped
    And the give-way marker is gone
    And the give-way log has 2 lines, the last of event "expired"
    And dispatch went on past the gate

  Scenario: on the home without a session is refused
    Given this host is the home
    When mw giveway on runs
    Then mw giveway is refused, saying the home never gives way
    And no process is stopped
    And the give-way marker is gone

  Scenario: a dispatch already running is lowered, not stopped, and status names it
    Given a mw dispatch is running in a service, with a build it started
    When mw giveway on runs
    Then the dispatch and its build are not stopped
    And the dispatch and its build are lowered
    And mw giveway status names the dispatch as lowered

  Scenario: a Builder's tool left behind in a session of its own is stopped and off continues it
    Given a Builder's tool left behind by its parent, in a session of its own, with a worktree for its cwd
    When mw giveway on runs
    Then the left-behind tool is stopped
    When mw giveway off runs
    Then no process is stopped
