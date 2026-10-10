Feature: A one-week Tester trial
  While the [tester] table names a rig and its until has not passed, every
  landing on that rig whose closing comment tells the Governor HOW TO CHECK IT
  springs a Tester story: a fresh session that drives the landed app on a
  phone-sized screen by those steps and by adversarial moves, commits nothing,
  and writes its FINDINGS on the landed story. mw next closes a Tester story
  without merging or raising anything, and tells the Mayor how many findings
  there were. mw tester report sums the trial.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "lampas" checked out from a bare origin of its own
    And a plan "mw-l" whose stories are worked on "vps" and target "main"

  Scenario: A lampas landing with HOW TO CHECK IT before until springs one Tester story
    Given a Tester trial on "lampas" until "2026-09-25T12:00:00Z", on "sonnet" at "high"
    And the story "mw-l.1" has been worked in its own worktree
    And the session of "mw-l.1" reported a plain success
    And the story "mw-l.1" carries the closing comment "Done and green.\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n2. Tap Needs you: the list opens.\nFor the rig memory: nothing"
    When mw closes out "mw-l.1"
    Then the work of "mw-l.1" is on "main" at the rig's origin
    And the story "mw-l.1" is closed
    And one Tester story "Test: The story mw-l.1" was filed under "mw-l", held and then let go
    And that Tester story is labelled "tester" and pathed to "lampas", "main", "vps", "sonnet", "high", formula "tester"
    And that Tester story's description names "mw-l.1" and quotes "2. Tap Needs you: the list opens."
    And the close-out report names that Tester story

  Scenario: The Tester story is a need of the epic's open demo, so the demo stays last
    Given a Tester trial on "lampas" until "2026-09-25T12:00:00Z", on "sonnet" at "high"
    And the rig names "demo" as the label of an epic's last story
    And the story "mw-l.2" is planned and ready to be worked here
    And the story "mw-l.2" is labelled "demo" for the trial
    And the story "mw-l.1" has been worked in its own worktree
    And the session of "mw-l.1" reported a plain success
    And the story "mw-l.1" carries the closing comment "Done and green.\nHOW TO CHECK IT, for the Governor:\n1. Open the app."
    When mw closes out "mw-l.1"
    Then one Tester story "Test: The story mw-l.1" was filed under "mw-l", held and then let go
    And the story "mw-l.2" waits on that Tester story

  Scenario Outline: No Tester story is sprung outside the trial or without HOW TO CHECK IT
    Given a Tester trial on "<rigs>" until "<until>", on "sonnet" at "high"
    And the story "mw-l.1" has been worked in its own worktree
    And the session of "mw-l.1" reported a plain success
    And the story "mw-l.1" carries the closing comment "<comment>"
    When mw closes out "mw-l.1"
    Then the work of "mw-l.1" is on "main" at the rig's origin
    And no Tester story was filed

    Examples:
      | rigs   | until                | comment                                                                   |
      | lampas | 2026-09-18T11:00:00Z | Done.\nHOW TO CHECK IT, for the Governor:\n1. Open the app.               |
      | orrery | 2026-09-25T12:00:00Z | Done.\nHOW TO CHECK IT, for the Governor:\n1. Open the app.               |
      | lampas | 2026-09-25T12:00:00Z | Done.\nInternal: nothing for the Governor to look at\nFor the rig memory: nothing |
      | lampas | 2026-09-25T12:00:00Z | Done and green, and nothing more to say.                                  |

  Scenario: A demo that lands springs no Tester story
    Given a Tester trial on "lampas" until "2026-09-25T12:00:00Z", on "sonnet" at "high"
    And the story "mw-l.1" has been worked in its own worktree
    And the story "mw-l.1" is labelled "demo" for the trial
    And the session of "mw-l.1" reported a plain success
    And the story "mw-l.1" carries the closing comment "Done.\nHOW TO CHECK IT, for the Governor:\n1. Open the app."
    When mw closes out "mw-l.1"
    Then the work of "mw-l.1" is on "main" at the rig's origin
    And no Tester story was filed

  Scenario: A Tester story is closed with nothing merged, its FINDINGS on the landed story, and the Mayor told how many
    Given a Tester trial on "lampas" until "2026-09-25T12:00:00Z", on "sonnet" at "high"
    And the rig's main holds a package.json and a package-lock.json at version "0.1.0"
    And the rig's file in the vault names the version files "package.json" and "package-lock.json"
    And the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    And the session of "mw-l.2" reported a plain success
    And the Tester of "mw-l.2" commented on "mw-l.1": "FINDINGS\n- [bug] The mic button stays red after Back.\n  Steps: hold the mic, tap Back. Shot: /tmp/tester-mw-l.2/04-back.png\n- [taste] The list jumps on rotate.\n  Steps: rotate. Shot: /tmp/tester-mw-l.2/06-rotate.png\nTester fuel: 55,100 tokens"
    And the Tester of "mw-l.2" commented on "mw-l.2": "Done.\nHOW TO CHECK IT, for the Governor:\n1. Open the app."
    When mw closes out "mw-l.2"
    Then nothing was landed on "main"
    And "main" at the rig's origin has exactly 0 commits saying "Version 0.1.1 (mw-l.2)"
    And the story "mw-l.2" is closed
    And nothing is left of the worktree of "mw-l.2"
    And the story "mw-l.1" carries a comment quoting: [bug] The mic button stays red after Back.
    And exactly one mail was sent, to "mayor" from "mw@vps"
    And that mail's subject is "Tested: mw-l.1: 2 findings"
    And the last ledger line holds:
      | mw-l.2                    |
      | tested mw-l.1: 2 findings |
    And no Tester story was filed

  Scenario: FINDINGS a Tester left on its own story are carried to the landed story
    Given the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    And the session of "mw-l.2" reported a plain success
    And the Tester of "mw-l.2" commented on "mw-l.2": "FINDINGS: none\nTester fuel: 55,100 tokens"
    When mw closes out "mw-l.2"
    Then the story "mw-l.2" is closed
    And the story "mw-l.1" carries a comment quoting: FINDINGS: none
    And that mail's subject is "Tested: mw-l.1: 0 findings"

  Scenario: A Tester story whose close failed is closed by the next run, with one ledger line and one mail
    Given the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    And the session of "mw-l.2" reported a plain success
    And the Tester of "mw-l.2" commented on "mw-l.1": "FINDINGS: none\nTester fuel: 55,100 tokens"
    And the tracker refuses to close "mw-l.2", saying: database is locked
    When mw closes out "mw-l.2"
    Then the story "mw-l.2" is not closed
    And nothing is left of the worktree of "mw-l.2"
    Given the tracker will take a close of "mw-l.2" again
    When mw closes out "mw-l.2" a second time
    Then the story "mw-l.2" is closed
    And the ledger holds exactly one line for "mw-l.2"
    And exactly one mail was sent, to "mayor" from "mw@vps"

  Scenario: A Tester story that committed something is refused, and nothing is merged
    Given the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    And the Tester of "mw-l.2" committed a script to its branch
    And the session of "mw-l.2" reported a plain success
    And the Tester of "mw-l.2" commented on "mw-l.1": "FINDINGS: none\nTester fuel: 55,100 tokens"
    When mw closes out "mw-l.2"
    Then nothing was landed on "main"
    And the story "mw-l.2" is not closed
    And the report says it stopped for "tester-committed"

  Scenario: A Tester story with no FINDINGS anywhere is refused
    Given the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    And the session of "mw-l.2" reported a plain success
    When mw closes out "mw-l.2"
    Then the story "mw-l.2" is not closed
    And the report says it stopped for "no-findings"

  Scenario: mw tester report sums the trial per rig
    Given a Tester trial on "lampas" until "2026-09-25T12:00:00Z", on "sonnet" at "high"
    And the trial saw these landings on "lampas": "mw-l.1", "mw-l.2", "mw-l.3"
    And the Tester story "mw-l.4" tested "mw-l.1" and found "FINDINGS\n- [bug] The mic stays red.\n- [taste] The list jumps.\nTester fuel: 55,100 tokens"
    And the Tester story "mw-l.5" tested "mw-l.2" and found "FINDINGS: none\nTester fuel: 55,100 tokens"
    And a bug story "mw-l.6" was filed naming "Tester finding on mw-l.1: the mic stays red"
    When mw tester report runs since "2026-09-17"
    Then the tester report says for "lampas":
      | landings in the trial | 3                    |
      | Tester runs           | 2                    |
      | findings              | 2 (1 bug, 1 taste)   |
      | [bug] from a finding  | 1                    |
    And the tester report gives "mw-l.4" its fuel "55,100 tokens"
    And the tester report gives "mw-l.5" its fuel "55,100 tokens"

  Scenario: mw check passes a Tester story with no commits and its FINDINGS written, and fails one without
    Given the story "mw-l.1" is planned and ready to be worked here
    And the Tester story "mw-l.2" testing "mw-l.1" has been worked in its own worktree, committing nothing
    When a session checks "mw-l.2"
    Then the check fails
    And the check printed: the Tester left no FINDINGS on mw-l.1, nor on mw-l.2
    Given the Tester of "mw-l.2" commented on "mw-l.1": "FINDINGS: none\nTester fuel: unknown"
    When a session checks "mw-l.2"
    Then the check passes
