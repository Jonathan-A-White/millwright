Feature: Showing a plan filed earlier
  The Governor approves a plan from a phone, later, and needs to see its tree
  before he does. `mw show <epic-id>` reads the epic back out of the tracker and
  prints exactly the tree `mw release` prints — every story with its path, what
  it still waits on and what the tracker says it is — and then stops. It writes
  nothing: every story is as held after it as before.

  Background:
    Given the plan, filed and held earlier:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "priority": 1,
          "description": "The smallest mw that closes the loop on the factory's own rig.",
          "success_criteria": "One story goes from the plan file to a commit.",
          "defaults": {
            "rig": "millwright",
            "branch": "main",
            "harness": "claude",
            "model": "opus",
            "effort": "high",
            "formula": "tdd-feature",
            "host": "vps"
          }
        },
        "stories": [
          {
            "key": "module",
            "title": "Go module and the Path domain",
            "priority": 1,
            "estimate": 60,
            "description": "Create the module and the Path value type.",
            "acceptance": "make test passes.",
            "needs": []
          },
          {
            "key": "gateway",
            "title": "Beads gateway",
            "priority": 1,
            "estimate": 90,
            "description": "Read and write stories through bd.",
            "acceptance": "make test passes.",
            "needs": ["module"]
          },
          {
            "key": "formulas",
            "title": "First formulas",
            "priority": 2,
            "estimate": 30,
            "description": "Author the tdd-feature and chore formulas.",
            "acceptance": "Both formulas cook.",
            "path": { "model": "sonnet", "formula": "chore" },
            "needs": []
          }
        ]
      }
      """

  Scenario: Showing a held epic prints its tree and leaves every story held
    When the epic is shown
    Then showing succeeds
    And the shown tree names the epic Walking skeleton
    And the shown tree shows the story "formulas" on the path millwright/main · claude/sonnet/high · chore · vps
    And the shown tree shows the story "gateway" waiting on "module"
    And the shown tree shows the story "module" as held
    And nothing was written to the tracker
    And every story of the filed plan is still held

  Scenario: Showing an epic prints the tree that releasing it prints
    When the epic is shown
    Then releasing the epic afterwards prints exactly the tree that was shown

  Scenario: Showing an epic tells what has been taken and finished, and writes nothing
    Given the story "module" of the filed plan is finished
    And the story "formulas" of the filed plan is taken by a session
    When the epic is shown
    Then showing succeeds
    And the shown tree shows the story "module" as closed
    And the shown tree shows the story "formulas" as in progress
    And the shown tree shows the story "gateway" as held
    And the shown tree shows the story "gateway" waiting on nothing
    And nothing was written to the tracker

  Scenario: An epic nobody filed is a plain refusal
    When the epic "f-nope" is shown
    Then showing is refused, saying: reading the epic f-nope
    And nothing was written to the tracker

  Scenario: A story is not an epic, and showing it is a plain refusal
    When the story "module" of the filed plan is shown as an epic
    Then showing is refused, saying: is a story, not an epic
    And nothing was written to the tracker

  Scenario: An epic filed under the epic is shown as an epic, not listed as a story
    Given an epic "f-1.90" called "Later work" is filed under the epic of the filed plan
    When the epic is shown
    Then showing succeeds
    And the shown tree shows "f-1.90" called "Later work" as an epic, not a story
    And the shown tree shows the story "module" as held
    And nothing was written to the tracker
    And releasing the epic afterwards prints exactly the tree that was shown
