Feature: The common-errors register
  An error class seen in one app is checked in every app it can occur in. The
  register is in the vault, seats/builder/common-errors/, one file a class, and
  kinds.toml beside them says what kinds each rig is. The Mayor keeps it with mw
  errors, which spends no fuel and only writes files; a Builder reads it by
  need, never at boot. A class found twice asks the Mayor whether a shared
  component would carry one fix to every rig; nothing is filed for him.

  Scenario: The Mayor adds a class and it is a file in the register
    Given an empty error register
    When the Mayor adds the error class "gloss-from-a-neighbour" of the kinds "word-gloss" found in "lampas" by "mw-5r3p30.360"
    Then the error register holds the class file "gloss-from-a-neighbour.md"
    And mw errors printed the path of "gloss-from-a-neighbour.md"

  Scenario: A slug already in the register is refused
    Given an empty error register
    And the Mayor adds the error class "gloss-from-a-neighbour" of the kinds "word-gloss" found in "lampas" by "mw-5r3p30.360"
    When the Mayor adds the error class "gloss-from-a-neighbour" of the kinds "pwa" found in "legend" by "mw-x.1"
    Then mw errors refuses, saying "already in the register"

  Scenario: A class found a second time asks whether a shared component would fix it everywhere
    Given an empty error register
    And the Mayor adds the error class "distractors-miss-the-point" of the kinds "quiz" found in "lampas" by "mw-i7xe0n.7"
    When the Mayor records the check of "distractors-miss-the-point" in "spell-forge" as "found:mw-abc.1"
    Then mw errors says "distractors-miss-the-point is seen 2 times"
    And mw errors asks the shared component question
    And the error register holds only class files

  Scenario: The seed writes the first three classes from the Lampas bugs, each with its kinds
    Given an empty error register
    When the Mayor seeds the error register
    And the Mayor lists the error register
    Then mw errors says "example-word-wrong-sound"
    And mw errors says "gloss-from-a-neighbour"
    And mw errors says "grind-ignores-a-readers-no"
    And every class in the error register names its kinds

  Scenario: mw status names a class not yet checked in every rig of its kinds
    Given an empty error register
    And the Mayor says the rig "lampas" is of the kinds "pwa grind"
    And the Mayor says the rig "spell-forge" is of the kinds "pwa"
    And the Mayor adds the error class "a-pwa-class" of the kinds "pwa" found in "lampas" by "mw-a.1"
    When mw status reads the error register
    Then the status shows "ERROR CLASSES TO CHECK (1)"
    And the status shows "a-pwa-class: not checked in spell-forge"

  Scenario: mw status says nothing of a register checked everywhere
    Given an empty error register
    And the Mayor says the rig "lampas" is of the kinds "pwa"
    And the Mayor adds the error class "a-pwa-class" of the kinds "pwa" found in "lampas" by "mw-a.1"
    When mw status reads the error register
    Then the status does not show "ERROR CLASSES TO CHECK"
