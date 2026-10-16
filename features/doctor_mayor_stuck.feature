Feature: mw doctor's mayor-stuck check
  A Mayor whose process is alive but whose every harness reply in the last 10
  minutes was an "API Error" (the network under it changed, a proxy it was
  started behind is dead) is stuck, and neither mayor-gone nor mayor-stale can
  see it. mayor-stuck reads the Mayor's transcript. Its cure closes the
  Mayor's window and starts a successor with no handover, after dropping a
  proxy setting whose port refuses a TCP connect from the successor's
  environment and from the tmux server's. A proxy that answers is kept. The
  Governor is told once per episode, on the emergency lane.

  A Mayor parked on the harness's usage-limit menu ("Stop and wait" / "Switch
  to usage credits") writes no API Error at all, so the transcript cannot see
  it: the check reads the pane too (mw-gq6.433, @138 sat there 2h12m on
  2026-10-15). A pane on the limit for 10 minutes is the factory stopped. A
  successor would meet the same limit, so the window is left alone and the
  Governor is paged once. Paged, the check is ok until the limit resets; a
  Mayor still on the limit after its reset (read off the harness's notice, a
  reset named without a date being the first such time after the limit was
  first seen) is restarted: its window is closed and a successor takes the
  seat, and the Governor is told so (mw-gq6.473).

  Background:
    Given a vault whose .mayor-acting names the window "mayor-2026-10-06-210"
    And a stand-in tmux listing that Mayor window with a live claude process
    And a stand-in bin/mayor-up that starts a Mayor in window "@9"

  Scenario: A Mayor whose every reply in the last 10 minutes was an API Error is stuck, and a successor takes the seat
    Given a transcript whose replies in the last 10 minutes were all API Errors
    When mw doctor's mayor-stuck check runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-stuck cured"
    And the Mayor's window was closed
    And mayor-up was run 1 time
    And no handover was written

  Scenario: One reply that succeeded in the last 10 minutes leaves the Mayor alone
    Given a transcript whose replies in the last 10 minutes were API Errors and one that succeeded
    When mw doctor's mayor-stuck check runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-stuck ok"
    And the Mayor's window was not closed
    And mayor-up was not run

  Scenario: API Errors older than 10 minutes do not make the Mayor stuck
    Given a transcript whose only replies were API Errors 20 minutes ago
    When mw doctor's mayor-stuck check runs
    Then the doctor log holds "mayor-stuck ok"
    And mayor-up was not run

  Scenario: A transcript with no replies yet is not stuck
    Given a transcript with no replies
    When mw doctor's mayor-stuck check runs
    Then the doctor log holds "mayor-stuck ok"
    And mayor-up was not run

  Scenario: --dry-run names a stuck Mayor and changes nothing
    Given a transcript whose replies in the last 10 minutes were all API Errors
    When mw doctor runs dry
    Then mw doctor printed "API Error"
    And the Mayor's window was not closed
    And mayor-up was not run
    And no alarm was sent

  Scenario: A dead proxy is dropped from the successor's environment and from the tmux server's
    Given a transcript whose replies in the last 10 minutes were all API Errors
    And the environment sets HTTPS_PROXY and https_proxy to a proxy whose port refuses connections
    And the tmux server's global environment sets HTTPS_PROXY, https_proxy and ALL_PROXY to that same proxy
    When mw doctor's mayor-stuck check runs
    Then mayor-up was run 1 time
    And the successor's environment has no proxy setting
    And tmux was told to unset HTTPS_PROXY, https_proxy and ALL_PROXY in its global environment

  Scenario: A proxy that answers is kept
    Given a transcript whose replies in the last 10 minutes were all API Errors
    And the environment sets HTTPS_PROXY to a proxy that answers
    When mw doctor's mayor-stuck check runs
    Then mayor-up was run 1 time
    And the successor's environment keeps HTTPS_PROXY
    And tmux was not told to unset anything

  Scenario: The Governor is told once on the emergency lane, not on every run
    Given a transcript whose replies in the last 10 minutes were all API Errors
    When mw doctor's mayor-stuck check runs
    And 5 minutes go by
    And mw doctor's mayor-stuck check runs
    And 5 minutes go by
    And mw doctor's mayor-stuck check runs
    Then exactly 1 alarm was sent
    And the alarm says "API Error"
    And mayor-up was run 1 time

  Scenario: A host that is not home holds no Mayor, so nothing is judged
    Given this host is not home
    And a transcript whose replies in the last 10 minutes were all API Errors
    When mw doctor's mayor-stuck check runs
    Then the doctor log holds "mayor-stuck ok"
    And mayor-up was not run

  Scenario: A Mayor parked on the weekly-limit menu for 10 minutes is reported, left alone, and the Governor paged once
    Given a transcript whose only replies were API Errors 20 minutes ago
    And the Mayor's pane shows the weekly-limit menu
    When mw doctor's mayor-stuck check runs
    Then the doctor log holds "mayor-stuck ok"
    When 10 minutes go by
    And mw doctor's mayor-stuck check runs
    And 5 minutes go by
    And mw doctor's mayor-stuck check runs
    And 40 minutes go by
    And mw doctor's mayor-stuck check runs
    Then the doctor log holds "stopped at the usage limit"
    And exactly 1 alarm was sent
    And the alarm says "the factory is stopped"
    And the alarm says "Switch to usage credits"
    And the Mayor's window was not closed
    And mayor-up was not run

  Scenario: --dry-run names a Mayor parked on the weekly-limit menu and pages nobody
    Given a transcript with no replies
    And the Mayor's pane shows the weekly-limit menu
    When mw doctor's mayor-stuck check runs
    And 10 minutes go by
    And mw doctor runs dry
    Then mw doctor printed "stopped at the usage limit"
    And the Mayor's window was not closed
    And no alarm was sent

  Scenario: A Mayor still parked on the usage-limit menu after its reset is restarted
    Given a transcript whose only replies were API Errors 20 minutes ago
    And the Mayor's pane shows the usage-limit menu under the notice "You've hit your session limit · resets 1pm (UTC)"
    When mw doctor's mayor-stuck check runs
    And 10 minutes go by
    And mw doctor's mayor-stuck check runs
    And 30 minutes go by
    And mw doctor's mayor-stuck check runs
    Then the Mayor's window was not closed
    And mayor-up was not run
    And exactly 1 alarm was sent
    When 25 minutes go by
    And mw doctor's mayor-stuck check runs
    Then the doctor log holds "mayor-stuck cured"
    And the Mayor's window was closed
    And mayor-up was run 1 time
    And exactly 2 alarms were sent
    And the alarm says "which reset at 2026-09-23 13:00Z"
