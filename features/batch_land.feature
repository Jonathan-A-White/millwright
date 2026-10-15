Feature: mw land lands a batch rig's queue under one gate run
  On a rig whose file says landing = "batch", mw next queues each story that
  passes its close-out (features/next_batch.feature). mw land <rig> lands the
  queue (plans/0117, Land Rush 3): in a landing worktree detached at the
  target branch as the origin has it, it merges the queued branches oldest
  first by the rig's union rules, raises the version once, runs the rig's tests
  once on the batch under the gate lock and the calm rule, and only when that
  run is green and no branch was set aside does it push the target branch, as
  a fast-forward. Each story is then closed run=landed with a ledger line
  naming the batch. A branch that conflicts is set aside and stays queued; a
  red gate pushes nothing. Either way the batch is held, every story in it
  stays queued, and one mail tells the Mayor each story and why. What the
  batch did is kept under runs/batch/<rig>/<n>/.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the rig lands its stories in batches

  Scenario: Three queued branches land in one push after one gate run
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the rig's tests were run once on the batch
    And these stories are on "main" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch 1:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And the batch records of batch 1 hold the merge order, the gate log and the outcome "landed"
    And the Mayor was sent no mail saying the batch was held

  Scenario: A fourth branch that conflicts is set aside and stays queued while the other three land
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And "mw-gq6.4" is queued for batch landing, writing "mw-gq6.1.md" with words of its own
    When mw lands the batch of the rig
    Then the batch set "mw-gq6.4" aside, naming "mw-gq6.1.md" in its conflict
    And the story "mw-gq6.4" is still queued for batch landing
    And the Mayor's mail names "mw-gq6.4" and its conflict in "mw-gq6.1.md"
    And these stories are on "main" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch 1:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And the rig's tests were run once on the batch

  Scenario: A red gate pushes nothing, and the whole batch is held and mailed
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And the rig's tests fail, saying "the batch is red"
    When mw lands the batch of the rig
    Then "main" at the rig's origin is where it was before the batch
    And these stories are still queued for batch landing, none of them closed:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And the batch is held, and the Mayor's mail names every story in it and says "the batch is red"
    And the batch records of batch 1 hold the merge order, the gate log and the outcome "held"

  Scenario: --dry-run prints the batch it would make and changes nothing
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig with --dry-run
    Then the dry run names "mw-gq6.1" then "mw-gq6.2" in the order it would merge them
    And "main" at the rig's origin is where it was before the batch
    And these stories are still queued for batch landing, none of them closed:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the rig's tests were not run on the batch
    And no batch record was kept
    And the Mayor was sent no mail saying the batch was held

  Scenario: The batch raises the version once, with one changelog entry per story
    Given the rig's main holds a package.json and a package-lock.json at version "1.4.2"
    And the rig's file in the vault also holds the line "version_files = ['package.json', 'package-lock.json']"
    And the rig's file in the vault also names the changelog files "public/changelog.json" and "CHANGELOG.md"
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And "main" at the rig's origin holds version "1.4.3" in "package.json" and "package-lock.json"
    And "main" at the rig's origin has exactly 1 commits saying "Version 1.4.3 (batch 1 of millwright)"
    And "public/changelog.json" at the rig's origin lists the versions "1.4.3" then "1.4.3"
    And the newest entry of "public/changelog.json" at the rig's origin is for "mw-gq6.2", the next for "mw-gq6.1"
