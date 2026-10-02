Feature: mw events follow marks a bead verified
  The follower turns a comment on a bead into an event. A comment that begins
  VERIFIED marks the bead's landing checked, the one rule the postern view and
  snapshot use too (commentMarksVerified): it is a bead_changed event from the
  bead's state to verified, with detail "verified", the first time for a bead.
  Any other comment, a later VERIFIED one included, is a plain comment event
  whose from and to are the bead's state.

  Scenario: A comment beginning VERIFIED on a closed bead is a move to verified
    Given the event follower watches the closed bead "mw-x"
    When the bead "mw-x" gains the comment "VERIFIED by the Governor 2026-10-01: it shows on the screen"
    Then the event follower's log holds "bead_changed mw-x closed->verified verified"

  Scenario: A second VERIFIED comment on the same bead is a plain comment
    Given the event follower watches the closed bead "mw-x"
    When the bead "mw-x" gains the comment "VERIFIED by the Governor 2026-10-01: it shows on the screen"
    And the bead "mw-x" gains the comment "VERIFIED again by the Mayor"
    Then the event follower's log holds "bead_changed mw-x closed->verified verified, bead_changed mw-x closed->closed comment"

  Scenario: A comment that only mentions VERIFIED is a plain comment
    Given the event follower watches the closed bead "mw-x"
    When the bead "mw-x" gains the comment "MAYOR via postern, txid direct:aa: Say VERIFIED on mw-x"
    Then the event follower's log holds "bead_changed mw-x closed->closed comment"

  Scenario: NOT VERIFIED is a plain comment
    Given the event follower watches the closed bead "mw-x"
    When the bead "mw-x" gains the comment "NOT VERIFIED: the screen is blank"
    Then the event follower's log holds "bead_changed mw-x closed->closed comment"
