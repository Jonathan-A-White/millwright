Feature: a landing's verify need folds
  A landing's verify need stays in Needs you for [postern] verify_days (default 4)
  after it closed. From then until the landing is verified, or 30 days after it
  closed, the view publishes it folded, and the first view that folds it sends
  the Mayor one verify_folded event naming the bead, so he verifies it from the
  Governor's use. A landing keeps its need whether or not its epic is still live,
  and past the week the view lists beads for.

  Background:
    Given the view's epic "mw-f" is live

  Scenario: A landing closed a day ago is a verify need that is not folded
    Given the view's bead "mw-f.1" under "mw-f" landed 1 day ago
    When the live view is built
    Then the view's verify need on "mw-f.1" is not folded

  Scenario: A landing closed five days ago is a folded verify need
    Given the view's bead "mw-f.1" under "mw-f" landed 5 days ago
    When the live view is built
    Then the view's verify need on "mw-f.1" is folded

  Scenario: A landing closed ten days ago, not verified, is a folded verify need
    Given the view's bead "mw-f.1" under "mw-f" landed 10 days ago
    When the live view is built
    Then the view's verify need on "mw-f.1" is folded

  Scenario: A landing closed 31 days ago has no verify need
    Given the view's bead "mw-f.1" under "mw-f" landed 31 days ago
    When the live view is built
    Then the view has no verify need on "mw-f.1"

  Scenario Outline: A verified landing has no verify need at any age
    Given the view's bead "mw-f.1" under "mw-f" landed <days> days ago
    And the view's bead "mw-f.1" has the comment "VERIFIED by the Mayor"
    When the live view is built
    Then the view has no verify need on "mw-f.1"

    Examples:
      | days |
      | 1    |
      | 5    |
      | 20   |

  Scenario: verify_days of 2 folds a landing at two days
    Given the view's bead "mw-f.1" under "mw-f" landed 1 day ago
    And the view's bead "mw-f.2" under "mw-f" landed 2 days ago
    When the live view is built with verify_days 2
    Then the view's verify need on "mw-f.1" is not folded
    And the view's verify need on "mw-f.2" is folded

  Scenario: A landing closed five days ago under an epic that has since closed is a folded verify need
    Given the view's bead "mw-f.1" under "mw-f" landed 5 days ago
    And the view's epic "mw-f" has since closed
    When the live view is built
    Then the view's verify need on "mw-f.1" is folded

  Scenario: The first view that folds a need sends the Mayor one event naming the bead, and the next sends none
    Given the view's bead "mw-f.1" under "mw-f" landed 1 day ago
    And the view's bead "mw-f.2" under "mw-f" landed 5 days ago
    When the live view is built with an event log
    Then the Mayor was sent 1 verify_folded event, naming "mw-f.2"
    When the live view is built with an event log
    Then the Mayor was sent 1 verify_folded event, naming "mw-f.2"
