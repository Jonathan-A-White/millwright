Feature: mw talk model
  In a talk the Governor may pick a model chip: "Sonnet". A session cannot
  change its own model, and typing /model into the Mayor's live window failed,
  so the switch is a fresh Mayor on the chosen model. mw talk model never types
  into any window: it speaks the switch on the talk, logs it, dated, to
  .mayor-talk.log in the vault, and prints the one line the Mayor runs next,
  bin/respawn-mayor with the effort and the model's full id. mw talk wait names
  the same line when a turn changes the model.

  Background:
    Given a throwaway Mayor postern key for the model switch
    And a throwaway Governor key for the model switch
    And a vault for the model switch
    And the Mayor's window "@2" is open in a fake terminal

  Scenario: Sonnet is spoken, logged and printed, and nothing is typed
    When mw talk model "sonnet" is run for talk "talk-7" turn 3
    Then no key was typed into any window
    And the Governor hears "Switching to Sonnet: a fresh Mayor takes the line in about a minute" on talk "talk-7" turn 3
    And mw talk model printed "bin/respawn-mayor high claude-sonnet-5-5"
    And the talk log's last line says "spoke the switch on talk talk-7 turn 3; hand off, then: bin/respawn-mayor high claude-sonnet-5-5"

  Scenario Outline: Each chip maps to its full model id
    When mw talk model "<chip>" is run for talk "talk-7" turn 1
    Then mw talk model printed "hand off, then: bin/respawn-mayor high <id>"
    And the Governor hears "Switching to <name>: a fresh Mayor takes the line in about a minute" on talk "talk-7" turn 1
    And no key was typed into any window

    Examples:
      | chip   | name   | id                        |
      | opus   | Opus   | claude-opus-5-5           |
      | fable  | Fable  | claude-fable-5-1          |
      | haiku  | Haiku  | claude-haiku-4-5-20251001 |

  Scenario: An unknown chip name is refused and nothing is said, logged or typed
    When mw talk model "gpt" is run for talk "talk-7" turn 3
    Then mw talk model is refused saying "opus, sonnet, fable or haiku"
    And nothing was spoken on the talk
    And the model switch was not logged
    And no key was typed into any window

  Scenario: When the answer cannot be sent, no respawn line is printed
    Given the postern backend will not take the switch
    When mw talk model "sonnet" is run for talk "talk-7" turn 3
    Then mw talk model is refused saying "backend said no"
    And mw talk model printed nothing
    And the talk log's last line says "could not speak the switch on talk talk-7 turn 3: mw talk say: the postern backend would not take the answer: backend said no"
    And no key was typed into any window
