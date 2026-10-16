Feature: A changed box_cap reaches the cloud boxes already up
  box_cap in the home's [cloud] table is the sessions a box runs at once. The
  box's own config says it too, written once when the box was made. Each cloud
  check sets the cap the table wants on every box up that runs another, over
  WireGuard, with no rebuild and no hand step, and counts each box's free
  sessions at the cap it really runs. A box it cannot reach is counted at the
  cap it has, and mw status says so. Lowering works the same way: running
  sessions finish, and none starts above the new cap.

  Background:
    Given a cloud of at most 2 boxes, capped at $75.00 a month, idle after 30 minutes, at $0.132 an hour
    And the hosts "desktop" and "laptop" run 2 sessions each, and a cloud box runs 2
    And the cloud's clock reads "2026-10-09T12:00:00Z"
    And every host is full

  Scenario: Raising box_cap sets the new cap on each box up and counts it
    Given the cloud boxes "cloud1" and "cloud2" are up at a cap of 2, dispatching
    When box_cap is changed to 3 and the cloud check runs
    Then the provider set the cap of "cloud1" to 3 and the cap of "cloud2" to 3
    And the book holds a move for each of "cloud1" and "cloud2" saying its cap went from 2 to 3
    And 1 cloud event says "box_cap 3 set on cloud1, cloud2"
    And the cloud check said "6 sessions free"
    And the book says "cloud1" runs 3 sessions at once
    When a minute passes and the cloud check runs
    Then the provider has been asked to set 2 caps in all

  Scenario: A box the check cannot reach keeps its old cap in the count
    Given the cloud boxes "cloud1" and "cloud2" are up at a cap of 2, dispatching
    And "cloud2" cannot be reached
    When box_cap is changed to 3 and the cloud check runs
    Then the book says "cloud1" runs 3 sessions at once
    And the book says "cloud2" runs 2 sessions at once
    And the cloud check said "5 sessions free"
    And a cloud event says "could not set cap 3 on cloud2"
    And mw status says "cap 2, not the 3 box_cap says"
    When "cloud2" can be reached again and a minute passes and the cloud check runs
    Then the book says "cloud2" runs 3 sessions at once
    And the cloud check said "6 sessions free"
    And mw status does not say "box_cap says"

  Scenario: An unreachable box is said once for the cap, not at each check
    Given the cloud boxes "cloud1" and "cloud2" are up at a cap of 2, dispatching
    And "cloud2" cannot be reached
    When box_cap is changed to 3 and the cloud check runs
    And a minute passes and the cloud check runs
    Then 1 cloud event says "could not set cap 3 on cloud2"

  Scenario: Lowering box_cap sets it and stops no running session
    Given the cloud boxes "cloud1" and "cloud2" are up at a cap of 2, dispatching
    And 2 stories wait for any host
    And "cloud1" runs 2 stories
    When box_cap is changed to 1 and the cloud check runs
    Then the provider set the cap of "cloud1" to 1 and the cap of "cloud2" to 1
    And "cloud1" still runs 2 stories
    And the provider was asked nothing
    And the cloud check said "1 sessions free"
    And 2 cloud boxes are up

  Scenario: A box that has not dispatched yet is left alone
    Given a box "cloud1" that is made and has not dispatched, at a cap of 2
    When box_cap is changed to 3 and the cloud check runs
    Then the provider was asked to set the cap of nothing

  Scenario: A box the book never set a cap on is set to box_cap
    Given the cloud boxes "cloud1" and "cloud2" are up at a cap of 0, dispatching
    When the cloud check runs
    Then the provider set the cap of "cloud1" to 2 and the cap of "cloud2" to 2
    And the book says "cloud1" runs 2 sessions at once
