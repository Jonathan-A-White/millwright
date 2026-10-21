Feature: A story is sized by the tests that call its files
  Two Lampas stories hit the turn cap because they were sized by the screens
  they name, not by the tests that call those screens. When a story names files,
  `mw file` and `mw path set` count the rig's test files that name them (by
  base name without extension), say it as 'test callers: N for size S (allows
  M)', and warn when N is more than the size allows: XS 5, S 10, M 20, L 35,
  XL 50. `mw file --check` then fails, unless the story says why not, as
  "callers_ok".

  Background:
    Given the callers rig "lampas" is a git checkout whose 12 test files name "Settings" and 3 do not
    And the callers plan:
      """
      {
        "epic": {
          "key": "settings",
          "title": "Settings",
          "priority": 1,
          "description": "Move the settings pages.",
          "success_criteria": "They move.",
          "defaults": {
            "rig": "lampas", "branch": "main", "harness": "claude", "model": "sonnet",
            "effort": "high", "formula": "tdd-feature", "host": "vps"
          }
        },
        "stories": [
          {
            "key": "pages",
            "title": "Settings sub pages",
            "priority": 1,
            "description": "Move them.",
            "acceptance": "They moved.\nExpect: none, because a test plan",
            "files": "src/screens/Settings.tsx",
            "path": { "size": "S" }
          }
        ]
      }
      """

  Scenario: A story over its size's allowance is warned of and fails the check
    When the callers plan is checked
    Then the callers check is refused
    And the callers output has the line: pages: test callers: 12 for size S (allows 10)
    And the callers warning has the line: WARN: 12 test callers for size S; split (a first story moving the callers onto one helper) or size up

  Scenario: A story sized up to fit passes the check
    Given the callers story "pages" is sized M
    When the callers plan is checked
    Then the callers check passes
    And the callers output has the line: pages: test callers: 12 for size M (allows 20)
    And the callers warning is empty

  Scenario: A story that says why the callers are fine passes the check
    Given the callers story "pages" says callers_ok "the callers only import the type"
    When the callers plan is checked
    Then the callers check passes
    And the callers output has the line: pages: test callers: 12 for size S (allows 10)
    And the callers warning is empty

  Scenario: A story naming files no test mentions has no callers
    Given the callers story "pages" names the files "src/screens/Gone.tsx"
    When the callers plan is checked
    Then the callers check passes
    And the callers output has the line: pages: test callers: 0 for size S (allows 10)

  Scenario: Filing warns of the callers and files the plan all the same
    When the callers plan is filed
    Then the callers plan is filed
    And the callers output has the line: f-1.1: test callers: 12 for size S (allows 10)
    And the callers warning has the line: WARN: 12 test callers for size S; split (a first story moving the callers onto one helper) or size up

  Scenario: Setting a story's files and size counts the callers too
    Given the callers story "B" is open on the rig "lampas"
    When the callers path of "B" is set with files=src/screens/Settings.tsx size=XS
    Then the callers output has the line: B: test callers: 12 for size XS (allows 5)
    And the callers warning has the line: WARN: 12 test callers for size XS; split (a first story moving the callers onto one helper) or size up

  Scenario: Setting a path key that is neither files nor size counts nothing
    Given the callers story "B" is open on the rig "lampas"
    When the callers path of "B" is set with model=opus
    Then the callers output does not mention: test callers

  Scenario: Setting callers_ok on a story over its allowance counts the callers and does not warn
    Given the callers story "B" is open on the rig "lampas"
    When the callers path of "B" is set with files=src/screens/Settings.tsx size=XS
    And the callers reason of "B" is set to "additive: new function beside the old"
    Then the callers story "B" has the metadata callers_ok "additive: new function beside the old"
    And the callers output has the line: B: test callers: 12 for size XS (allows 5)
    And the callers warning is empty

  Scenario: A story that carries callers_ok is not warned of when its size is set later
    Given the callers story "B" is open on the rig "lampas"
    And the callers story "B" already carries callers_ok "additive: new function beside the old"
    When the callers path of "B" is set with files=src/screens/Settings.tsx size=XS
    Then the callers output has the line: B: test callers: 12 for size XS (allows 5)
    And the callers warning is empty

  Scenario: An empty callers_ok is refused
    Given the callers story "B" is open on the rig "lampas"
    When the callers path of "B" is set with callers_ok=
    Then the callers check is refused
    And the callers story "B" has no metadata callers_ok

  Scenario: Filing a plan story with callers_ok writes it to the filed story
    Given the callers story "pages" says callers_ok "the callers only import the type"
    When the callers plan is filed
    Then the callers plan is filed
    And the callers story "f-1.1" has the metadata callers_ok "the callers only import the type"
    And the callers warning is empty

  Scenario: Copying a sibling's path does not copy its callers_ok
    Given the callers story "A" is open on the rig "lampas"
    And the callers story "A" already carries callers_ok "additive: new function beside the old"
    And the callers story "B" is open on the rig "lampas"
    When the callers path of "B" is copied from "A" with files=src/screens/Settings.tsx size=XS
    Then the callers story "B" has no metadata callers_ok
    And the callers warning has the line: WARN: 12 test callers for size XS
