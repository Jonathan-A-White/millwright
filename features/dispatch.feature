Feature: Dispatching the stories this host is ready to work
  mw dispatch is how a story becomes a running session. It brings this host
  level with the other one, asks the work tracker what is ready here, and for
  each story it may take: claims it, cuts a worktree of its rig on a branch of
  its own from the freshly fetched target branch, pours its formula into step
  beads, assembles the boot, and starts the session through the runner. What it
  started is recorded on the story. A dispatch that fails after claiming gives
  the claim back, so that no story is left held by a session that never ran.

  Background:
    Given a vault holding the charter of the "builder" seat
    And the rig "millwright" is checked out here from its origin
    And the formula "tdd-feature" is installed
    And an epic "mw-gq6" whose stories are planned with the path:
      | rig     | millwright  |
      | branch  | main        |
      | harness | claude      |
      | model   | opus        |
      | effort  | high        |
      | formula | tdd-feature |
      | host    | vps         |

  Scenario: One ready story starts one session in its own worktree
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the worktree of "mw-gq6.1" is a checkout of the rig on branch "mw/mw-gq6.1"
    And the session for "mw-gq6.1" runs in the worktree of "mw-gq6.1"
    And the story "mw-gq6.1" is claimed by this host
    And the story "mw-gq6.1" is recorded as running

  Scenario: The worktree is cut from the freshly fetched target branch
    Given a ready story "mw-gq6.1" of that epic
    And the other host has pushed a later commit to the rig's origin
    When dispatch runs on "vps" with a cap of 1
    Then the worktree of "mw-gq6.1" holds the later commit

  Scenario: The hosts are brought level before anything is asked for or claimed
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then the work tracker was asked, in this order:
      | Sync           |
      | RunningStories |
      | ReadyForHost   |
      | ClaimStory     |

  Scenario: A sync that halts stops the dispatch before anything is claimed
    Given a ready story "mw-gq6.1" of that epic
    And the beads sync halts with exit code 2
    When dispatch runs on "vps" with a cap of 1
    Then dispatch failed, saying: merge conflict
    And no session was started
    And the story "mw-gq6.1" is not claimed

  Scenario: The concurrency cap is respected
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.2" is not claimed

  Scenario: The more urgent story is started first, however old the other is
    Given a ready story "mw-gq6.1" of that epic at priority 3, filed at "2026-09-01T09:00:00Z"
    And a ready story "mw-gq6.2" of that epic at priority 1, filed at "2026-09-02T09:00:00Z"
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.2"
    And the story "mw-gq6.1" is not claimed

  Scenario: Of two stories as urgent as each other the older is started first
    Given a ready story "mw-gq6.1" of that epic at priority 2, filed at "2026-09-02T09:00:00Z"
    And a ready story "mw-gq6.2" of that epic at priority 2, filed at "2026-09-01T09:00:00Z"
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.2"
    And the story "mw-gq6.1" is not claimed

  Scenario: A story with no creation time is started after those that have one
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic at priority 2, filed at "2026-09-01T09:00:00Z"
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.2"

  Scenario: A story already running here counts against the cap
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is not claimed

  Scenario: A story for the other host is left alone even when this host is idle
    Given a ready story "mw-gq6.2" of that epic that overrides "host" with "laptop"
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.2" is not claimed

  Scenario: A story whose path names no host is not dispatchable
    Given an epic "mw-abc" whose stories are planned with the path:
      | rig     | millwright |
      | branch  | main       |
      | harness | claude     |
      | model   | opus       |
      | effort  | high       |
    And a ready story "mw-abc.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-abc.1" is not claimed

  Scenario: A story the Governor must be present for is passed over
    Given a ready story "mw-gq6.1" of that epic labelled "hitl"
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: the Governor must be present for it

  Scenario: A dry run passes it over too
    Given a ready story "mw-gq6.1" of that epic labelled "hitl"
    When dispatch runs on "vps" with a cap of 1 as a dry run
    Then no session was started
    And dispatch passed over "mw-gq6.1", saying: the Governor must be present for it

  Scenario: A story the Governor must be present for does not use up the cap
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic labelled "hitl" is already running here
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"

  Scenario: A story the Governor must be present for does not count as a session running here
    Given a story "mw-gq6.9" of that epic labelled "hitl" is already running here
    When dispatch runs on "vps" with a cap of 1 as a dry run
    Then the dry run report says 0 of 1 sessions were already running

  Scenario: A failure after claiming releases the claim
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And the story "mw-gq6.1" carries a comment saying the dispatch failed
    And there is no worktree for "mw-gq6.1"

  Scenario: A story whose earlier session is lying dead is dispatched again
    Given a ready story "mw-gq6.1" of that epic
    And an earlier session for "mw-gq6.1" lies dead
    When dispatch runs on "vps" with a cap of 1
    Then the dead session for "mw-gq6.1" was closed
    And one session was started, for "mw-gq6.1"
    And the session for "mw-gq6.1" is running
    And the session for "mw-gq6.1" runs in the worktree of "mw-gq6.1"
    And the worktree of "mw-gq6.1" is a checkout of the rig on branch "mw/mw-gq6.1"
    And the story "mw-gq6.1" is claimed by this host

  Scenario: A story whose session is still running is not started twice
    Given a ready story "mw-gq6.1" of that epic
    And a session for "mw-gq6.1" is still running in its worktree
    When dispatch runs on "vps" with a cap of 1
    Then dispatch failed, saying: is still running
    And nothing was closed
    And the session for "mw-gq6.1" is still running in the worktree it began in
    And the worktree of "mw-gq6.1" is a checkout of the rig on branch "mw/mw-gq6.1"
    And the story "mw-gq6.1" is not claimed

  Scenario: The story's formula is poured and its step beads reach the boot file
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then the formula "tdd-feature" was poured for "mw-gq6.1"
    And the boot file of "mw-gq6.1" holds every poured step, in order

  Scenario: A story whose recorded molecule is still open is dispatched without a second pour
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has the formula "tdd-feature" poured and recorded, with its first step closed
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And nothing more was poured
    And the story "mw-gq6.1" still records its first molecule
    And the boot file of "mw-gq6.1" holds only the steps still open, in order

  Scenario: A story whose recorded molecule is closed gets a fresh pour
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has the formula "tdd-feature" poured and recorded, with its molecule closed
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the formula "tdd-feature" was poured again for "mw-gq6.1"
    And the story "mw-gq6.1" records the molecule it was poured as
    And the boot file of "mw-gq6.1" holds every poured step, in order

  Scenario: A story whose recorded molecule is missing gets a fresh pour
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" records a molecule that does not exist
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the formula "tdd-feature" was poured for "mw-gq6.1"
    And the story "mw-gq6.1" records the molecule it was poured as
    And the boot file of "mw-gq6.1" holds every poured step, in order

  Scenario: A story whose recorded molecule has no step left open gets a fresh pour
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has the formula "tdd-feature" poured and recorded, with every step closed
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the formula "tdd-feature" was poured again for "mw-gq6.1"
    And the boot file of "mw-gq6.1" holds every poured step, in order

  Scenario: A dry run starts nothing
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1 as a dry run
    Then dispatch would start "mw-gq6.1"
    And no session was started
    And the story "mw-gq6.1" is not claimed
    And there is no worktree for "mw-gq6.1"
    And nothing was poured
    And there is no boot file for "mw-gq6.1"

  Scenario: A dry run lists the stories in the order they would be started
    Given a ready story "mw-gq6.1" of that epic at priority 3, filed at "2026-09-01T09:00:00Z"
    And a ready story "mw-gq6.2" of that epic at priority 2, filed at "2026-09-03T09:00:00Z"
    And a ready story "mw-gq6.3" of that epic at priority 2, filed at "2026-09-02T09:00:00Z"
    And a ready story "mw-gq6.4" of that epic at priority 1, filed at "2026-09-04T09:00:00Z"
    When dispatch runs on "vps" with a cap of 4 as a dry run
    Then dispatch would start, in this order:
      | mw-gq6.4 |
      | mw-gq6.3 |
      | mw-gq6.2 |
      | mw-gq6.1 |
    And the dry run report lists them in that order
