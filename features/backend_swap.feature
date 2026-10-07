Feature: The home swaps a staged backend itself
  A landing that changes a rig's backend is built and staged on the home, with
  its swap written as a hands step on a hitl bead (mw-gq6.185). Unless the rig
  says swap = "hands", the home's tick then runs that step itself, with no tap,
  once no Talk is open: a restart in the middle of a Talk would drop the
  Governor's line. It says the result once on the swap bead's channel, closes
  the bead when the swap went right, and leaves it open and hitl, with the
  output, when it did not. A staged commit is begun once and never again.

  Background:
    Given a home that has staged the swap of a Postern landing

  Scenario: With no Talk open the tick swaps, says so once and closes the bead
    When the home's tick runs
    Then the swap ran once
    And the Governor was told once, on the swap bead's channel, "backend 4c9db71 is live and answering"
    And the swap bead is closed

  Scenario: With a Talk open the swap waits for the first tick after it
    Given the Governor is in a Talk
    When the home's tick runs
    Then the swap did not run
    When the Talk ends
    And the home's tick runs
    Then the swap ran once

  Scenario: A swap whose health check fails is said, left open and not retried
    Given the swap's health check will fail and put the old backend back
    When the home's tick runs
    And the home's tick runs
    Then the swap ran once
    And the Governor was told once, on the swap bead's channel, that the swap failed and the old backend was put back
    And the swap bead is open, hitl, and carries the output as a comment

  Scenario: A second tick never runs the same commit again
    When the home's tick runs
    And the home's tick runs
    Then the swap ran once
    And the Governor was told once, on the swap bead's channel, "backend 4c9db71 is live and answering"

  Scenario: A rig that keeps the tap never has its swap run
    Given the rig keeps the tap
    When the home's tick runs
    Then the swap did not run
    And the swap bead is open, hitl, and carries no output

  Scenario: A story with no epic has its backend staged and its swap filed under nothing
    Given the landed story is a standalone bug fix with no epic
    When the home's tick runs
    Then the swap bead is filed with no epic and its swap is written
    And the swap ran once
    And the Governor was told once, on the swap bead's channel, "backend 4c9db71 is live and answering"
    And the swap bead is closed
