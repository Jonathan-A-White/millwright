Feature: mw todo rank puts every To do card in a row
  One short model pass (`mw todo rank`) reads every open card on the
  Governor's To do board, the cards he closed in the last 30 days and his
  about file (seats/mayor/todo-about.md), and puts each open card in one of
  four rows with a one-line why: now, make-time, later or let-go. It writes
  the metadata todo_row and todo_row_why on each card whose row or why
  changed, and a pass log under runs/todo-rank/<date>.json. The pass is all
  or nothing: an answer that misses a card, names a bead it was not given,
  says a row that is not one of the four, puts more than three cards in now
  or in make-time, or lets a starred card go is refused, and nothing is
  written. --dry-run prints the rows and writes nothing.

  Background:
    Given the ranking clock stands at "2026-10-16T09:00:00Z"
    And six To do cards are open on the list "Errands"
    And the card "mw-todo.2" is starred for the ranking

  Scenario: A pass over six cards writes a row and a why on each
    Given the ranking model answers:
      | bead      | row       | why                        |
      | mw-todo.1 | now       | Due tomorrow               |
      | mw-todo.2 | now       | Starred and past due       |
      | mw-todo.3 | make-time | Waiting longest            |
      | mw-todo.4 | make-time | Matters, no date           |
      | mw-todo.5 | later     | Nothing presses            |
      | mw-todo.6 | let-go    | Untouched for six weeks    |
    When mw todo rank runs
    Then the todo rank pass succeeds
    And the card "mw-todo.1" is ranked "now" because "Due tomorrow"
    And the card "mw-todo.2" is ranked "now" because "Starred and past due"
    And the card "mw-todo.3" is ranked "make-time" because "Waiting longest"
    And the card "mw-todo.4" is ranked "make-time" because "Matters, no date"
    And the card "mw-todo.5" is ranked "later" because "Nothing presses"
    And the card "mw-todo.6" is ranked "let-go" because "Untouched for six weeks"
    And at most 3 cards are ranked "now"
    And at most 3 cards are ranked "make-time"

  Scenario: A starred card put in let-go is refused and nothing is written
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | now       | Due tomorrow         |
      | mw-todo.2 | let-go    | Never touched        |
      | mw-todo.3 | make-time | Waiting longest      |
      | mw-todo.4 | make-time | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
      | mw-todo.6 | later     | Nothing presses      |
    When mw todo rank runs
    Then the todo rank pass is refused, saying: mw-todo.2 is starred
    And nothing was written by the todo rank pass

  Scenario: An answer missing a card is refused and nothing is written
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | now       | Due tomorrow         |
      | mw-todo.2 | now       | Starred              |
      | mw-todo.3 | make-time | Waiting longest      |
      | mw-todo.4 | make-time | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
    When mw todo rank runs
    Then the todo rank pass is refused, saying: mw-todo.6 has no row
    And nothing was written by the todo rank pass

  Scenario: An answer naming a bead it was not given is refused
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | now       | Due tomorrow         |
      | mw-todo.2 | now       | Starred              |
      | mw-todo.3 | make-time | Waiting longest      |
      | mw-todo.4 | make-time | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
      | mw-todo.6 | later     | Nothing presses      |
      | mw-else.9 | later     | Made up              |
    When mw todo rank runs
    Then the todo rank pass is refused, saying: mw-else.9 is not a card it was given
    And nothing was written by the todo rank pass

  Scenario: An answer with a row that is not one of the four is refused
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | someday   | Due tomorrow         |
      | mw-todo.2 | now       | Starred              |
      | mw-todo.3 | make-time | Waiting longest      |
      | mw-todo.4 | make-time | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
      | mw-todo.6 | later     | Nothing presses      |
    When mw todo rank runs
    Then the todo rank pass is refused, saying: "someday" is not a row
    And nothing was written by the todo rank pass

  Scenario: An answer with four cards in now is refused
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | now       | Due tomorrow         |
      | mw-todo.2 | now       | Starred              |
      | mw-todo.3 | now       | Waiting longest      |
      | mw-todo.4 | now       | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
      | mw-todo.6 | later     | Nothing presses      |
    When mw todo rank runs
    Then the todo rank pass is refused, saying: 4 cards in now, at most 3
    And nothing was written by the todo rank pass

  Scenario: --dry-run prints the rows and writes nothing
    Given the ranking model answers:
      | bead      | row       | why                  |
      | mw-todo.1 | now       | Due tomorrow         |
      | mw-todo.2 | now       | Starred              |
      | mw-todo.3 | make-time | Waiting longest      |
      | mw-todo.4 | make-time | Matters, no date     |
      | mw-todo.5 | later     | Nothing presses      |
      | mw-todo.6 | let-go    | Untouched for weeks  |
    When mw todo rank runs with --dry-run
    Then the todo rank pass succeeds
    And nothing was written by the todo rank pass
    And the todo rank pass printed "now        mw-todo.1  Card mw-todo.1 — Due tomorrow"
    And the todo rank pass printed "let-go     mw-todo.6  Card mw-todo.6 — Untouched for weeks"

  Scenario: The model is given the about file and 30 days of Done, under one system prompt
    Given the about file says "The train is my best time for big things"
    And the card "mw-done.1" titled "Mow the lawn" was closed 10 days ago on the list "Errands"
    And the card "mw-done.2" titled "Paint the shed" was closed 40 days ago on the list "Errands"
    And the ranking model answers every card "later" because "Nothing presses"
    When mw todo rank runs
    And mw todo rank runs
    Then the model's message holds "The train is my best time for big things"
    And the model's message holds "Mow the lawn"
    And the model's message does not hold "Paint the shed"
    And the model's message holds "mw-todo.6"
    And the system prompt was byte-identical across the two passes
    And the system prompt holds neither a card nor the about file

  Scenario: The pass log holds every card's row and why
    Given the ranking model answers every card "later" because "Nothing presses"
    When mw todo rank runs
    Then the pass log "2026-10-16.json" holds every card ranked "later" because "Nothing presses"

  Scenario: A card already in its row with the same why is not written again
    Given the ranking model answers every card "later" because "Nothing presses"
    And mw todo rank runs
    When mw todo rank runs
    Then the second todo rank pass wrote nothing to the cards

  Scenario: A starred card past its due time is marked slipped once
    Given the card "mw-todo.2" is due for the ranking at "2026-10-15T17:00:00Z"
    And the ranking model answers every card "later" because "Nothing presses"
    When mw todo rank runs
    Then the card "mw-todo.2" is marked slipped at "2026-10-15T17:00:00Z"
    And the card "mw-todo.1" is not marked slipped
