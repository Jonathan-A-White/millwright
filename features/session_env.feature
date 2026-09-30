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

  # mw-j3iis.2: beads.env names the server host as it was when the file was
  # written, and is read first. The host mw resolves from the home file is
  # exported after it, so a move of the home is followed without the file being
  # rewritten.
  Scenario: A Builder session on a boost reaches the server of the home the home file names
    Given a home directory whose beads.env names the server host "host-a"
    And the vault's home file names "desktop" as the home and this host is "laptop"
    And the vault holds .beads/dolt
    When a Builder session is started and its line runs, with a stand-in for the harness
    Then the harness saw the server host "desktop.mw"

  Scenario: The same session, once this host is the home, reaches its own server
    Given a home directory whose beads.env names the server host "host-a"
    And the vault's home file names "laptop" as the home and this host is "laptop"
    And the vault holds .beads/dolt
    When a Builder session is started and its line runs, with a stand-in for the harness
    Then the harness saw the server host "127.0.0.1"

  Scenario: A home that holds no .beads/dolt leaves the session the file's host
    Given a home directory whose beads.env names the server host "host-a"
    And the vault's home file names "laptop" as the home and this host is "laptop"
    And the vault holds no .beads/dolt
    When a Builder session is started and its line runs, with a stand-in for the harness
    Then the harness saw the server host "host-a"

  Scenario: A seat window follows the home as a Builder session does
    Given a home directory whose beads.env names the server host "host-a"
    And the vault's home file names "desktop" as the home and this host is "laptop"
    And the vault holds .beads/dolt
    When a seat window is started and its command runs, with a stand-in for the harness
    Then the harness saw the server host "desktop.mw"
