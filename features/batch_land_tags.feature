Feature: A landing carries the release tags its stories made to the origin
  A tag a story makes stays in the checkout it was made in, and landing pushed
  only the branch, so each release needed the Governor's hand (bsv-kit v0.2.0
  and v1.0.0, mw-t7c831.14). Once the branch is pushed, a landing pushes every
  tag that points at a commit of the landed range and is not at the origin,
  names each in the Landed mail, never moves a tag the origin has elsewhere,
  and warns when a tag the story's acceptance criteria name is still not at the
  origin (mw-gq6.673).

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the rig lands its stories in batches

  Scenario: A tag on a landed commit is pushed and the Landed mail names it
    Given "mw-gq6.1" is queued for batch landing, its branch tagged "v0.3.0"
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the tag "v0.3.0" is at the rig's origin on the commit of "mw-gq6.1"
    And that mail's body says: v0.3.0 pushed to origin

  Scenario: A tag outside the landed range is not pushed
    Given the rig has the tag "v0.1.0" on its first commit
    And "mw-gq6.1" is queued for batch landing, its branch tagged "v0.3.0"
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the tag "v0.1.0" is not at the rig's origin

  Scenario: A tag the origin has at another commit is neither moved nor forced
    Given the rig's origin has the tag "v0.3.0" on its first commit
    And "mw-gq6.1" is queued for batch landing, its branch tagged "v0.3.0"
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And the tag "v0.3.0" at the rig's origin is still on its first commit
    And that mail's body says: v0.3.0 on origin points elsewhere

  Scenario: A tag the acceptance criteria name that is nowhere gets a warning in the Landed mail
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
    And the story "mw-gq6.1" has the acceptance:
      """
      1. The release v1.2.3 is tagged.
      """
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And that mail's body says: WARN: tag v1.2.3 named in the AC is not on origin

  Scenario: A version of another project's tag the criteria name gets no warning
    Given these stories are queued for batch landing:
      | mw-gq6.1 |
    And the story "mw-gq6.1" has the acceptance:
      """
      1. The apparatus links that book's with-variants file at v3.3.2.
      2. The link points at tag v3.3.2, e.g. REV.csv.
      """
    When mw lands the batch of the rig
    Then the batch landed on "main" in one push, forcing nothing
    And that mail's body does not say "named in the AC is not on origin"
