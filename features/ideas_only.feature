Feature: mw ideas-only
  The Governor can put his key in Ideas only on the postern backend: until a
  time, or on a weekly schedule, he is away on purpose, and the factory runs on
  without him (postern's docs/protocol.md section 25). mw ideas-only says
  whether the mode is on, until when, and what mw is keeping for him meanwhile:
  one line for each record of his, held until the mode ends. It asks the
  backend with the Mayor's key and keeps its own note of the answer
  (~/.local/state/mw/postern/ideas-only/state.json), so that a backend out of
  reach shows what was last known, and a note that says on is not believed past
  its end.

  Background:
    Given a host that has no note of the Governor's Ideas only
    And the clock of Ideas only stands at "2026-10-12T12:00:00Z"

  Scenario: it says off, with nothing held, when the backend says the mode is off
    Given the backend says the Governor's Ideas only is off
    When mw ideas-only is run
    Then Ideas only printed "off"
    And Ideas only did not print "held"

  Scenario: it says until when the mode is on, and lists what is held
    Given the backend says the Governor's Ideas only is on until "2026-10-12T21:00:00Z"
    And Ideas only is holding "direct:101" sent in "general" saying "Buy flour for the bread"
    When mw ideas-only is run
    Then Ideas only printed "on until 21:00 UTC"
    And Ideas only printed "1 idea held"
    And Ideas only printed "direct:101"
    And Ideas only printed "Buy flour for the bread"
    And the note of Ideas only says it is on until "2026-10-12T21:00:00Z"

  Scenario: it names his time zone when his schedule has one
    Given the backend says the Governor's Ideas only is on until "2026-10-12T21:00:00Z", on a schedule in "America/New_York"
    When mw ideas-only is run
    Then Ideas only printed "on until 21:00 UTC (17:00 America/New_York)"

  Scenario: an end on another day carries its date
    Given the backend says the Governor's Ideas only is on until "2026-10-13T11:00:00Z"
    When mw ideas-only is run
    Then Ideas only printed "on until 2026-10-13 11:00 UTC"

  Scenario: a mode that has ended is off, and the ideas it kept are still listed
    Given the backend says the Governor's Ideas only is off
    And Ideas only is holding "direct:101" sent in "general" saying "Buy flour for the bread"
    When mw ideas-only is run
    Then Ideas only printed "off"
    And Ideas only printed "1 idea held"

  Scenario: a backend out of reach shows what was last known
    Given the note of Ideas only says it is on until "2026-10-12T21:00:00Z"
    And the backend does not answer the Ideas only route
    When mw ideas-only is run
    Then Ideas only printed "on until 21:00 UTC"
    And Ideas only said "showing what was last known"

  Scenario: a note that says on is not believed past its end
    Given the note of Ideas only says it is on until "2026-10-12T11:00:00Z"
    And the backend does not answer the Ideas only route
    When mw ideas-only is run
    Then Ideas only printed "off"

  # The end of the mode (postern's docs/protocol.md section 25): mw gives the
  # Governor's ideas back, once. One live card 'Your ideas from Ideas only (N)'
  # goes to him, the taps he made while it was on are applied in order, and the
  # ideas are unread again in the inbox, so that the Mayor handles each as a
  # normal message.

  Scenario: mw ideas-only release gives the ideas back once: one card, the held tap applied, the ideas unread
    Given a rig that can release the Governor's ideas
    And the story "mw-act.1" is held
    And Ideas only is holding the Governor's idea "direct:101" in "general" saying "Buy flour for the bread"
    And Ideas only is holding the Governor's tap "direct:102" to release "mw-act.1"
    And Ideas only is holding the Governor's idea "direct:103" in "general" saying "Ask the roofer about the quote"
    And the backend says the Governor's Ideas only is off
    When mw ideas-only release is run
    Then Ideas only printed "Ideas only ended"
    And Ideas only printed "3 ideas"
    And exactly one card was sent to the Governor, titled "Your ideas from Ideas only (3)", with 3 items
    And card item 1 reads "Buy flour for the bread" and says "general"
    And card item 2 reads "release mw-act.1"
    And card item 3 reads "Ask the roofer about the quote"
    And the story "mw-act.1" stands "open"
    And the last comment on "mw-act.1" starts "RELEASED by the Governor via postern, txid direct:102"
    And Ideas only is holding nothing, and the held ideas were kept in the released folder
    And the postern inbox counts 3 unread
    When a plain mw postern inbox is run
    Then the postern inbox printed "Buy flour for the bread"
    And the postern inbox printed "Ask the roofer about the quote"
    And the postern inbox printed "already applied earlier: applied release mw-act.1 txid direct:102"
    And the postern inbox did not print "held for after"
    When mw ideas-only release is run
    Then Ideas only printed "Ideas only ended: nothing held"
    And exactly one card was sent to the Governor, titled "Your ideas from Ideas only (3)", with 3 items

  Scenario: with nothing held, mw ideas-only release sends no card and says so
    Given a rig that can release the Governor's ideas
    And the backend says the Governor's Ideas only is off
    When mw ideas-only release is run
    Then Ideas only printed "Ideas only ended: nothing held"
    And no card was sent to the Governor

  Scenario: while the mode is still on, mw ideas-only release gives nothing back
    Given a rig that can release the Governor's ideas
    And Ideas only is holding the Governor's idea "direct:101" in "general" saying "Buy flour for the bread"
    And the backend says the Governor's Ideas only is on until "2026-10-12T21:00:00Z"
    When mw ideas-only release is run
    Then Ideas only printed "still on until 21:00 UTC"
    And no card was sent to the Governor
    And Ideas only is holding 1 idea

  Scenario: a mode that ended by the clock is released although the backend cannot be asked
    Given a rig that can release the Governor's ideas
    And the note of Ideas only says it is on until "2026-10-12T11:00:00Z"
    And Ideas only is holding the Governor's idea "direct:101" in "general" saying "Buy flour for the bread"
    And the backend does not answer the Ideas only route
    When mw ideas-only release is run
    Then Ideas only printed "Ideas only ended 11:00: 1 idea"
    And exactly one card was sent to the Governor, titled "Your ideas from Ideas only (1)", with 1 items

  Scenario: a release that died after its card was sent finishes without sending another
    Given a rig that can release the Governor's ideas
    And Ideas only is holding the Governor's idea "direct:101" in "general" saying "Buy flour for the bread"
    And the backend says the Governor's Ideas only is off
    And an earlier release died after it sent the card "direct:card-1"
    When mw ideas-only release is run
    Then Ideas only printed "card direct:card-1"
    And no card was sent to the Governor
    And Ideas only is holding 0 ideas

  Scenario: a card that cannot be sent leaves the ideas held, and says so
    Given a rig that can release the Governor's ideas
    And Ideas only is holding the Governor's idea "direct:101" in "general" saying "Buy flour for the bread"
    And the backend says the Governor's Ideas only is off
    And the backend will not take the card
    When mw ideas-only release is run
    Then the release failed saying "giving back the ideas of Ideas only"
    And Ideas only is holding 1 idea
