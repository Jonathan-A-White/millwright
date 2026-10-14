Feature: The two hosts take turns at a rig's merge slot
  A close-out lands under its rig's merge slot, and each host has its own.
  Until mw-gq6.359 a race between the two hosts was left to the remote, and a
  host whose merged-result tests take longer than the other host takes to land
  a whole story never landed: each push was refused, each try ran the tests
  again, the other host landed again meanwhile, and the story stopped push-lost
  (mw-y3qno5.3: the desktop's Lampas gate is 27 minutes, and the Laptop lands
  Lampas every ten). So a rig's merge slot spans hosts as well: a lease on the
  beads server both hosts share, taken inside this host's own slot, waited for
  while the other host holds it, taken over once it has run out, and skipped,
  saying so, when the server cannot be reached.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the session of "mw-gq6.1" reported a plain success

  Scenario: A host whose merged-result tests outlast the other host's landings waits its turn and lands
    Given the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the other host "laptop" lands a story of its own each time this host's merged-result tests run, before they finish
    And "laptop" holds the merge slot of "millwright" across hosts, landing a story of its own
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And mw pushed once, losing no push to the other host
    And "laptop" landed 2 stories of its own on "main" meanwhile
    And the printed report says it waited for laptop's landing of millwright
    And the story "mw-gq6.1" is closed
    And nobody holds the merge slot of "millwright" across hosts

  Scenario: A lease whose holder went away is taken over, naming the holder and its age
    Given "builder@laptop closing out mw-old.9" took the merge slot of "millwright" across hosts on "laptop" 47 minutes ago, and its lease ran out 37 minutes ago
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the printed report says "took over the cross-host merge slot of millwright from builder@laptop closing out mw-old.9: its lease was taken 47m ago and ran out 37m ago"
    And the story "mw-gq6.1" is closed
    And nobody holds the merge slot of "millwright" across hosts

  Scenario: The landing goes ahead under this host's slot alone when the beads server cannot be reached
    Given the beads server cannot be reached for the merge slot across hosts, saying "dial tcp 127.0.0.1:3307: connect: connection refused"
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the printed report says "the cross-host merge slot of millwright was skipped, so this landing went ahead under this host's merge slot alone: the beads server could not be reached: dial tcp 127.0.0.1:3307: connect: connection refused"
    And the story "mw-gq6.1" is closed
