Feature: A rig's landing gate runs only the tests related to the branch's changes
  A rig whose whole test line takes the better part of an hour can name a
  [related_tests] line beside its [tests] line: a command with {files} in it,
  which the landing gate runs in place of [tests], given the files the story's
  branch added, copied, modified or renamed since its merge-base with the target
  branch. A batch landing is given the union of its stories' files. A rig with no
  such line, a branch that left no file to give it, mw check and mw suite all run
  the rig's whole [tests] line as they always did; the nightly suite is what
  runs everything.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"

  Scenario: The gate of a story runs the related-tests line in place of the tests, on the files the story changed
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that logs the files it is given
    And the story "mw-gq6.1" also added the file "src/dates.ts"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" is closed
    And the related-tests command was run once, given: mw-gq6.1.md src/dates.ts
    And the rig's tests were run 0 times

  Scenario: A rig with no related-tests line runs its tests as it always did
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" also added the file "src/dates.ts"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the related-tests command was not run
    And the rig's tests were run 1 times

  Scenario: A branch that only deleted files runs the whole tests, and says so on stderr
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that logs the files it is given
    And the story "mw-gq6.1" changed nothing but deleting the file "README.md"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the related-tests command was not run
    And the rig's tests were run 1 times
    And mw said on stderr: no file to run the related-tests line on

  Scenario: A failing related-tests line refuses the landing as tests-fail
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that fails, saying "1 failed: dates.test.ts:12"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the report says it stopped for "tests-fail"
    And the story "mw-gq6.1" carries a comment quoting: 1 failed: dates.test.ts:12
    And the related-tests command was run once, given: mw-gq6.1.md
    And the rig's tests were run 0 times

  Scenario: The gate on a merged result runs the related-tests line too
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that logs the files it is given
    And the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the rig's tests were run 0 times
    And the related-tests command was run 2 times, each given: mw-gq6.1.md

  Scenario: The gate of a batch runs the related-tests line on the union of its stories' files
    # Each story's own close-out has run it on that story's files first.
    Given the rig lands its stories in batches
    And the rig has a related-tests command that logs the files it is given
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the related-tests command was last given: mw-gq6.1.md mw-gq6.2.md
    And the rig's tests were not run on the batch

  Scenario: The version a batch raises is not one of the files its related-tests line is given
    Given the rig lands its stories in batches
    And the rig's main holds a package.json and a package-lock.json at version "1.4.2"
    And the rig's file in the vault also holds the line "version_files = ['package.json', 'package-lock.json']"
    And the rig has a related-tests command that logs the files it is given
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And "main" at the rig's origin holds version "1.4.3" in "package.json" and "package-lock.json"
    And the related-tests command was last given: mw-gq6.1.md mw-gq6.2.md

  Scenario: The version a landing raises is not one of the files its related-tests line is given on the merged result
    Given the rig's main holds a package.json and a package-lock.json at version "0.1.0"
    And the rig's file in the vault names the version files "package.json" and "package-lock.json"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that logs the files it is given
    And the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And "main" at the rig's origin holds version "0.1.1" in "package.json" and "package-lock.json"
    And the related-tests command was run 2 times, each given: mw-gq6.1.md

  Scenario: A batch of a rig with no related-tests line runs its tests once
    Given the rig lands its stories in batches
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the related-tests command was not run
    And the rig's tests were run once on the batch

  Scenario: mw check runs the whole tests even for a rig with a related-tests line
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the rig has a related-tests command that logs the files it is given
    When a session checks "mw-gq6.1"
    Then the check passes
    And the rig's tests were run 1 times
    And the related-tests command was not run
