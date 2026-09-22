Feature: A re-dispatched story never leaves a tracked file dirty in the vault
  A story is sometimes dispatched more than once: given back after a session
  ends without landing it, or tried again once whatever stopped it is fixed.
  Nothing about a later attempt may touch what an earlier one already
  committed to the vault (mw-gq6.87), because both hosts and the sync between
  them read that vault as git, and a tracked file a re-dispatch leaves
  modified is one dispatch and sync both refuse to go near until somebody
  commits it by hand.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And the vault is a real git clone
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" is planned and ready to be worked here
    And the first attempt of "mw-gq6.1" already committed its boot file and result to the vault

  Scenario: A second dispatch writes its own boot file and result, leaving the first attempt's untouched
    When mw dispatches "mw-gq6.1" again
    Then one session was started again, for "mw-gq6.1"
    And the vault holds no modified tracked file
    And the first attempt of "mw-gq6.1" is unchanged in the vault

  Scenario: mw next still finds and commits the second attempt's result
    When mw dispatches "mw-gq6.1" again
    And the session of "mw-gq6.1" finished its second attempt
    And mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the vault holds no modified tracked file
    And the vault's last commit holds the result of "mw-gq6.1"'s second attempt
