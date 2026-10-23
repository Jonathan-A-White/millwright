Feature: mw doctor unit-failed
  When a unit ends in failure, systemd starts contrib/systemd/mw-unit-failed@.service,
  which runs `mw doctor unit-failed <unit>`. A unit a timer runs fails now and then and
  is clean on its next run, so the Mayor is told of the second failure in a row, not
  the first: the first is recorded, with the end of the unit's journal; a second with no
  clean run between them is mailed to the Mayor naming both, with both journals' last
  lines; a clean run in between starts the count again. A unit no timer runs (a oneshot
  started by hand) is mailed at its first failure.

  Scenario: A timer unit failing once is recorded and no mail is sent
    Given the unit "mw-dispatch.service" is run by a timer
    When the unit "mw-dispatch.service" fails
    Then no mail was sent to the Mayor
    And one failure of "mw-dispatch.service" is recorded

  Scenario: A timer unit failing twice in a row is mailed once, naming both
    Given the unit "mw-dispatch.service" is run by a timer
    And the unit "mw-dispatch.service" failed with the journal "first run: bd sync exited 1"
    When the unit "mw-dispatch.service" fails with the journal "second run: bd sync exited 1"
    Then one mail was sent to the Mayor
    And the mail subject is "unit failed twice in a row: mw-dispatch.service on laptop"
    And the mail is from "doctor@laptop"
    And the mail body says "first run: bd sync exited 1"
    And the mail body says "second run: bd sync exited 1"
    And no failure of "mw-dispatch.service" is recorded

  Scenario: Fail, pass, fail sends no mail
    Given the unit "mw-dispatch.service" is run by a timer
    And the unit "mw-dispatch.service" failed with the journal "first run: bd sync exited 1"
    And the unit "mw-dispatch.service" then ran clean
    When the unit "mw-dispatch.service" fails with the journal "third run: bd sync exited 1"
    Then no mail was sent to the Mayor
    And one failure of "mw-dispatch.service" is recorded
    And the recorded journal of "mw-dispatch.service" says "third run: bd sync exited 1"

  Scenario: A unit no timer runs is mailed at its first failure
    Given the unit "mw-view-follow.service" is run by no timer
    When the unit "mw-view-follow.service" fails with the journal "mw events follow: no ledger"
    Then one mail was sent to the Mayor
    And the mail subject is "unit failed: mw-view-follow.service on laptop"
    And the mail body says "mw events follow: no ledger"
    And no failure of "mw-view-follow.service" is recorded

  Scenario: Two units' failures are counted apart
    Given the unit "mw-dispatch.service" is run by a timer
    And the unit "mw-doctor.service" is run by a timer
    And the unit "mw-dispatch.service" failed with the journal "dispatch broke"
    When the unit "mw-doctor.service" fails with the journal "doctor broke"
    Then no mail was sent to the Mayor
    And one failure of "mw-doctor.service" is recorded
    And one failure of "mw-dispatch.service" is recorded
