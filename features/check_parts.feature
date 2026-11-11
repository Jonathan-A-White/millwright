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
