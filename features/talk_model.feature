Feature: mw talk model
  In a talk the Governor may say "use Sonnet", and the Mayor's next turn should
  run on it. mw talk model starts detached and watches the acting Mayor's
  window, the one .mayor-acting names; once the window is idle at an empty
  input line on two looks in a row, it types /model and the model, and Enter.
  It never types over a draft: one wrong keystroke would corrupt what the
  Governor was writing. It gives up after its limit, typing nothing, and every
  watch logs what it did, dated, to .mayor-talk.log in the vault.

  The terminal here is a fake tmux program: it shows a screen of the scenario's
  making for capture-pane and records every key sent, so the screens are read by
  the very rule the reaper uses.

  Background:
    Given a vault where the Mayor acts as "Mayor after handoff 141 (tmux window 2 'mayor-2026-10-01-141'), since 2026-10-01T02:43:16Z"
    And a fake terminal with the window "@2" named "mayor-2026-10-01-141"

  Scenario: At an empty idle prompt it types /model and the model, and Enter
    Given the window "@2" shows an empty input line
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then the keys sent to the terminal are "/model sonnet" and Enter, into "@2", on look 2
    And the talk log's last line says "typed /model sonnet into mayor-2026-10-01-141 (@2)"

  Scenario: A dim suggestion after the prompt is an empty input line
    Given the window "@2" shows the suggestion "Wait for the next turn."
    When mw talk model "opus" watches, looking every 2 seconds for up to 1 minutes
    Then the keys sent to the terminal are "/model opus" and Enter, into "@2", on look 2

  Scenario: With a draft on the input line it never types, and gives up on the limit
    Given the window "@2" shows the draft "half a sente"
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then no key was sent to the terminal
    And mw talk model gave up
    And the talk log's last line says "gave up after 1m0s, typing nothing: the window mayor-2026-10-01-141 (@2) was last seen not at an empty input line"

  Scenario: It waits while the Mayor is working, and types once the window is idle
    Given the window "@2" is working
    And before look 4 the window "@2" shows an empty input line
    When mw talk model "fable" watches, looking every 2 seconds for up to 1 minutes
    Then the keys sent to the terminal are "/model fable" and Enter, into "@2", on look 5

  Scenario: A draft begun between two idle looks stops it typing
    Given the window "@2" shows an empty input line
    And before look 2 the window "@2" shows the draft "use"
    And before look 3 the window "@2" shows an empty input line
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then the keys sent to the terminal are "/model sonnet" and Enter, into "@2", on look 4

  Scenario: A question on the screen is not a prompt, and it gives up on the limit
    Given the window "@2" shows a question with no prompt
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then no key was sent to the terminal
    And mw talk model gave up

  Scenario: No window the acting file names is open, so it types nothing
    Given the Mayor's acting file now says "Mayor after handoff 142 (tmux window 5 'mayor-2026-10-01-142'), since 2026-10-01T03:00:00Z"
    And the window "@2" shows an empty input line
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then no key was sent to the terminal
    And mw talk model gave up
    And the talk log's last line says "gave up after 1m0s, typing nothing: .mayor-acting named no window that is open"

  Scenario: Arming is logged before the first look
    Given the window "@2" shows an empty input line
    When mw talk model "sonnet" watches, looking every 2 seconds for up to 1 minutes
    Then the talk log's line 1 says "armed; waiting for the mayor's window to be idle at an empty input line to type /model sonnet"

  Scenario: A model it does not switch to is refused before anything is watched
    Given the window "@2" shows an empty input line
    When mw talk model "gpt" watches, looking every 2 seconds for up to 1 minutes
    Then mw talk model is refused saying "opus, sonnet or fable"
    And no key was sent to the terminal
