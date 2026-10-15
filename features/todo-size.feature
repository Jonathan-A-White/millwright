Feature: A To do card's size and Then line
  A card on the Governor's To do board (an open bead labelled hitl:todo) can
  carry a size, the metadata todo_size (quick, hour or half-day), and a Then
  line, the metadata todo_then: free text under 200 characters naming what
  follows it. The Mayor sets or clears them with `mw todo size <bead> <size>`
  and `mw todo then <bead> "<text>"` (--clear takes either off). Anything but
  a card, an unknown size and a Then line that is too long are refused before
  anything is written.

  Background:
    Given the sized card "mw-todo.1" is open
    And the sized bead "mw-plain.1" is an open story with no hitl:todo label

  Scenario Outline: mw todo size sets each size on a card
    When mw todo size is run on "mw-todo.1" with "<size>"
    Then the todo size command succeeds
    And the card "mw-todo.1" has the todo_size "<size>"

    Examples:
      | size     |
      | quick    |
      | hour     |
      | half-day |

  Scenario: mw todo size takes the size in any case and trims it
    When mw todo size is run on "mw-todo.1" with "  Half-Day "
    Then the todo size command succeeds
    And the card "mw-todo.1" has the todo_size "half-day"

  Scenario: mw todo size clears it
    Given the card "mw-todo.1" is sized "hour"
    When mw todo size is cleared on "mw-todo.1"
    Then the todo size command succeeds
    And the card "mw-todo.1" has no todo_size

  Scenario: mw todo size refuses a size it does not know
    When mw todo size is run on "mw-todo.1" with "all-day"
    Then the todo size command is refused, saying: quick, hour or half-day
    And nothing was written by the todo size command

  Scenario: mw todo size refuses a bead that is not a card
    When mw todo size is run on "mw-plain.1" with "quick"
    Then the todo size command is refused, saying: not an item on a to-do list
    And nothing was written by the todo size command

  Scenario: mw todo size refuses a closed card
    Given the sized card "mw-todo.1" is closed
    When mw todo size is run on "mw-todo.1" with "quick"
    Then the todo size command is refused, saying: already closed
    And nothing was written by the todo size command

  Scenario: mw todo size refuses a bead that does not exist
    When mw todo size is run on "mw-nope.9" with "quick"
    Then the todo size command is refused, saying: mw-nope.9
    And nothing was written by the todo size command

  Scenario: mw todo then sets the Then line, trimmed
    When mw todo then is run on "mw-todo.1" with "  Fit the hose to the tap "
    Then the todo then command succeeds
    And the card "mw-todo.1" has the todo_then "Fit the hose to the tap"

  Scenario: mw todo then clears it
    Given the card "mw-todo.1" has the Then line "Fit the hose to the tap"
    When mw todo then is cleared on "mw-todo.1"
    Then the todo then command succeeds
    And the card "mw-todo.1" has no todo_then

  Scenario: mw todo then accepts a line of exactly 200 characters
    When mw todo then is run on "mw-todo.1" with a line of 200 characters
    Then the todo then command succeeds

  Scenario: mw todo then refuses a line of 201 characters
    When mw todo then is run on "mw-todo.1" with a line of 201 characters
    Then the todo then command is refused, saying: 200
    And nothing was written by the todo then command

  Scenario: mw todo then refuses a bead that is not a card
    When mw todo then is run on "mw-plain.1" with "Fit the hose"
    Then the todo then command is refused, saying: not an item on a to-do list
    And nothing was written by the todo then command

  Scenario: mw todo then refuses a closed card
    Given the sized card "mw-todo.1" is closed
    When mw todo then is run on "mw-todo.1" with "Fit the hose"
    Then the todo then command is refused, saying: already closed
    And nothing was written by the todo then command

  Scenario: mw todo then refuses a bead that does not exist
    When mw todo then is run on "mw-nope.9" with "Fit the hose"
    Then the todo then command is refused, saying: mw-nope.9
    And nothing was written by the todo then command
