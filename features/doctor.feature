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

  Scenario: A check that cannot tell writes a note naming the verdict, the reason and its last log lines
    Given a doctor check "widget" whose probe cannot tell "no reading"
    When mw doctor runs
    Then mw doctor leaves with the status 0
    And the note "doctor.widget" holds "cannot-tell"
    And the note "doctor.widget" holds "no reading"
    And the note "doctor.widget" holds "widget cannot-tell no reading"

  Scenario: A cure that fails once leaves no note, but failing again writes one
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s damper is 1 minute, cap 3
    And the check "widget"'s cure fails, saying "no wrench found"
    When mw doctor runs
    Then the note "doctor.widget" does not exist
    When 1 minute goes by
    And mw doctor runs
    Then the note "doctor.widget" holds "cure-failed"
    And the note "doctor.widget" holds "no wrench found"

  Scenario: A check back to ok clears its note
    Given a doctor check "widget" whose probe cannot tell "no reading"
    When mw doctor runs
    Then the note "doctor.widget" holds "cannot-tell"
    When the check "widget"'s probe says ok
    And mw doctor runs
    Then the note "doctor.widget" does not exist

  Scenario: A notes port that fails does not change the exit status
    Given a doctor check "widget" whose probe says faulty "misaligned"
    And the check "widget"'s damper is 1 minute, cap 1
    And the notes port fails, saying "kv unavailable"
    When mw doctor runs
    And 1 minute goes by
    And mw doctor runs
    Then mw doctor leaves with the status 6
    And the doctor log holds "note-failed"
    And the doctor log holds "kv unavailable"

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

  Scenario: The real vault-dirty check commits a modified run file by its exact path, the log line naming the commit to revert
    Given a vault with a modified tracked file "runs/mw-gq6.11/result.json"
    When mw doctor's vault-dirty check runs for real
    Then mw doctor leaves with the status 0
    And the doctor log holds "vault-dirty cured"
    And the doctor log holds a way back naming the commit it made

  Scenario: The real timers check starts an enabled timer that is inactive with systemctl --user start, the way back naming it
    Given a fake systemctl reporting the timer "mw-dispatch.timer" enabled and inactive
    When mw doctor's timers check runs for real
    Then mw doctor leaves with the status 0
    And systemctl was run with "--user start mw-dispatch.timer"
    And the doctor log holds "timers cured"
    And the doctor log holds "systemctl --user stop mw-dispatch.timer"

  Scenario: The real beads-size check past its budget has no cure, and writes a note once damped
    Given a vault whose .beads is 200 bytes, past a 100 byte budget
    When mw doctor's beads-size check runs for real
    Then mw doctor leaves with the status 6
    And the doctor log holds "beads-size cure-failed no cure"
    And the note "doctor.beads-size" does not exist
    When mw doctor's beads-size check runs for real
    Then mw doctor leaves with the status 6
    And the doctor log holds "beads-size damped"
    And the note "doctor.beads-size" holds "damped"
    And the note "doctor.beads-size" holds "200"
    And the note "doctor.beads-size" holds "100"

  Scenario: The real mayor-gone check with no .mayor-acting is cannot-tell, and mayor-up never runs
    Given a vault with no .mayor-acting
    When mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-gone cannot-tell"
    And the doctor log holds ".mayor-acting"
    And mayor-up was not run

  Scenario: The real mayor-gone check whose window is open with a live process is ok
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux listing that window with a live claude process
    And a stand-in bin/mayor-up in that vault that starts a Mayor in window "@7"
    When mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-gone ok"
    And mayor-up was not run

  Scenario: The real mayor-gone check whose window is gone runs mayor-up, the way back naming the window it started
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux with no window open
    And a stand-in bin/mayor-up in that vault that starts a Mayor in window "@7"
    When mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-gone cured"
    And the doctor log holds "tmux kill-window -t '@7'"
    And mayor-up was run 1 time

  Scenario: The same mayor-gone fault within 30 minutes is damped, and mayor-up is not run again
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux with no window open
    And a stand-in bin/mayor-up in that vault that starts a Mayor in window "@7"
    When mw doctor's mayor-gone check runs for real
    And 10 minutes go by
    And mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 6
    And the doctor log holds "mayor-gone damped"
    And mayor-up was run 1 time

  Scenario: mayor-up failing twice writes the check's own note and spends no third cure in the episode
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux with no window open
    And a stand-in bin/mayor-up in that vault that always exits 4, saying "cannot: no live Mayor could be started"
    When mw doctor's mayor-gone check runs for real
    And 30 minutes go by
    And mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 6
    And the note "doctor.mayor-gone" holds "cure-failed"
    And mayor-up was run 2 times
    When 30 minutes go by
    And mw doctor's mayor-gone check runs for real
    Then mayor-up was run 2 times

  Scenario: bin/mayor-up missing is cannot-tell, and nothing runs
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux with no window open
    When mw doctor's mayor-gone check runs for real
    Then mw doctor leaves with the status 0
    And the doctor log holds "mayor-gone cannot-tell"
    And the doctor log holds "bin/mayor-up"
    And mayor-up was not run

  Scenario: --dry-run on a faulty mayor-gone check prints the mayor-up line, and changes nothing
    Given a vault whose .mayor-acting names the window "mayor-2026-09-23-39"
    And a stand-in tmux with no window open
    And a stand-in bin/mayor-up in that vault that starts a Mayor in window "@7"
    When mw doctor runs dry
    Then mw doctor leaves with the status 0
    And mw doctor printed "bin/mayor-up"
    And mayor-up was not run
    And the doctor log is empty
