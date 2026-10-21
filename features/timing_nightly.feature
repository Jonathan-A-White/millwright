Feature: The follower runs a rig's timing tests on the home at night
  A rig whose file says timing_tests keeps its tests that need a real clock out
  of the landing gate. On the home, from the suite's hour and after the night's
  suite, the follower runs them on a calm host with MW_TIMING=only when a file a
  tagged test covers has changed since the last night they passed. It keeps the
  result in the vault with the host's uptime, and files one held [bug] under
  the rig's suite_epic for each failing file, never a second for a file that
  already has one open. The pass is told in the log as the job timing@<host>.

  Background:
    Given the timing follower is running on "desktop" and the home is "desktop"
    And the rig "lampas" says timing_tests = "tests/timing/**" and suite_epic = "mw-suite"
    And the timing tests of "lampas" are "tests/timing/sw.test.ts" covering "src/sw/**"
    And main of "lampas" is at "bbb"
    And the last passing timing night of "lampas" was at "aaa" on "2026-10-19"
    And the uptime reads "up 3 days, load average: 0.20"

  Scenario: A covered file changed and the run fails in one file: one held bug names the file, the commit and the uptime
    Given "src/sw/backoff.ts" changed in "lampas" between "aaa" and "bbb"
    And the timing run of "lampas" fails in "tests/timing/sw.test.ts"
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    Then the timing command of "lampas" was run with MW_TIMING=only 1 time
    And the suite epic "mw-suite" holds 1 bug
    And the new story for "tests/timing/sw.test.ts" is held and labelled bug and timing
    And that bug names "tests/timing/sw.test.ts", "bbb", "src/sw/backoff.ts" and "load average: 0.20"
    And the timing result of "lampas" for "2026-10-21" is kept as failed at "bbb"
    And the timing log tells the job "timing@desktop" as scheduled, running, done

  Scenario: A second failing night of the same file files no second bug
    Given "src/sw/backoff.ts" changed in "lampas" between "aaa" and "bbb"
    And the timing run of "lampas" fails in "tests/timing/sw.test.ts"
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    And the timing clock reads "2026-10-22 09:00" UTC
    And the timing follower runs a cycle
    Then the timing command of "lampas" was run with MW_TIMING=only 2 times
    And the suite epic "mw-suite" holds 1 bug

  Scenario: A passing night keeps a result that passed, with its commit
    Given "src/sw/backoff.ts" changed in "lampas" between "aaa" and "bbb"
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    Then the timing result of "lampas" for "2026-10-21" is kept as passed at "bbb"
    And the suite epic "mw-suite" holds 0 bugs

  Scenario: Only a file no timing test covers changed: no run, nothing kept
    Given "README.md" changed in "lampas" between "aaa" and "bbb"
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    Then the timing command of "lampas" was run with MW_TIMING=only 0 times
    And nothing is kept for "lampas" on "2026-10-21"
    And the log has no event of the job "timing@desktop"

  Scenario: A host that never calms skips the night and says how loaded it was
    Given "src/sw/backoff.ts" changed in "lampas" between "aaa" and "bbb"
    And the timing host is at load 6.5 of 4 cores
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    Then the timing command of "lampas" was run with MW_TIMING=only 0 times
    And nothing is kept for "lampas" on "2026-10-21"
    And the suite epic "mw-suite" holds 0 bugs
    And the last event of the job "timing@desktop" is done and says "6.5 of 4 cores"

  Scenario: On a host that is not the home it never runs
    Given the timing follower is running on "laptop" and the home is "desktop"
    And "src/sw/backoff.ts" changed in "lampas" between "aaa" and "bbb"
    And the timing follower has taken its bearings
    When the timing clock reads "2026-10-21 09:00" UTC
    And the timing follower runs a cycle
    Then the timing command of "lampas" was run with MW_TIMING=only 0 times
