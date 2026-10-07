Feature: mw grist grind grinds several grists at once, up to [grist] concurrency
  The mill has one grind slot for each grist it may grind at once (config
  [grist] concurrency, default 2), each a lock file of its own. A pass grinds
  the oldest grists first, as many at once as there are slots free, and a grist
  past them waits for a grind of the pass to end, so a second child's turn
  never waits on the first. Every slot held is one of the host's sessions: the
  cap counts them, and mw dispatch counts them. The cursor moves only past
  grists whose grind has ended. With one slot the mill grinds as it always has
  (features/grist.feature).

  Background:
    Given a mill on the host "laptop" with a cap of 4
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And each grist carries a photo of its own
    And the grind answers with a sweep result
    And the mill keeps its runs under its state directory
    And the mill's clock moves a second at each look

  Scenario: Two grists grind at once and the third waits for a slot
    Given the mill grinds up to 2 grists at once
    And the grinds are held until 2 of them are running at once
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 3, refused 0, failed 0, and left 0 waiting
    And 3 grinds were run
    And at most 2 grinds ran at once
    And 3 answers were delivered
    And the mill's cursor is past all 3 grists
    When mw grist runs lists the runs
    Then the first two runs overlap in their timing

  Scenario: With one slot the grists grind one at a time, as ever
    Given the mill grinds up to 1 grists at once
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 2, refused 0, failed 0, and left 0 waiting
    And at most 1 grind ran at once

  Scenario: The grinds a pass runs count against the host's cap with the stories running
    Given the mill grinds up to 4 grists at once
    And a story is already running on "laptop"
    And the grinds are held until 3 of them are running at once
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    When the mill grinds
    Then the mill answered 4, refused 0, failed 0, and left 0 waiting
    And at most 3 grinds ran at once

  Scenario: mw dispatch counts every running grind as one of its sessions
    Given the mill grinds up to 2 grists at once
    And 2 grinds are running on "laptop"
    And a story is ready on "laptop"
    When mw dispatch runs on "laptop" with a cap of 2
    Then dispatch started nothing, since "laptop has taken 2 of the 2 sessions it may run at once"
    And dispatch says 2 grist grinds hold 2 of its sessions

  Scenario: mw dispatch counts only the slots that are held
    Given the mill grinds up to 2 grists at once
    And 1 grind is running on "laptop"
    And a story is ready on "laptop"
    When mw dispatch runs on "laptop" with a cap of 1
    Then dispatch started nothing, since "laptop has taken 1 of the 1 sessions it may run at once"
    And dispatch says a grist grind holds one of its sessions
