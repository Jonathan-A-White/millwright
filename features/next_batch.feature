Feature: A rig that lands in batches queues each story that passes its close-out
  A rig whose file says landing = "batch" lands its stories several at a time,
  under one gate (plans/0117, Land Rush). mw next runs every check it runs today
  and then, in place of the merge: pushes the story's branch to the rig's origin
  as mw/<id>, records run=queued, writes a ledger line saying it is queued,
  closes the session and dispatches the next story. The worktree stays, and the
  target branch is not touched. When mw check passed on the very commit being
  queued, mw next trusts that run and does not run the rig's tests again, and
  the ledger names the commit it trusted. A rig on landing = "each", or with no
  landing key at all, lands as it always has.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: A batch rig's story that passes its close-out is queued, not landed
    Given the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    And the terminal still holds the exited session of "mw-gq6.1"
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    And the branch of "mw-gq6.1" is on the rig's origin as "mw/mw-gq6.1"
    And nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the last ledger line holds:
      | mw-gq6.1                              |
      | queued for batch landing              |
      | mw next re-ran the rig's tests: pass  |
    And the rig's tests were run 1 times
    And the worktree of "mw-gq6.1" is still there
    And the terminal holds no session of "mw-gq6.1"
    And a fresh session is running for "mw-gq6.2"

  Scenario: A rig that says landing = "each" lands the same story as today
    Given the rig lands its stories each by itself
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And it landed as a fast-forward
    And the story "mw-gq6.1" is closed
    And the last ledger line holds:
      | mw-gq6.1                             |
      | landed on main                       |
      | mw next re-ran the rig's tests: pass |
    And the rig's tests were run 1 times
    And nothing is left of the worktree of "mw-gq6.1"

  Scenario: mw check passed on the branch's tip, so the close-out trusts it and does not run the tests again
    Given the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    When a session checks "mw-gq6.1"
    And mw closes out "mw-gq6.1"
    Then the check passes
    And the story "mw-gq6.1" is recorded as queued for batch landing
    And the rig's tests were run 1 times
    And the last ledger line says it was tested by mw check at the tip of "mw-gq6.1"
    And the last ledger line does not say "mw next re-ran the rig's tests"

  Scenario: The branch moved on after mw check passed, so the close-out runs the tests itself
    Given the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    When a session checks "mw-gq6.1"
    Then the check passes
    Given "mw-gq6.1" wrote the file "more.md" holding "more work after the check"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    And the rig's tests were run 2 times
    And the last ledger line holds:
      | queued for batch landing             |
      | mw next re-ran the rig's tests: pass |
    And the last ledger line does not say "tested by mw check"

  Scenario: mw check failed on the branch's tip, so the close-out runs the tests itself
    Given the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    And the rig's tests fail the first time and pass the second, saying "flaky"
    When a session checks "mw-gq6.1"
    And mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    And the last ledger line holds:
      | mw next re-ran the rig's tests: pass |

  Scenario: A queued story closed out again is left as it was
    Given the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    And mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    And the ledger holds exactly one line for "mw-gq6.1"
    And the close-out says the story is queued already

  Scenario: A story its batch landed and left open, claimed on another host, is closed by the next mw next
    Given the tracker acts as "mw@laptop", this host's mw
    And the rig lands its stories in batches
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    Given the tracker refuses to close "mw-gq6.1", saying: database is locked
    When mw lands the batch of the rig
    Then the story "mw-gq6.1" is recorded as landed and is still open
    Given the tracker will take a close of "mw-gq6.1" again
    And "mw-gq6.1" is claimed by "mw@cloud1", on another host
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And the comment on "mw-gq6.1" says: mw@laptop took the claim over from mw@cloud1 to close it

  Scenario: A rig flipped to batch while its story ran is queued, not stopped
    Given the rig lands its stories each by itself
    And the session of "mw-gq6.1" reported a plain success
    And the rig's file is changed to landing = "batch" while the rig's tests run
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is recorded as queued for batch landing
    And the branch of "mw-gq6.1" is on the rig's origin as "mw/mw-gq6.1"
    And nothing was landed on "main"
    And the close-out never says "only mw land pushes"
    And the close-out says the rig's file was read again after the tests and said batch
