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

  Scenario: A suite still running when the follower restarts is adopted, and its real outcome is told
    Given the nightly suite runs apart from the follower
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower begins a cycle
    And the suite of "lampas" is running
    And the nightly follower stops
    And the nightly follower begins a cycle
    And the suite of "lampas" is running
    And the suite of "lampas" ends having failed
    And the nightly follower finishes its cycle
    Then "mw suite lampas --file" was begun 1 time
    And the last event of the job "suite@laptop" is failed
    And the log tells the job "suite@laptop" as scheduled, running, done, scheduled, running, failed

  Scenario: A suite that ends well after a restart is told as done, not as cut short
    Given the nightly suite runs apart from the follower
    And the nightly clock reads "2026-10-13 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 08:00" UTC
    And the nightly follower begins a cycle
    And the suite of "lampas" is running
    And the nightly follower stops
    And the nightly follower begins a cycle
    And the suite of "lampas" is running
    And the suite of "lampas" ends having passed
    And the nightly follower finishes its cycle
    Then "mw suite lampas --file" was begun 1 time
    And the last event of the job "suite@laptop" is done
    And that event does not say the follower stopped

  # The first cycle of a follower only reads the log, so each scenario below runs one first.
  # suite_every and suite_host (mw-t5shinh.15): a rig that names both has its
  # whole suite run on its suite host whenever main has moved and suite_every has
  # passed since the newest kept result began; the home keeps the nightly only as
  # a fallback for a suite host that is down.

  Scenario: The suite host runs the suite when the newest result began 5 hours ago at another sha than main's head
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 05:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 10:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time

  Scenario: The suite host does not run the suite when the newest result began 3 hours ago
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 07:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 10:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: The suite host does not run the suite when main's head is the sha of the newest result
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 05:00" UTC at "abc"
    And main's head of "lampas" is "abc"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 10:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: The suite host runs at any hour of the day, not only from the nightly hour
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 00:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-13 04:30" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time

  Scenario: The suite host runs again after suite_every when main has moved, and not before
    Given the nightly follower is running on "desktop" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And main's head of "lampas" is "def"
    And the nightly follower runs a cycle
    And the nightly clock reads "2026-10-13 10:00" UTC
    And the nightly follower runs a cycle
    When main's head of "lampas" is "ghi"
    And the nightly clock reads "2026-10-13 13:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
    When the nightly clock reads "2026-10-13 14:30" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 2 times

  Scenario: The home's follower does not run the suite at the nightly hour while the newest result is 10 hours old
    Given the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 22:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly clock reads "2026-10-14 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-14 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: The home's follower runs the suite at the nightly hour when the newest result is 27 hours old
    Given the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-13 05:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly clock reads "2026-10-14 07:55" UTC
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-14 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time

  Scenario: A host that is neither the suite host nor the home never runs it
    Given the nightly follower is running on "mercy" and the home is "laptop"
    And the rig "lampas" says suite_every = "4h" and suite_host = "desktop"
    And the newest result of "lampas" began "2026-10-12 05:00" UTC at "abc"
    And main's head of "lampas" is "def"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-14 09:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 0 times

  Scenario: A rig that names neither key keeps its nightly as before
    Given the newest result of "lampas" began "2026-10-13 22:00" UTC at "abc"
    And the nightly follower runs a cycle
    When the nightly clock reads "2026-10-14 08:00" UTC
    And the nightly follower runs a cycle
    Then "mw suite lampas --file" was run 1 time
