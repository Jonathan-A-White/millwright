Feature: mw seat reap
  A finished session's window closes itself. mw seat reap is a detached,
  zero-token watcher on one window that looks every half minute and closes the
  window when its session is done: never before, and never over anything typed
  on its input line. A session ends its own window; no session closes another's.

  In successor mode, the default, the window closes once the seat's acting file
  names someone else than it did when the watch was armed, that someone's window
  is open, and the pane is idle — an empty input line and nothing running — on
  two looks in a row. In idle mode, for a session with nobody to hand over to,
  the window closes once a handoff newer than the window has been written and
  the pane is idle on two looks in a row.

  A seat with a mailbox, the Deputy's, is not closed over unread mail. In idle
  mode, once the window would close, the reaper counts the box's unread mail:
  with some, it types the mail nudge into the idle pane, logs that it nudged and
  goes on waiting, closing only for a handoff written after the nudge with the
  box empty; a count that fails closes nothing and is tried again.

  The watch gives up after its limit and says so. Arming, closing and giving up
  each append one dated line to the seat's reaper log in the vault.

  Background:
    Given a vault for the "mayor" seat
    And the clock reads "2026-09-19T12:00:00Z"
    And the window "@3" named "mayor-2026-09-19-12" was opened at "2026-09-19T10:00:00Z"
    And the acting file says "Mayor after handoff 11 (tmux window 3 'mayor-2026-09-19-12'), since 2026-09-19T10:00:05Z"

  Scenario: Successor mode closes the window once the seat is held by someone else whose window is open, and the pane is idle twice
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    And before look 2 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 3 hours
    Then the reaper closed the window "@3" on look 3
    And the window "@4" is still open
    And the reaper log holds 2 lines
    And the reaper log's line 1 says "reap @3: armed"
    And the reaper log's line 2 says "reap @3: closed"

  Scenario: Successor mode waits while the acting file still says what it said when it was armed
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper gave up
    And the window "@3" is still open

  Scenario: Successor mode waits while the acting file names no window that is open
    Given before look 2 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper gave up

  Scenario: Successor mode closes once the successor's window opens
    Given before look 2 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    And before look 4 the window "@4" named "mayor-2026-09-19-13" is opened
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 5

  Scenario: One idle look is not enough, and a pane that starts working starts the count again
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    And before look 1 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    And before look 2 the pane of "@3" is working
    And before look 3 the pane of "@3" is idle
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 4

  Scenario: Text on the input line prevents closing in successor mode
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    And before look 1 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    And the pane of "@3" has text on its input line
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the window "@3" is still open
    And the reaper gave up

  Scenario: Text typed and then cleared delays closing until two idle looks follow
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    And before look 1 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    And the pane of "@3" has text on its input line
    And before look 3 the pane of "@3" is idle
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 4

  Scenario: Idle mode waits for a handoff newer than the window
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T09:00:00Z"
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper gave up

  Scenario: Idle mode closes on two idle looks once a handoff newer than the window exists
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T09:00:00Z"
    And before look 2 the seat writes the handoff "2026-09-19-12"
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 3
    And the reaper log holds 2 lines
    And the reaper log's line 2 says "reap @3: closed"

  Scenario: Idle mode does not close a window whose input line holds text
    Given the seat has written the handoff "2026-09-19-12" at "2026-09-19T11:00:00Z"
    And the pane of "@3" has text on its input line
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the window "@3" is still open
    And the reaper gave up

  Scenario: Idle mode does not close a window that is still working
    Given the seat has written the handoff "2026-09-19-12" at "2026-09-19T11:00:00Z"
    And the pane of "@3" is working
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper gave up

  Scenario: Idle mode nudges a seat whose box holds unread mail instead of closing, and closes once it hands off again with the box empty
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T11:00:00Z"
    And the seat has a mailbox "deputy" holding 2 unread messages
    And before look 4 the mailbox is empty
    And before look 4 the seat writes the handoff "2026-09-19-13"
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 5
    And the nudge "New mail for deputy: 2 message(s). Run bd mail inbox." was typed into "@3" once
    And the reaper log holds 3 lines
    And the reaper log's line 2 says "reap @3: nudged: 2 unread mail(s) for deputy; not closing"
    And the reaper log's line 3 says "reap @3: closed"

  Scenario: Idle mode does not close a seat whose box still holds mail, and nudges once
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T11:00:00Z"
    And the seat has a mailbox "deputy" holding 2 unread messages
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the nudge "New mail for deputy: 2 message(s). Run bd mail inbox." was typed into "@3" once
    And the reaper gave up

  Scenario: Idle mode does not close a seat whose mail count fails
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T11:00:00Z"
    And the seat has a mailbox "deputy" whose count fails
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper log's line 2 says "could not be counted"
    And the reaper gave up

  Scenario: A seat with no mailbox is closed over no mail
    Given the seat has written the handoff "2026-09-19-11" at "2026-09-19T11:00:00Z"
    When the reaper watches "@3" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed the window "@3" on look 2

  Scenario: A window nothing can date is treated as opened when the watch was armed
    Given the window "@5" named "millhand-2026-09-19-01" of no known age
    And the seat has written the handoff "2026-09-19-12" at "2026-09-19T11:00:00Z"
    When the reaper watches "@5" in idle mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper gave up

  Scenario: The limit ends the watch with one logged line saying so
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 3 hours
    Then the reaper gave up
    And the reaper closed no window
    And the reaper log holds 2 lines
    And the reaper log's line 2 says "reap @3: gave up after 3h0m0s without closing anything"
    And the reaper says "gave up after 3h0m0s"

  Scenario: A window closed by someone else is left at that, and logged
    Given before look 2 the window "@3" is closed by someone else
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 10 minutes
    Then the reaper closed no window
    And the reaper log holds 2 lines
    And the reaper log's line 2 says "reap @3: the window is already gone; nothing to do"

  Scenario: Every line of the log begins with the date it was written
    Given the window "@4" named "mayor-2026-09-19-13" was opened at "2026-09-19T11:59:00Z"
    And before look 2 the acting file says "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since 2026-09-19T12:01:00Z"
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 3 hours
    Then the reaper log's line 1 begins "2026-09-19T12:00:00Z "
    And the reaper log's line 2 begins "2026-09-19T12:01:30Z "

  Scenario: What is already in the log is kept
    Given the reaper log already holds the line "2026-09-18T08:00:00Z reap @1: closed: an earlier watch"
    When the reaper watches "@3" in successor mode, looking every 30 seconds for up to 2 minutes
    Then the reaper log holds 3 lines
    And the reaper log's line 1 says "an earlier watch"

  Scenario: A watch with no window to watch is refused before anything is logged
    When the reaper watches "" in successor mode, looking every 30 seconds for up to 3 hours
    Then the reaper is refused saying which window it is to watch
    And the reaper log holds 0 lines
