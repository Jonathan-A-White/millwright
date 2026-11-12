Feature: mw needs sweep verifies the internal landings itself and names a landing that was reverted
  The Needs you cleanup checked the unverified landings by script. mw needs sweep
  is that check as a command: it reads each unverified landing, tells it apart by
  what git and the bead can show, and with --write comments VERIFIED only on an
  internal landing whose commit is in the rig's main. It writes nothing it cannot
  show, and a landing whose merge was reverted gets one line in one mail.

  Background:
    Given a sweep rig with a git history, a tracker and a mailbox
    And the sweep story "sw-internal" landed 2 days ago with an internal closing step and was merged
    And the sweep story "sw-his" landed 2 days ago with steps for the Governor and was merged
    And the sweep story "sw-reverted" landed 2 days ago with an internal closing step and was merged and then reverted
    And the sweep story "sw-fresh" landed 1 hours ago with an internal closing step and was merged

  Scenario: a dry run prints a line for each unverified landing and writes nothing
    When mw needs sweep runs without --write
    Then the sweep prints a line for "sw-internal" of class "internal" that says it would write
    And the sweep prints a line for "sw-his" of class "no-deploy"
    And the sweep prints a line for "sw-reverted" of class "reverted"
    And the sweep prints a line for "sw-fresh" of class "fresh"
    And the sweep wrote no comment and sent no mail

  Scenario: --write comments the internal landing only and mails the Mayor the revert
    When mw needs sweep runs with --write
    Then the sweep commented only "sw-internal", and its comment opens VERIFIED and says it is not his use
    And the Mayor's mail names "sw-reverted" and the commit that reverted it

  Scenario: a second --write run writes nothing
    When mw needs sweep runs with --write
    And mw needs sweep runs with --write
    Then the sweep wrote 1 comment in all and sent 1 mail in all

  Scenario: a failing tracker write stops the run naming the bead
    Given the tracker refuses to comment on "sw-internal"
    When mw needs sweep runs with --write
    Then the sweep stopped with an error naming "sw-internal"
    And the sweep wrote no comment and sent no mail
