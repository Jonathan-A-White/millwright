Feature: Ready stories for a host
  The factory dispatches on one host at a time. Asking the work tracker for an
  epic's ready stories on a host must return exactly the stories that can be
  started there right now: open, unclaimed, and whose Path names that host —
  whether the story names the host itself or inherits it from the epic.

  Background:
    Given an epic "mw-gq6" with the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A story that inherits the epic's host is ready on that host
    Given a story "mw-gq6.1" of that epic with no overrides
    When the ready stories of "mw-gq6" on "vps" are listed
    Then the ready stories are "mw-gq6.1"
    And the ready story "mw-gq6.1" is worked on model "opus"

  Scenario: A story that overrides the host is ready on the host it names
    Given a story "mw-gq6.1" of that epic with no overrides
    And a story "mw-gq6.2" of that epic that overrides "host" with "laptop"
    When the ready stories of "mw-gq6" on "laptop" are listed
    Then the ready stories are "mw-gq6.2"

  Scenario: A story for another host is not ready here
    Given a story "mw-gq6.2" of that epic that overrides "host" with "laptop"
    When the ready stories of "mw-gq6" on "vps" are listed
    Then there are no ready stories

  Scenario: A claimed story is no longer ready
    Given a story "mw-gq6.1" of that epic with no overrides
    And the story "mw-gq6.1" is claimed
    When the ready stories of "mw-gq6" on "vps" are listed
    Then there are no ready stories

  Scenario: A closed story is no longer ready
    Given a story "mw-gq6.1" of that epic with no overrides
    And the story "mw-gq6.1" is closed because "shipped"
    When the ready stories of "mw-gq6" on "vps" are listed
    Then there are no ready stories

  Scenario: A story of another epic is not listed
    Given a story "mw-gq6.1" of that epic with no overrides
    And an epic "mw-abc" with the default path:
      | rig     | fellowship |
      | branch  | main       |
      | harness | claude     |
      | model   | sonnet     |
      | effort  | low        |
      | host    | vps        |
    And a story "mw-abc.1" of that epic with no overrides
    When the ready stories of "mw-gq6" on "vps" are listed
    Then the ready stories are "mw-gq6.1"
