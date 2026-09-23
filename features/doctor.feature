Feature: mw doctor
  mw doctor works a table of checks, each its own probe (read-only), cure (run
  only when the probe says faulty and the damper allows it), damper (a
  minimum wait between cures and a cap on how many one fault episode may
  spend) and way back. Every run logs one dated line per check on this host.
  It leaves with 0 when every check is ok or cured, and 6 when any check is
  left faulty and uncured — damped, or its cure failed. --dry-run prints what
  a faulty check would do and changes nothing.

  Scenario: A check whose probe is ok is logged ok and cures nothing
    Given a doctor check "widget" whose probe says ok
    When mw doctor runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "widget ok"
    And the check "widget" was not cured

  Scenario: A faulty check is cured once, its way back logged beside it
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s way back is "realign it by hand"
    When mw doctor runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "widget cured misaligned"
    And the doctor log holds "realign it by hand"
    And the check "widget" was cured 1 time

  Scenario: The same fault within the damper's wait is damped, not cured
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s damper is 10 minutes, cap 3
    When mw doctor runs
    And 5 minutes go by
    And mw doctor runs
    Then mw doctor leaves with the status 6
    And the doctor log holds "widget damped"
    And the check "widget" was cured 1 time

  Scenario: A check cured to its cap is damped until the probe says ok, then cured again
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s damper is 1 minute, cap 2
    When mw doctor runs
    And 1 hour goes by
    And mw doctor runs
    And 1 hour goes by
    Then the check "widget" was cured 2 times
    When mw doctor runs
    Then mw doctor leaves with the status 6
    And the check "widget" was cured 2 times
    When the check "widget"'s probe says ok
    And mw doctor runs
    And the check "widget"'s probe says faulty "misaligned" again
    And mw doctor runs
    Then the check "widget" was cured 3 times

  Scenario: A cure that fails is logged and leaves the fault exit
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s cure fails, saying "no wrench found"
    When mw doctor runs
    Then mw doctor leaves with the status 6
    And the doctor log holds "widget cure-failed no wrench found"

  Scenario: --dry-run names the check, the cure and the way back, and changes nothing
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s way back is "realign it by hand"
    When mw doctor runs dry
    Then mw doctor leaves with the status 0
    And mw doctor printed "widget"
    And mw doctor printed "misaligned"
    And mw doctor printed "realign it by hand"
    And the doctor log is empty
    And the check "widget" was not cured

  Scenario: The real daemon-reload check cures with systemctl --user daemon-reload
    Given a fake systemctl that says "mw-dispatch.service" needs a reload
    When mw doctor's daemon-reload check runs for real
    Then mw doctor leaves with the status 0
    And systemctl was run with "--user daemon-reload"
    And the doctor log holds "daemon-reload cured"

  Scenario: The real wifi check waits five minutes, then bounces the network with netsh, damped after 30 minutes and capped at 3
    Given a fake netsh reporting the network "Whitehouse"
    And a fake powershell that exists
    And the internet is unreachable
    When mw doctor's wifi check runs
    Then the doctor log holds "wifi cannot-tell faulty (waiting 5m)"
    And netsh was not run
    When 5 minutes go by
    And mw doctor's wifi check runs
    Then mw doctor leaves with the status 0
    And the doctor log holds "wifi cured internet unreachable since"
    And the doctor log holds "netsh wlan connect name=Whitehouse"
    And netsh was run with "wlan connect name=Whitehouse" 1 time
    When mw doctor's wifi check runs
    Then mw doctor leaves with the status 6
    And the doctor log holds "wifi damped"
    And netsh was run with "wlan connect name=Whitehouse" 1 time
    When 30 minutes go by
    And mw doctor's wifi check runs
    Then netsh was run with "wlan connect name=Whitehouse" 2 times
    When 30 minutes go by
    And mw doctor's wifi check runs
    Then netsh was run with "wlan connect name=Whitehouse" 3 times
    When 30 minutes go by
    And mw doctor's wifi check runs
    Then mw doctor leaves with the status 6
    And the doctor log holds "wifi damped"
    And netsh was run with "wlan connect name=Whitehouse" 3 times
