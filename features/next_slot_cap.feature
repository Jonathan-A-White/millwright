Feature: A close-out that waits the overall most for the merge slot hands its story back for its close-out alone
  A close-out waits for its rig's merge slot while another close-out has it, up
  to the overall most a close-out waits (2 hours). Reaching it says nothing
  against the story: its session finished and its work is committed on its
  branch. So the story is not stopped and held for the Mayor, and not given
  back for a fresh Builder session either, as mw-emk3xt.1 was (mw-gq6.467): it
  keeps its claim, its worktree and its branch, is recorded handed back
  (run=close-out-again), and the next dispatch tick runs its close-out alone
  again (features/dispatch.feature).

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: The merge slot still held at the overall most hands the story back for its close-out alone
    Given the session of "mw-gq6.1" reported a plain success
    And the merge slot of the rig is still held at the overall most a close-out waits
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is recorded as handed back for its close-out alone
    And the story "mw-gq6.1" is not recorded as blocked
    And the story "mw-gq6.1" is not closed
    And the story "mw-gq6.1" keeps its claim
    And the worktree of "mw-gq6.1" is still there
    And the story "mw-gq6.1" carries a comment quoting: the next dispatch tick runs its close-out alone again
