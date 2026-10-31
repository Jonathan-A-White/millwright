Feature: One rig gate runs at a time on a host
  The rig's tests are heavy enough to fill every core of a host, and two of them
  side by side fail each other on timeouts that have nothing to do with either
  story (mw-neuq78.10 and mw-5r3p30.229, refused at load 22 to 32 of 16 cores,
  mw-gq6.388). So a close-out takes this host's gate lock, whatever the rig,
  before it runs the rig's tests, and waits for it while another gate holds it.
  The merge slot, which keeps two close-outs off one rig's target branch, is
  another lock and is unchanged. The report names how long the gate waited and
  for which story, and says nothing when the lock was free.

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"
    And the story "mw-gq6.1" has been worked in its own worktree
    And the session of "mw-gq6.1" reported a plain success

  Scenario: A gate that would overlap another waits for it, and the report says for whom
    Given this host's gate lock is held by the gate of "mw-gq6.9", which ends once another gate is seen waiting
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the rig's tests of "mw-gq6.1" began after the gate of "mw-gq6.9" ended
    And the printed report says the gate waited for "mw-gq6.9"
    And the mail to the Mayor says the gate waited for "mw-gq6.9"
    And this host's gate lock is free again

  Scenario: A gate whose lock is free runs at once and says nothing of waiting
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And the printed report says nothing of waiting for the gate lock
    And this host's gate lock is free again

  Scenario: A gate that fails gives the lock back
    Given the rig's tests fail, saying "still red"
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is not closed
    And this host's gate lock is free again

  # mw-gq6.872: the target is fetched after the wait for the gate lock, not before.
  Scenario: A merged result whose red the target fixed while the gate waited is tested on the target as it is then
    Given the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the rig's tests fail, saying "--- FAIL: TestTogether (0.01s)", with the other host's work merged in unless the target holds the fix
    And the rig has a named-tests command that fails on main alone unless the target holds the fix, saying "--- FAIL: TestTogether"
    And "main" at the origin gets the fix while the gate lock is asked for the 2nd time and found held
    When mw closes out "mw-gq6.1"
    Then the work of "mw-gq6.1" is on "main" at the rig's origin
    And "main" at the origin holds the fix
    And the story "mw-gq6.1" is closed
    And the named-tests command was not run

  Scenario: Main alone is checked on the target as it is after the wait for the gate lock, and passes what the fix mended
    Given the other host landed its own work on "main" while "mw-gq6.1" was worked
    And the rig's tests fail, saying "--- FAIL: TestTogether (0.01s)", with the other host's work merged in unless the target holds the fix
    And the rig has a named-tests command that fails on main alone unless the target holds the fix, saying "--- FAIL: TestTogether"
    And "main" at the origin gets the fix while the gate lock is asked for the 3rd time and found held
    When mw closes out "mw-gq6.1"
    Then the named-tests command was run once, given: TestTogether
    And the last ledger line holds:
      | main alone passes TestTogether (checked in |
    And the last ledger line does not say "main is red"
