Feature: Checking a rig whose gate is split into parts
  A rig's whole gate can outrun the 590 seconds a Builder's tool call is given,
  and a gate run in the background is a wait the auto-mode classifier refuses
  (mw-gq6.567). A host can name a rig's gate as parts, in its config file. The
  Builder then runs mw check with --part N once for each part, each call short
  enough to finish in the foreground, and one last mw check, with no part, which
  runs nothing of the gate again: it reads what each part said at the branch's
  tip and reports the whole gate green, or which part is not.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the rig's gate is split into these parts:
      | name      | command                          |
      | lint      | printf 'lint\n' >> {log}         |
      | unit 1/2  | printf 'unit-1\n' >> {log}       |
      | unit 2/2  | printf 'unit-2\n' >> {log}       |

  Scenario: One part is run on its own, and only that part
    When a session checks "mw-gq6.1" part 2
    Then the check part passes
    And the parts that ran, in order, were: unit-1
    And the rig's tests were run 0 times
    And the check prints: part 2 of 3 (unit 1/2) passed

  Scenario: A part waits for a busy host to calm, and runs once it has (mw-gq6.987)
    Given the host's load reads in turn: busy, far over, calm
    When a session checks "mw-gq6.1" part 2
    Then the check part passes
    And the parts that ran, in order, were: unit-1
    And the check prints: waiting up to

  Scenario: A part is not started on a host that stays busy, and says the load and the part to run again (mw-gq6.987)
    Given the host's load reads in turn: busy
    When a session checks "mw-gq6.1" part 2
    Then the check part fails
    And the check part error says: part 2 of 3 (unit 1/2) did not start
    And the check part error says: load 39.0 of 20 cores
    And the check part error says: mw check mw-gq6.1 --part 2
    And the check part error says: nothing was run

  Scenario: A part that fails is named with what it printed, and fails the call
    Given the part "unit 2/2" fails, saying "FAIL src/review.test.ts"
    When a session checks "mw-gq6.1" part 3
    Then the check part fails
    And the check prints: part 3 of 3 (unit 2/2) failed
    And the check prints: FAIL src/review.test.ts

  Scenario: A part the rig does not have is refused, naming how many it has
    When a session checks "mw-gq6.1" part 4
    Then the check part fails
    And the check part error says: the gate of the rig millwright has 3 parts

  Scenario: The last call reports the whole gate green once every part has passed at the tip
    When a session checks "mw-gq6.1" part 1
    And a session checks "mw-gq6.1" part 2
    And a session checks "mw-gq6.1" part 3
    And a session checks "mw-gq6.1"
    Then the check passes
    And the check prints: the gate ran as 3 parts and every part passed at the tip
    And the parts that ran, in order, were: lint, unit-1, unit-2
    And the rig's tests were run 0 times
    And the check says the branch would be landed

  Scenario: The last call names the parts that have not been run
    When a session checks "mw-gq6.1" part 2
    And a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: part 1 (lint) has not been run at this commit: mw check mw-gq6.1 --part 1
    And the check prints: part 3 (unit 2/2) has not been run at this commit: mw check mw-gq6.1 --part 3
    And the check does not print: part 2 (unit 1/2) has not been run

  Scenario: The last call names a part that failed
    Given the part "unit 1/2" fails, saying "FAIL src/deck.test.ts"
    When a session checks "mw-gq6.1" part 1
    And a session checks "mw-gq6.1" part 2
    And a session checks "mw-gq6.1" part 3
    And a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: part 2 (unit 1/2) failed at this commit: mw check mw-gq6.1 --part 2
    And the check does not print: part 1 (lint) failed

  Scenario: A part passed before a new commit does not count at the new tip
    When a session checks "mw-gq6.1" part 1
    And a session checks "mw-gq6.1" part 2
    And a session checks "mw-gq6.1" part 3
    And the session of "mw-gq6.1" commits another change
    And a session checks "mw-gq6.1"
    Then the check fails
    And the check prints: part 1 (lint) has not been run at this commit: mw check mw-gq6.1 --part 1
    And the check prints: an earlier commit; any commit since, a doc too, makes it run again

  Scenario: A part that fails once the host has calmed is not run a second time
    Given the host's load reads in turn: busy, calm
    And the part "lint" fails, saying "timeout"
    When a session checks "mw-gq6.1" part 1
    Then the check part fails
    And the parts that ran, in order, were: lint
    And the check prints: waiting up to

  Scenario: A part that fails while the host was far over its cores is run once more when it calms, and passes (mw-gq6.1060)
    Given the host's load reads in turn: calm, far over, calm
    And the part "lint" fails its first run only, saying "timeout"
    When a session checks "mw-gq6.1" part 1
    Then the check part passes
    And the parts that ran, in order, were: lint, lint
    And the check prints: ran twice
    And the check prints: the first run failed while

  Scenario: A part that fails again on the calm run is that run's failure, and says it ran twice (mw-gq6.1060)
    Given the host's load reads in turn: calm, far over, calm
    And the part "lint" fails, saying "timeout"
    When a session checks "mw-gq6.1" part 1
    Then the check part fails
    And the parts that ran, in order, were: lint, lint
    And the check prints: ran twice
    And the check prints: part 1 of 3 (lint) failed
    And the check part error says: did not pass

  Scenario: A part that fails while the host was only busy is not run again (mw-gq6.1060)
    Given the host's load reads in turn: calm, busy
    And the part "lint" fails, saying "timeout"
    When a session checks "mw-gq6.1" part 1
    Then the check part fails
    And the parts that ran, in order, were: lint
    And the check does not print: ran twice

  Scenario: A part that failed on a far over host and finds it still so at the bound is handed back, not counted (mw-gq6.1060)
    Given the host's load reads in turn: calm, far over
    And the part "lint" fails, saying "timeout"
    When a session checks "mw-gq6.1" part 1
    Then the check part fails
    And the parts that ran, in order, were: lint
    And the check part error says: failed once while
    And the check part error says: mw check mw-gq6.1 --part 1
    And the check part error says: not a failure of the part

  Scenario: A part that outlasts 540 seconds ends the call with a line to run it again, as today (mw-t5shinh.14)
    Given the part "unit 1/2" runs for 600 seconds on the call's clock
    When a session checks "mw-gq6.1" part 2 in one foreground call
    Then the check part fails
    And the check part error says: part 2 of 3 (unit 1/2) is still running
    And the check part error says: mw check mw-gq6.1 --part 2

  Scenario: A part that outlasts 540 seconds is waited for in the same call when the rig gave its Builders a 55 minute call (mw-t5shinh.14)
    Given the call's BASH_MAX_TIMEOUT_MS is "3300000"
    And the part "unit 1/2" runs for 600 seconds on the call's clock
    When a session checks "mw-gq6.1" part 2 in one foreground call
    Then the check part passes
    And the check prints: part 2 of 3 (unit 1/2) passed
    And the parts that ran, in order, were: unit-1

  Scenario: A rig whose gate is not split refuses --part
    Given the rig's gate is not split into parts
    When a session checks "mw-gq6.1" part 1
    Then the check part fails
    And the check part error says: the gate of the rig millwright is not split into parts

  # A rig whose stories sometimes change nothing but data files names them, and the
  # command that checks them, under [data_parts.<rig>] (mw-gq6.1589). A story whose
  # every changed file is inside those paths is gated by that command, as one part,
  # in place of the rig's parts; one file outside and the gate is the rig's own.

  Scenario: A story that changes only the data files runs the data's own check in place of the rig's parts (mw-gq6.1589)
    Given the host names the rig's data files as "data/**,docs/lxx-*.md" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
      | docs/lxx-notes.md           |
    When a session checks "mw-gq6.2" part 1
    Then the check part passes
    And the parts that ran, in order, were: data
    And the rig's tests were run 0 times
    And the check prints: data scope  2 changed file(s)
    And the check prints: part 1 of 1 (data scope) passed

  Scenario: The last call of a data-scope story reports its gate green from the one data part, and says so first (mw-gq6.1589)
    Given the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    When a session checks "mw-gq6.2" part 1
    And a session checks "mw-gq6.2"
    Then the check passes
    And the check prints: data scope  1 changed file(s)
    And the check prints: the gate ran in its data scope (1 changed file(s))
    And the check does not print: the gate ran as 3 parts
    And the parts that ran, in order, were: data
    And the check says the branch would be landed
    And the check log of "mw-gq6.2" holds: data scope  1 changed file(s)

  Scenario: A data-scope story whose data check has not run is not green, and names the one part to run (mw-gq6.1589)
    Given the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    When a session checks "mw-gq6.2"
    Then the check fails
    And the check prints: part 1 (data scope) has not been run at this commit: mw check mw-gq6.2 --part 1
    And the check does not print: part 2

  Scenario: A story with one changed file outside the data paths runs the rig's parts as before (mw-gq6.1589)
    Given the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
      | src/review.ts               |
    When a session checks "mw-gq6.2" part 2
    Then the check part passes
    And the parts that ran, in order, were: unit-1
    And the check prints: part 2 of 3 (unit 1/2) passed
    And the check does not print: data scope

  Scenario: A rig with no data key is gated by its parts whatever its story changes (mw-gq6.1589)
    Given the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    When a session checks "mw-gq6.2" part 3
    Then the check part passes
    And the parts that ran, in order, were: unit-2
    And the check does not print: data scope

  Scenario: A batch rig queues a data-scope story on the pass of its data check, with no part run again (mw-gq6.1589)
    Given the rig lands its stories in batches
    And the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    And the session of "mw-gq6.2" reported a plain success
    When a session checks "mw-gq6.2" part 1
    And mw closes out "mw-gq6.2"
    Then the story "mw-gq6.2" is recorded as queued for batch landing
    And the parts that ran, in order, were: data
    And the rig's tests were run 0 times
    And the last ledger line holds:
      | queued for batch landing                      |
      | tested by mw check, data scope at             |
      | pass (1 changed file(s))                      |
    And the last ledger line does not say "mw next re-ran the rig's tests"
    And the last ledger line does not say "parts at"

  Scenario: A batch rig queues a data-scope story on a whole check's pass, and the ledger says it was the data scope (mw-gq6.1589)
    Given the rig lands its stories in batches
    And the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    And the session of "mw-gq6.2" reported a plain success
    When a session checks "mw-gq6.2" part 1
    And a session checks "mw-gq6.2"
    And mw closes out "mw-gq6.2"
    Then the story "mw-gq6.2" is recorded as queued for batch landing
    And the parts that ran, in order, were: data
    And the last ledger line holds:
      | tested by mw check, data scope at |
    And the last ledger line does not say "tested by mw check at"
    And the last ledger line does not say "mw next re-ran the rig's tests"

  Scenario: A batch rig's story with a file outside the data paths is queued on the rig's parts as today (mw-gq6.1589)
    Given the rig lands its stories in batches
    And the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
      | src/review.ts               |
    And the session of "mw-gq6.2" reported a plain success
    When a session checks "mw-gq6.2" part 1
    And a session checks "mw-gq6.2" part 2
    And a session checks "mw-gq6.2" part 3
    And mw closes out "mw-gq6.2"
    Then the story "mw-gq6.2" is recorded as queued for batch landing
    And the parts that ran, in order, were: lint, unit-1, unit-2
    And the last ledger line holds:
      | tested by mw check, parts at |
    And the last ledger line does not say "data scope"

  Scenario: A rig that lands each lands a data-scope story on the pass of its data check, with no part run again (mw-gq6.1589)
    Given the rig lands its stories each by itself
    And the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
    And the session of "mw-gq6.2" reported a plain success
    When a session checks "mw-gq6.2" part 1
    And mw closes out "mw-gq6.2"
    Then the story "mw-gq6.2" is closed
    And the parts that ran, in order, were: data
    And the last ledger line holds:
      | landed on main                    |
      | tested by mw check, data scope at |
    And the last ledger line does not say "mw next re-ran the rig's tests: pass"

  Scenario: A rig that lands each runs the rig's gate at the close-out for a story with a file outside the data paths, as today (mw-gq6.1589)
    Given the rig lands its stories each by itself
    And the host names the rig's data files as "data/**" and checks them with "printf 'data\n' >> {log}"
    And the story "mw-gq6.2" has been worked in its own worktree, changing only these files:
      | data/lxx/choices/gen-01.tsv |
      | src/review.ts               |
    And the session of "mw-gq6.2" reported a plain success
    When mw closes out "mw-gq6.2"
    Then the story "mw-gq6.2" is closed
    And the parts that ran, in order, were: lint, unit-1, unit-2
    And the last ledger line holds:
      | mw next re-ran the rig's tests: pass |
    And the last ledger line does not say "data scope"
