Feature: A grind example stopped on a host with no mill is finished on the home
  A branch that adds or changes a grind example is landed only with a live
  `mw grist eval` line for it (mw-gq6.772), and the live mill and the grist test
  key are on the home alone. A story that went to a host with neither stops
  grist-example-unproved after its whole session (mw-1knt69p): mw next pushes
  its branch to the rig's origin and names the evals to run from the home, and
  the next attempt, on whichever host, continues that pushed branch, not main.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" was started once
    And the session of "mw-gq6.1" reported a plain success
    And a commit on the branch of "mw-gq6.1" adds the grind example "grinds/examples/bible-talk/verse-text-ask.json"
    And a formula was poured for "mw-gq6.1" and every step, the closing-comment step too, is closed
    And the comment "Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit

  Scenario: A host with no grist test key pushes the branch and names the evals to run from the home
    Given this host holds no grist test key
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "grist-example-unproved"
    And the branch of "mw-gq6.1" is on the rig's origin as "mw/mw-gq6.1"
    And the printed report says "mw grist eval <app> bible-talk --example verse-text-ask --ref mw/mw-gq6.1"
    And the printed report says "Pushed mw/mw-gq6.1 to origin"
    And the comment on "mw-gq6.1" says: Pushed mw/mw-gq6.1 to origin

  Scenario: The home stops as before and pushes nothing
    Given this host holds the grist test key
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "grist-example-unproved"
    And the branch of "mw-gq6.1" is not on the rig's origin
    And the comment on "mw-gq6.1" says: Run each on the branch against the real mill

  Scenario: The next attempt, on a host that never held the branch, continues the pushed one
    Given this host holds no grist test key
    When mw closes out "mw-gq6.1"
    Then the report says it stopped for "grist-example-unproved"
    Given the worktree and branch of "mw-gq6.1" are gone from this host
    When the next dispatch tick runs with the leftovers kept
    Then one session was started again, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" holds the grind example "grinds/examples/bible-talk/verse-text-ask.json", rebased onto "main"
    And the boot of attempt 2 of "mw-gq6.1" says "You continue attempt 1's branch: read git log first"
