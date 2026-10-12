Feature: mw doctor's mayor-stale check on a Mayor parked on a harness prompt
  A Mayor whose pane stands on the harness's confirmation prompt ("Auto mode
  classifier requires confirmation ... Do you want to proceed?") waits on the
  Governor, not on a restart: on 2026-10-11 one sat there 28 hours. The check
  tells the Governor once, with the prompt line and its numbered choices, and
  leaves the window alone. Its cure line says what the cure did: "would" is
  for a dry run only.

  Background:
    Given a vault whose .mayor-acting names the window "mayor-2026-10-11-333"
    And a stand-in tmux showing the Mayor's pane from a file
    And a stand-in bin/mayor-up in that vault that starts a Mayor in window "@9"

  Scenario: A parked prompt is told to the Governor once, with its choices, and the window stays
    Given the Mayor's pane shows a confirmation prompt with three choices
    When mw doctor's mayor-stale check runs
    And 20 minutes go by
    And mw doctor's mayor-stale check runs
    And 5 minutes go by
    And mw doctor's mayor-stale check runs
    And 40 minutes go by
    And mw doctor's mayor-stale check runs
    Then exactly 1 alarm was sent
    And the alarm says "Do you want to proceed?"
    And the alarm says "2. Yes, allow reading from /home/jwhite/.config/mw"
    And the alarm says "3. No"
    And the Mayor's window was not closed
    And mayor-up was not run

  Scenario: The cure line of a parked prompt says the window was left alone
    Given the Mayor's pane shows a confirmation prompt with three choices
    When mw doctor's mayor-stale check runs
    And 20 minutes go by
    And mw doctor's mayor-stale check runs
    Then the doctor log holds "left the Mayor's window alone"
    And the doctor log does not hold "would close"

  Scenario: A dry run says it would leave a parked prompt alone and sends nothing
    Given the Mayor's pane shows a confirmation prompt with three choices
    When mw doctor's mayor-stale check runs
    And 20 minutes go by
    And mw doctor runs dry
    Then mw doctor printed "would leave the Mayor's window alone"
    And no alarm was sent

  Scenario: A frozen turn is closed, and a dry run says it would close it
    Given the Mayor's pane shows a turn whose clock has stopped
    When mw doctor's mayor-stale check runs
    And 20 minutes go by
    And mw doctor runs dry
    Then mw doctor printed "would close the Mayor's window"
    And the Mayor's window was not closed
    When mw doctor's mayor-stale check runs
    Then the Mayor's window was closed
    And the doctor log holds "tmux kill-window -t '@9'"
    And the doctor log does not hold "would close"
