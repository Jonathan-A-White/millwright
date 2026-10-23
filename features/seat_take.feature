Feature: mw seat take
  A Mayor that takes the seat without a handover (after bin/mayor-up started it,
  or after a timeout) used to write .mayor-acting by hand, with no window id and
  no session id, and the doctor's mayor-gone probe then found no window it
  named, reported the Mayor gone every five minutes and woke the Millhand for
  it. mw seat take, run inside the Mayor's own tmux pane, writes the acting file
  for it: the Mayor's name, the window id and the session id, in the form the
  probe matches. bin/mayor-up names a new window from the handoff, not from the
  new Mayor's name, so take renames the window to the name it is given with
  --as.

  Background:
    Given a vault for the "mayor" seat taking by hand
    And the clock for taking reads "2026-10-22T13:33:02Z"
    And a take window "@1005" named "mayor-2026-10-22-550" that the command runs in
    And the session id is "7f3c1a90-2b4d-4e8f-9a61-0c5d8e2b7a14"

  Scenario: Taking the seat in a window already named for the Mayor writes its name, the window id and the session id
    When the Mayor takes the seat
    Then the take succeeded
    And the take's acting file holds "Mayor mayor-2026-10-22-550"
    And the take's acting file holds "window @1005"
    And the take's acting file holds "session 7f3c1a90-2b4d-4e8f-9a61-0c5d8e2b7a14"
    And the take's acting file holds "since 2026-10-22T13:33:02Z"
    And the take window "@1005" is called "mayor-2026-10-22-550"

  Scenario: Taking the seat as a new name renames the window to it, so the window is named from the new Mayor and not from the handoff
    When the Mayor takes the seat as "mayor-2026-10-22-552"
    Then the take succeeded
    And the take window "@1005" is called "mayor-2026-10-22-552"
    And the take's acting file holds "Mayor mayor-2026-10-22-552"
    And the take's acting file holds "window @1005"
    And the take's acting file holds "session 7f3c1a90-2b4d-4e8f-9a61-0c5d8e2b7a14"
    And the take says "mayor-2026-10-22-552"

  Scenario: A note is kept in the acting file for whoever reads it
    When the Mayor takes the seat as "mayor-2026-10-22-552" with the note "seat from event 316733; Mayor 551's window closed by the doctor"
    Then the take succeeded
    And the take's acting file holds "seat from event 316733; Mayor 551's window closed by the doctor"

  Scenario: A window not named for the seat is refused unless a name is given, and nothing is written
    Given a take window "@1006" named "bash" that the command runs in
    When the Mayor takes the seat
    Then the take was refused saying "pass --as"
    And the take's acting file is empty
    And the take window "@1006" is called "bash"

  Scenario: A name that is not the seat's is refused, and the window keeps its name
    When the Mayor takes the seat as "deputy-2026-10-22-1"
    Then the take was refused saying "must begin with mayor-"
    And the take's acting file is empty
    And the take window "@1005" is called "mayor-2026-10-22-550"

  Scenario: Outside tmux the take is refused, because there is no window to name
    Given the command runs in no window
    When the Mayor takes the seat
    Then the take was refused saying "tmux"
    And the take's acting file is empty

  Scenario: Without a session id the take is refused, because the acting file would name no session
    Given the session id is not known
    When the Mayor takes the seat
    Then the take was refused saying "session id"
    And the take's acting file is empty

  Scenario: The doctor's mayor-gone probe says ok for a window whose name differs from the acting name
    Given the Mayor takes the seat as "mayor-2026-10-22-552"
    And something else renames the take window "@1005" to "mayor-2026-10-22-550"
    When the mayor-gone probe looks
    Then the mayor-gone probe says ok

  Scenario: A window id in the acting file does not vouch for a window that is not a Mayor's
    Given the Mayor takes the seat as "mayor-2026-10-22-552"
    And something else renames the take window "@1005" to "scratch"
    When the mayor-gone probe looks
    Then the mayor-gone probe says faulty
