Feature: mw land says who smokes a batch that changes grist on a host with no [grist-apps]
  A landing of grist made on a host that names no [grist-apps] cannot be smoked
  there. mw next says the home's GristLevel tick smokes it, or that it was not
  smoked (mw-gq6.728); a batch that mw land makes says the same, once, through
  the same code (mw-gq6.734).

  Scenario: A batch of grist landed on a host with no [grist-apps] says the home smokes it
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "lampas" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the rig lands its stories in batches
    And the batch's landing changes the rig's grist, and this host names no [grist-apps]
    And the batch's vault names "desktop" as the home
    And these stories are queued for batch landing:
      | mw-gq6.1 |
      | mw-gq6.2 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And mw land says "grist smoke: handed to the home (desktop)"

  Scenario: A batch of grist landed on a host with no [grist-apps] and no other home says it was not smoked
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "lampas" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the rig lands its stories in batches
    And the batch's landing changes the rig's grist, and this host names no [grist-apps]
    And these stories are queued for batch landing:
      | mw-gq6.1 |
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And mw land says "grist smoke: not run, this host names no [grist-apps]"
