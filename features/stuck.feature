Feature: mw stuck reads a written hold however the Mayor wrote it
  A held bead with a live bead waiting behind it and no written reason is a
  stuck root. A comment opening "HELD" followed by a colon or a space is a
  written reason, in any case: "HELD: waits", "HELD by the Mayor 921: waits",
  "Held by Mayor 891". The sleep pass's old-holds line for the bead carries
  that comment's first line as its reason (mw-gq6.1379).

  Scenario: A hold written "HELD by the Mayor 921:" is a written reason
    Given a held bead "mw-s.1" whose only comment is "HELD by the Mayor 921: waits for the new rig"
    And a live bead "mw-s.2" that waits behind "mw-s.1"
    When mw stuck looks
    Then mw stuck lists no root
    And the sleep pass's old-holds line for "mw-s.1" has the reason "HELD by the Mayor 921: waits for the new rig"

  Scenario: A held bead with no hold comment is still a stuck root
    Given a held bead "mw-s.1" whose only comment is "The story was HELD: by accident"
    And a live bead "mw-s.2" that waits behind "mw-s.1"
    When mw stuck looks
    Then mw stuck lists the root "mw-s.1"
    And the sleep pass's old-holds line for "mw-s.1" has the reason "no reason on the bead"
