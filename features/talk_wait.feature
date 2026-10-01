Feature: mw talk wait
  mw talk wait is a zero-token blocking command, for the Mayor's harness to run
  in the background. It holds the postern backend's event stream open,
  reconnecting when it drops, and on a message event pages the records since
  its own cursor. It ends at the first talk turn the Governor sent to the
  Mayor's key, printing the turn (talk id, turn, model, cut and text), how long
  from the event to the print, and any new mail the Deputy sent the Mayor. It
  ends on its time limit like contrib/mail-wait. Records of other classes, to
  other keys, or from anyone but the Governor do not end it; and mw postern
  inbox and its --unread-count leave talk records alone, so that a turn never
  wakes the Mayor twice.

  Background:
    Given a postern backend that streams its events
    And mw talk wait trusts "governor-key" as the Governor's key

  Scenario: it ends within a second of the Governor's turn, printing it
    Given mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "What landed today?" is indexed, on model "sonnet", cutting the last answer
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-7 turn 3"
    And the wait printed "model sonnet"
    And the wait printed "cut yes"
    And the wait printed "What landed today?"
    And the wait printed an index-to-print time in milliseconds

  Scenario: a turn that changes no model and cuts nothing says so
    Given mw talk wait is armed
    When the Governor's turn 1 of talk "talk-8" saying "Hello" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "model unchanged"
    And the wait printed "cut no"

  Scenario: it ignores records that are not the Governor's talk turns to the Mayor
    Given mw talk wait is armed
    When a "message" record to the Mayor is indexed
    And a talk turn to another key is indexed
    And a talk turn from "stranger-key" to the Mayor is indexed
    And a talk record with role "answer" from the Governor to the Mayor is indexed
    Then mw talk wait is still waiting
    When the Governor's turn 2 of talk "talk-7" saying "The real one" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "The real one"

  Scenario: it resumes from its cursor, so one turn wakes it once
    Given mw talk wait's cursor is at the start of the index
    And the Governor's turn 1 of talk "talk-9" saying "First turn" has been indexed
    And the Governor's turn 2 of talk "talk-9" saying "Second turn" has been indexed
    When mw talk wait is armed
    Then mw talk wait ends within 1 second
    And the wait printed "First turn"
    And the wait did not print "Second turn"
    When mw talk wait is armed again
    Then mw talk wait ends within 1 second
    And the wait printed "Second turn"
    And the wait did not print "First turn"

  Scenario: it ignores the turns that were indexed before it first ran
    Given the Governor's turn 1 of talk "talk-1" saying "An old turn" has been indexed
    And mw talk wait is armed
    Then mw talk wait is still waiting
    When the Governor's turn 2 of talk "talk-1" saying "A new turn" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "A new turn"
    And the wait did not print "An old turn"

  Scenario: it reconnects when the stream drops
    Given mw talk wait is armed
    When the stream drops
    And the Governor's turn 4 of talk "talk-2" saying "After the drop" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "After the drop"

  Scenario: the Governor's end of a talk wakes it too
    Given mw talk wait is armed
    When the Governor ends talk "talk-3"
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-3"
    And the wait printed "role end"

  Scenario: it ends on its time limit with nothing to say
    Given mw talk wait has a time limit of 300 milliseconds
    When mw talk wait is armed
    Then mw talk wait ends within 2 seconds
    And the wait printed "the wait ended on its time limit"

  Scenario: it reports new mail the Deputy sent the Mayor once
    Given the Deputy has mailed the Mayor "Released mw-abc.1"
    And the Builder has mailed the Mayor "Done mw-abc.2"
    And mw talk wait is armed
    When the Governor's turn 5 of talk "talk-4" saying "Anything new?" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "Released mw-abc.1"
    And the wait did not print "Done mw-abc.2"
    When mw talk wait is armed again
    And the Governor's turn 6 of talk "talk-4" saying "And now?" is indexed
    Then mw talk wait ends within 1 second
    And the wait did not print "Released mw-abc.1"

  Scenario: mw postern inbox --unread-count and a read ignore talk records
    Given the Governor's turn 1 of talk "talk-5" saying "A talk turn" has been indexed
    And a "message" record to the Mayor has been indexed
    When mw postern inbox --unread-count is run against the backend
    Then the backend inbox counts 1 unread
    When mw postern inbox is run against the backend
    Then the backend inbox printed "message text"
    And the backend inbox did not print "A talk turn"

  Scenario: with a holding reply set, a Governor turn is answered at once, before the wait prints it
    Given mw talk wait has the holding reply "One moment."
    And mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "What landed today?" is indexed
    Then mw talk wait ends within 1 second
    And the wait sent one holding record "One moment." for talk "talk-7" turn 3 before it printed
    And the wait printed "What landed today?"
    And the wait printed a holding-sent time in milliseconds

  Scenario: with no holding reply set, nothing is sent
    Given mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "What landed today?" is indexed
    Then mw talk wait ends within 1 second
    And the wait sent no holding record
    And the wait did not print "holding sent"

  Scenario: the Governor's end of a talk gets no holding reply
    Given mw talk wait has the holding reply "One moment."
    And mw talk wait is armed
    When the Governor ends talk "talk-3"
    Then mw talk wait ends within 1 second
    And the wait sent no holding record
    And the wait did not print "holding sent"
