Feature: mw postern bead
  mw postern bead prints one bead in full for the Governor's app (postern's
  docs/protocol.md section 12): its fields, what it waits on and what waits on
  it within its epic, its children if it is an epic, its full description and
  every comment, oldest first. It is sealed exactly as the live view is, and
  the postern backend runs it for every GET /api/beads/{id}, so it only reads.
  A bead the tracker does not know leaves mw with status 3, which the backend
  answers 404.

  Background:
    Given the detailed epic "mw-d" holds "mw-d.1", "mw-d.2" and "mw-d.3"
    And the detailed bead "mw-d.2" waits on "mw-d.1" and "mw-d.3" waits on "mw-d.2"
    And the detailed bead "mw-d.2" carries the comments "first" and "second"

  Scenario: A story's detail names what it waits on, what waits on it, and every comment
    When mw postern bead "mw-d.2" is read
    Then the detail waits on "mw-d.1" and blocks "mw-d.3"
    And the detail carries the comments "first, second", oldest first
    And the detail read wrote nothing

  Scenario: An epic's detail names its children
    When mw postern bead "mw-d" is read
    Then the detail's children are "mw-d.1, mw-d.2, mw-d.3"

  Scenario: A bead the tracker does not know leaves with status 3
    When mw postern bead "mw-gone" is read
    Then the detail is refused as missing, status 3

  Scenario: The detail is sealed as the view is, and opens from gzip
    When mw postern bead "mw-d.2" is run for the backend
    Then the printed detail opens, from gzip, to "mw-d.2"
