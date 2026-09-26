Feature: A claim is a lease
  A story claimed through the work tracker carries a lease the tracker keeps,
  five minutes long as bd 1.3.0 grants it. A heartbeat from the claim's holder
  pushes it forward; a claim whose lease has run out with no heartbeat is a
  session that went away, which StaleClaims reports and which can be reclaimed
  — given back, open and unassigned, for any dispatcher to take. A claim is
  also conditional: it takes a story nobody else holds, and fails cleanly,
  writing nothing, when another assignee already holds it. A release is
  conditional the same way: it gives back only the claim its own actor holds,
  writing nothing when another actor holds the story instead.

  Background:
    Given an epic "mw-gq6" with the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |
    And a story "mw-gq6.1" of that epic with no overrides

  Scenario: A claim records a lease
    When the story "mw-gq6.1" is claimed at 10:00
    Then the story "mw-gq6.1" holds a lease until 10:05

  Scenario: A heartbeat extends the lease
    Given the story "mw-gq6.1" is claimed at 10:00
    When the claim on "mw-gq6.1" is heartbeaten at 10:03
    Then the story "mw-gq6.1" holds a lease until 10:08

  Scenario: An expired lease is reported by StaleClaims and can be reclaimed
    Given a story "mw-gq6.2" of that epic with no overrides
    And the story "mw-gq6.1" is claimed at 10:00
    And the story "mw-gq6.2" is claimed at 10:04
    Then the stale claims at 10:06 are "mw-gq6.1"
    When the claim on "mw-gq6.1" is reclaimed at 10:06
    Then the claim on "mw-gq6.1" was reclaimed
    And the story "mw-gq6.1" is open and held by nobody
    And the stale claims at 10:06 are ""

  Scenario: A claim whose lease still holds is not reclaimed
    Given the story "mw-gq6.1" is claimed at 10:00
    When the claim on "mw-gq6.1" is reclaimed at 10:04
    Then the claim on "mw-gq6.1" was not reclaimed
    And the story "mw-gq6.1" holds a lease until 10:05

  Scenario: A heartbeat on a reclaimed story fails, so its session learns to stop
    Given the story "mw-gq6.1" is claimed at 10:00
    And the claim on "mw-gq6.1" is reclaimed at 10:06
    When the claim on "mw-gq6.1" is heartbeaten at 10:07
    Then the heartbeat fails
    And the story "mw-gq6.1" is open and held by nobody

  Scenario: A conditional claim fails cleanly when another assignee holds the story
    Given the story "mw-gq6.1" is held by "mw@laptop" with a lease until 10:05
    When the story "mw-gq6.1" is claimed at 10:01
    Then the claim fails because "mw@laptop" holds the story
    And the story "mw-gq6.1" is held by "mw@laptop" with a lease until 10:05

  Scenario: A release by another actor leaves the claim untouched
    Given the story "mw-gq6.1" is held by "mw@laptop" with a lease until 10:05
    When the story "mw-gq6.1" is released
    Then the story "mw-gq6.1" is held by "mw@laptop" with a lease until 10:05

  Scenario: A session that runs 7 minutes keeps its claim's lease alive
    Given the story "mw-gq6.1" is claimed at 10:00
    And the session of "mw-gq6.1" is running
    When mw next heartbeats "mw-gq6.1" while its session runs for 7 minutes
    Then the story "mw-gq6.1" was heartbeated at least every 2 minutes
    And the story "mw-gq6.1" was never among the stale claims while its session ran
    And the stale claims at 10:07 are ""
    And mw sweep on "vps" at 10:07 finds nothing newly stuck

  Scenario: A dispatched session heartbeats its claim while the harness runs and stops when the harness exits
    When a dispatched session's shell line runs its harness for a while, heartbeating alongside it
    Then the heartbeat ran more than once while the harness ran
    And the heartbeat stopped once the harness exited
