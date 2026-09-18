Feature: Path validation
  A story is worked by a Path: the rig, target branch, harness, model, effort,
  formula and host the Mayor planned for it. A story's Path is the epic's
  defaults overlaid with the story's own overrides, and it must be complete
  and legal before a session can occupy a seat and work the story.

  Background:
    Given an epic with the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: A story with no rig has no path
    Given the epic's default rig is not set
    And a story with no overrides
    When the story's path is built
    Then the path is rejected because: rig is required

  Scenario: A story with no target branch has no path
    Given the epic's default branch is not set
    And a story with no overrides
    When the story's path is built
    Then the path is rejected because: branch is required

  Scenario: An unknown model is rejected
    Given a story that overrides "model" with "gpt"
    When the story's path is built
    Then the path is rejected because: unknown model "gpt"

  Scenario: A story override beats the epic default
    Given a story that overrides "model" with "haiku"
    And the story also overrides "effort" with "low"
    When the story's path is built
    Then the path is accepted
    And the path's "model" is "haiku"
    And the path's "effort" is "low"
    And the path's "rig" is "millwright"
    And the path's "formula" is "tdd-feature"
