Feature: mw after-landing runs a rig's after-landing command again by hand
  A landing runs the command the [after_landing] table names for its rig once,
  and a deploy that failed on a passing network fault is not run again by
  anything but a landing of some other story. `mw after-landing <rig>` runs the
  rig's command now, in the rig's checkout, under the rig's after-landing lock
  and limits, and says ok or what failed.

  Scenario: mw after-landing runs the rig's command and says ok
    Given a rig "millwright" whose after-landing command succeeds
    When mw after-landing is run for "millwright"
    Then the command ran once, in the rig checkout
    And mw after-landing printed: after landing: <the command>: ok
    And mw after-landing succeeded

  Scenario: mw after-landing says what failed, and fails
    Given a rig "millwright" whose after-landing command prints "make: *** No rule to make target 'build'" and exits 2
    When mw after-landing is run for "millwright"
    Then the command ran once, in the rig checkout
    And mw after-landing failed, saying: after landing: <the command>: exit status 2: make: *** No rule to make target 'build'

  Scenario: mw after-landing runs the command again after a passing network fault, and says it did
    Given a rig "millwright" whose after-landing command fails the first time on a name-resolution fault, exit 255, and then succeeds
    And the command is run again after 50 milliseconds
    When mw after-landing is run for "millwright"
    Then the command ran twice, in the rig checkout
    And mw after-landing printed: after landing: <the command>: retried once after 50ms
    And mw after-landing printed: after landing: <the command>: ok
    And mw after-landing succeeded

  Scenario: mw after-landing waits for a landing that holds the rig's after-landing lock
    Given a rig "millwright" whose after-landing command succeeds
    And a landing holds the rig's after-landing lock for 400 milliseconds
    When mw after-landing is run for "millwright"
    Then the command ran once, in the rig checkout
    And the command did not start until the landing let go
    And mw after-landing succeeded

  Scenario: mw after-landing refuses a rig this host names no command for
    Given a rig "millwright" whose after-landing command succeeds
    When mw after-landing is run for "elsewhere"
    Then the command did not run
    And mw after-landing failed, saying: the rig elsewhere is not one this host has checked out

  Scenario: mw after-landing refuses a rig the host's table names no command for
    Given a rig "millwright" with no after-landing command
    When mw after-landing is run for "millwright"
    Then the command did not run
    And mw after-landing failed, saying: this host names no after-landing command for millwright

  Scenario: a millwright landing's grist smoke is made by the mw the landing built, not by the mw the landing runs in
    Given a rig "millwright" whose landing changes how the grist smoke judges the app "cairn"
    When the landing of "millwright" makes its grist smoke of "cairn"
    Then the mw the landing built made the smoke of "cairn"
    And the grist smoke of "cairn" passed
    And the mw the landing runs in smoked nothing itself

  Scenario: a landing of another rig makes its grist smoke with the mw the landing runs in
    Given a rig "cairn" whose landing changes how the grist smoke judges the app "cairn"
    When the landing of "cairn" makes its grist smoke of "cairn"
    Then the mw the landing runs in made the smoke of "cairn"
    And the grist smoke of "cairn" failed

  Scenario: a millwright landing whose build left no mw makes its grist smoke with the mw it runs in
    Given a rig "millwright" whose build left no mw and whose running mw fails the smoke of "cairn"
    When the landing of "millwright" makes its grist smoke of "cairn"
    Then the mw the landing runs in made the smoke of "cairn"
    And the grist smoke of "cairn" failed
