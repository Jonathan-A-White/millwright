Feature: A Builder cannot end its session with a formula step open
  A Builder's session is launched with a Stop hook, `mw step guard`: when the
  session is about to end, the hook lists the story's open formula steps and
  keeps the session going, so that each is closed, or marked the Governor's with
  the reason only he can do it. A step marked his lets the guard pass; mw next
  still refuses the story's open steps, and treats it as it treats a story with
  a hands step waiting on him: the report counts it as hands waiting, not as
  steps left open, and the Mayor's mail names the step and the reason. Claude
  sessions only: Codex's launch has no Stop hook.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: guard: a step marked as the Governor's
    Given the session of "mw-gq6.1" reported a plain success
    And a formula was poured for "mw-gq6.1" and one of its steps is still open
    When the session of "mw-gq6.1" tries to end and the guard is asked
    Then the guard keeps the session going, naming the open step
    When the Builder of "mw-gq6.1" marks the open step the Governor's, saying "only the Governor can sign in to the bank"
    And the session of "mw-gq6.1" tries to end and the guard is asked
    Then the guard lets the session end
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "open-steps"
    And that mail's body holds:
      | waiting on the Governor, marked his by the session: |
      | Implement until green: only the Governor can sign in to the bank |
    And the report counts the open-steps refusal as hands waiting on him, and none as left open
