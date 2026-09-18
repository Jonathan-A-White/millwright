Feature: mw status
  mw status reads, for this host, what is running, what is ready, what is
  blocked, and today's fuel — and writes nothing at all: no claim, no
  comment, no state, no ledger line, no session. Every line of the report
  fits a phone-width terminal, at most 60 columns, however long a title is.

  Background:
    Given the status epic "mw-gq6" on the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A running story is listed under RUNNING, with its session name
    Given a status story "mw-gq6.1" filed under it
    And the status story "mw-gq6.1" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.1" running with session "mw-gq6_1"

  Scenario: Ready and blocked stories are listed under their own headings
    Given a status story "mw-gq6.2" filed under it
    And a status story "mw-gq6.3" filed under it, waiting on "mw-gq6.2"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "mw-gq6.2" as ready
    And the report lists "mw-gq6.3" as blocked

  Scenario: A story filed with path overrides only is shown with the epic's defaults filled in
    Given a status story "mw-gq6.4" filed under it, overriding "model" with "sonnet"
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.4" on the rig "millwright"

  Scenario: A claimed story with an open formula step says its close-out is blocked
    Given a status story "mw-gq6.5" filed under it
    And the status story "mw-gq6.5" is claimed with its session running
    And the formula poured for "mw-gq6.5" has a step still open
    When mw status reads the host
    Then reading status succeeds
    And the report says the close-out of "mw-gq6.5" is blocked by an open formula step

  Scenario: A story marked run=stopped is shown as stopped, not running
    Given a status story "mw-gq6.6" filed under it
    And the status story "mw-gq6.6" is claimed with its session running
    And the status story "mw-gq6.6" is marked run=stopped
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.6" as run=stopped, not running

  Scenario: A story marked run=stuck is shown as stuck, not running
    Given a status story "mw-gq6.7" filed under it
    And the status story "mw-gq6.7" is claimed with its session running
    And the status story "mw-gq6.7" is marked run=stuck
    When mw status reads the host
    Then reading status succeeds
    And the report shows "mw-gq6.7" as run=stuck, not running

  Scenario: Today's fuel is summed from the ledger lines dated today
    Given the builder's ledger holds a line from today burning 12000 tokens
    And the builder's ledger holds a line from today burning 3000 tokens
    And the builder's ledger holds a line from 2020-01-01 burning 900000 tokens
    When mw status reads the host
    Then reading status succeeds
    And the report says today's fuel is 15,000 tokens

  Scenario: Today's fuel is zero when the seat has no ledger yet
    When mw status reads the host
    Then reading status succeeds
    And the report says today's fuel is 0 tokens

  Scenario: No line of the report exceeds 60 columns, even with a long title
    Given a status story "mw-gq6.8" titled "A story whose title runs on and on and on and on and on and on and on and on, well past a phone screen" filed under it
    And the status story "mw-gq6.8" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And every line of the report is at most 60 columns wide

  Scenario: mw status writes nothing
    Given a status story "mw-gq6.9" filed under it
    And a status story "mw-gq6.10" filed under it, waiting on "mw-gq6.9"
    And a status story "mw-gq6.11" filed under it
    And the status story "mw-gq6.11" is claimed with its session running
    When mw status reads the host
    Then reading status succeeds
    And nothing was written through the tracker, the ledger or the runner
