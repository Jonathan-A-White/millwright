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
