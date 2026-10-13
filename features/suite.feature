Feature: Running a rig's whole test suite on its main
  A story's gate runs only the tests it touched, so a landing can break a test
  nobody touched and no gate sees it until another story is refused for it.
  mw suite <rig> runs the rig's whole suite on a fresh worktree of its origin's
  main, once the host is calm, runs each failing test file once more alone, and
  keeps the result in the vault under runs/suite/<rig>/ for the day. It spends
  no tokens, and exits 1 when any file failed.

  Background:
    Given the suite rig "lampas" whose tests are "npm test"
    And the suite finds the origin's main at "9f3c2ab"
    And the suite is run on the day "2026-10-13" at 08:00 UTC

  Scenario: A suite that passes is written down and committed
    When mw suite runs "lampas"
    Then the suite exits 0
    And the suite ran "npm test" in a worktree named "suite-lampas" beside the rig, not in the rig's own checkout
    And the suite wrote the log and the result of "lampas" for "2026-10-13"
    And the suite result names the sha "9f3c2ab" and no failing file
    And the suite committed both files
    And the suite's worktree is gone
    And the suite prints: all passed

  Scenario: A file that fails twice and a file that fails once are told apart
    Given the whole run of the tests prints:
      """
      FAIL  src/dates.test.ts > parses dates
      AssertionError: expected 1 to be 2
       at src/dates.test.ts:12:5
      FAIL  src/config.test.ts > reads the config
      Error: boom
      """
    And the tests fail
    And alone, "src/dates.test.ts" fails again
    And alone, "src/config.test.ts" passes
    When mw suite runs "lampas"
    Then the suite exits 1
    And the suite ran "npm test src/dates.test.ts" alone
    And the suite ran "npm test src/config.test.ts" alone
    And the suite result lists "src/dates.test.ts" as failed twice
    And the suite result lists "src/config.test.ts" as flaky
    And the suite result names "parses dates" as the first failing test of "src/dates.test.ts"
    And the suite result quotes "AssertionError: expected 1 to be 2" for "src/dates.test.ts"
    And the suite committed both files
    And the suite prints: 2 files failed

  Scenario: The run waits for a busy host to calm and records the load it started at
    Given the host is at load 9.0 of 4 cores, then 6.0, then 1.5
    When mw suite runs "lampas"
    Then the suite exits 0
    And the suite read the load 3 times before it ran anything
    And the suite result records the load at start as 1.5 of 4 cores
    And the suite result says it did not give up waiting

  Scenario: A host that never calms is run on anyway, and the result says so
    Given the host is always at load 9.0 of 4 cores
    And the suite waits at most 1 hour for the host to calm
    When mw suite runs "lampas"
    Then the suite exits 0
    And the suite ran "npm test" in a worktree named "suite-lampas" beside the rig, not in the rig's own checkout
    And the suite result says it gave up waiting after 1 hour at load 9.0 of 4 cores
    And the suite prints: ran anyway

  Scenario: A rig with a changed-specs command has it run with no spec list
    Given the suite rig "lampas" has the changed-specs command "npm run build && npx playwright test --project=shots {specs}"
    When mw suite runs "lampas"
    Then the suite exits 0
    And the suite ran "npm run build && npx playwright test --project=shots " after "npm test"

  Scenario: A spec file that fails is run again alone with only that spec
    Given the suite rig "lampas" has the changed-specs command "npx playwright test {specs}"
    And the whole run of the specs prints:
      """
        1) [shots] › tests/e2e/immersive-reader.spec.ts:44:5 › shows the settings group ──────

          Error: expect(locator).toBeVisible()
          Locator: getByText('Immersive reader card')
      """
    And the specs fail
    And alone, "tests/e2e/immersive-reader.spec.ts" fails again
    When mw suite runs "lampas"
    Then the suite exits 1
    And the suite ran "npx playwright test tests/e2e/immersive-reader.spec.ts" alone
    And the suite result lists "tests/e2e/immersive-reader.spec.ts" as failed twice
    And the suite result quotes "expect(locator).toBeVisible()" for "tests/e2e/immersive-reader.spec.ts"

  Scenario: A rig with no changed-specs command runs only its tests
    When mw suite runs "lampas"
    Then the suite ran exactly 1 command

  Scenario: A rig with no tests entry is refused and nothing is written
    Given the suite rig "orchard" with no tests entry
    When mw suite runs "orchard"
    Then the suite is refused, saying: no [tests] entry
    And the suite made no worktree, ran nothing and wrote nothing
