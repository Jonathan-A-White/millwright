Feature: mw seat up
  mw seat up starts a seat's next session: one interactive Claude Code session,
  in a window of its own, booted from the seat's charter and told to carry on
  from the seat's newest handoff. Nothing else the vault holds is read or
  passed — no ledger, no memory of a rig, no tracker prime — and the seat's own
  kickoff file, when it has one, is what the session is told.

  It refuses, and starts nothing, when there is no charter to boot into, no
  handoff to boot from, or the seat is already acting in a window that is still
  open and has written no handoff since.

  Background:
    Given a vault holding the "mayor" seat
    And the "mayor" seat's charter
    And the "mayor" seat has written the handoffs:
      | 2026-09-18-01 |
      | 2026-09-19-02 |
    And the "mayor" seat also holds a ledger, a vision and a memory of a rig
    And today is "2026-09-19"

  Scenario: A first boot starts exactly one window, named past the newest handoff
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And exactly one window was opened
    And the window is named "mayor-2026-09-19-03"
    And seat up says it started the seat in the window "mayor-2026-09-19-03"

  Scenario: The session is the seat's charter, the model, the effort, the seat and a kickoff
    When mw seat up starts the "mayor" seat on "opus" at "high" for the reason "the map says so"
    Then seat up succeeds
    And the window's command carries:
      | --model opus                |
      | --effort high               |
      | --permission-mode auto      |
      | --name mayor-2026-09-19-03  |
    And the window's command primes the session from the vault's "seats/mayor/charter.md"
    And the window runs in the vault
    And the window's environment holds:
      | MW_SEAT | mayor |
    And the kickoff prompt of the window holds:
      | seats/mayor/handoffs/2026-09-19-02.md |
      | the map says so                       |

  Scenario: A seat nobody is watching denies a permission instead of asking for it
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the window's session denies a permission it would have asked for
    And the window's command carries:
      | --permission-mode auto |
    And the window's command holds none of:
      | --print               |
      | --permission-prompts  |

  Scenario: A seat brought up attended is asked about permissions
    When mw seat up starts the "mayor" seat attended
    Then seat up succeeds
    And the window's session is asked about permissions, as a person's own would be
    And the window's command carries:
      | --permission-mode auto |

  Scenario: Nothing else in the vault is read or passed
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the window's command holds none of:
      | Mayor of millwright                  |
      | mw-old was worked and closed here    |
      | where this factory is going          |
      | what the mayor knows about millwright |

  Scenario: A seat with no kickoff file of its own is told the default
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the kickoff prompt of the window holds:
      | You are booting into the mayor seat   |
      | seats/mayor/handoffs/2026-09-19-02.md |

  Scenario: The seat's own kickoff file replaces the default text
    Given the "mayor" seat holds the kickoff text "Boot by procedures.md, section Boot."
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the kickoff prompt of the window holds:
      | Boot by procedures.md, section Boot.  |
      | seats/mayor/handoffs/2026-09-19-02.md |
    And the kickoff prompt of the window holds none of:
      | You are booting into the mayor seat |

  Scenario: A seat that keeps its handoffs per host boots from this host's newest
    Given the "mayor" seat keeps its handoffs on this host, and has written:
      | 2026-09-19-07 |
      | 2026-09-19-08 |
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the kickoff prompt of the window holds:
      | seats/mayor/hosts/laptop/handoffs/2026-09-19-08.md |
    And the kickoff prompt of the window holds none of:
      | seats/mayor/handoffs/2026-09-19-02.md |
    And the window is named "mayor-2026-09-19-09"

  Scenario: A seat with no charter is refused
    Given the "mayor" seat has no charter
    When mw seat up starts the "mayor" seat
    Then seat up is refused saying the seat has no charter
    And no window was opened

  Scenario: A seat that has written no handoff is refused
    Given the "mayor" seat has written no handoff
    When mw seat up starts the "mayor" seat
    Then seat up is refused saying the seat has no handoff
    And no window was opened

  Scenario: A seat already acting in a window that is still open is refused
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T08:00:00Z"
    And the "mayor" seat's acting file names that window
    And the newest handoff was written at "2026-09-19T07:00:00Z"
    When mw seat up starts the "mayor" seat
    Then seat up is refused saying the seat is already acting in "mayor-2026-09-19-02"
    And no window was opened

  Scenario: A seat whose window has gone is started again
    Given the "mayor" seat's acting file names the window "mayor-2026-09-19-02"
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And exactly one window was opened

  Scenario: A seat that has handed off since its window was opened is started again
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T08:00:00Z"
    And the "mayor" seat's acting file names that window
    And the newest handoff was written at "2026-09-19T09:00:00Z"
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And exactly one window was opened
    And the window is named "mayor-2026-09-19-03"

  Scenario: The window is numbered past the windows already open as well as the handoffs
    Given the window "mayor-2026-09-19-05" was opened at "2026-09-19T08:00:00Z"
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And the window is named "mayor-2026-09-19-06"

  Scenario: Run from the window the acting file names, seat up arms a reaper on that window
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T05:00:00Z"
    And the "mayor" seat's acting file names that window
    And the seat up was run from the window "mayor-2026-09-19-02"
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And a reaper was armed on the window "mayor-2026-09-19-02" in successor mode for the "mayor" seat
    And seat up says the window will close itself once its successor has the seat

  Scenario: Run from a window the acting file does not name, seat up arms nothing
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T05:00:00Z"
    And the "mayor" seat's acting file names that window
    And the window "mayor-scratch" was opened at "2026-09-19T05:30:00Z"
    And the seat up was run from the window "mayor-scratch"
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And no reaper was armed

  Scenario: Run outside any window, seat up arms nothing
    When mw seat up starts the "mayor" seat
    Then seat up succeeds
    And no reaper was armed

  Scenario: With --reap-when-idle, seat up arms an idle-mode reaper on the window it opened
    When mw seat up starts the "mayor" seat and reaps its window when idle
    Then seat up succeeds
    And a reaper was armed on the window "mayor-2026-09-19-03" in when-idle mode for the "mayor" seat
    And seat up says the new window will close itself once it is idle after a handoff

  Scenario: With --reap-when-idle and run from the window the acting file names, both windows are armed
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T05:00:00Z"
    And the "mayor" seat's acting file names that window
    And the seat up was run from the window "mayor-2026-09-19-02"
    When mw seat up starts the "mayor" seat and reaps its window when idle
    Then seat up succeeds
    And a reaper was armed on the window "mayor-2026-09-19-02" in successor mode for the "mayor" seat
    And a reaper was armed on the window "mayor-2026-09-19-03" in when-idle mode for the "mayor" seat

  Scenario: A refused seat up arms nothing
    Given the window "mayor-2026-09-19-02" was opened at "2026-09-19T10:00:00Z"
    And the "mayor" seat's acting file names that window
    And the seat up was run from the window "mayor-2026-09-19-02"
    When mw seat up starts the "mayor" seat and reaps its window when idle
    Then seat up is refused saying the seat is already acting in "mayor-2026-09-19-02"
    And no reaper was armed

  Scenario: A reaper that could not be started is said, and the seat is still started
    Given the reaper cannot be started
    When mw seat up starts the "mayor" seat and reaps its window when idle
    Then the seat's session was started all the same
    And seat up fails saying the reaper could not be armed
