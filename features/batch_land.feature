Feature: mw land lands a batch rig's queue under one gate run
  On a rig whose file says landing = "batch", mw next queues each story that
  passes its close-out (features/next_batch.feature). mw land <rig> lands the
  queue (plans/0117, Land Rush 3): in a landing worktree detached at the
  target branch as the origin has it, it merges the queued branches oldest
  first by the rig's union rules, raises the version once, runs the rig's tests
  once on the batch under the gate lock and the calm rule, and only when that
  run is green and no branch was set aside does it push the target branch, as
  a fast-forward. Each story is then closed run=landed with a ledger line
  naming the batch; one whose claim another host holds is closed by this host
  taking the claim over, which a comment on the story records. A branch that conflicts is set aside and stays queued; a
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
    And metrics/factory.jsonl holds a line for batch 1 of the rig: 3 stories, 1 gate run, no fixer, outcome "green"
    And the Mayor was sent no mail saying the batch was held

  Scenario: The after-landing command runs once for a batch whose rig checkout was already level with the batch commit (mw-gq6.484)
    Given the rig names a command to run after a landing
    And something else brings the rig checkout level with the origin the moment a push reaches it
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the rig's after-landing command ran once, in the rig checkout, at the commit that landed on "main"
    And the batch report says: after landing: <the command>: ok
    And the batch report does not say: not brought up

  Scenario: The ledger lines of a batch are committed in the vault before the after-landing command runs (mw-gq6.493)
    Given the vault is a real git clone
    And the rig names a command to run after a landing, which looks at what the vault holds uncommitted
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the rig's after-landing command found no modified tracked file in the vault
    And the vault holds no modified tracked file
    And the batch report says: record  runs/mw-gq6.2/after-landing.log
    And the vault's git history holds "runs/mw-gq6.2/after-landing.log", unmodified

  Scenario: A story claimed on another host is closed by this host once its batch lands, and the takeover is noted on it
    Given the tracker acts as "mw@laptop", this host's mw
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And "mw-gq6.2" is claimed by "mw@cloud1", on another host
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And these stories are on "main" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch 1:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the comment on "mw-gq6.2" says: mw@laptop took the claim over from mw@cloud1 to close it

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
    And metrics/factory.jsonl holds a line for batch 1 of the rig: 3 stories, 1 gate run, no fixer, outcome "held"

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
    And metrics/factory.jsonl holds no line for the rig
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

  # Land Rush 5: a batch that was set aside or red is given one fixer, a Builder
  # session in the batch's landing worktree, and mw runs the rig's tests on the
  # batch again itself once the fixer has ended: the fixer's word is not the gate.

  Scenario: A red batch starts exactly one fixer, told every branch, the tests' tail and what it may not do, and lands once mw's own run is green
    Given the rig's file in the vault also holds the line "batch_fixer_model = 'haiku'"
    And the rig's file in the vault also holds the line "batch_fixer_turns = 12"
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
      | mw-gq6.3 |
    And the rig's tests fail on the batch, saying "the batch is red", until "FIXED" is committed
    And mw land may start a fixer, which commits "FIXED" and says it touched "mw-gq6.2"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And the fixer was started on the model "haiku" with at most 12 turns
    And the fixer's prompt names the branch of every story in the batch and the tests' last words "the batch is red"
    And the fixer's prompt says it may not push, revert a branch, drop a story's change, or delete, skip or loosen a test without saying so on that story
    And the batch landed on "main" in one push, forcing nothing
    And the rig's tests were run twice on the batch
    And the ledger line of "mw-gq6.2" says "resolved by the fixer", and those of the others do not:
      | mw-gq6.1 |
      | mw-gq6.3 |
    And metrics/factory.jsonl holds a line for batch 1 of the rig: 3 stories, 2 gate runs, a fixer, outcome "fixed"

  Scenario: The fixer merges a branch that was set aside, keeping both sides, and mw's own run lands the whole batch
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And "mw-gq6.3" is queued for batch landing, writing "mw-gq6.1.md" with words of its own
    And mw land may start a fixer, which merges "mw-gq6.3" keeping both sides and says it touched "mw-gq6.3"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And the fixer's prompt names "mw-gq6.1.md" in the conflict of "mw-gq6.3"
    And the batch landed on "main" in one push, forcing nothing
    And the rig's tests were run once on the batch
    And these stories are on "main" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch 1:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And "mw-gq6.3" is closed and recorded as landed, and "mw-gq6.1.md" at the rig's origin holds the words of both "mw-gq6.1" and "mw-gq6.3"
    And the ledger line of "mw-gq6.3" says "resolved by the fixer", and those of the others do not:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the Mayor was sent no mail saying the batch was held

  Scenario: A batch of which every branch was set aside for its conflicts is given the one fixer too, and lands once it has merged them (mw-gq6.503)
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the other host landed a change to the same file as "mw-gq6.1" on "main"
    And the other host landed a change to the same file as "mw-gq6.2" on "main"
    And mw land may start a fixer, which merges "mw-gq6.1" and "mw-gq6.2" keeping both sides and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And the fixer's prompt names "mw-gq6.1.md" in the conflict of "mw-gq6.1"
    And the fixer's prompt names "mw-gq6.2.md" in the conflict of "mw-gq6.2"
    And the batch landed on "main" in one push, forcing nothing
    And the rig's tests were run once on the batch
    And "mw-gq6.1" is closed and recorded as landed, and "mw-gq6.1.md" at the rig's origin holds the words of both "mw-gq6.1" and "other host"
    And the Mayor was sent no mail saying the batch was held

  Scenario: A batch of which every branch was set aside, when the fixer merges none of them, is held and mailed, and no second fixer is started (mw-gq6.503)
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the other host landed a change to the same file as "mw-gq6.1" on "main"
    And the other host landed a change to the same file as "mw-gq6.2" on "main"
    And mw land may start a fixer, which commits "TRIED" and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And the rig's tests were not run on the batch
    And these stories are still queued for batch landing, none of them closed:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the batch is held, and the Mayor's mail names every story in it and says "no branch of the batch merged onto main, even after its fixer"

  Scenario: A push the origin refuses holds the batch without a fixer, though one may be started (mw-gq6.503)
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the origin refuses every push, saying:
      """
      push refused by the scenario
      """
    And mw land may start a fixer, which commits "TRIED" and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then no fixer was started
    And the batch is held, and the Mayor's mail names every story in it and says "the push to main failed"

  Scenario: A rig with nothing queued is given no fixer (mw-gq6.503)
    Given mw land may start a fixer, which commits "TRIED" and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then no fixer was started

  Scenario: When mw's own run after the fixer is red, the batch is held and mailed, and no second fixer is started
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the rig's tests fail, saying "the batch is still red"
    And mw land may start a fixer, which commits "TRIED" and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And the rig's tests were run twice on the batch
    And "main" at the rig's origin is where it was before the batch
    And these stories are still queued for batch landing, none of them closed:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the batch is held, and the Mayor's mail names every story in it and says "the batch is still red"
    And the Mayor's mail about the batch says the fixer was given it and no second fixer was started
    And metrics/factory.jsonl holds a line for batch 1 of the rig: 2 stories, 2 gate runs, a fixer, outcome "held"

  # mw next refuses a branch whose commits sign the work as a machine's
  # (AIAttribution); mw land holds a batch's branches to the same rule
  # (mw-gq6.506).

  Scenario: A branch with a signed commit is set aside naming the commit and the line, and a clean branch beside it still lands (mw-gq6.506)
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.3 |
    And "mw-gq6.2" is queued for batch landing, its branch then carrying a commit whose message says "Co-Authored-By: A Model <noreply@example.com>"
    When mw lands the batch of the rig
    Then the batch set "mw-gq6.2" aside, naming its signed commit and the line "Co-Authored-By: A Model <noreply@example.com>"
    And the story "mw-gq6.2" is still queued for batch landing
    And the Mayor's mail names "mw-gq6.2", its signed commit and the line "Co-Authored-By: A Model <noreply@example.com>"
    And the batch landed on "main" in one push, forcing nothing
    And these stories are on "main" at the rig's origin, closed and recorded as landed, each with a ledger line naming batch 1:
      | mw-gq6.1 |
      | mw-gq6.3 |
    And "mw-gq6.2.md" is not on "main" at the rig's origin

  Scenario: A fixer commit that signs the work as a machine's stops the batch before any push, naming the commit (mw-gq6.506)
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the rig's tests fail on the batch, saying "the batch is red", until "FIXED" is committed
    And mw land may start a fixer, which commits "FIXED" under a message saying "Generated with Claude Code" and says it touched "mw-gq6.1"
    When mw lands the batch of the rig
    Then exactly one fixer was started, for batch 1, in a landing worktree holding the work of every story in it
    And "main" at the rig's origin is where it was before the batch
    And these stories are still queued for batch landing, none of them closed:
      | mw-gq6.1 |
      | mw-gq6.2 |
    And the batch is held, and the Mayor's mail names every story in it and says "Generated with Claude Code"
    And the batch's hold names the fixer's signed commit
