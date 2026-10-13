Feature: The follower springs the nightly whole suite on the home
  A rig whose file says nightly_suite = true has its whole suite run once a day
  at a quiet hour (08:00 UTC unless the config says another), on the home host
  only, by the follower: it runs mw suite <rig> --file for each such rig, one
  at a time, and the pass is told in the log as the job suite@<host>. A day
  whose result is already kept is not run again.

  Background:
    Given the nightly follower is running on "laptop" and the home is "laptop"
    And the rig "lampas" says nightly_suite = true
    And the nightly suite is sprung at hour 8

  Scenario: It runs once a day at the hour, and again the next day
    Given the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
    When the nightly clock reads "2026-10-13 08:05" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
    When the nightly clock reads "2026-10-14 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 2 times

  Scenario: It does not run before the hour
    Given the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 07:59" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: Its pass is told in the log as the job suite@<host>
    Given the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    Then the log tells the job "suite@laptop" as scheduled, running, done

  Scenario: A suite that fails is told as a failed job, and is not run again that day
    Given the suite of "lampas" cannot be run
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    And the nightly clock reads "2026-10-13 08:10" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
    And the log tells the job "suite@laptop" as scheduled, running, failed

  Scenario: On a host that is not the home it never runs
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    And the nightly clock reads "2026-10-14 09:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: Two rigs run one after the other, never together
    Given the rig "mercy" says nightly_suite = true
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
    And "mw suite mercy --file" was run 1 time
    And no two suites were running together

  Scenario: A rig that is not opted in is never run
    Given the rig "cairn" says nightly_suite = false
    And the rig "plain" says nothing of the nightly suite
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite cairn --file" was run 0 times
    And "mw suite plain --file" was run 0 times
    And "mw suite lampas --file" was run 1 time

  Scenario: A rig whose day is already kept is not run again
    Given the result of "lampas" for "2026-10-13" is already kept
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: A different hour is the one the config names
    Given the nightly suite is sprung at hour 3
    And the nightly clock reads "2026-10-13 02:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 03:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
