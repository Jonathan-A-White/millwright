Feature: A landing in progress keeps its claim's lease alive
  The lease bd grants a claim lasts five minutes, and mw next renews it beside
  the Builder's session. The session ends before the landing does: the merge
  queue and the rig's tests that follow can take longer than the lease, and a
  claim whose lease lapsed is one mw sweep marks run=stuck. So mw next goes on
  renewing the lease until its close-out is written.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree

  Scenario: A landing that outlasts the lease keeps the claim from going stale
    Given the session of "mw-gq6.1" reported a plain success
    And the rig's tests take a while
    And every wait for a heartbeat passes 2 minutes of the claim's clock
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the claim on "mw-gq6.1" was heartbeaten while it was landed
    And the claim on "mw-gq6.1" was never among the stale claims while it was landed
    And the claim on "mw-gq6.1" is not heartbeaten once the close-out has returned
