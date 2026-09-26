Feature: The postern snapshot carries a bead's description and comments
  mw postern snapshot names, for every needs-you and working bead, its
  description and newest three comments, newest first. A landed bead carries
  the same, but only when it landed within the last day; one landed longer
  ago drops off the list, though the epic's closed_count still counts it.

  Scenario: A bead landed yesterday is listed, one landed three days ago is not, and a working bead shows its text
    Given a live epic "mw-a"
    And the bead "mw-a.1" under "mw-a" is being worked, with a description and four comments
    And the bead "mw-a.2" under "mw-a" landed yesterday
    And the bead "mw-a.3" under "mw-a" landed three days ago
    When the postern snapshot is built
    Then the working bead "mw-a.1" in the snapshot shows its description and its newest three comments, newest first
    And the snapshot's landed beads under "mw-a" are exactly "mw-a.2"
    And the snapshot's closed_count under "mw-a" is 2
