Feature: Per-rig epic requirements
  A rig can ask more of its epics than the plan format does: the rig's file in
  the vault lists the sections an epic's description must contain
  (epic_sections) and the labels of the stories that must come last
  (epic_last_story_labels). `mw file` refuses a plan that does not meet them,
  naming each one that is missing, and writes nothing. Only the Governor's own
  word waives one, on the command line with the words he said, and the epic
  carries that record. A rig whose file lists neither is not checked.

  Background:
    Given the rig "spell-forge" requires the epic sections "Demo" and the last-story labels "demo"

  Scenario: A plan with no Demo section and no demo story is refused naming both
    Given the requiring plan for the rig "spell-forge" with a description of "Cast spells." and these stories:
      | key | labels | needs |
      | one |        |       |
      | two |        | one   |
    When the requiring plan is filed
    Then filing is refused, saying: the Demo section
    And filing is refused, saying: the last story labelled demo
    And nothing is written

  Scenario: A plan with a Demo section and a demo story waiting on every sibling is filed
    Given the requiring plan for the rig "spell-forge" with a description of "Cast spells.\n\n## Demo\n\nShow a spell." and these stories:
      | key  | labels | needs    |
      | one  |        |          |
      | two  |        | one      |
      | show | demo   | one, two |
    When the requiring plan is filed
    Then filing succeeds
    And the filed story "show" carries the label "demo"

  Scenario: A demo story that does not wait on every sibling is refused
    Given the requiring plan for the rig "spell-forge" with a description of "Demo: show it" and these stories:
      | key  | labels | needs |
      | one  |        |       |
      | two  |        |       |
      | show | demo   | one   |
    When the requiring plan is filed
    Then filing is refused, saying: does not wait on two
    And nothing is written

  Scenario: A rig whose file lists neither key files as it always has
    Given the requiring plan for the rig "millwright" with a description of "Plain." and these stories:
      | key | labels | needs |
      | one |        |       |
    When the requiring plan is filed
    Then filing succeeds

  Scenario: A waiver with the Governor's words files the plan and is written on the epic
    Given the requiring plan for the rig "spell-forge" with a description of "Cast spells." and these stories:
      | key | labels | needs |
      | one |        |       |
    When the requiring plan is filed waiving "Demo" and "demo" because "no demo for this one, it is a rename"
    Then filing succeeds
    And the filed epic carries the waiver labels for "Demo" and "demo"
    And the filed epic has a comment quoting "no demo for this one, it is a rename"

  Scenario: A waiver without the Governor's words is refused
    Given the requiring plan for the rig "spell-forge" with a description of "Cast spells." and these stories:
      | key | labels | needs |
      | one |        |       |
    When the requiring plan is filed waiving "Demo" and "demo" with no words
    Then filing is refused, saying: --because
    And nothing is written

  Scenario: A waiver of something the rig does not require is refused
    Given the requiring plan for the rig "spell-forge" with a description of "Demo: x" and these stories:
      | key | labels | needs |
      | one |        |       |
    When the requiring plan is filed waiving "Rollback" because "he said so"
    Then filing is refused, saying: does not require Rollback
    And nothing is written

  Scenario: Waiving the section does not waive the last story of the same name
    Given the requiring plan for the rig "spell-forge" with a description of "Cast spells." and these stories:
      | key | labels | needs |
      | one |        |       |
    When the requiring plan is filed waiving "Demo" because "no demo section, but he wants the story"
    Then filing is refused, saying: the last story labelled demo
    And nothing is written
