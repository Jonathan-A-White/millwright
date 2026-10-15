Feature: A close-out waits out a live holder of the merge slot

  A close-out lands its story under its rig's merge slot, and waits for it when
  another close-out has it. It used to give up on one holder after a per-holder
  limit even when that holder's process was alive and still in its close-out,
  re-running the rig's tests on a host under load: mw-emk3xt.1 stopped after 52
  minutes on mw-5r3p30.318's live close-out, and its finished work cost a fresh
  attempt (mw-gq6.467). A holder whose process is alive is waited on up to the
  overall most a close-out waits; the per-holder limit is only for a holder whose
  process is gone. Time here is a wound clock: nothing truly waits.

  Background:
    Given a rig whose close-outs wait 20 minutes on one holder of its merge slot and 2 hours in all

  Scenario: A live holder past the per-holder limit does not end the waiter
    Given the merge slot is held by "builder@laptop closing out mw-5r3p30.318", whose process is alive
    And that holder lets go after 52 minutes
    When a close-out of "mw-emk3xt.1" takes the merge slot
    Then the close-out took the merge slot after 52 minutes

  Scenario: A live holder that never lets go ends the waiter at the overall most, saying so
    Given the merge slot is held by "builder@laptop closing out mw-5r3p30.318", whose process is alive
    When a close-out of "mw-emk3xt.1" takes the merge slot
    Then the close-out did not get the merge slot after 2 hours, the overall most a close-out waits
    And the failure names "builder@laptop closing out mw-5r3p30.318"

  Scenario: A dead holder's slot is taken at once, as the kernel gave its lock back
    Given the merge slot names "builder@laptop closing out mw-5r3p30.318", whose process is gone
    When a close-out of "mw-emk3xt.1" takes the merge slot
    Then the close-out took the merge slot after 0 minutes

  Scenario: A slot still locked under a holder whose process is gone is given up on after the per-holder limit
    Given the merge slot is held by "builder@laptop closing out mw-5r3p30.318", whose process is gone
    When a close-out of "mw-emk3xt.1" takes the merge slot
    Then the close-out did not get the merge slot after 20 minutes, one holder having had it that long
    And the failure names "builder@laptop closing out mw-5r3p30.318"
