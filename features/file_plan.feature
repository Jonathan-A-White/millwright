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

  Scenario: A story whose title is over the tracker's limit is rejected and nothing is written
    Given the story "gateway" has a title of 501 characters
    When the plan is filed
    Then filing is refused, saying: story gateway has a title 501 bytes long, and the tracker takes at most 500
    And nothing is written

  Scenario: An epic whose title is over the tracker's limit is rejected and nothing is written
    Given the epic has a title of 501 characters
    When the plan is filed
    Then filing is refused, saying: the epic has a title 501 bytes long, and the tracker takes at most 500
    And nothing is written

  Scenario: Titles at the tracker's limit are filed
    Given the story "gateway" has a title of 500 characters
    And the epic has a title of 500 characters
    When the plan is filed
    Then filing succeeds
    And the stories are filed in the order module, gateway, formulas

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

  Scenario: A story's files in the plan are written as its files key
    Given the story "gateway" names the files "application/file.go, domain/*.go"
    When the plan is filed
    Then the filed story "gateway" carries the files key "application/file.go, domain/*.go"

  Scenario: A story whose files overlap a running story's names it, and the dependency to add
    Given the story "gateway" names the files "application/file.go, domain/*.go"
    And the story "module" names the files "go.mod"
    And the story "formulas" names the files "formulas/*.toml"
    And the story "mw-run1" "Rework the plan" is running on the rig "millwright" with the files "domain/plan.go"
    When the plan is filed
    Then filing succeeds
    And the tree shows under the story "gateway": in flight: mw-run1 Rework the plan (shares domain/plan.go)
    And the tree shows under the story "gateway" the dependency to add on "mw-run1"
    And the tree shows nothing in flight under the story "module"
    And the tree shows nothing in flight under the story "formulas"
    And no dependency was added

  Scenario: An open story whose files overlap is named too
    Given the story "gateway" names the files "application/file.go"
    And the story "mw-open1" "Tidy the file use case" is open on the rig "millwright" with the files "application/"
    When the plan is filed
    Then the tree shows under the story "gateway": in flight: mw-open1 Tidy the file use case (shares application/file.go)

  Scenario: A story whose files overlap nothing running prints nothing in flight
    Given the story "gateway" names the files "application/file.go"
    And the story "mw-run1" "Docs only" is running on the rig "millwright" with the files "docs/"
    When the plan is filed
    Then filing succeeds
    And the tree shows nothing in flight under the story "gateway"

  Scenario: A running story on another rig is not in flight for this plan
    Given the story "gateway" names the files "application/file.go"
    And the story "mw-run1" "Same file, other rig" is running on the rig "fellowship" with the files "application/file.go"
    When the plan is filed
    Then the tree shows nothing in flight under the story "gateway"

  Scenario: A story that names no files lists the rig's running stories
    Given the story "mw-run1" "Rework the plan" is running on the rig "millwright" with the files "domain/plan.go"
    And the story "mw-run2" "Elsewhere" is running on the rig "fellowship" with the files "domain/plan.go"
    When the plan is filed
    Then the tree shows under the story "module": in flight: mw-run1 Rework the plan (same rig, no files named)
    And the tree does not name "mw-run2"

  # A bug filed without a Sweep: line is refused by mw dispatch as no-sweep; mw
  # file refuses it first, with the same words, before anything is written
  # (mw-gq6.447).
  Scenario: A bug story with no line beginning 'Sweep:' is refused and nothing is written
    Given the plan:
      """
      {
        "epic": {
          "key": "fixes",
          "title": "Reader fixes",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "crash", "title": "[bug] The reader crashes on an empty book", "acceptance": "1. An empty book opens.\n2. Sweep listed in the closing comment.", "needs": [] },
          { "key": "page", "title": "The page is lost after rotate", "labels": ["bug"], "acceptance": "Rotating keeps the page.", "needs": [] },
          { "key": "lazy", "title": "[bug] The title wraps", "acceptance": "1. It fits.\n2. Sweep: none", "needs": [] }
        ]
      }
      """
    When the plan is filed
    Then filing is refused, saying: story crash: it is a bug and no line begins with 'Sweep:' (a check over the whole class of the fault, or 'Sweep: none, because <reason>'): mw dispatch would refuse it as no-sweep
    And filing is refused, saying: story page: it is a bug and no line begins with 'Sweep:'
    And filing is refused, saying: story lazy: it is a bug and its 'Sweep: none' gives no reason
    And nothing is written

  Scenario: A bug story with 'Sweep: none, because <reason>' or a numbered Sweep line is filed, and a story that is no bug needs none
    Given the plan:
      """
      {
        "epic": {
          "key": "fixes",
          "title": "Reader fixes",
          "defaults": { "rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": "vps" }
        },
        "stories": [
          { "key": "crash", "title": "[bug] The reader crashes on an empty book", "acceptance": "1. An empty book opens.\nSweep: none, because only the reader opens books.", "needs": [] },
          { "key": "page", "title": "The page is lost after rotate", "labels": ["bug"], "acceptance": "1. Rotating keeps the page.\n7. Sweep: every viewport effect is grepped for state it drops.", "needs": [] },
          { "key": "theme", "title": "A dark theme", "acceptance": "The reader can be dark.", "needs": [] }
        ]
      }
      """
    When the plan is filed
    Then filing succeeds
    And the stories are filed in the order crash, page, theme
