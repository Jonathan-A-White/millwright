Feature: The cloud grows and shrinks by demand
  The home's follower runs the cloud check each minute (mw cloud check by
  hand). It costs no tokens: it compares the stories waiting for any host with
  the sessions free across the hosts and the cloud boxes. When stories wait and
  every host is full it makes one cloud box; a box with no work for
  idle_minutes is destroyed; and no box is made, or kept, past the month's
  spend cap in the [cloud] table. The month's spend is counted from the box
  hours the check made, kept in the vault, and the provider's own billing when
  it answers. Every move is a cloud event, and mw status shows the boxes.

  Background:
    Given a cloud of at most 2 boxes, capped at $75.00 a month, idle after 30 minutes, at $0.132 an hour
    And the hosts "desktop" and "laptop" run 2 sessions each, and a cloud box runs 2
    And the cloud's clock reads "2026-10-09T12:00:00Z"

  Scenario: Stories waiting with every host full make one box
    Given every host is full
    And 5 stories wait for any host
    When the cloud check runs
    Then the provider was asked to make "cloud1" and nothing else
    And 1 cloud box is up
    And a cloud event says "up cloud1"

  Scenario: A free session anywhere makes no box
    Given "desktop" runs 1 story and "laptop" runs 2
    And 1 story waits for any host
    When the cloud check runs
    Then the provider was asked nothing
    And 0 cloud boxes are up

  Scenario: Stories pathed to one host make no box
    Given every host is full
    And 3 stories wait for "desktop"
    When the cloud check runs
    Then the provider was asked nothing

  Scenario: However many stories wait, no more than max_boxes are made
    Given every host is full
    And 20 stories wait for any host
    When the cloud check runs 5 times, a minute apart
    Then the provider was asked to make "cloud1", "cloud2" and nothing else
    And 2 cloud boxes are up

  Scenario: A box idle past idle_minutes is destroyed
    Given every host is full
    And 2 stories wait for any host
    And the cloud check runs
    And "cloud1" runs 2 stories
    And 10 minutes pass and the cloud check runs
    And the stories on "cloud1" are finished
    And the cloud check runs
    When 29 minutes pass and the cloud check runs
    Then 1 cloud box is up
    When 2 minutes pass and the cloud check runs
    Then the provider was asked to destroy "cloud1"
    And 0 cloud boxes are up
    And a cloud event says "down cloud1"

  Scenario: At the monthly cap no box is made, and the cap is said once
    Given the vault counts 567 box hours this month
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked nothing
    And 1 cloud event says "cap reached"

  Scenario: The provider's billing, when it answers more, is the month's spend
    Given the provider's billing says $74.90 this month
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs
    Then the provider was asked nothing
    And 1 cloud event says "cap reached"

  Scenario: A box up when the next hour would pass the cap is destroyed
    Given every host is full
    And 5 stories wait for any host
    And the cloud check runs
    And "cloud1" runs 2 stories
    And the provider's billing says $74.95 this month
    When a minute passes and the cloud check runs
    Then the provider was asked to destroy "cloud1"
    And 0 cloud boxes are up
    And 1 cloud event says "cap reached"

  Scenario: A box that fails to come up is destroyed and tried once more
    Given the provider fails to make a box 1 time
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs
    Then the provider was asked to make "cloud1", destroy "cloud1", make "cloud1"
    And 1 cloud box is up
    And a cloud event says "failed cloud1"

  Scenario: A box that fails twice is not tried a third time
    Given the provider fails to make a box 5 times
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked to make "cloud1", destroy "cloud1", make "cloud1", destroy "cloud1"
    And 0 cloud boxes are up
    And 2 cloud events say "failed cloud1"

  Scenario: mw status shows each box, its age, and the month's spend against the cap
    Given every host is full
    And 5 stories wait for any host
    And the cloud check runs
    When 45 minutes pass on the cloud's clock
    Then mw status says "CLOUD: $0.13 spent of $75.00 this month, 1 of 2 boxes"
    And mw status says "cloud1  up 45m"
