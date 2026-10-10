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

  Scenario: A sync that cannot resolve a name is tried again, and the dispatch goes on once it can
    Given a ready story "mw-gq6.1" of that epic
    And the sync cannot resolve a name for its first 2 tries
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the sync was tried 3 times, waiting 15s between the tries
    And the dispatch report says the sync was retried 2 times

  Scenario: A sync that never resolves a name is a local network fault, not a failed run
    Given a ready story "mw-gq6.1" of that epic
    And the sync can never resolve a name
    When dispatch runs on "vps" with a cap of 1
    Then dispatch printed the one line "local network fault: ssh: Could not resolve hostname github.com: Temporary failure in name resolution; nothing dispatched"
    And no session was started
    And the story "mw-gq6.1" is not claimed
    And the sync was tried 3 times, waiting 15s between the tries
    And dispatch leaves with status 7

  Scenario: Any other sync failure is not tried again and fails as it always did
    Given a ready story "mw-gq6.1" of that epic
    And the vault's pull is refused with "git@github.com: Permission denied (publickey)."
    When dispatch runs on "vps" with a cap of 1
    Then dispatch failed, saying: Permission denied (publickey)
    And no session was started
    And the story "mw-gq6.1" is not claimed
    And the sync was tried 1 time, waiting nothing
    And dispatch leaves with status 1

  Scenario: A halted beads sync is not tried again either
    Given a ready story "mw-gq6.1" of that epic
    And the beads sync halts with exit code 2
    When dispatch runs on "vps" with a cap of 1
    Then the sync was tried 1 time, waiting nothing
    And dispatch leaves with status 2

  Scenario: How many times the sync is tried, and how long dispatch waits, come from the config
    Given the config file says dispatch_sync_tries is 2 and dispatch_sync_wait is "7s"
    And a ready story "mw-gq6.1" of that epic
    And the sync can never resolve a name
    When dispatch runs on "vps" with a cap of 1
    Then the sync was tried 2 times, waiting 7s between the tries
    And dispatch leaves with status 7

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

  # mw-y0dkzp: a session that died with no close-out is not a try the story
  # failed, so the reclaim gives the attempt back (it never goes below 0).

  Scenario: A dead pane with an expired lease is reclaimed, its attempt is refunded, and it is started again
    Given a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.9" has been tried 1 time
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.9" records 1 attempt
    And dispatch reclaimed "mw-gq6.9" for a dead pane with an expired lease
    And the reclaim comment on "mw-gq6.9" says the attempt was refunded

  Scenario: A story tried max_attempts times whose pane is dead is reclaimed and started, not exhausted
    Given a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.9" has been tried 3 times
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.9" records 3 attempts
    And dispatch reclaimed "mw-gq6.9" for a dead pane with an expired lease
    And the story "mw-gq6.9" carries no comment saying it used up its attempts

  Scenario: A story never tried that is reclaimed for a dead pane is not refunded below none
    Given a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.9" has been tried 0 times
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.9" records 1 attempt

  # mw-gq6.344: the refund is given twice per story, so a session that dies
  # every time still reaches max_attempts.

  Scenario: A story whose pane is dead on three reclaims in a row is refunded twice and not the third time
    Given a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.9" has been tried 1 time
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    And the session of "mw-gq6.9" dies again with its lease expired
    And dispatch runs on "vps" with a cap of 1
    And the session of "mw-gq6.9" dies again with its lease expired
    And dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And dispatch reclaimed "mw-gq6.9" for a dead pane with an expired lease
    And the story "mw-gq6.9" records 2 attempts
    And the story "mw-gq6.9" records 2 dead-pane refunds
    And the latest reclaim comment on "mw-gq6.9" says it was not refunded: 2 dead-pane refunds already

  # mw-gq6.182: a story its close-out refused is evidence, not a dead session.
  # The claim, the worktree and the branch stay as the session left them until
  # a person acts (mw retry, a hold, a give-back by hand).

  Scenario: A story its close-out refused is not reclaimed for its dead pane
    Given a story "mw-gq6.9" of that epic was dispatched and left its worktree with 2 commits
    And the story "mw-gq6.9" has been tried 2 times
    And the close-out of "mw-gq6.9" refused it
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then dispatch started nothing, leaving the dead window of "mw-gq6.9" as it was
    And nothing was closed
    And the story "mw-gq6.9" is claimed by this host
    And the story "mw-gq6.9" records 2 attempts
    And the worktree of "mw-gq6.9" is a checkout of the rig on branch "mw/mw-gq6.9"
    And the branch of "mw-gq6.9" still has its 2 commits
    And dispatch said once that "mw-gq6.9" is refused and waits for the Mayor
    And dispatch leaves with status 0

  Scenario: A crashed session with no refusal recorded is still reclaimed and started again
    Given a story "mw-gq6.9" of that epic was dispatched and left its worktree with 1 commit
    And the story "mw-gq6.9" has been tried 1 time
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.9" records 1 attempt
    And dispatch reclaimed "mw-gq6.9" for a dead pane with an expired lease

  # mw-gq6.107: a dead-pane reclaim gives the claim back but leaves the
  # worktree and branch an earlier attempt cut exactly as they were, so the
  # fresh attempt below finds them in the way of its own cut.

  Scenario: A leftover worktree and branch from a reclaimed dead pane are kept under their attempt before the fresh cut
    Given a story "mw-gq6.9" of that epic was dispatched and left its worktree with 2 commits
    And the story "mw-gq6.9" has been tried 1 time
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.9" records 1 attempt
    And the worktree of "mw-gq6.9" is a checkout of the rig on branch "mw/mw-gq6.9"
    And the earlier branch of "mw-gq6.9" was kept as "mw/mw-gq6.9-attempt1" with its 2 commits
    And the dispatch line for "mw-gq6.9" names the kept branch and the attempt

  # mw-gq6.326: a dirty leftover is never touched; the story fails with the
  # worktree named (and is mailed as stuck on its second identical failure),
  # and the other stories ready this tick are still worked.

  Scenario: A leftover worktree with uncommitted work is left as it was, and named
    Given a story "mw-gq6.9" of that epic was dispatched and left its worktree with 1 commit
    And the worktree of "mw-gq6.9" holds uncommitted work
    And the story "mw-gq6.9" has been tried 1 time
    And the session of "mw-gq6.9" has a dead pane and its lease has expired
    And a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 2
    Then dispatch failed, saying: holds uncommitted work
    And a session was started for "mw-gq6.1" all the same
    And the branch of "mw-gq6.9" still has its 1 commit
    And the worktree of "mw-gq6.9" still holds its uncommitted work
    And the story "mw-gq6.9" is not claimed

  Scenario: A dead pane whose lease has not expired still counts against the cap
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    And the session of "mw-gq6.9" has a dead pane but its lease has not expired
    When dispatch runs on "vps" with a cap of 1
    Then dispatch reports 1 of 1 sessions were already running
    And the story "mw-gq6.1" is not claimed

  Scenario: A story for the other host is left alone even when this host is idle
    Given a ready story "mw-gq6.2" of that epic that overrides "host" with "laptop"
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.2" is not claimed

  # host=auto: it does not matter which host works the story, so whichever is
  # under its cap and its load takes it. The claim is what writes the host.

  Scenario: A story that may run on any host is taken here when this host is under its cap and its load
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 3.0 of 16 cores
    When dispatch runs on "vps" with a cap of 4
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" is claimed by this host

  Scenario: A story that may run on any host is passed over while this host's load has reached its cores
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 16.5 of 16 cores
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: vps has no room: load 16.5 of 16 cores

  Scenario: A story that may run on any host is still passed over when this host is at its cap
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And a story "mw-gq6.9" of that epic is already running here
    And this host is at load 3.0 of 16 cores
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: vps has taken 1 of the 1 sessions it may run at once

  # Room (mw-t0z3fu.1): a host takes on a story only while its load is under its
  # core count and its available memory over a floor, for a story that names the
  # host as much as for host=auto. The cap stays the ceiling. A story held back
  # for want of room stays ready and unclaimed.

  Scenario: A story that names this host is not started while the load has reached its cores
    Given a ready story "mw-gq6.1" of that epic
    And this host is at load 16.5 of 16 cores
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: vps has no room: load 16.5 of 16 cores

  Scenario: A story is not started while the available memory is under the floor
    Given a ready story "mw-gq6.1" of that epic
    And this host is at load 3.0 of 16 cores
    And this host has 1500 MB of memory available
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: vps has no room: 1500 MB free, under 2048 MB

  Scenario: A story is started when the load and the memory both have room and the host is under its cap
    Given a ready story "mw-gq6.1" of that epic
    And this host is at load 3.0 of 16 cores
    And this host has 8000 MB of memory available
    When dispatch runs on "vps" with a cap of 4
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" is claimed by this host

  Scenario: A host with room still starts no more than its cap
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    And a ready story "mw-gq6.3" of that epic
    And this host is at load 0.5 of 16 cores
    And this host has 64000 MB of memory available
    When dispatch runs on "vps" with a cap of 2
    Then 2 sessions were started
    And dispatch passed over "mw-gq6.3", saying: vps has taken 2 of the 2 sessions it may run at once

  Scenario: A load that cannot be read does not hold a story back
    Given a ready story "mw-gq6.1" of that epic
    And this host's load cannot be read
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"

  Scenario: A memory that cannot be read does not hold a story back
    Given a ready story "mw-gq6.1" of that epic
    And this host is at load 3.0 of 16 cores
    And this host's memory cannot be read
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"

  Scenario: The cap holds a story back even when the load and the memory cannot be read
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    And this host's load cannot be read
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And dispatch passed over "mw-gq6.1", saying: vps has taken 1 of the 1 sessions it may run at once

  Scenario: The config file's room limits move where the host has no room
    Given a ready story "mw-gq6.1" of that epic
    And this host is at load 9.0 of 16 cores
    And this host has 3000 MB of memory available
    And the config file says room_load_per_core is 0.5 and room_min_free_mb is 4096
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And dispatch passed over "mw-gq6.1", saying: vps has no room: load 9.0 of 16 cores at 0.5 a core; 3000 MB free, under 4096 MB

  Scenario: A host with no room is told once when it loses its room and once when it has it back
    Given this host is at load 3.0 of 16 cores
    When dispatch runs on "vps" with a cap of 4
    Then the event log holds no room events
    When this host is then at load 20.0 of 16 cores
    And dispatch runs on "vps" with a cap of 4
    And dispatch runs on "vps" with a cap of 4
    Then the event log holds 1 room event, the last saying the host has no room: load 20.0 of 16 cores
    When this host is then at load 3.0 of 16 cores
    And dispatch runs on "vps" with a cap of 4
    And dispatch runs on "vps" with a cap of 4
    Then the event log holds 2 room events, the last saying the host has room again

  Scenario: A rehearsal writes no room event
    Given this host is at load 20.0 of 16 cores
    When dispatch runs on "vps" with a cap of 4 as a dry run
    Then the event log holds no room events

  # Grist comes first in the caps (mw-t0z3fu.4): while a tutor is being used (a
  # grind running or one answered in the last grist_recent_s) the host starts no
  # more stories than grist_cap, which is its cap less 2 unless the config file
  # says; and while tutor answers run past 1.5 x their par it starts none.
  # Stories already running are never stopped.

  Scenario: A grind in flight caps the stories this host starts at the grist cap
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    And a ready story "mw-gq6.3" of that epic
    And a tutor grind is in flight
    When dispatch runs on "vps" with a cap of 4
    Then 2 sessions were started
    And dispatch passed over "mw-gq6.3", saying: vps has taken 2 of the 2 sessions it may run at once while a tutor is in use (grist first)

  Scenario: A grind that ended minutes ago still caps the stories this host starts
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    And a ready story "mw-gq6.3" of that epic
    And 3 tutor-turn grinds ended in the last 5 minutes, 10 seconds each
    When dispatch runs on "vps" with a cap of 4
    Then 2 sessions were started

  Scenario: Grist that is not recent leaves the host's whole cap
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    And a ready story "mw-gq6.3" of that epic
    And 3 tutor-turn grinds ended 20 minutes ago, 10 seconds each
    When dispatch runs on "vps" with a cap of 4
    Then 3 sessions were started

  Scenario: A story already running counts against the grist cap and is never stopped
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.8" of that epic is already running here
    And a story "mw-gq6.9" of that epic is already running here
    And a tutor grind is in flight
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And dispatch passed over "mw-gq6.1", saying: vps has taken 2 of the 2 sessions it may run at once while a tutor is in use (grist first)

  Scenario: The config file's grist_cap moves the cap while a tutor is in use
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    And a ready story "mw-gq6.3" of that epic
    And a tutor grind is in flight
    And the config file says grist_cap is 3
    When dispatch runs on "vps" with a cap of 4
    Then 3 sessions were started

  Scenario: Tutor answers past their par start no story, and the reason is the room event's
    Given a ready story "mw-gq6.1" of that epic
    And 45 tutor-turn grinds of 10 seconds each ended long ago, then 5 of 30 seconds each ended in the last 5 minutes
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: vps has no room: slow grist: tutor-turn 30 s, par 10 s
    And the event log holds 1 room event, the last saying the host has no room: slow grist: tutor-turn 30 s, par 10 s

  Scenario: Tutor answers back near their par lift the block
    Given a ready story "mw-gq6.1" of that epic
    And 45 tutor-turn grinds of 10 seconds each ended long ago, then 5 of 30 seconds each ended in the last 5 minutes
    And 3 tutor-turn grinds of 10 seconds each then ended in the last 2 minutes
    When dispatch runs on "vps" with a cap of 4
    Then one session was started, for "mw-gq6.1"

  Scenario: A host with no grist log is not held back
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.2" of that epic
    When dispatch runs on "vps" with a cap of 2
    Then 2 sessions were started

  Scenario: A story that names another host is passed over whatever the tracker offers
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "desktop"
    And the work tracker offers dispatch every ready story, whichever host it names
    When dispatch runs on "laptop" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: it is worked on desktop

  Scenario: The claim of a story that may run on any host writes this host as its host
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 3.0 of 16 cores
    When dispatch runs on "vps" with a cap of 4
    Then the story "mw-gq6.1" is claimed by this host
    And the metadata of the story "mw-gq6.1" names the host "vps"

  Scenario: A story that may run on any host is given back as it was when its session cannot start
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 3.0 of 16 cores
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 4
    Then the story "mw-gq6.1" is not claimed
    And the metadata of the story "mw-gq6.1" names the host "auto"

  # A claim is a lease (features/claim_lease.feature: "A conditional claim fails
  # cleanly when another assignee holds the story"), so once the sync has shown
  # this host the other host's claim, the story is no longer ready here.
  Scenario: A story that may run on any host and was claimed by the other host is not taken here
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 3.0 of 16 cores
    And the story "mw-gq6.1" was claimed by the other host before the sync
    When dispatch runs on "vps" with a cap of 4
    Then no session was started
    And the story "mw-gq6.1" is still held by the other host
    And the metadata of the story "mw-gq6.1" names the host "auto"

  Scenario: A dry run names a story that may run on any host as takeable and writes nothing
    Given a ready story "mw-gq6.1" of that epic that overrides "host" with "auto"
    And this host is at load 3.0 of 16 cores
    When dispatch runs on "vps" with a cap of 4 as a dry run
    Then dispatch says it would take "mw-gq6.1" (host=auto) here
    And no session was started
    And the story "mw-gq6.1" is not claimed
    And the metadata of the story "mw-gq6.1" names the host "auto"

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

  Scenario: A story whose blocker is claimed and running is not dispatched
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.1" waits on "mw-gq6.9"
    When dispatch runs on "vps" with a cap of 2
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: waits on mw-gq6.9 (in_progress)

  Scenario: A story whose blocker is still open is not dispatched either
    Given a ready story "mw-gq6.1" of that epic
    And a ready story "mw-gq6.9" of that epic
    And the story "mw-gq6.1" waits on "mw-gq6.9"
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.9"
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: waits on mw-gq6.9 (open)

  Scenario: A dry run says the same of a story whose blocker is not finished
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.1" waits on "mw-gq6.9"
    When dispatch runs on "vps" with a cap of 2 as a dry run
    Then no session was started
    And dispatch passed over "mw-gq6.1", saying: waits on mw-gq6.9 (in_progress)

  Scenario: Once its blocker is closed the story is dispatched as before
    Given a ready story "mw-gq6.1" of that epic
    And a story "mw-gq6.9" of that epic is already running here
    And the story "mw-gq6.1" waits on "mw-gq6.9"
    And the story "mw-gq6.9" is closed
    When dispatch runs on "vps" with a cap of 2
    Then one session was started, for "mw-gq6.1"

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

  Scenario: A claim a failed start gave back leaves no run state behind
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" still carries the run state "blocked" from an earlier host
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 1
    Then the story "mw-gq6.1" is not claimed
    And the story "mw-gq6.1" carries no run state

  Scenario: A story carrying a run state from an earlier host is never shown refused when it is claimed
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" still carries the run state "blocked" from an earlier host
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" went from open to claimed to running, and was never refused

  Scenario: A story whose formula is not installed is not claimed, and told once
    Given a ready story "mw-gq6.1" of that epic that overrides "formula" with "story"
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is not claimed
    And dispatch passed over "mw-gq6.1", saying: its formula story is not installed here
    And the story "mw-gq6.1" carries exactly one comment saying its formula is not installed

  Scenario: A second dispatch does not comment again about the same uninstalled formula
    Given a ready story "mw-gq6.1" of that epic that overrides "formula" with "story"
    When dispatch runs on "vps" with a cap of 1
    And dispatch runs on "vps" with a cap of 1
    Then the story "mw-gq6.1" carries exactly one comment saying its formula is not installed

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

  Scenario: A story whose title is near bd's limit is still poured, with its title shortened in the step titles (mw-gq6.295)
    Given a ready story "mw-gq6.1" of that epic with a title 499 characters long
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the formula "tdd-feature" was poured for "mw-gq6.1"
    And no step poured for "mw-gq6.1" has a title over 500 characters
    And the story "mw-gq6.1" is claimed by this host

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

  # The log is what lets a run that failed be told from one that worked, when
  # nobody was watching: mw status counts what it holds, and only a run that
  # was really made is in it.
  Scenario: A run that starts a story adds one dated line to the dispatch log
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds exactly one line: "2026-09-21T12:00:00Z ok: 1 started"

  Scenario: A run that finds nothing ready adds one line saying so
    When dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds exactly one line: "2026-09-21T12:00:00Z ok: nothing ready"

  Scenario: A run that only passed stories over is a good run that started none
    Given a ready story "mw-gq6.1" of that epic labelled "hitl"
    When dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds exactly one line: "2026-09-21T12:00:00Z ok: 0 started"

  Scenario: A local network fault adds one line, and it is not called a failure
    Given a ready story "mw-gq6.1" of that epic
    And the sync can never resolve a name
    When dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds exactly one line: "2026-09-21T12:00:00Z local network fault"

  Scenario: A failed run still adds its line
    Given a ready story "mw-gq6.1" of that epic
    And the vault's pull is refused with "git@github.com: Permission denied (publickey)."
    When dispatch runs on "vps" with a cap of 1
    Then dispatch leaves with status 1
    And the dispatch log holds exactly one line that begins "2026-09-21T12:00:00Z failed: " and says "Permission denied (publickey)"

  Scenario: A run that claimed a story and could not start it is a failed run
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds exactly one line that begins "2026-09-21T12:00:00Z failed: " and says "mw-gq6.1"

  Scenario: Each run adds a line of its own
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    And dispatch runs on "vps" with a cap of 1
    Then the dispatch log holds 2 lines

  Scenario: A dry run adds nothing to the dispatch log
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1 as a dry run
    Then the dispatch log holds 0 lines

  # A story is tried a bounded number of times. Every session mw starts for it is
  # an attempt, written on the story; at the cap mw dispatch stops, and the
  # Mayor is the one who decides what happens next.

  Scenario: A story's first dispatch records one attempt
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" records 1 attempt

  Scenario: A story given back and dispatched again records a second attempt
    Given a ready story "mw-gq6.1" of that epic
    When dispatch runs on "vps" with a cap of 1
    And the story "mw-gq6.1" is given back once its session has ended
    And dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" records 2 attempts

  Scenario: A dispatch that fails before a session starts leaves the count as it was
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" records 0 attempts

  # A story that fails to start with the same error tick after tick tells nobody
  # by itself (mw-gq6.259): the second identical failure mails the Mayor once.

  Scenario: One failure to start a story tells the Mayor nothing yet
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything, saying "bad object refs/heads/mw/mw-gq6.1"
    When dispatch runs on "vps" with a cap of 1
    Then the Mayor has 0 mails

  Scenario: The same failure to start a story twice tells the Mayor once, and not a third time
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything, saying "bad object refs/heads/mw/mw-gq6.1"
    When dispatch runs on "vps" with a cap of 1
    And dispatch runs on "vps" with a cap of 1
    Then the Mayor has 1 mail
    And the subject of the mail to the Mayor is "Stuck: mw-gq6.1 on vps: starting the session of mw-gq6.1: bad object refs/heads/mw/mw-gq6.1"
    When dispatch runs on "vps" with a cap of 1
    Then the Mayor has 1 mail

  Scenario: A different failure to start the same story is told again, once it repeats
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything, saying "bad object refs/heads/mw/mw-gq6.1"
    When dispatch runs on "vps" with a cap of 1
    And dispatch runs on "vps" with a cap of 1
    And the runner refuses to start anything, saying "invalid gitfile format"
    And dispatch runs on "vps" with a cap of 1
    Then the Mayor has 1 mail
    When dispatch runs on "vps" with a cap of 1
    Then the Mayor has 2 mails
    And the subject of the mail to the Mayor is "Stuck: mw-gq6.1 on vps: starting the session of mw-gq6.1: invalid gitfile format"

  Scenario: A story that starts between two failures is not stuck
    Given a ready story "mw-gq6.1" of that epic
    And the runner refuses to start anything, saying "disk is read-only"
    When dispatch runs on "vps" with a cap of 1
    And the runner starts sessions again
    And dispatch runs on "vps" with a cap of 1
    And the story "mw-gq6.1" is given back once its session has ended
    And the runner refuses to start anything, saying "disk is read-only"
    And dispatch runs on "vps" with a cap of 1
    Then the Mayor has 0 mails

  Scenario: A story tried once and then failed before its session starts is still tried once
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 1 time
    And the runner refuses to start anything
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" records 1 attempt

  Scenario: A story tried twice is started a third time when the config says nothing about attempts
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 2 times
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" records 3 attempts

  Scenario: At the cap the story is not started, is blocked, and the Mayor is told once
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 3 times
    And the story "mw-gq6.1" carries the comment "mw next on vps did not close this story out (tests-fail): go test failed in ./application"
    And the story "mw-gq6.1" carries the comment "mw next on vps did not close this story out (open-steps): 2 formula steps are still open"
    When dispatch runs on "vps" with a cap of 1
    And dispatch runs on "vps" with a cap of 1
    Then no session was started
    And there is no worktree for "mw-gq6.1"
    And the story "mw-gq6.1" is not claimed
    And the story "mw-gq6.1" is recorded as blocked for the reason "attempts-exhausted"
    And the story "mw-gq6.1" carries exactly one comment saying it used up its attempts
    And the Mayor has 1 mail
    And the mail to the Mayor says "mw-gq6.1"
    And the mail to the Mayor says "started 3 times"
    And the mail to the Mayor says "(tests-fail)"
    And the mail to the Mayor says "(open-steps)"
    And the mail to the Mayor says "attempts=0"

  Scenario: A story at the cap does not use up the cap on sessions
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 3 times
    And a ready story "mw-gq6.2" of that epic
    When dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.2"

  Scenario: The cap on attempts comes from the config
    Given the config file says max_attempts is 2
    And a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 2 times
    When dispatch runs on "vps" with a cap of 1
    Then no session was started
    And the story "mw-gq6.1" is recorded as blocked for the reason "attempts-exhausted"

  Scenario: A dry run says a story is at the cap and writes nothing
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 3 times
    When dispatch runs on "vps" with a cap of 1 as a dry run
    Then dispatch passed over "mw-gq6.1", saying: started 3 times
    And the story "mw-gq6.1" is not marked blocked
    And the story "mw-gq6.1" carries no comment saying it used up its attempts
    And the Mayor has 0 mails

  Scenario: Once the counter is reset by hand the story is dispatched again
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 3 times
    When dispatch runs on "vps" with a cap of 1
    And the counter of "mw-gq6.1" is reset by hand
    And dispatch runs on "vps" with a cap of 1
    Then one session was started, for "mw-gq6.1"
    And the story "mw-gq6.1" records 1 attempt
    And the story "mw-gq6.1" is recorded as running

  Scenario: A story that uses its attempts up again after a reset is told to the Mayor again
    Given a ready story "mw-gq6.1" of that epic
    And the story "mw-gq6.1" has been tried 3 times
    When dispatch runs on "vps" with a cap of 1
    And the counter of "mw-gq6.1" is reset by hand to 2
    And dispatch runs on "vps" with a cap of 1
    And the story "mw-gq6.1" is given back once its session has ended
    And dispatch runs on "vps" with a cap of 1
    Then the story "mw-gq6.1" is not claimed
    And the story "mw-gq6.1" carries 2 comments saying it used up its attempts
    And the Mayor has 2 mails
