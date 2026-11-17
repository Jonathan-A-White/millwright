Feature: A story whose acceptance changed after its claim is handed back
  A Builder reads its story once, at boot. An acceptance criterion the Mayor
  adds after the claim never reaches it, and the story would land without it
  (mw-gq6.503's AC4, mw-gq6.509). A dispatch records the acceptance a story was
  claimed with; mw next, at close-out, reads the bead's acceptance again and,
  when it no longer matches, lands nothing and gives the claim back so that the
  next session works the whole text.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" was started once
    And the session of "mw-gq6.1" reported a plain success

  Scenario: A story whose acceptance gained a line after the claim is refused and handed back
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works.
      2. go test ./... passes.
      """
    And the acceptance of "mw-gq6.1" now reads:
      """
      1. The first thing works.
      2. go test ./... passes.
      3. mw land refuses a commit carrying an AI attribution.
      """
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "acceptance-changed"
    And the story "mw-gq6.1" carries a comment quoting: the acceptance changed after the claim
    And the story "mw-gq6.1" carries a comment quoting: 3. mw land refuses a commit carrying an AI attribution.
    And the story "mw-gq6.1" is open and unclaimed
    And the story "mw-gq6.1" records attempt 1

  Scenario: A story whose acceptance did not change lands as before
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works.
      2. go test ./... passes.
      """
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin

  Scenario: A story whose acceptance was only re-spaced lands as before
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works.
      2. go test ./... passes.
      """
    And the acceptance of "mw-gq6.1" now reads:
      """
      1.   The first thing works.

      2. go test ./... passes.
      """
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin

  Scenario: A story claimed before the acceptance was recorded lands as before
    Given the story "mw-gq6.1" has the acceptance:
      """
      1. The first thing works.
      2. A line added after the claim.
      """
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin

  Scenario: A story the Mayor re-scoped after the work was done lands on his word
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works.
      2. go test ./... passes.
      """
    And the acceptance of "mw-gq6.1" now reads:
      """
      1. The first thing works.
      2. go test ./... passes, headless.
      """
    And the Mayor wrote a closing comment on "mw-gq6.1" after its newest commit
    And mw next is run with --rescoped-by-mayor "AC 2 reworded after the headless artefact"
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" carries a comment quoting: RESCOPED-BY-MAYOR: AC 2 reworded after the headless artefact

  Scenario: A re-scope with no closing comment of the Mayor's after it is still refused
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works.
      """
    And the acceptance of "mw-gq6.1" now reads:
      """
      1. The first thing works, differently.
      """
    And mw next is run with --rescoped-by-mayor "reworded"
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "acceptance-changed"

  # mw-gq6.1378: a Builder's closing comment says an AC is not met, the Mayor
  # ran it himself and it passed. The acceptance did not change, so nothing is
  # re-scoped: --met-by-mayor answers the comment on the bead.
  Scenario: A story whose Builder said an AC was not met lands on the Mayor's word that he ran it and it passed
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works at load 20.
      2. go test ./... passes.
      """
    And a formula was poured for "mw-gq6.1" and every step, the closing-comment step too, is closed
    And the comment "AC 1 NOT met: the desktop could not reach load 20. Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit
    And mw next is run with --met-by-mayor "ran AC 1 on the laptop at load 23: 1.68 s"
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the story "mw-gq6.1" carries exactly 1 comment quoting: MET-BY-MAYOR:
    And the story "mw-gq6.1" carries a comment quoting: ran AC 1 on the laptop at load 23: 1.68 s
    And the story "mw-gq6.1" carries a comment quoting: AC 1
    And the story "mw-gq6.1" carries a comment quoting: 2026-09-18T12:00:00Z

  Scenario: A story whose Builder said an AC was not met is still refused without the Mayor's word
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works at load 20.
      """
    And a formula was poured for "mw-gq6.1" and every step, the closing-comment step too, is closed
    And the comment "AC 1 NOT met: the desktop could not reach load 20. Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "unmet-criterion"

  Scenario: A blank --met-by-mayor is refused and asks for the run
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works at load 20.
      """
    And a formula was poured for "mw-gq6.1" and every step, the closing-comment step too, is closed
    And the comment "AC 1 NOT met: the desktop could not reach load 20. Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit
    And mw next is run with --met-by-mayor ""
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the story "mw-gq6.1" is not closed
    And the close-out failed saying: --met-by-mayor needs what the Mayor ran and what it printed
    And the story "mw-gq6.1" carries no comment quoting: MET-BY-MAYOR:

  Scenario: A Builder's new closing comment after the Mayor's word is not answered by it
    Given the story "mw-gq6.1" was claimed with the acceptance:
      """
      1. The first thing works at load 20.
      """
    And a formula was poured for "mw-gq6.1" and every step, the closing-comment step too, is closed
    And the comment "AC 1 NOT met: the desktop could not reach load 20. Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit
    And the comment "MET-BY-MAYOR: AC 1: ran it, 1.68 s" was written on "mw-gq6.1" after its newest commit
    And the comment "AC 1 NOT met again. Internal: nothing for the Governor to look at. For the rig memory: nothing" was written on "mw-gq6.1" after its newest commit
    When mw closes out "mw-gq6.1"
    Then nothing was landed on "main"
    And the report says it stopped for "unmet-criterion"
