Feature: mw events follow springs the mill when a grist arrives
  An app's grist reaches the mill within seconds, not on mw dispatch's tick:
  the follower, on a host whose config has a [grist] table, springs one
  mw grist grind pass in the cycle it sees a new grist record for the mill key
  at the postern backend, as it springs dispatch on a bead that opens. History
  springs nothing, a record for another key or of another class springs
  nothing, and a host with no [grist] table never springs the mill.

  Scenario: A new grist record springs one grind pass in the cycle it is seen
    Given the event follower springs the mill on a host with a [grist] table
    When a grist record for the mill key reaches the postern backend
    And the follower runs a cycle
    Then the mill has made 1 pass

  Scenario: A cycle that sees no new grist springs no pass
    Given the event follower springs the mill on a host with a [grist] table
    When the follower runs a cycle
    Then the mill has made 0 passes

  Scenario: A grist already waiting when the follower starts is left to the dispatch tick
    Given a grist record for the mill key waits at the postern backend
    And the event follower springs the mill on a host with a [grist] table
    When the follower runs a cycle
    Then the mill has made 0 passes

  Scenario: A record for another key or of another class springs no pass
    Given the event follower springs the mill on a host with a [grist] table
    When a grist record for another key reaches the postern backend
    And a message record for the mill key reaches the postern backend
    And the follower runs a cycle
    Then the mill has made 0 passes

  Scenario: A second grist while a pass runs is a no-op until it ends, then one more pass
    Given the event follower springs the mill on a host with a [grist] table
    And the mill's pass is held running
    When a grist record for the mill key reaches the postern backend
    And the follower runs a cycle
    And a grist record for the mill key reaches the postern backend
    And the follower runs a cycle
    Then the mill has begun 1 pass
    When the mill's pass is let go
    Then the mill has made 2 passes

  Scenario: A host with no [grist] table never springs the mill
    Given the event follower springs the mill on a host with no [grist] table
    When a grist record for the mill key reaches the postern backend
    And the follower runs a cycle
    Then the mill has made 0 passes
