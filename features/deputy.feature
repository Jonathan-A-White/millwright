Feature: mw deputy
  mw deputy brings up this host's Deputy: a thin command over `mw seat up
  deputy`, as mw millhand is for the Millhand. The Deputy runs at high effort on
  config `deputy_model` (default Sonnet), and the idle reaper is armed, so the
  window closes itself once the Deputy has handed off. The kickoff tells it to
  arm mw events wait on its own mailbox, work the mail, report by mail and hand off
  when idle, and says the --reason it was woken for.

  If a Deputy window is already open, mw starts nothing, says so in one line and
  leaves with status 8, so the Mayor can mail the Deputy and fire, knowing one
  is there to read it. mw refuses to start a Deputy that has no charter.

  A Deputy that is up and sitting idle at an empty input line, with unread mail
  in its box, is woken: mw types the mail nudge into its pane, as the Mayor's
  notifier does, and says so. A pane that is busy is left alone and mw says the
  mail waits (status 8 as well, for nothing was started). With no mail to wake
  it for, an idle Deputy is just already up.

  A Deputy whose session has already handed off — a handoff written after its
  window was opened — will not work the mail, so nudging it leaves the mail
  unread. When its pane is idle, mw closes that window and starts a fresh
  Deputy, which reads the mail from its handoff. A handed-off Deputy that is
  still busy is left alone, as any busy Deputy is.

  Background:
    Given a vault holding the "deputy" seat
    And the "deputy" seat's charter
    And the "deputy" seat has written the handoffs:
      | 2026-09-30-01 |
    And today is "2026-09-30"

  Scenario: The Deputy is brought up on Sonnet at high effort with the idle reaper armed
    When mw deputy is run
    Then mw deputy succeeds
    And exactly one window was opened
    And the window is named "deputy-2026-09-30-02"
    And the window's command carries:
      | --model sonnet |
      | --effort high  |
    And a reaper was armed on the window "deputy-2026-09-30-02" in when-idle mode for the "deputy" seat

  Scenario: The kickoff says to arm mw events wait on the Deputy's mailbox, work the mail and hand off when idle
    When mw deputy is run for the reason "the Mayor mailed a task"
    Then mw deputy succeeds
    And the kickoff prompt of the window holds:
      | mw events wait --for deputy --kinds mail |
      | report by mail                           |
      | hand off                                 |
      | the Mayor mailed a task                  |
      | seats/deputy/handoffs/2026-09-30-01.md   |

  Scenario: A Deputy window already up starts nothing and leaves with status 8
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    When mw deputy is run
    Then mw deputy is refused saying the Deputy is already up in "deputy-2026-09-30-02"
    And mw deputy leaves with the status 8
    And no window was opened
    And no reaper was armed

  Scenario: An idle Deputy with unread mail is woken by the mail nudge
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    And the Deputy's box holds 2 unread messages
    When mw deputy is run
    Then mw deputy succeeds
    And mw deputy says it nudged the Deputy in the window "deputy-2026-09-30-02"
    And the line "New mail for deputy: 2 message(s). Run bd mail inbox." was typed into the window "deputy-2026-09-30-02"
    And no window was opened
    And no reaper was armed

  Scenario: The mail nudge whose Enter was lost is submitted with a second Enter, and the Deputy says so
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    And the Deputy's box holds 2 unread messages
    And the window "deputy-2026-09-30-02" loses the first Enter key it is sent
    When mw deputy is run
    Then mw deputy succeeds
    And mw deputy says it nudged the Deputy in the window "deputy-2026-09-30-02"
    And mw deputy says it pressed Enter again
    And the Enter key was pressed 2 times in the window "deputy-2026-09-30-02"

  Scenario: A pane that never takes Enter is retried once and the Deputy says the line is on its input line still
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    And the Deputy's box holds 2 unread messages
    And the window "deputy-2026-09-30-02" loses every Enter key it is sent
    When mw deputy is run
    Then mw deputy succeeds
    And mw deputy says the nudge is on its input line still
    And the Enter key was pressed 2 times in the window "deputy-2026-09-30-02"

  Scenario: A busy Deputy is left alone and told of the mail waiting
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    And the pane of the window "deputy-2026-09-30-02" is busy
    And the Deputy's box holds 1 unread messages
    When mw deputy is run
    Then mw deputy is refused saying the Deputy is busy in "deputy-2026-09-30-02" and the mail waits
    And mw deputy leaves with the status 8
    And nothing was typed into the window "deputy-2026-09-30-02"
    And no window was opened

  Scenario: An idle Deputy that has handed off is closed and a fresh Deputy is started
    Given the window "deputy-2026-09-29-02" was opened at "2026-09-29T08:00:00Z"
    And the "deputy" seat's newest handoff was written at "2026-09-29T09:00:00Z"
    And the Deputy's box holds 2 unread messages
    When mw deputy is run
    Then mw deputy succeeds
    And the window "deputy-2026-09-29-02" was closed
    And exactly one window was opened
    And seat up says it started the seat in the window "deputy-2026-09-30-02"
    And a reaper was armed on the window "deputy-2026-09-30-02" in when-idle mode for the "deputy" seat

  Scenario: A Deputy that has handed off but is still busy is left alone
    Given the window "deputy-2026-09-29-02" was opened at "2026-09-29T08:00:00Z"
    And the "deputy" seat's newest handoff was written at "2026-09-29T09:00:00Z"
    And the pane of the window "deputy-2026-09-29-02" is busy
    And the Deputy's box holds 1 unread messages
    When mw deputy is run
    Then mw deputy is refused saying the Deputy is busy in "deputy-2026-09-29-02" and the mail waits
    And the window "deputy-2026-09-29-02" was not closed
    And no window was opened

  Scenario: An idle Deputy with no unread mail is already up and is typed nothing
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    When mw deputy is run
    Then mw deputy is refused saying the Deputy is already up in "deputy-2026-09-30-02"
    And nothing was typed into the window "deputy-2026-09-30-02"

  Scenario: Another seat's window does not count as the Deputy's
    Given the window "millhand-2026-09-30-05" was opened at "2026-09-30T08:00:00Z"
    When mw deputy is run
    Then mw deputy succeeds
    And exactly one window was opened

  Scenario: A Deputy with no charter is refused and starts nothing
    Given the "deputy" seat has no charter
    When mw deputy is run
    Then mw deputy is refused saying there is no charter
    And mw deputy leaves with the status 1
    And no window was opened

  Scenario: The config key deputy_model overrides the model
    Given the config file sets "deputy_model" to "haiku"
    When mw deputy is run
    Then mw deputy succeeds
    And the window's command carries:
      | --model haiku |
