Feature: A To do card's due time
  A card on the Governor's To do board (an open bead labelled hitl:todo) can
  carry a due time, the metadata todo_due in RFC 3339. The Mayor sets or
  clears it with `mw todo due <bead> <time>`; from his phone todo_edit does
  the same with its due field. At the due time the follower sends him one
  Postern push naming the card, and only one: the card is marked reminded for
  the due time it was sent for. A closed card, a card with no due time and a
  card not yet due are never reminded.

  Background:
    Given the to-do clock stands at "2026-10-14T09:00:00Z"
    And the to-do card "mw-todo.1" titled "Buy a hose" is open
    And the to-do bead "mw-plain.1" is an open story with no hitl:todo label

  Scenario: A card due in the past and not yet reminded gets exactly one push
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    When the to-do reminders run
    Then exactly 1 reminder was sent
    And the reminder is an alarm threaded on "mw-todo.1" and names "Buy a hose"
    And the card "mw-todo.1" is marked reminded for "2026-10-14T08:00:00Z"

  Scenario: A second run sends none
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    When the to-do reminders run
    And the to-do reminders run
    Then exactly 1 reminder was sent

  Scenario: A closed card gets none
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    And the to-do card "mw-todo.1" is closed
    When the to-do reminders run
    Then exactly 0 reminders were sent

  Scenario: A card without a due time gets none
    When the to-do reminders run
    Then exactly 0 reminders were sent

  Scenario: A card not yet due gets none, and one once the time has come
    Given the card "mw-todo.1" is due "2026-10-14T10:00:00Z"
    When the to-do reminders run
    Then exactly 0 reminders were sent
    When the to-do clock stands at "2026-10-14T10:00:00Z"
    And the to-do reminders run
    Then exactly 1 reminder was sent

  Scenario: A card due in another zone is reminded at the moment it names
    Given the card "mw-todo.1" is due "2026-10-14T04:30:00-04:00"
    When the to-do reminders run
    Then exactly 1 reminder was sent

  Scenario: A push that could not be sent is tried again on the next run
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    And the postern cannot be reached
    When the to-do reminders run
    Then the to-do reminders failed, saying: not reachable
    And exactly 0 reminders were sent
    And the card "mw-todo.1" is not marked reminded
    When the postern can be reached
    And the to-do reminders run
    Then exactly 1 reminder was sent

  Scenario: A card given a new due time after its reminder is reminded again for it
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    When the to-do reminders run
    And the card "mw-todo.1" is due "2026-10-14T08:30:00Z"
    And the to-do reminders run
    Then exactly 2 reminders were sent

  Scenario: A due time that is not a time is passed over
    Given the card "mw-todo.1" is due "tomorrow-ish"
    When the to-do reminders run
    Then exactly 0 reminders were sent

  Scenario: mw todo due sets the due time on a card
    When mw todo due is run on "mw-todo.1" with "2026-10-15T17:00:00Z"
    Then the todo due command succeeds
    And the card "mw-todo.1" has the todo_due "2026-10-15T17:00:00Z"

  Scenario: mw todo due clears it
    Given the card "mw-todo.1" is due "2026-10-15T17:00:00Z"
    When mw todo due is cleared on "mw-todo.1"
    Then the todo due command succeeds
    And the card "mw-todo.1" has no todo_due

  Scenario: Setting the same due time again does not remind twice
    Given the card "mw-todo.1" is due "2026-10-14T08:00:00Z"
    When the to-do reminders run
    And mw todo due is run on "mw-todo.1" with "2026-10-14T08:00:00Z"
    And the to-do reminders run
    Then exactly 1 reminder was sent

  Scenario: mw todo due refuses a bead that is not a card
    When mw todo due is run on "mw-plain.1" with "2026-10-15T17:00:00Z"
    Then the todo due command is refused, saying: not an item on a to-do list
    And nothing was written by the todo due command

  Scenario: mw todo due refuses a closed card
    Given the to-do card "mw-todo.1" is closed
    When mw todo due is run on "mw-todo.1" with "2026-10-15T17:00:00Z"
    Then the todo due command is refused, saying: already closed
    And nothing was written by the todo due command

  Scenario: mw todo due refuses a time that is not RFC 3339
    When mw todo due is run on "mw-todo.1" with "next tuesday"
    Then the todo due command is refused, saying: RFC 3339
    And nothing was written by the todo due command

  Scenario: mw todo due refuses a bead that does not exist
    When mw todo due is run on "mw-nope.9" with "2026-10-15T17:00:00Z"
    Then the todo due command is refused, saying: mw-nope.9
