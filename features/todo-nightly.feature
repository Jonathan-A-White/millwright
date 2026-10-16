Feature: The follower's todo-rank job ranks the To do board once a night
  The follower's todo-rank job (mw events follow) makes one ranking pass
  (`mw todo rank`) a calendar day, from [todo] rank_hour (07:00 UTC when the
  config does not say), on the home host only. After a pass it tells the
  Governor once, in the card's own thread, of a starred card that slipped past
  its due time, and on the first pass of the week (Monday in his zone, [todo]
  week_start) it posts one decision card, "Let go", listing the let-go cards,
  to Keep them all or Drop them all. A pass that fails is tried the next night,
  never twice in one.

  Background:
    Given the nightly clock stands at "2026-10-15T07:00:00Z"
    And six To do cards are open on the list "Errands" for the nightly job
    And the nightly host is home
    And the nightly ranking model answers every card "later" because "Nothing presses"

  Scenario: The job runs once at the hour and not again that day
    When the todo-rank job is asked
    Then the todo-rank job ran 1 time
    When the nightly clock stands at "2026-10-15T19:30:00Z"
    And the todo-rank job is asked
    Then the todo-rank job ran 1 time
    When the nightly clock stands at "2026-10-16T07:05:00Z"
    And the todo-rank job is asked
    Then the todo-rank job ran 2 times

  Scenario: The job does not run before the hour
    Given the nightly clock stands at "2026-10-15T06:59:00Z"
    When the todo-rank job is asked
    Then the todo-rank job ran 0 times

  Scenario: The job runs from a later rank_hour only
    Given the todo-rank hour is 9
    When the todo-rank job is asked
    Then the todo-rank job ran 0 times
    When the nightly clock stands at "2026-10-15T09:00:00Z"
    And the todo-rank job is asked
    Then the todo-rank job ran 1 time

  Scenario: The job does not run on a host that is not home
    Given the nightly host is not home
    When the todo-rank job is asked
    Then the todo-rank job ran 0 times

  Scenario: A pass that fails is tried the next night, not twice in one
    Given the nightly ranking model answers every card "someday" because "Nothing presses"
    When the todo-rank job is asked
    Then the todo-rank job ran 1 time
    And the todo-rank job failed, saying: "someday" is not a row
    When the nightly clock stands at "2026-10-15T08:00:00Z"
    And the todo-rank job is asked
    Then the todo-rank job ran 1 time
    Given the nightly ranking model answers every card "later" because "Nothing presses"
    And the nightly clock stands at "2026-10-16T07:00:00Z"
    When the todo-rank job is asked
    Then the todo-rank job ran 2 times

  Scenario: A card that slipped is told of once, in its own thread
    Given the card "mw-todo.2" is starred and due at "2026-10-14T17:00:00Z" for the nightly job
    When the todo-rank job is asked
    Then exactly 1 message was posted in the thread of "mw-todo.2"
    And the message in the thread of "mw-todo.2" says it is past due
    When the nightly clock stands at "2026-10-16T07:00:00Z"
    And the todo-rank job is asked
    Then exactly 1 message was posted in the thread of "mw-todo.2"

  Scenario: A card given a new due time that then slips is told of again
    Given the card "mw-todo.2" is starred and due at "2026-10-14T17:00:00Z" for the nightly job
    And the todo-rank job is asked
    And the card "mw-todo.2" is starred and due at "2026-10-15T20:00:00Z" for the nightly job
    When the nightly clock stands at "2026-10-16T07:00:00Z"
    And the todo-rank job is asked
    Then exactly 2 messages were posted in the thread of "mw-todo.2"

  Scenario: A telling that fails is tried again the next night
    Given the card "mw-todo.2" is starred and due at "2026-10-14T17:00:00Z" for the nightly job
    And a post in a thread fails
    When the todo-rank job is asked
    Then the todo-rank job failed, saying: a slipped card was not told
    Given a post in a thread does not fail
    When the nightly clock stands at "2026-10-16T07:00:00Z"
    And the todo-rank job is asked
    Then exactly 1 message was posted in the thread of "mw-todo.2"

  Scenario: On Monday the weekly card lists the let-go cards, and not again on Tuesday
    Given the nightly clock stands at "2026-10-19T07:00:00Z"
    And the nightly ranking model answers cards "mw-todo.5" and "mw-todo.6" "let-go" and the rest "later"
    When the todo-rank job is asked
    Then a decision card was sent on the list "Errands" with the options "A: Keep them all" and "B: Drop them all"
    And the decision card lists "mw-todo.5" and "mw-todo.6" and not "mw-todo.4"
    And the decision card says a tap on a card's own Keep or Drop is finer
    When the nightly clock stands at "2026-10-20T07:00:00Z"
    And the todo-rank job is asked
    Then exactly 1 decision card was sent in all

  Scenario: No weekly card is sent when nothing is let go
    Given the nightly clock stands at "2026-10-19T07:00:00Z"
    When the todo-rank job is asked
    Then exactly 0 decision cards were sent in all

  Scenario: The weekly card is sent on the day week_start names, in his zone
    Given the todo week starts on "wednesday"
    And the nightly clock stands at "2026-10-19T07:00:00Z"
    And the nightly ranking model answers cards "mw-todo.5" and "mw-todo.6" "let-go" and the rest "later"
    When the todo-rank job is asked
    Then exactly 0 decision cards were sent in all
    When the nightly clock stands at "2026-10-21T07:00:00Z"
    And the todo-rank job is asked
    Then exactly 1 decision card was sent in all

  Scenario: An A answer keeps every let-go card as later
    Given the weekly card was sent with "mw-todo.5" and "mw-todo.6" let go
    When he answers the weekly card "A: Keep them all"
    Then the card "mw-todo.5" is ranked "later" for the nightly job
    And the card "mw-todo.6" is ranked "later" for the nightly job
    And the card "mw-todo.6" is still open for the nightly job
    And the card "mw-todo.6" was commented KEPT by the answer

  Scenario: A B answer drops every let-go card
    Given the weekly card was sent with "mw-todo.5" and "mw-todo.6" let go
    When he answers the weekly card "B: Drop them all"
    Then the card "mw-todo.5" was dropped for the nightly job with the reason "let-go"
    And the card "mw-todo.6" was dropped for the nightly job with the reason "let-go"
    And the card "mw-todo.4" is still open for the nightly job

  Scenario: A card he already dropped by hand is left alone by the answer
    Given the weekly card was sent with "mw-todo.5" and "mw-todo.6" let go
    And the card "mw-todo.5" is closed by hand for the nightly job
    When he answers the weekly card "A: Keep them all"
    Then the card "mw-todo.6" is ranked "later" for the nightly job
    And the card "mw-todo.5" is still closed for the nightly job
