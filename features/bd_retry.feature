Feature: A Builder session's bd writes ride out a passing fault of the beads server
  mw-gq6.393: on the desktop, a lost connection to the Laptop's beads server
  swallowed every step close of a finished story, and mw next stopped it for
  open steps. The bd a Builder session runs is mw bd: it runs the real bd again
  for up to two minutes when it meets a passing fault, never writes a comment
  twice, and fails loudly, naming the fault, when the server stays down.

  Scenario: A step close that meets a passing fault is tried again and closes the step
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the next 2 writes to the beads server fail, saying: dolt server unreachable
    When the session runs bd close "mw-mol-1"
    Then bd left with status 0
    And the step "mw-mol-1" is closed
    And bd's standard error says "dolt server unreachable"

  Scenario: A step close that landed but was reported indeterminate is not run again
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the next write to the beads server lands but is reported: write commit result indeterminate
    When the session runs bd close "mw-mol-1"
    Then bd left with status 0
    And the step "mw-mol-1" is closed
    And bd ran the close of "mw-mol-1" once

  Scenario: A beads server that stays down fails the close loudly, naming the fault
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the beads server is down, saying: dolt server unreachable
    When the session runs bd close "mw-mol-1"
    Then bd left with a status that is not 0
    And the step "mw-mol-1" is open
    And bd's standard error says "giving up"
    And bd's standard error says "dolt server unreachable"
    And bd waited about 2 minutes

  Scenario: A comment whose first write landed but was reported indeterminate is never written twice
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the next write to the beads server lands but is reported: write commit result indeterminate
    When the session runs bd comment "mw-mol-1" "All done"
    Then bd left with status 0
    And the bead "mw-mol-1" holds exactly 1 comment, "All done"

  Scenario: A comment whose first write did not land is written once
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the next 2 writes to the beads server fail, saying: i/o timeout
    When the session runs bd comment "mw-mol-1" "All done"
    Then bd left with status 0
    And the bead "mw-mol-1" holds exactly 1 comment, "All done"

  Scenario: A refusal that is not a passing fault is not tried again
    Given a bd with the open steps "mw-mol-1" and "mw-mol-2"
    And the next 1 writes to the beads server fail, saying: no issue found matching "nope"
    When the session runs bd close "mw-mol-1"
    Then bd left with a status that is not 0
    And bd was run 1 time
