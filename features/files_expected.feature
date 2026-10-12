Feature: A rig that expects a files key warns when a story has none
  The overlap guard of mw dispatch holds back a ready story only when its path
  names `files`. A story filed without them is not guarded, and silently so.
  A rig whose file in the vault says `files_expected = true` (rigs/<rig>.toml)
  has mw status, mw release and mw path show say so for each open or held story
  of the rig that has no files key. They only warn: each still does what it did.

  Background:
    Given the files-expected epic "ep" on the rig "lampas" holds the stories:
      | id    | files          |
      | ep.1  |                |
      | ep.2  | src/Reader.tsx |
    And the files-expected story "ep.1" is held
    And the files-expected story "ep.2" is held

  Scenario: mw status lists a held story with no files key on a rig that expects one
    Given the rig file of "lampas" says:
      """
      files_expected = true
      """
    When mw status reads the host for files
    Then the files-expected output has the line: no files key: ep.1 (lampas expects one; mw path set ep.1 files=...)
    And the files-expected output does not mention: no files key: ep.2

  Scenario: mw release prints the same line and still releases
    Given the rig file of "lampas" says:
      """
      files_expected = true
      """
    When the files-expected epic is released
    Then the files-expected output has the line: no files key: ep.1 (lampas expects one; mw path set ep.1 files=...)
    And the files-expected output does not mention: no files key: ep.2
    And the files-expected story "ep.1" is now open
    And the files-expected story "ep.2" is now open

  Scenario: mw path show prints the same line for a story with no files key
    Given the rig file of "lampas" says:
      """
      files_expected = true
      """
    When the files-expected path of "ep.1" is shown
    Then the files-expected output has the line: no files key: ep.1 (lampas expects one; mw path set ep.1 files=...)
    And the files-expected output has the line: rig=lampas
    When the files-expected path of "ep.2" is shown
    Then the files-expected output does not mention: no files key

  Scenario: A rig whose file says false says nothing new
    Given the rig file of "lampas" says:
      """
      files_expected = false
      """
    When mw status reads the host for files
    Then the files-expected output does not mention: no files key
    When the files-expected epic is released
    Then the files-expected output does not mention: no files key
    And the files-expected story "ep.1" is now open

  Scenario: A rig whose file has no such key says nothing new
    Given the rig file of "lampas" says:
      """
      epic_sections = ["Demo"]
      """
    When mw status reads the host for files
    Then the files-expected output does not mention: no files key

  Scenario: A rig with no file says nothing new
    When mw status reads the host for files
    Then the files-expected output does not mention: no files key
    When the files-expected path of "ep.1" is shown
    Then the files-expected output does not mention: no files key

  Scenario: A story in another rig is not named for this rig's expectation
    Given the rig file of "lampas" says:
      """
      files_expected = true
      """
    And the files-expected epic also holds the story "ep.9", on the rig "legend", with no files
    When mw status reads the host for files
    Then the files-expected output does not mention: no files key: ep.9
