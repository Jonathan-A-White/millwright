Feature: mw deputy
  mw deputy brings up this host's Deputy: a thin command over `mw seat up
  deputy`, as mw millhand is for the Millhand. The Deputy runs at high effort on
  config `deputy_model` (default Sonnet), and the idle reaper is armed, so the
  window closes itself once the Deputy has handed off. The kickoff tells it to
  arm mail-wait on its own mailbox, work the mail, report by mail and hand off
  when idle, and says the --reason it was woken for.

  If a Deputy window is already open, mw starts nothing, says so in one line and
  leaves with status 8, so the Mayor can mail the Deputy and fire, knowing one
  is there to read it. mw refuses to start a Deputy that has no charter.

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

  Scenario: The kickoff says to arm mail-wait on the Deputy's mailbox, work the mail and hand off when idle
    When mw deputy is run for the reason "the Mayor mailed a task"
    Then mw deputy succeeds
    And the kickoff prompt of the window holds:
      | MW_MAIL_MAILBOX=deputy                  |
      | mail-wait                               |
      | report by mail                          |
      | hand off                                |
      | the Mayor mailed a task                 |
      | seats/deputy/handoffs/2026-09-30-01.md  |

  Scenario: A Deputy window already up starts nothing and leaves with status 8
    Given the window "deputy-2026-09-30-02" was opened at "2026-09-30T08:00:00Z"
    When mw deputy is run
    Then mw deputy is refused saying the Deputy is already up in "deputy-2026-09-30-02"
    And mw deputy leaves with the status 8
    And no window was opened
    And no reaper was armed

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
