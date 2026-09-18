Feature: Releasing a plan filed earlier
  `mw file` files a plan and holds every story back, because the Governor is
  usually not at the machine when it is filed. `mw release <epic-id>` is how he
  says yes afterwards: it reads the epic back out of the tracker, prints the
  same tree `mw file` printed — every story with its path, what it still waits
  on and what the tracker says it is — and releases the ones still held.
  Nothing else is touched: a story somebody already took, or already finished,
  is left exactly as it is, and releasing an epic twice is the same as
  releasing it once.

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

  Scenario: Releasing a held epic makes exactly the stories with no open needs ready
    When the epic is released
    Then releasing succeeds
    And the release tree names the epic Walking skeleton
    And the release tree shows the story "formulas" on the path millwright/main · claude/sonnet/high · chore · vps
    And the release tree shows the story "gateway" waiting on "module"
    And the release tree shows the story "module" as held
    And the stories ready on vps once released are module, formulas
    And the release says: Released the 3 held stories

  Scenario: Releasing the same epic twice changes nothing
    When the epic is released
    And the epic is released again
    Then releasing succeeds
    And the stories ready on vps once released are module, formulas
    And the release says: none of the 3 stories
    And the story "module" of the filed plan is now open

  Scenario: A story already taken, already finished or already released is untouched
    Given the story "module" of the filed plan is finished
    And the story "formulas" of the filed plan is taken by a session
    When the epic is released
    Then releasing succeeds
    And the release tree shows the story "module" as closed
    And the release tree shows the story "formulas" as in progress
    And the release tree shows the story "gateway" as held
    And the story "module" of the filed plan is now closed
    And the story "formulas" of the filed plan is now in progress
    And the story "gateway" of the filed plan is now open
    And the release says: Released the 1 held story
    And the stories ready on vps once released are gateway

  Scenario: An epic nobody filed is a plain refusal, and nothing is released
    When the epic "f-nope" is released
    Then releasing is refused, saying: reading the epic f-nope
    And every story of the filed plan is still held
