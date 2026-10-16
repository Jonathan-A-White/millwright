Feature: A run leaves nothing stray in the vault
  mw writes some files into the vault that it never commits: a session's boot
  file, the temp file its result is redirected into, the close-out's own log,
  mw check's log, the seats' reaper and talk logs and acting files, the key
  metrics. The .gitignore mw init lays in a fresh vault names each of them, so
  that after a run git status of the vault is quiet and a seat booted into the
  vault is not shown hundreds of leftovers (mw-gq6.461). What mw does commit is
  never ignored: a close-out, a retry, a suite run and a batch still commit
  their own files, and anything they leave uncommitted still shows.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And the vault holds the .gitignore mw init lays
    And the vault is a real git clone
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"

  Scenario: A story run by its launch line, checked and closed out leaves git status of the vault quiet
    Given the story "mw-gq6.1" has been worked in its own worktree
    When a session checks "mw-gq6.1"
    And the session of "mw-gq6.1" ends as its launch line runs it, its harness reporting success
    And mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And git status of the vault is quiet

  Scenario: A session killed before its result is moved into place leaves git status of the vault quiet
    Given the story "mw-gq6.1" has been worked in its own worktree
    When the session of "mw-gq6.1" is killed as its launch line runs it, before its result is moved into place
    Then the run of "mw-gq6.1" holds its result's temp file
    And git status of the vault is quiet

  Scenario: A batch landed leaves git status of the vault quiet
    Given the rig lands its stories in batches
    And what the vault holds so far is committed
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And git status of the vault is quiet

  Scenario: What the seats, the daily report and the todo ranking write beside the runs leaves git status of the vault quiet
    When the mayor's and the millhand's reapers each note a line
    And the mayor's talk model notes a line
    And the mayor is handed over
    And the day's key metrics are kept
    And a todo ranking pass keeps its log
    Then git status of the vault is quiet

  Scenario: What the vault's own scripts write for a seat's session is ignored
    Then the vault ignores each of these:
      | .mayor-launch.sh              |
      | .mayor-settings.json          |
      | .millhand-laptop-launch.sh    |
      | .millhand-laptop-acting       |
      | .mayor-acting.tmp             |

  Scenario: What mw commits is never ignored, so a file it left uncommitted still shows
    Then the vault ignores none of these:
      | runs/mw-gq6.1/result.json                       |
      | runs/mw-gq6.1/result-2.json                     |
      | runs/mw-gq6.1/landing-error.txt                 |
      | runs/mw-gq6.1/gate.log                          |
      | runs/mw-gq6.1/gate-2.log                        |
      | runs/mw-gq6.1/close-out-unrecorded.json         |
      | runs/mw-gq6.1/attempt-1.bundle                  |
      | runs/batch/millwright/1/merge-order.txt         |
      | runs/batch/millwright/1/gate.log                |
      | runs/batch/millwright/1/outcome.txt             |
      | runs/suite/millwright/2026-10-16.json           |
      | runs/suite/millwright/2026-10-16.log            |
      | seats/builder/ledger.md                         |
      | plans/investments/ledger.jsonl                  |
