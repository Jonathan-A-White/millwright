Feature: mw invest
  mw invest keeps an append-only ledger of the factory's investments in
  plans/investments/ledger.jsonl in the vault: for each, the aim, the gain it
  predicts, how that is measured and when to look again, and then what
  actually happened (win, loss or mixed), with the lesson. Records are never
  edited: a later outcome is a new line, and the newest is the one the views
  show. mw invest due lists the investments whose review date has passed with
  no outcome, mw invest list shows them all, and mw report has an
  INVESTMENTS section that celebrates the window's wins and tells each loss
  with its lesson and the better approach.

  Background:
    Given an investment ledger whose clock reads "2026-10-15T12:00:00Z" and whose actor is "mayor@desktop"

  Scenario: Adding an investment appends one record
    When the Mayor adds the investment "mw-a1" aiming "cut the Builder's tokens" predicting "20% fewer tokens per landing" measured by "tokens per landing in mw report" and reviewed in 7 days
    Then the investment ledger file holds 1 line
    And the investment ledger line 1 is an investment of "mw-a1" made at "2026-10-15T12:00:00Z" by "mayor@desktop" with the review in 7 days
    And the investment command printed "mw-a1"
    And the investment command printed "2026-10-22"

  Scenario: A second add for the same bead is refused
    Given the investment "mw-a1" was added aiming "cut the Builder's tokens"
    When the Mayor adds the investment "mw-a1" aiming "something else" predicting "more" measured by "somehow" and reviewed in 3 days
    Then the investment command is refused, saying "already"
    And the investment ledger file holds 1 line

  Scenario: An add without an aim, a prediction, a measure or a review is refused
    When the Mayor adds the investment "mw-a2" aiming "" predicting "a gain" measured by "a number" and reviewed in 7 days
    Then the investment command is refused, saying "aim"
    When the Mayor adds the investment "mw-a2" aiming "an aim" predicting "a gain" measured by "a number" and reviewed in 0 days
    Then the investment command is refused, saying "review"
    And the investment ledger file holds 0 lines

  Scenario: An outcome is a new line and the older lines stay as they were
    Given the investment "mw-a1" was added aiming "cut the Builder's tokens"
    When the Mayor records the outcome "mixed" for "mw-a1" saying "tokens fell 5%" with the lesson "smaller prompts help a little"
    And the Mayor records the outcome "win" for "mw-a1" saying "tokens fell 22% over two weeks" with the lesson "the trim was the part that mattered" and next "extend it to the Tester"
    Then the investment ledger file holds 3 lines
    And the investment ledger line 1 is an investment of "mw-a1" made at "2026-10-15T12:00:00Z" by "mayor@desktop" with the review in 7 days
    And the investment ledger line 3 is a "win" outcome of "mw-a1" saying "tokens fell 22% over two weeks"

  Scenario: An outcome for a bead with no investment, or with no result I know, is refused
    When the Mayor records the outcome "win" for "mw-zz" saying "it worked" with the lesson "keep going"
    Then the investment command is refused, saying "no investment"
    Given the investment "mw-a1" was added aiming "cut the Builder's tokens"
    When the Mayor records the outcome "triumph" for "mw-a1" saying "it worked" with the lesson "keep going"
    Then the investment command is refused, saying "win, loss or mixed"
    And the investment ledger file holds 1 line

  Scenario: Due lists the investments past their review date with no outcome
    Given the investment "mw-old" was added on "2026-10-01T09:00:00Z" aiming "faster gate" reviewed in 7 days
    And the investment "mw-done" was added on "2026-10-01T09:00:00Z" aiming "fewer refusals" reviewed in 7 days
    And the investment "mw-fresh" was added on "2026-10-14T09:00:00Z" aiming "cheaper tests" reviewed in 7 days
    And the outcome "loss" was recorded for "mw-done" on "2026-10-09T09:00:00Z"
    When mw invest due runs
    Then the investment command printed "mw-old"
    And the investment command printed "faster gate"
    And the investment command did not print "mw-done"
    And the investment command did not print "mw-fresh"

  Scenario: Due with nothing past review says so
    Given the investment "mw-fresh" was added on "2026-10-14T09:00:00Z" aiming "cheaper tests" reviewed in 7 days
    When mw invest due runs
    Then the investment command printed "nothing is due"

  Scenario: List shows the aim, prediction, measure, outcomes in order and the lesson
    Given the investment "mw-a1" was added aiming "cut the Builder's tokens"
    And the outcome "mixed" was recorded for "mw-a1" on "2026-10-16T09:00:00Z"
    When mw invest list runs
    Then the investment command printed "mw-a1"
    And the investment command printed "cut the Builder's tokens"
    And the investment command printed "predicted: 20% fewer tokens per landing"
    And the investment command printed "measured by: tokens per landing in mw report"
    And the investment command printed "mixed"
    And the investment command printed "lesson: a lesson was learned"

  Scenario: The report celebrates a win in the window with its measured gain
    Given the investment "mw-win" was added on "2026-10-01T09:00:00Z" aiming "cut the Builder's tokens" reviewed in 7 days
    And the outcome "win" was recorded for "mw-win" on "2026-10-12T09:00:00Z" saying "tokens per landing fell 22%"
    When the investment report runs
    Then the investment report section prints "WIN mw-win"
    And the investment report section prints "cut the Builder's tokens"
    And the investment report section prints "gain: tokens per landing fell 22%"

  Scenario: The report tells a loss with its lesson and the better approach
    Given the investment "mw-loss" was added on "2026-10-01T09:00:00Z" aiming "a faster gate" reviewed in 7 days
    And the outcome "loss" was recorded for "mw-loss" on "2026-10-13T09:00:00Z" saying "the gate got 4% slower" with the lesson "caching hid the real cost" and next "mw-better"
    When the investment report runs
    Then the investment report section prints "LOSS mw-loss"
    And the investment report section prints "lesson: caching hid the real cost"
    And the investment report section prints "better: mw-better"

  Scenario: The report counts the investments still due and uses only the newest outcome
    Given the investment "mw-flip" was added on "2026-10-01T09:00:00Z" aiming "fewer refusals" reviewed in 7 days
    And the outcome "win" was recorded for "mw-flip" on "2026-10-11T09:00:00Z" saying "refusals fell"
    And the outcome "loss" was recorded for "mw-flip" on "2026-10-13T09:00:00Z" saying "they came back"
    And the investment "mw-late" was added on "2026-10-02T09:00:00Z" aiming "cheaper tests" reviewed in 7 days
    When the investment report runs
    Then the investment report section prints "LOSS mw-flip"
    And the investment report section prints no "WIN mw-flip"
    And the investment report section prints "1 still due"

  Scenario: An outcome from before the window is left out of the report
    Given the investment "mw-past" was added on "2026-09-01T09:00:00Z" aiming "older" reviewed in 7 days
    And the outcome "win" was recorded for "mw-past" on "2026-09-20T09:00:00Z" saying "long ago"
    When the investment report runs
    Then the investment report section prints no "mw-past"
    And the investment report section prints "none this window"
