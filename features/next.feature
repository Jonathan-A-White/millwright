Feature: Closing out a finished story and carrying on
  mw next runs when a story's session ends: the dispatch command line chains it
  after the harness exits, whatever the harness exited with. It reads what the
  session reported and, on a good run, checks the work, lands the story's branch
  on its target branch under the rig's merge slot, appends one line to the seat's
  ledger, closes the story and dispatches whatever is ready next. On a bad run
  nothing is landed and nothing is closed: what happened is written on the story
  and in the ledger, and mw stops there.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: A finished story is landed, ledgered with its fuel, closed, and the next story dispatched
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "success",
        "is_error": false,
        "num_turns": 37,
        "duration_ms": 1680000,
        "session_id": "s-1",
        "total_cost_usd": 4.21,
        "usage": {
          "input_tokens": 1200,
          "output_tokens": 18000,
          "cache_read_input_tokens": 280000,
          "cache_creation_input_tokens": 12000
        }
      }
      """
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And it landed as a fast-forward
    And the story "mw-gq6.1" is closed
    And the last ledger line holds:
      | mw-gq6.1       |
      | landed on main |
      | opus/high      |
      | 311,200 tokens |
      | 37 turns       |
      | $4.21          |
    And the rig's tests were run 1 times
    And nothing is left of the worktree of "mw-gq6.1"
    And a fresh session is running for "mw-gq6.2"

  Scenario: The ledger is only ever appended to
    Given the ledger already holds a line from an earlier story
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the ledger still holds every line it held before
    And the last ledger line names "mw-gq6.1"

  Scenario: A story refused twice is charged for its session's fuel once
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "success",
        "is_error": false,
        "num_turns": 26,
        "duration_ms": 900000,
        "session_id": "s-3",
        "total_cost_usd": 0.44,
        "usage": {
          "input_tokens": 1200,
          "output_tokens": 18000,
          "cache_read_input_tokens": 280000,
          "cache_creation_input_tokens": 12000
        }
      }
      """
    And the rig's tests fail, saying "remote: fatal error in commit_refs"
    When mw closes out "mw-gq6.1"
    And mw closes out "mw-gq6.1" a second time
    Then the ledger holds 2 lines for "mw-gq6.1"
    And the first ledger line for "mw-gq6.1" holds:
      | not landed     |
      | 311,200 tokens |
      | session s-3    |
    And the last ledger line holds:
      | not landed      |
      | already charged |
      | session s-3     |
    And the last ledger line holds no token figure
    And the ledger still holds, unchanged, what the first close-out left in it
    And mw status counts today's fuel as 311,200 tokens

  Scenario: A story refused and then landed is charged for its session's fuel once
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "success",
        "is_error": false,
        "num_turns": 26,
        "duration_ms": 900000,
        "session_id": "s-3",
        "total_cost_usd": 0.44,
        "usage": {
          "input_tokens": 1200,
          "output_tokens": 18000,
          "cache_read_input_tokens": 280000,
          "cache_creation_input_tokens": 12000
        }
      }
      """
    And the rig's tests fail, saying "remote: fatal error in commit_refs"
    When mw closes out "mw-gq6.1"
    Given the rig's tests pass
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And the ledger holds 2 lines for "mw-gq6.1"
    And the last ledger line holds:
      | landed on main  |
      | already charged |
      | session s-3     |
    And the last ledger line holds no token figure
    And mw status counts today's fuel as 311,200 tokens

  Scenario: A failing test leaves the story open and blocked, and nothing is landed
    Given the session of "mw-gq6.1" reported a plain success
    And the rig's tests fail, saying "undefined: Ledger"
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And the story "mw-gq6.1" carries a comment quoting: undefined: Ledger
    And the worktree of "mw-gq6.1" is still there
    And the last ledger line holds:
      | mw-gq6.1   |
      | not landed |
    And no fresh session was started

  Scenario: Tests that could not be run are not reported as failing, and nothing is landed
    Given the session of "mw-gq6.1" reported a plain success
    And the rig's tests cannot be run, saying "make: go: No such file or directory"
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And the story "mw-gq6.1" carries a comment quoting: could not be run
    And the story "mw-gq6.1" carries a comment quoting: make: go: No such file or directory
    And the story "mw-gq6.1" carries no comment quoting: tests fail
    And the worktree of "mw-gq6.1" is still there
    And the last ledger line holds:
      | mw-gq6.1                            |
      | not landed                          |
      | could not be run                    |
    And no fresh session was started

  Scenario: An errored session is recorded truthfully and nothing is landed
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "error_during_execution",
        "is_error": true,
        "num_turns": 4,
        "duration_ms": 30000,
        "session_id": "s-2",
        "total_cost_usd": 0.12,
        "result": "the session ran out of fuel",
        "usage": {"input_tokens": 10, "output_tokens": 20, "cache_read_input_tokens": 23000}
      }
      """
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And the story "mw-gq6.1" carries a comment quoting: the session ran out of fuel
    And the last ledger line holds:
      | mw-gq6.1      |
      | not landed    |
      | 23,030 tokens |
    And the worktree of "mw-gq6.1" is still there
    And no fresh session was started
    And the rig's tests were run 0 times

  Scenario: A session that left no result at all is not taken for a success
    Given the session of "mw-gq6.1" left no result at all
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And no fresh session was started

  Scenario: A branch with no commits on it is not landed
    Given the story "mw-gq6.9" has been worked in its own worktree, committing nothing
    And the session of "mw-gq6.9" reported a plain success
    When mw closes out "mw-gq6.9"
    Then nothing was landed on "main"
    And the story "mw-gq6.9" is not closed
    And the story "mw-gq6.9" is held blocked
    And the rig's tests were run 0 times

  Scenario: A branch with no commits but a dirty worktree says the session left work uncommitted
    Given the story "mw-gq6.9" has been worked in its own worktree, committing nothing
    And the session of "mw-gq6.9" left these uncommitted in its worktree:
      | README.md          |
      | notes/half-done.md |
      | the-feature.go     |
    And the session of "mw-gq6.9" reported a plain success
    When mw closes out "mw-gq6.9"
    Then nothing was landed on "main"
    And the story "mw-gq6.9" is not closed
    And the story "mw-gq6.9" is held blocked
    And the worktree of "mw-gq6.9" is still there
    And the rig's tests were run 0 times
    And the comment on "mw-gq6.9" and the report say uncommitted work was left, listing:
      | README.md          |
      | notes/half-done.md |
      | the-feature.go     |
    And the last ledger line holds:
      | mw-gq6.9         |
      | uncommitted work |
      | the-feature.go   |

  Scenario: A commit signed by an AI is not landed
    Given the session of "mw-gq6.1" reported a plain success
    And a commit on the branch of "mw-gq6.1" carries "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And the story "mw-gq6.1" carries a comment quoting: Co-Authored-By: Claude Sonnet 5
    And the comment on "mw-gq6.1" and the report name that commit
    And the worktree of "mw-gq6.1" is still there
    And the last ledger line holds:
      | mw-gq6.1   |
      | not landed |
    And the rig's tests were run 0 times
    And no fresh session was started

  Scenario: A commit that says it was generated with an AI is not landed either
    Given the session of "mw-gq6.1" reported a plain success
    And a commit on the branch of "mw-gq6.1" carries "Generated with [Claude Code](https://claude.com/claude-code)"
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is held blocked
    And the comment on "mw-gq6.1" and the report name that commit
    And the worktree of "mw-gq6.1" is still there

  Scenario: A formula step the session never closed stops the close-out
    Given the session of "mw-gq6.1" reported a plain success
    And a formula was poured for "mw-gq6.1" and one of its steps is still open
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" is held blocked
    And the story "mw-gq6.1" carries a comment quoting: formula step

  Scenario: The target branch moved on the origin while the story was worked
    Given the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the other host's work is still on "main" at the rig's origin
    And it landed as a merge commit
    And the rig's tests were run 2 times
    And the story "mw-gq6.1" is closed

  Scenario: The push is rejected once and then succeeds
    Given the other host lands its own work the moment mw first tries to push
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the other host's work is still on "main" at the rig's origin
    And mw pushed twice and forced nothing
    And the story "mw-gq6.1" is closed

  Scenario: A push the origin refuses with a many-line error keeps the whole error beside the run
    Given the session of "mw-gq6.1" reported a plain success
    And the origin refuses every push, saying:
      """
      refusing to update refs/heads/main
      the branch is protected
      required status checks are expected
      contact an administrator
      fatal error in commit_refs
      """
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the ledger holds exactly one line for "mw-gq6.1"
    And that ledger line holds only the first line of what the origin said
    And the run of "mw-gq6.1" holds a landing error with every line the origin said
    And the comment on "mw-gq6.1" quotes every line the origin said
    And mw committed to the vault exactly:
      | seats/builder/ledger.md            |
      | seats/builder/rigs/millwright.md   |
      | runs/mw-gq6.1/result.json          |
      | runs/mw-gq6.1/landing-error.txt    |

  Scenario: A story claimed here with no session behind it is not left looking like work in flight
    Given the story "mw-gq6.3" is claimed here with no session behind it
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the story "mw-gq6.3" is recorded as stopped
    And the story "mw-gq6.3" carries a comment quoting: claimed here with no session behind it
    And the story "mw-gq6.3" is not closed

  Scenario: A close the tracker refuses after the landing leaves the story landed but open, and the next run closes it
    Given the session of "mw-gq6.1" reported a plain success
    And the tracker refuses to close "mw-gq6.1", saying: assignee is root, actor is mw@vps; reclaim or use --force
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" is not closed
    And the close-out says the story is landed but still open
    And the last ledger line names "mw-gq6.1"
    And nothing is left of the worktree of "mw-gq6.1"
    Given the tracker will take a close of "mw-gq6.1" again
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And the ledger holds exactly one line for "mw-gq6.1"
    And git was asked to merge once and to push once
    And the rig's tests were run 1 times

  Scenario: What mw and the session wrote in the vault is committed, so the sync that follows can run
    Given the session of "mw-gq6.1" reported a plain success
    And the run of "mw-gq6.1" left its boot file beside the result
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And mw committed to the vault exactly:
      | seats/builder/ledger.md          |
      | seats/builder/rigs/millwright.md |
      | runs/mw-gq6.1/result.json        |
    And that vault commit names "mw-gq6.1" and signs nothing
    And the vault commit does not hold "runs/mw-gq6.1/boot.md"
    And the hosts were brought level
    And a fresh session is running for "mw-gq6.2"

  Scenario: Any other uncommitted vault file still stops the sync, and the story is landed and closed all the same
    Given the session of "mw-gq6.1" reported a plain success
    And the vault holds work of its own that nobody committed, to "seats/mayor/ledger.md"
    And the story "mw-gq6.2" is planned and ready to be worked here
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" is closed
    And the close-out says the hosts could not be brought level, naming "seats/mayor/ledger.md"
    And no fresh session was started

  Scenario: A close-out that lands nothing commits the line it wrote saying so
    Given the session of "mw-gq6.1" reported a plain success
    And the rig's tests fail, saying "still red here"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is not closed
    And mw committed to the vault exactly:
      | seats/builder/ledger.md          |
      | seats/builder/rigs/millwright.md |
      | runs/mw-gq6.1/result.json        |

  Scenario: A session that left no result is not landed, and the close-out says the run record was missing
    Given the session of "mw-gq6.1" left no result at all
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is not closed
    And mw committed to the vault exactly:
      | seats/builder/ledger.md          |
      | seats/builder/rigs/millwright.md |
    And the report says the run record of "mw-gq6.1" was missing

  Scenario: A ledger line an earlier run could not commit is committed by the run that closes the story
    Given the session of "mw-gq6.1" reported a plain success
    And the vault refuses a commit, saying: another git process seems to be running
    And the tracker refuses to close "mw-gq6.1", saying: assignee is root, actor is mw@vps; reclaim or use --force
    When mw closes out "mw-gq6.1"
    Then the close-out says the story is landed but still open
    And the close-out notes that the vault could not be committed
    Given the vault takes a commit again
    And the tracker will take a close of "mw-gq6.1" again
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And the ledger holds exactly one line for "mw-gq6.1"
    And mw committed to the vault exactly:
      | seats/builder/ledger.md          |
      | seats/builder/rigs/millwright.md |
      | runs/mw-gq6.1/result.json        |

  Scenario: A re-run that finds the run record gone still commits the rest, and says so
    Given the session of "mw-gq6.1" reported a plain success
    And the vault refuses a commit, saying: another git process seems to be running
    And the tracker refuses to close "mw-gq6.1", saying: assignee is root, actor is mw@vps; reclaim or use --force
    When mw closes out "mw-gq6.1"
    Then the close-out says the story is landed but still open
    Given the vault takes a commit again
    And the tracker will take a close of "mw-gq6.1" again
    And the session of "mw-gq6.1" left no result at all
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And mw committed to the vault exactly:
      | seats/builder/ledger.md          |
      | seats/builder/rigs/millwright.md |
    And the report says the run record of "mw-gq6.1" was missing

  Scenario: The merge slot is given back once the landing is done
    Given the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the merge slot of the rig is free again

  Scenario: A close-out that lands nothing gives the merge slot back too
    Given the session of "mw-gq6.1" reported a plain success
    And the rig's tests fail, saying "still red"
    When mw closes out "mw-gq6.1"
    Then the merge slot of the rig is free again
