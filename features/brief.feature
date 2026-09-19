Feature: mw brief
  mw brief prints, for each bead named and in the order given, what a booting
  seat still needs of it: one line for the bead itself, then only the children
  that are not closed — in progress, open, held — each with its priority, the
  host and model its path names, and what it waits on; then how many children
  are closed. It prints no description. With --comments N the newest N comments
  of the bead follow in full. It only reads: nothing is written to the tracker.

  Background:
    Given the brief epic "mw-6ww" titled "The map" at priority 1 on the default path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | laptop      |

  Scenario: Only the live children are shown, under their headings, with the closed counted
    Given a brief story "mw-6ww.1" titled "Shipped one" filed under it, which is closed
    And a brief story "mw-6ww.2" titled "Shipped two" filed under it, which is closed
    And a brief story "mw-6ww.3" titled "Being worked" filed under it, which is in progress
    And a brief story "mw-6ww.4" titled "Ready to take" filed under it, which is open
    And a brief story "mw-6ww.5" titled "Not yet approved" filed under it, which is held
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the brief opens with the line "The map · mw-6ww · open · P1"
    And the brief lists "mw-6ww.3" under the heading "In progress"
    And the brief lists "mw-6ww.4" under the heading "Open"
    And the brief lists "mw-6ww.5" under the heading "Held"
    And the brief does not list "mw-6ww.1"
    And the brief does not list "mw-6ww.2"
    And the brief says "2 closed" on a line of its own

  Scenario: A group with nothing in it has no heading, and a bead with nothing closed says so
    Given a brief story "mw-6ww.1" titled "Ready to take" filed under it, which is open
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the brief lists "mw-6ww.1" under the heading "Open"
    And the brief has no heading "In progress"
    And the brief has no heading "Held"
    And the brief says "0 closed" on a line of its own

  Scenario: Each child carries its priority and the host and model of its path
    Given a brief story "mw-6ww.1" titled "Plain one" filed under it, which is open
    And a brief story "mw-6ww.2" titled "Cheaper one" filed under it, which is open
    And the brief story "mw-6ww.2" overrides "model" with "sonnet" and "host" with "vps"
    And the brief story "mw-6ww.2" is at priority 0
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the line of the brief for "mw-6ww.1" says "P2 · laptop · opus"
    And the line of the brief for "mw-6ww.2" says "P0 · vps · sonnet"

  Scenario: A child that waits on an open blocker names the blocker by its title
    Given a brief story "mw-6ww.1" titled "The blocker" filed under it, which is open
    And a brief story "mw-6ww.2" titled "The waiter" filed under it, which is open
    And the brief story "mw-6ww.2" waits on "mw-6ww.1"
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the line of the brief for "mw-6ww.2" says "waits on The blocker"
    And the line of the brief for "mw-6ww.1" does not say "waits on"

  Scenario: A blocker that is closed is not named
    Given a brief story "mw-6ww.1" titled "The finished blocker" filed under it, which is closed
    And a brief story "mw-6ww.2" titled "The waiter" filed under it, which is open
    And the brief story "mw-6ww.2" waits on "mw-6ww.1"
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the line of the brief for "mw-6ww.2" does not say "waits on"
    And the brief does not name "The finished blocker"

  Scenario: A blocker in another epic is named by its title too, unless it is closed
    Given a brief story "mw-6ww.1" titled "Across the epics" filed under it, which is open
    And the brief epic "mw-0om" titled "The other map" at priority 2 on the default path:
      | host | laptop |
    And a brief story "mw-0om.1" titled "Elsewhere and open" filed under it, which is open
    And a brief story "mw-0om.2" titled "Elsewhere and done" filed under it, which is closed
    And the brief story "mw-6ww.1" waits on "mw-0om.1"
    And the brief story "mw-6ww.1" waits on "mw-0om.2"
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the line of the brief for "mw-6ww.1" says "waits on Elsewhere and open"
    And the brief does not name "Elsewhere and done"

  Scenario: A long title is clipped, a long blocker's title more so, and a comment never is
    Given a brief story "mw-6ww.1" titled "A blocker whose title runs on and on and on" filed under it, which is open
    And a brief story "mw-6ww.2" titled "A title that runs on and on past what a phone screen shows in one line" filed under it, which is open
    And the brief story "mw-6ww.2" waits on "mw-6ww.1"
    And the brief epic "mw-6ww" has the comment:
      """
      A title that runs on and on past what a phone screen shows in one line
      """
    When mw brief reads "mw-6ww" with the newest 1 comments
    Then reading the brief succeeds
    And the line of the brief for "mw-6ww.2" says "A title that runs on and on past what a phone screen shows … · mw-6ww.2"
    And the line of the brief for "mw-6ww.2" says "waits on A blocker whose title r…"
    And the line of the brief for "mw-6ww.2" does not say "in one line"
    And the brief prints the comment in full:
      """
      A title that runs on and on past what a phone screen shows in one line
      """

  Scenario: --comments 2 prints the newest two comments whole, and no others
    Given the brief epic "mw-6ww" has the comment:
      """
      The very first comment.
      """
    And the brief epic "mw-6ww" has the comment:
      """
      The second comment.
      """
    And the brief epic "mw-6ww" has the comment:
      """
      The Governor said: do it,
      and do it in full,

      with a blank line and no clipping.
      """
    And the brief epic "mw-6ww" has the comment:
      """
      The newest comment.
      """
    When mw brief reads "mw-6ww" with the newest 2 comments
    Then reading the brief succeeds
    And the brief prints the comment in full:
      """
      The Governor said: do it,
      and do it in full,

      with a blank line and no clipping.
      """
    And the brief prints the comment in full:
      """
      The newest comment.
      """
    And the brief does not name "The very first comment."
    And the brief does not name "The second comment."

  Scenario: Without --comments no comment is printed
    Given the brief epic "mw-6ww" has the comment:
      """
      A comment nobody asked for.
      """
    When mw brief reads "mw-6ww"
    Then reading the brief succeeds
    And the brief does not name "A comment nobody asked for."

  Scenario: Two ids print two briefs in the order given
    Given the brief epic "mw-0om" titled "The other map" at priority 2 on the default path:
      | host | laptop |
    When mw brief reads "mw-0om" and "mw-6ww"
    Then reading the brief succeeds
    And the brief names "The other map" before "The map"

  Scenario: An unknown id is a plain error naming it, and nothing is printed
    When mw brief reads "mw-6ww" and "mw-nope"
    Then the brief is refused, naming "mw-nope"
    And the brief printed nothing

  Scenario: Briefing writes nothing to the tracker
    Given a brief story "mw-6ww.1" titled "Being worked" filed under it, which is in progress
    And a brief story "mw-6ww.2" titled "Ready to take" filed under it, which is open
    And the brief epic "mw-6ww" has the comment:
      """
      A comment.
      """
    When mw brief reads "mw-6ww" with the newest 1 comments
    Then reading the brief succeeds
    And the tracker recorded no writes for the brief
