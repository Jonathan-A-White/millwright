Feature: A session mw starts reads the host's beads environment first
  A host whose beads database is served keeps the names and values that reach
  it in ~/.config/mw/beads.env. A session mw starts has no shell profile, so
  unless the session reads that file itself its bd, and the close-out chained
  on after it, never reach the served database. The line holds the file's
  path, never anything in it.

  Scenario: A story session and its close-out see the names in beads.env
    Given a home directory whose beads.env sets BEADS_DOLT_SERVER_HOST and BEADS_DOLT_PASSWORD to made-up values
    When the story session's line runs, with stand-ins for the harness, the heartbeat and the close-out
    Then the harness, the heartbeat and the close-out each recorded the name BEADS_DOLT_SERVER_HOST
    And the harness, the heartbeat and the close-out each recorded the name BEADS_DOLT_PASSWORD
    And the log holds no value from beads.env

  Scenario: A seat window sees the names in beads.env and the kickoff arrives unchanged
    Given a home directory whose beads.env sets BEADS_DOLT_SERVER_HOST and BEADS_DOLT_PASSWORD to made-up values
    When the seat window's command runs, with a stand-in for the harness and a kickoff full of quotes
    Then the harness recorded the name BEADS_DOLT_SERVER_HOST
    And the harness recorded the name BEADS_DOLT_PASSWORD
    And the harness was handed the kickoff exactly as it was written
    And the log holds no value from beads.env

  Scenario: The story line and the seat window hold the path of beads.env and none of its values
    Given a home directory whose beads.env sets BEADS_DOLT_SERVER_HOST and BEADS_DOLT_PASSWORD to made-up values
    When the story session and the seat window are assembled
    Then the story line holds the path of beads.env and no value from it
    And the seat window's command holds the path of beads.env and no value from it

  Scenario: A host with no beads.env runs its sessions as it always did
    Given a home directory with no beads.env
    When the story session's line runs, with stand-ins for the harness, the heartbeat and the close-out
    And the seat window's command runs, with a stand-in for the harness and a kickoff full of quotes
    Then the stand-ins all ran
    And no name starting BEADS_DOLT_ was recorded
    And the harness was handed the kickoff exactly as it was written

  Scenario: A host with an empty beads.env runs its sessions as it always did
    Given a home directory whose beads.env is empty
    When the story session's line runs, with stand-ins for the harness, the heartbeat and the close-out
    And the seat window's command runs, with a stand-in for the harness and a kickoff full of quotes
    Then the stand-ins all ran
    And no name starting BEADS_DOLT_ was recorded
    And the harness was handed the kickoff exactly as it was written
