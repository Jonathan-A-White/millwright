Feature: Setting and showing a story's path
  Every story filed under an epic that does not give it a path needs its seven
  path keys set by hand. `mw path set <id> --from <sibling> [key=value ...]`
  sets them in one command: it copies the sibling's path, applies any named
  keys over it, and writes the lot as one write, so the tracker's single-writer
  rule holds by construction. `mw path show <id>` prints the seven keys.
  A path is only adjusted before a story starts.

  Background:
    Given the path-set story "A" is open with the metadata:
      | rig       | millwright   |
      | branch    | main         |
      | harness   | claude       |
      | model     | sonnet       |
      | effort    | high         |
      | formula   | tdd-feature  |
      | host      | laptop       |
      | attempts  | 2            |
      | molecule  | mw-mol-xyz   |
      | claimed_at | 2026-10-11T10:00:00Z |
    And the path-set story "B" is open with no metadata

  Scenario: Copying a sibling's path gives exactly its seven path keys, in one write
    When the path of "B" is set from "A"
    Then setting the path succeeds
    And the story "B" has exactly the metadata:
      | rig       | millwright   |
      | branch    | main         |
      | harness   | claude       |
      | model     | sonnet       |
      | effort    | high         |
      | formula   | tdd-feature  |
      | host      | laptop       |
    And the path-set took exactly 1 write
    And the path-set printed the line: rig=millwright
    And the path-set printed the line: host=laptop

  Scenario: Keys named after --from override the sibling's
    When the path of "B" is set from "A" with model=opus effort=max
    Then setting the path succeeds
    And the story "B" has the path key "model" as "opus"
    And the story "B" has the path key "effort" as "max"
    And the story "B" has the path key "rig" as "millwright"
    And the story "B" has the path key "host" as "laptop"

  Scenario: Named keys alone set only those keys
    When the path of "B" is set with rig=lampas
    Then setting the path succeeds
    And the story "B" has exactly the metadata:
      | rig | lampas |

  Scenario: An unknown key is refused with the list of the seven
    When the path of "B" is set with colour=red
    Then setting the path is refused, saying: rig, branch, harness, model, effort, formula, host
    And nothing was written

  Scenario: A story already started is refused and nothing is written
    Given the path-set story "C" is in progress
    When the path of "C" is set from "A"
    Then setting the path is refused, saying: in_progress
    And nothing was written

  Scenario: A closed story is refused and nothing is written
    Given the path-set story "D" is closed
    When the path of "D" is set from "A"
    Then setting the path is refused, saying: closed
    And nothing was written

  Scenario: A sibling with no rig is refused
    When the path of "B" is set from "B"
    Then setting the path is refused, saying: has no rig
    And nothing was written

  Scenario: A sibling the tracker does not have is refused
    When the path of "B" is set from "nope"
    Then setting the path is refused, saying: reading the story nope
    And nothing was written

  Scenario: Setting a path with nothing to set is refused
    When the path of "B" is set with no keys
    Then setting the path is refused, saying: --from
    And nothing was written

  Scenario: Showing a path prints the seven keys, unset for a missing one
    Given the path-set story "E" is open with the metadata:
      | rig | lampas |
    When the path of "E" is shown
    Then the path shown has the line: rig=lampas
    And the path shown has the line: branch=(unset)
    And the path shown has the line: host=(unset)

  # files (mw-gq6.349): the eighth key, the repo paths or globs a story expects
  # to change. It is the story's own, so --from never copies it.

  Scenario: The files key is set by name and shown
    When the path of "B" is set with rig=lampas files=src/Reader.tsx,src/data/*.ts
    Then setting the path succeeds
    And the story "B" has the path key "files" as "src/Reader.tsx,src/data/*.ts"
    When the path of "B" is shown
    Then the path shown has the line: files=src/Reader.tsx,src/data/*.ts

  Scenario: Copying a sibling's path never copies its files
    Given the path-set story "F" is open with the metadata:
      | rig   | lampas         |
      | host  | laptop         |
      | files | src/Reader.tsx |
    When the path of "B" is set from "F"
    Then setting the path succeeds
    And the story "B" has exactly the metadata:
      | rig  | lampas |
      | host | laptop |

  Scenario: A path with no files shows the key unset
    When the path of "A" is shown
    Then the path shown has the line: files=(unset)
