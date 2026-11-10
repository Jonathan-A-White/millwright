Feature: A landing whose closing comment lists what the Builder found and did not fix
  A Builder does not file beads: it lists what it saw and left alone in a
  section of its closing comment headed 'Found, not fixed:', one finding to a
  line. mw next files each line as a held bug under the story's epic, in the
  Builder's own words, so the Mayor decides on it without re-typing it. The
  landing stands whatever becomes of the bugs.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the session of "mw-gq6.1" reported a plain success

  Scenario: Two lines are two held bugs under the story's epic, each quoting its line
    Given the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\n- The parser drops a trailing line (application/parse.go:12)\n2. The loader reads the file twice (application/load.go:40)\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And exactly 2 bugs were filed under "mw-gq6"
    And the bugs filed under "mw-gq6" are held and labelled "bug" and "found-at-close"
    And a bug filed under "mw-gq6" quotes: The parser drops a trailing line (application/parse.go:12)
    And a bug filed under "mw-gq6" quotes: The loader reads the file twice (application/load.go:40)
    And the bugs filed under "mw-gq6" all quote: Seen by: mw-gq6.1
    And the bugs filed under "mw-gq6" have the path of "mw-gq6.1" and no acceptance
    And that mail's subject is "Landed: The story mw-gq6.1"
    And that mail's body says: Filed: mw-gq6.2, held.
    And that mail's body says: Filed: mw-gq6.3, held.

  Scenario: The word nothing files no bug
    Given the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed: nothing\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And exactly 0 bugs were filed under "mw-gq6"

  Scenario: The word nothing on a line of its own files no bug
    Given the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\nnothing\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then exactly 0 bugs were filed under "mw-gq6"

  Scenario: A comment with no such section files no bug
    Given the story "mw-gq6.1" carries the closing comment "Done. Everything works.\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And exactly 0 bugs were filed under "mw-gq6"

  Scenario: A heading named inside a sentence or between quote marks files no bug
    Given the story "mw-gq6.1" carries the closing comment "The formula asks for a section headed 'Found, not fixed:' and I wrote one.\nThe step says `Found, not fixed:` and then a line each.\n'Found, not fixed:' is what it is called\nThe parser dropped a line.\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And exactly 0 bugs were filed under "mw-gq6"

  Scenario: The newest closing comment is the one read
    Given the story "mw-gq6.1" carries the closing comment "Found, not fixed:\nAn old finding (application/old.go:1)\nFor the rig memory: nothing"
    And the story "mw-gq6.1" carries the closing comment "Done again.\nFound, not fixed: nothing\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then exactly 0 bugs were filed under "mw-gq6"

  Scenario: Closing out the same landing twice files nothing new
    Given the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\nThe parser drops a trailing line (application/parse.go:12)\nThe loader reads the file twice (application/load.go:40)\nFor the rig memory: nothing"
    And the tracker cannot record the run state, saying: the database is locked
    And the tracker refuses to close "mw-gq6.1", saying: assignee is root, actor is mw@vps; reclaim or use --force
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is not closed
    And exactly 2 bugs were filed under "mw-gq6"
    Given the tracker can record the run state again
    And the tracker will take a close of "mw-gq6.1" again
    When mw closes out "mw-gq6.1" a second time
    Then the story "mw-gq6.1" is closed
    And exactly 2 bugs were filed under "mw-gq6"

  Scenario: Seven lines file five bugs and one that lists the other two
    Given the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\nFinding one (a.go:1)\nFinding two (a.go:2)\nFinding three (a.go:3)\nFinding four (a.go:4)\nFinding five (a.go:5)\nFinding six (a.go:6)\nFinding seven (a.go:7)\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And exactly 6 bugs were filed under "mw-gq6"
    And a bug filed under "mw-gq6" quotes: Finding five (a.go:5)
    And one bug filed under "mw-gq6" quotes both: Finding six (a.go:6) | Finding seven (a.go:7)

  Scenario: The same fault named by a second story, worded differently, is not filed again
    Given the story "mw-gq6.5" has been worked in its own worktree
    And the session of "mw-gq6.5" reported a plain success
    And the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\ngofmt -l lists cmd/mw/member.go (cmd/mw/member.go:1)\nFor the rig memory: nothing"
    And the story "mw-gq6.5" carries the closing comment "Done.\nFound, not fixed:\nA formatting fault: cmd/mw/member.go is not gofmt-clean\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then exactly 1 bugs were filed under "mw-gq6"
    When mw closes out "mw-gq6.5"
    Then the story "mw-gq6.5" is closed
    And exactly 1 bugs were filed under "mw-gq6"
    And the mail "Landed: The story mw-gq6.5" says: Already filed: mw-gq6.3.

  Scenario: A finding that names a file takes that file as its files key
    Given the finder "mw-gq6.1" expects to change the files "cmd/mw/afterlanding.go"
    And the story "mw-gq6.1" carries the closing comment "Done.\nFound, not fixed:\nThe parser drops a trailing line (application/parse.go:12)\nThe loader reads the file twice (application/load.go:40) and application/load_test.go:3 does not cover it\nNobody has named a file here\nFor the rig memory: nothing"
    When mw closes out "mw-gq6.1"
    Then the bug filed under "mw-gq6" quoting "The parser drops" has the files "application/parse.go"
    And the bug filed under "mw-gq6" quoting "The loader reads" has the files "application/load.go,application/load_test.go"
    And the bug filed under "mw-gq6" quoting "Nobody has named" has the files "cmd/mw/afterlanding.go"
