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
    And a cloud event says "made cloud1"

  Scenario: The check makes the box in the background and does not wait for it
    Given every host is full
    And 5 stories wait for any host
    When the cloud check runs and the box is still being made
    Then the cloud check said "making cloud1 in the background; the book says when it is up"
    And the book holds "cloud1" being made
    And 0 cloud boxes are up
    And a cloud event says "making cloud1"
    When the box being made comes up
    Then 1 cloud box is up
    And the book holds no box being made
    And a cloud event says "made cloud1"

  Scenario: A caller cancelled 10 seconds into the apply leaves the apply to finish
    Given every host is full
    And 5 stories wait for any host
    When the cloud check runs and its caller is cancelled 10 seconds into the apply
    Then the provider was asked to make "cloud1" and nothing else
    And 1 cloud box is up
    And the book holds no box being made
    And a cloud event says "made cloud1"

  Scenario: A box still being made is not made twice, nor destroyed idle
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs and the box is still being made
    When 40 minutes pass and the cloud check runs and the box is still being made
    Then the provider was asked nothing
    And the book holds "cloud1" being made

  Scenario: A make that never finished is destroyed by a later check
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs and the box is still being made
    And the process making the box dies
    When 80 minutes pass and the cloud check runs
    Then the provider was asked to destroy "cloud1"
    And the book holds no box being made
    And a cloud event says "failed cloud1"

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

  Scenario: A box made in longer than idle_minutes is not idle on arrival
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs and the box is still being made
    And 31 minutes pass on the cloud's clock
    When the box being made comes up
    And the cloud check runs
    Then the provider was asked to make "cloud1" and nothing else
    And 1 cloud box is up
    And 0 cloud events say "down cloud1"
    When 29 minutes pass and the cloud check runs
    Then 1 cloud box is up
    When 2 minutes pass and the cloud check runs
    Then the provider was asked to destroy "cloud1"
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

  Scenario: A box Vultr holds that no state knows is destroyed, and the check says so
    Given Vultr holds "cloud1" that no state knows
    And Vultr holds "cloud2" that no state knows
    When the cloud check runs
    Then Vultr's "cloud1" was destroyed
    And Vultr's "cloud2" was destroyed
    And a cloud event says "destroyed orphan cloud1"
    And a cloud event says "destroyed orphan cloud2"
    And 0 cloud boxes are up

  Scenario: A box the book is making is not an orphan
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs and the box is still being made
    And Vultr holds "cloud1" that no state knows
    When a minute passes and the cloud check runs and the box is still being made
    Then the provider was asked nothing
    And the book holds "cloud1" being made

  Scenario: An orphan Vultr will not destroy yet is said once and tried at the next check
    Given Vultr holds "cloud1" that no state knows
    And Vultr will not destroy an instance yet, for it is locked
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked to destroy "cloud1" by its id 3 times
    And 1 cloud event says "could not destroy orphan cloud1"
    When Vultr can destroy instances again
    And a minute passes and the cloud check runs
    Then Vultr's "cloud1" was destroyed
    And a cloud event says "destroyed orphan cloud1"

  Scenario: Vultr's monthly fee limit is a money cap, not a box that failed to come up
    Given Vultr refuses to make a box: "Your account has reached its maximum monthly fee limit"
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs
    Then the provider was asked to make "cloud1", destroy "cloud1"
    And a cloud event says "Vultr fee limit reached: raise it at my.vultr.com or wait for the month"
    And 0 cloud events say "failed cloud1"
    And 0 cloud events say "again"
    And the Governor was sent 1 card
    And the Governor's card says "Vultr fee limit reached: raise it at my.vultr.com or wait for the month"
    And mw status says "Vultr fee limit reached since"

  Scenario: The fee limit is not tried every minute, and the Governor has one card
    Given Vultr refuses to make a box: "Your account has reached its maximum monthly fee limit"
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked to make "cloud1", destroy "cloud1"
    When 11 minutes pass and the cloud check runs
    Then the provider was asked to make "cloud1", destroy "cloud1", make "cloud1", destroy "cloud1"
    And the Governor was sent 1 card
    And 1 cloud event says "Vultr fee limit reached"

  Scenario: Once the limit is raised a box comes up
    Given Vultr refuses to make a box: "Your account has reached its maximum monthly fee limit"
    And every host is full
    And 5 stories wait for any host
    And the cloud check runs
    And the provider fails to make a box 0 times
    When 11 minutes pass and the cloud check runs
    Then 1 cloud box is up
    And a cloud event says "made cloud1"

  Scenario: A refusal that a retry cannot mend is said in plain words and tried once
    Given Vultr refuses to make a box: "Invalid API token"
    And every host is full
    And 5 stories wait for any host
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked to make "cloud1", destroy "cloud1"
    And a cloud event says "Vultr refused the API token"
    And 0 cloud events say "again"

  Scenario: A make is given up_minutes, not twenty, and a check waits that long and 15 more
    Given a box is given 90 minutes to come up
    And every host is full
    And 1 story waits for any host
    And the cloud check runs and the box is still being made
    When 80 minutes pass and the cloud check runs and the box is still being made
    Then the provider was asked nothing
    And the book holds "cloud1" being made
    When the process making the box dies
    And 24 minutes pass and the cloud check runs and the box is still being made
    Then the provider was asked nothing
    When 2 minutes pass and the cloud check runs
    Then the provider was asked to destroy "cloud1"
    And a cloud event says "failed cloud1"

  Scenario: A make that reaches its deadline while Vultr still restores the box keeps it, and makes no second
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    When the cloud check runs
    Then the provider was asked to make "cloud1" and nothing else
    And Vultr holds 1 instance labelled "cloud1"
    And the book holds "cloud1" restoring
    And 0 cloud boxes are up
    And a cloud event says "restoring cloud1"
    And 0 cloud events say "destroyed"
    And 0 cloud events say "tried once more"
    And mw status says "cloud1  restoring"

  Scenario: A restoring box is not made twice while it restores
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    And the cloud check runs
    When the cloud check runs 3 times, a minute apart
    Then the provider was asked to make "cloud1" and nothing else
    And Vultr holds 1 instance labelled "cloud1"
    And the book holds "cloud1" restoring

  Scenario: The next check adopts a restoring box that Vultr says is up
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    And the cloud check runs
    When Vultr finishes restoring "cloud1"
    And a minute passes and the cloud check runs
    Then the provider was asked to make "cloud1", adopt "cloud1"
    And 1 cloud box is up
    And the book holds no box restoring
    And a cloud event says "made cloud1"
    And 0 cloud events say "destroyed"

  Scenario: A restoring box Vultr is still restoring is waited for, then destroyed after up_minutes more
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    And the cloud check runs
    When 59 minutes pass and the cloud check runs
    Then the provider was asked to make "cloud1" and nothing else
    And the book holds "cloud1" restoring
    When 2 minutes pass and the cloud check runs
    Then Vultr's "cloud1" was destroyed
    And a cloud event says "destroyed cloud1"
    And the book holds no box restoring

  Scenario: A restoring box Vultr would not delete is kept in the book and says why
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    And the cloud check runs
    And Vultr will not destroy an instance yet, for it is locked
    When 61 minutes pass and the cloud check runs
    Then the book holds "cloud1" restoring
    And a cloud event says "Vultr would not delete (locked)"
    And 0 cloud events say "destroyed"
    And Vultr holds 1 instance labelled "cloud1"

  Scenario: A restoring box Vultr no longer holds is dropped from the book
    Given Vultr is still restoring the box when the make's deadline comes
    And every host is full
    And 1 story waits for any host
    And the cloud check runs
    When Vultr loses "cloud1"
    And a minute passes and the cloud check runs
    Then the book holds no box restoring
    And a cloud event says "cloud1 is gone from Vultr"

  Scenario: A box Vultr still holds after a down is not written down until Vultr lets go of it
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs
    And "cloud1" runs 1 stories
    And the stories on "cloud1" are finished
    And the cloud check runs
    And the wrapper finds no state for a box it is asked to destroy
    When 31 minutes pass and the cloud check runs
    Then Vultr's "cloud1" was destroyed
    And a cloud event says "down cloud1"
    And Vultr holds 0 instances labelled "cloud1"

  Scenario: A box Vultr will not delete stays in the book, and no down is written
    Given every host is full
    And 1 story waits for any host
    And the cloud check runs
    And "cloud1" runs 1 stories
    And the stories on "cloud1" are finished
    And the cloud check runs
    And the wrapper finds no state for a box it is asked to destroy
    And Vultr will not destroy an instance yet, for it is locked
    When 31 minutes pass and the cloud check runs
    Then a cloud event says "Vultr would not delete (locked)"
    And 0 cloud events say "down cloud1"
    And 1 cloud box is up
    And Vultr holds 1 instance labelled "cloud1"

  Scenario: A failed make's retry never starts while its box is still on Vultr
    Given the provider fails to make a box 1 time
    And the failed make leaves its instance on Vultr
    And the wrapper finds no state for a box it is asked to destroy
    And Vultr will not destroy an instance yet, for it is locked
    And every host is full
    And 1 story waits for any host
    When the cloud check runs
    Then the provider made "cloud1" 1 time
    And a cloud event says "Vultr would not delete (locked)"
    And Vultr holds 1 instance labelled "cloud1"

  Scenario: A make the wrapper calls done while Vultr holds no such box is not written up
    Given the wrapper says a make is done though Vultr holds no box, 1 time
    And every host is full
    And 1 story waits for any host
    When the cloud check runs
    Then the provider was asked to make "cloud1", destroy "cloud1", make "cloud1"
    And a cloud event says "failed cloud1"
    And 1 cloud event says "made cloud1"
    And Vultr holds 1 instance labelled "cloud1"

  Scenario: A box is not said up until its first dispatch reaches the beads server
    Given every host is full
    And 1 story waits for any host
    When the cloud check runs
    Then a cloud event says "made cloud1"
    And 0 cloud events say "up cloud1"
    And mw status says "cloud1  up 0s, waiting for its first dispatch"
    When "cloud1" dispatches and its sync reaches the beads server
    And a minute passes and the cloud check runs
    Then a cloud event says "up cloud1: its first dispatch reached the beads server"
    When a minute passes and the cloud check runs
    Then 1 cloud event says "up cloud1"
    And mw status says "cloud1  up 2m, idle"

  Scenario: A box whose dispatch never reaches the beads server is said up, not dispatching, and why
    Given "cloud1" last reached the beads server at "2026-10-09T11:00:00Z", before this box
    And every host is full
    And 1 story waits for any host
    When the cloud check runs
    And 19 minutes pass and the cloud check runs
    Then 0 cloud events say "not dispatching"
    When 2 minutes pass and the cloud check runs
    Then a cloud event says "cloud1 up, not dispatching: no dispatch of cloud1 has reached the beads server in 21m since it was made"
    And a cloud event says "10-09 11:00Z, before this box was made"
    And 0 cloud events say "up cloud1"
    And mw status says "cloud1  up 21m, not dispatching"
    When 3 minutes pass and the cloud check runs
    Then 1 cloud event says "not dispatching"
    When "cloud1" dispatches and its sync reaches the beads server
    And a minute passes and the cloud check runs
    Then a cloud event says "up cloud1: its first dispatch reached the beads server"
