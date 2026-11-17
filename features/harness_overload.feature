Feature: A story the harness stopped is given back whole once its time has come
  When the harness stops a close-out for something that is not the work's (an
  overloaded API, a usage limit), mw next says the attempt is not counted and
  that mw dispatch gives the story back itself once the stop's time has come
  (mw-gq6.1382). It must: the claim, and the run state that keeps the story
  out of every tick's reach, both given back by the first tick after that time,
  so that the tick after it takes the story as the same attempt with no
  mw retry. Until then, and after, no starved alarm asks for one.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" was claimed at "2026-09-18T11:50:00Z" for its attempt 1

  Scenario: A close-out stopped on a 529 is given back whole by the first tick after its time
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "success",
        "is_error": true,
        "num_turns": 70,
        "duration_ms": 600000,
        "result": "API Error: 529 Overloaded. This is a server-side issue, usually temporary — try again in a moment."
      }
      """
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "overloaded"
    And the story "mw-gq6.1" is held blocked
    When a dispatch tick runs at "2026-09-18T12:20:00Z" with room for 0 sessions
    Then the story "mw-gq6.1" is open with no claim and no run state label
    And the story "mw-gq6.1" records attempt 0
    And the story "mw-gq6.1", untouched since "2026-09-18T12:00:00Z", is named by no refused-open alarm at "2026-09-18T13:00:00Z"
    When a dispatch tick runs at "2026-09-18T12:21:00Z" with room for 1 session
    Then the dispatch started "mw-gq6.1" again as attempt 1

  Scenario: A usage limit found only in the pane is given back whole the same way
    Given the session of "mw-gq6.1" left an empty result
    And the pane of "mw-gq6.1" last printed "You’ve hit your weekly limit · resets Sep 18, 12:15pm (UTC)"
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "usage-limit"
    And the story "mw-gq6.1" is held blocked
    When the pane of "mw-gq6.1" is dead
    And a dispatch tick runs at "2026-09-18T12:20:00Z" with room for 0 sessions
    Then the story "mw-gq6.1" is open with no claim and no run state label
    When a dispatch tick runs at "2026-09-18T12:21:00Z" with room for 1 session
    Then the dispatch started "mw-gq6.1" again as attempt 1

  Scenario: A story waiting on a usage limit that resets in hours is not named as refused open
    Given the session of "mw-gq6.1" reported this result:
      """
      {
        "type": "result",
        "subtype": "success",
        "is_error": true,
        "num_turns": 1,
        "duration_ms": 600000,
        "result": "You've hit your weekly limit · resets Sep 19, 9am (UTC)"
      }
      """
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "usage-limit"
    And the story "mw-gq6.1", untouched since "2026-09-18T12:00:00Z", is named by no refused-open alarm at "2026-09-18T13:00:00Z"
    When a dispatch tick runs at "2026-09-19T09:05:00Z" with room for 0 sessions
    Then the story "mw-gq6.1" is open with no claim and no run state label
