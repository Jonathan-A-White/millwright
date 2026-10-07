Feature: Status lists the epics that miss their rig's requirements
  `mw status` reads the open epics of every rig whose vault file lists epic
  requirements and says which do not meet them, in an EPICS MISSING
  REQUIREMENTS section, and which the Governor waived, in EPICS WAIVED. An
  open epic is only reported, never blocked, and nothing is said of a
  compliant epic or of a rig that requires nothing.

  Background:
    Given the status rig "spell-forge" requires the epic sections "Demo" and the last-story labels "demo"

  Scenario: An open epic without the section or the last story is named
    Given the status epic "ep-1" of the rig "spell-forge" described as "Cast spells."
    And a status story "ep-1.1" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report lists "ep-1" under EPICS MISSING REQUIREMENTS saying "Demo"
    And the report lists "ep-1" under EPICS MISSING REQUIREMENTS saying "demo"

  Scenario: A compliant epic is not mentioned
    Given the status epic "ep-2" of the rig "spell-forge" described as "## Demo\nShow it."
    And a status story "ep-2.1" filed under it
    And a status story "ep-2.2" filed under it, waiting on "ep-2.1"
    And the status story "ep-2.2" is labelled "demo"
    When mw status reads the host
    Then reading status succeeds
    And the report has no EPICS MISSING REQUIREMENTS section
    And the report has no EPICS WAIVED section

  Scenario: An epic of a rig that requires nothing is not mentioned
    Given the status epic "ep-3" of the rig "millwright" described as "Plain."
    And a status story "ep-3.1" filed under it
    When mw status reads the host
    Then reading status succeeds
    And the report has no EPICS MISSING REQUIREMENTS section

  Scenario: A waived epic is listed as waived and not as missing
    Given the status epic "ep-4" of the rig "spell-forge" described as "Cast spells."
    And a status story "ep-4.1" filed under it
    And the status epic "ep-4" carries the waiver labels for "Demo" and "demo"
    When mw status reads the host
    Then reading status succeeds
    And the report lists "ep-4" under EPICS WAIVED saying "Demo"
    And the report has no EPICS MISSING REQUIREMENTS section

  Scenario: A closed epic is not read
    Given the status epic "ep-5" of the rig "spell-forge" described as "Cast spells."
    And the status epic "ep-5" is closed
    When mw status reads the host
    Then reading status succeeds
    And the report has no EPICS MISSING REQUIREMENTS section

  Scenario: A map is not read, though it names a rig
    Given the status epic "ep-6" of the rig "spell-forge" described as "Cast spells."
    And the status epic "ep-6" is labelled "wayfinder:map"
    And the status epic "ep-7" of the rig "spell-forge" described as "Cast spells."
    When mw status reads the host
    Then reading status succeeds
    And the report lists "ep-7" under EPICS MISSING REQUIREMENTS saying "Demo"
    And the report lists "ep-7" under EPICS MISSING REQUIREMENTS saying "demo"
    And the report does not list "ep-6" under EPICS MISSING REQUIREMENTS
