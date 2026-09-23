Feature: Filing the Mayor's plan
  The Mayor writes a plan: one epic with the default path its stories inherit,
  and the stories, each with its acceptance criteria, its estimate, whatever it
  overrides of the default path, and what it needs done first. `mw file` reads
  that plan, checks the whole of it before writing any of it, files the epic
  and its stories, and prints the tree. Every story is filed held, so that
  nothing can be dispatched from a plan nobody has approved; approving the plan
  releases them, and beads then holds back the ones still waiting on another.

  Background:
    Given the plan:
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

  Scenario: A valid plan is filed as an epic with its stories under it
    When the plan is filed
    Then filing succeeds
    And the stories are filed in the order module, gateway, formulas
    And the filed story "formulas" carries its acceptance criteria
    And the filed story "formulas" carries an estimate of 30 minutes
    And the filed story "formulas" is worked on model sonnet by formula chore

  Scenario: The tree shows every story's path and what it waits on
    When the plan is filed
    Then the tree names the epic Walking skeleton
    And the tree shows the story "module" on the path millwright/main · claude/opus/high · tdd-feature · vps
    And the tree shows the story "formulas" on the path millwright/main · claude/sonnet/high · chore · vps
    And the tree shows the story "gateway" waiting on "module"
    And the tree shows the story "module" waiting on nothing
    And the tree shows the story "gateway" held

  Scenario: A story that resolves to no branch is rejected and nothing is written
    Given the plan:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "defaults": { "rig": "millwright", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "module", "title": "Go module", "acceptance": "make test passes.", "needs": [] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: branch is required
    And nothing is written

  Scenario: Stories that wait on each other are rejected and nothing is written
    Given the plan:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "module", "title": "Go module", "acceptance": "make test passes.", "needs": ["gateway"] },
          { "key": "gateway", "title": "Beads gateway", "acceptance": "make test passes.", "needs": ["module"] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: wait on each other
    And nothing is written

  Scenario: A story waiting on a story the plan does not have is rejected
    Given the plan:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "module", "title": "Go module", "acceptance": "make test passes.", "needs": ["runner"] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: waits on runner, which no story in this plan is
    And nothing is written

  Scenario: A story whose formula is not installed is rejected and nothing is written
    Given the plan:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "module", "title": "Go module", "acceptance": "make test passes.", "path": { "formula": "story" }, "needs": [] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: filing story module: formula story is not installed in this vault (installed: chore, tdd-feature)
    And nothing is written

  Scenario: A story with no acceptance criteria is rejected
    Given the plan:
      """
      {
        "epic": {
          "key": "skeleton",
          "title": "Walking skeleton",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "module", "title": "Go module", "needs": [] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: has no acceptance criteria
    And nothing is written

  Scenario: An unapproved plan leaves nothing to dispatch
    When the plan is filed
    Then filing succeeds
    And no story of the epic is ready on vps
    And the tree says: nothing here can be dispatched

  Scenario: An approved plan releases exactly the stories that wait on nothing
    When the plan is filed and approved
    Then filing succeeds
    And the ready stories of the epic on vps are module, formulas

  Scenario: An approved plan leaves a story ready once what it waits on is done
    When the plan is filed and approved
    And the story "module" is closed
    Then the ready stories of the epic on vps are gateway, formulas
