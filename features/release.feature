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
            "acceptance": "make test passes.\nExpect: none, because a test plan",
            "needs": []
          },
          {
            "key": "gateway",
            "title": "Beads gateway",
            "priority": 1,
            "estimate": 90,
            "description": "Read and write stories through bd.",
            "acceptance": "make test passes.\nExpect: none, because a test plan",
            "needs": ["module"]
          },
          {
            "key": "formulas",
            "title": "First formulas",
            "priority": 2,
            "estimate": 30,
            "description": "Author the tdd-feature and chore formulas.",
            "acceptance": "Both formulas cook.\nExpect: none, because a test plan",
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

  Scenario: Releasing a held epic opens the epic too, so its released stories show on the phone
    Given the epic of the filed plan is held
    When the epic is released
    Then releasing succeeds
    And the epic of the filed plan is now open
    And the story "module" of the filed plan is now open
    And the story "gateway" of the filed plan is now open
    And the story "formulas" of the filed plan is now open
    And the release says: Opened the epic

  Scenario: An open epic is left as it is when its held stories are released
    When the epic is released
    Then releasing succeeds
    And the epic of the filed plan is now open
    And the story "module" of the filed plan is now open

  Scenario: A held epic with no held story to release stays held
    Given the epic of the filed plan is held
    And the story "module" of the filed plan is finished
    And the story "gateway" of the filed plan is finished
    And the story "formulas" of the filed plan is finished
    When the epic is released
    Then releasing succeeds
    And the epic of the filed plan is now held
    And the release says: Nothing to release

  Scenario: A story parked on the Governor's word on its parent is refused, and nothing is released
    Given the epic of the filed plan carries the comment "Parked on the Governor's word 2026-10-12: file for later; held until he asks"
    When the held story "module" is released
    Then releasing is refused, saying: held on the Governor's word
    And releasing is refused, saying: file for later
    And every story of the filed plan is still held

  Scenario: Releasing the whole epic is refused when one of its stories is held on his word
    Given the story "gateway" of the filed plan carries the comment "HELD: his word, do not release yet"
    When the epic is released
    Then releasing is refused, saying: do not release yet
    And every story of the filed plan is still held

  Scenario: --over-hold releases the story and writes the reason on it
    Given the epic of the filed plan carries the comment "Parked on the Governor's word 2026-10-12: file for later; held until he asks"
    When the held story "module" is released over the hold "he asked for it in chat"
    Then releasing succeeds
    And the story "module" of the filed plan is now open
    And the story "module" of the filed plan carries the comment "RELEASED OVER HOLD: he asked for it in chat"

  Scenario: A hold older than a release over it no longer holds the story
    Given the epic of the filed plan carries the comment "Parked on the Governor's word: held" dated 2026-10-12T13:25:00Z
    And the story "module" of the filed plan carries the comment "RELEASED OVER HOLD: asked for" dated 2026-10-16T00:24:00Z
    When the held story "module" is released
    Then releasing succeeds
    And the story "module" of the filed plan is now open

  Scenario: Comments that do not hold a story on his word release as before
    Given the epic of the filed plan carries the comment "Held for review by the Mayor"
    And the story "module" of the filed plan carries the comment "Estimate revised; the Governor approved the plan"
    When the held story "module" is released
    Then releasing succeeds
    And the story "module" of the filed plan is now open
