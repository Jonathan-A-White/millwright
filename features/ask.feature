Feature: mw ask
  A bead may record who outside the factory asked for its work, or whom the
  factory asked something of: asked-by:<login> for an inbound ask,
  asked-of:<login> for an outbound one, and ask:<role> for who that person is
  to the Governor (tl, skip, drqs), so the history survives a reorg. When an
  outbound ask is sent, mw ask waiting labels the bead waiting:others and
  records when; mw ask done clears it. Needs you lists the waiting beads and
  chases one that has not moved for three working days (postern_view.feature).

  Background:
    Given the ask epic "mw-ask" with the story "mw-ask.1" titled "Get the sign-off"

  Scenario: An inbound ask is labelled asked-by with the asker's role
    When the ask is recorded on "mw-ask.1" as asked by "pat" in the role "tl"
    Then the ask succeeds
    And the ask story "mw-ask.1" carries the labels "asked-by:pat, ask:tl"

  Scenario: An outbound ask is labelled asked-of
    When the ask is recorded on "mw-ask.1" as asked of "sam" in the role "skip"
    Then the ask succeeds
    And the ask story "mw-ask.1" carries the labels "asked-of:sam, ask:skip"

  Scenario: A login with an at sign or capitals is labelled the way it is read back
    When the ask is recorded on "mw-ask.1" as asked of "@Sam" in the role ""
    Then the ask story "mw-ask.1" carries the labels "asked-of:sam"

  Scenario: An empty login is refused
    When the ask is recorded on "mw-ask.1" as asked of "" in the role ""
    Then the ask is refused, saying: who was asked
    And the ask story "mw-ask.1" carries no labels

  Scenario: Waiting labels the bead waiting:others and records the time
    Given the ask clock reads "2026-10-05T14:30:00Z"
    When "mw-ask.1" is marked waiting on "sam"
    Then the ask succeeds
    And the ask story "mw-ask.1" carries the labels "asked-of:sam, waiting:others"
    And the ask story "mw-ask.1" has been waiting since "2026-10-05T14:30:00Z"
    And the ask says: mw-ask.1 is waiting on sam since 5 Oct 14:30 UTC

  Scenario: Waiting again keeps the time it first waited since
    Given the ask clock reads "2026-10-05T14:30:00Z"
    And "mw-ask.1" is marked waiting on "sam"
    And the ask clock reads "2026-10-07T09:00:00Z"
    When "mw-ask.1" is marked waiting on "sam"
    Then the ask story "mw-ask.1" has been waiting since "2026-10-05T14:30:00Z"

  Scenario: Waiting on a bead already closed is refused
    Given the ask story "mw-ask.1" is closed
    When "mw-ask.1" is marked waiting on "sam"
    Then the ask is refused, saying: mw-ask.1 is closed
    And the ask story "mw-ask.1" carries no labels

  Scenario: Done clears the waiting label and the time but keeps who was asked
    Given the ask clock reads "2026-10-05T14:30:00Z"
    And "mw-ask.1" is marked waiting on "sam"
    When "mw-ask.1" is marked done waiting
    Then the ask succeeds
    And the ask story "mw-ask.1" carries the labels "asked-of:sam"
    And the ask story "mw-ask.1" has no waiting time
    And the ask says: mw-ask.1 is no longer waiting on others

  Scenario: Done on a bead that was not waiting changes nothing
    When "mw-ask.1" is marked done waiting
    Then the ask succeeds
    And the ask story "mw-ask.1" carries no labels
    And the ask says: mw-ask.1 was not waiting on others
