Feature: mw talk wait
  mw talk wait is a zero-token blocking command, for the Mayor's harness to run
  in the background. It holds the postern backend's event stream open,
  reconnecting when it drops, and on a message event pages the records since
  its own cursor. It ends at the first talk turn the Governor sent to the
  Mayor's key, printing the turn (talk id, turn, model, cut and text), how long
  from the event to the print, and any new mail the Deputy sent the Mayor. It
  ends on its time limit like contrib/mail-wait. It also ends at a new postern
  message for the Mayor's key that mw postern inbox has not read, printing its
  channel, txid and first line; a Governor turn wins if both arrive. Talk
  records to other keys, or from anyone but the Governor, do not end it; and mw
  postern inbox and its --unread-count leave talk records alone, so that a turn
  never wakes the Mayor twice.

  Background:
    Given a postern backend that streams its events
    And mw talk wait trusts "governor-key" as the Governor's key

  Scenario: it ends within a second of the Governor's turn, printing it
    Given mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "What landed today?" is indexed, on model "sonnet", cutting the last answer
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-7 turn 3"
    And the wait printed "model sonnet"
    And the wait printed "hand off, then: bin/respawn-mayor high claude-sonnet-5-5"
    And the wait printed "cut yes"
    And the wait printed "What landed today?"
    And the wait printed an index-to-print time in milliseconds

  Scenario: a talk opened from a bead says what it is about
    Given mw talk wait is armed
    When the Governor's turn 1 of talk "talk-10" saying "Why is this blocked?" is indexed, about the bead "mw-xyz.1" titled "Fix the login form"
    Then mw talk wait ends within 1 second
    And the wait printed "text: Why is this blocked?"
    And the wait printed "about: bead mw-xyz.1 Fix the login form"

  Scenario: a talk record with no about prints no about line
    Given mw talk wait is armed
    When the Governor's turn 1 of talk "talk-11" saying "Plain question" is indexed
    Then mw talk wait ends within 1 second
    And the wait did not print "about:"

  Scenario: a turn that changes no model and cuts nothing says so
    Given mw talk wait is armed
    When the Governor's turn 1 of talk "talk-8" saying "Hello" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "model unchanged"
    And the wait did not print "bin/respawn-mayor"
    And the wait printed "cut no"

  Scenario: it ignores records that are not the Governor's talk turns to the Mayor
    Given mw talk wait is armed
    When a talk turn to another key is indexed
    And a talk turn from "stranger-key" to the Mayor is indexed
    And a talk record with role "answer" from the Governor to the Mayor is indexed
    Then mw talk wait is still waiting
    When the Governor's turn 2 of talk "talk-7" saying "The real one" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "The real one"

  Scenario: a new postern message to the Mayor ends it, printing its channel, txid and first line
    Given mw talk wait is armed
    When a "message" record to the Mayor is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "new postern message"
    And the wait printed "channel general"
    And the wait printed "direct:1"
    And the wait printed "message text"

  Scenario: a postern message mw postern inbox has read does not wake it
    Given mw talk wait's cursor is at the start of the index
    And a "message" record to the Mayor has been indexed
    When mw postern inbox is run against the backend
    And mw talk wait is armed
    Then mw talk wait is still waiting

  Scenario: a Governor turn that arrives with a postern message wins
    Given mw talk wait's cursor is at the start of the index
    And a "message" record to the Mayor has been indexed
    And the Governor's turn 2 of talk "talk-6" saying "The turn wins" has been indexed
    When mw talk wait is armed
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-6 turn 2"
    And the wait printed "The turn wins"
    And the wait printed "new postern message"

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

  Scenario: mw postern inbox reads past the first page of records
    Given 450 talk turns to another key have been indexed
    And a "message" record to the Mayor has been indexed
    When mw postern inbox --unread-count is run against the backend
    Then the backend inbox counts 1 unread

  Scenario: the Governor's call request ends it at once, printing it
    Given mw talk wait is armed
    When the Governor's call request "Call me" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "call direct:1 at "
    And the wait printed ": Call me"
    And the wait did not print "talk "

  Scenario: the Governor's later on a ring ends it, naming the ring
    Given mw talk wait is armed
    When the Governor's later on the ring "direct:ring9" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "later direct:ring9"

  Scenario: a call record to another key, or from anyone else, does not end it
    Given mw talk wait is armed
    When a call request to another key is indexed
    And a call request from "stranger-key" to the Mayor is indexed
    Then mw talk wait is still waiting

  Scenario: mw postern inbox leaves call records alone
    Given the Governor's call request "Call me" has been indexed
    And a "message" record to the Mayor has been indexed
    When mw postern inbox --unread-count is run against the backend
    Then the backend inbox counts 1 unread

  Scenario: it remembers which way the Governor's newest record came, for mw talk call
    Given mw talk wait is armed
    When the Governor's call request "Call me" is indexed with the bare txid "ab12cd34"
    Then mw talk wait ends within 1 second
    And mw talk wait remembered the Governor's newest record came by "chain"

  Scenario: a record that came direct is remembered as direct
    Given mw talk wait is armed
    When the Governor's turn 1 of talk "talk-9" saying "Hello" is indexed
    Then mw talk wait ends within 1 second
    And mw talk wait remembered the Governor's newest record came by "direct"

  # Ideas only (postern's docs/protocol.md section 25): while the Governor's key
  # is in the mode, what he sends is kept, not acted on. The wait says so when it
  # starts, keeps each turn, call request and message of his in the held file,
  # answers it at once from the Mayor's key without waking anyone, and goes on
  # waiting. It still ends on its limit, on a handover, and on mail or a message
  # from anyone else. The stream's ideas-only event keeps its note of the mode level.

  Scenario: With Ideas only on, the Governor's turn is kept and answered, and the wait goes on waiting
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait has a time limit of 1200 milliseconds
    And mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "An idea: a quieter dashboard" is indexed
    Then mw talk wait is still waiting
    When the wait comes to its time limit
    Then the wait printed "Ideas only until 21:00 UTC (0 ideas held)"
    And the wait printed "1 idea held"
    And the wait did not print "talk talk-7 turn 3"
    And the wait did not print "quieter dashboard"
    And Ideas only kept "direct:1" from the talk, saying "An idea: a quieter dashboard"
    And the Governor was answered in talk "talk-7" turn 3 saying "Recorded for after 21:00 UTC"
    And the Governor was answered 1 time in all

  Scenario: With Ideas only on, a call request is kept and answered in the general thread
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait has a time limit of 1200 milliseconds
    And mw talk wait is armed
    When the Governor's call request "Call me about the roof" is indexed
    Then mw talk wait is still waiting
    When the wait comes to its time limit
    Then the wait did not print "call direct:1"
    And Ideas only kept "direct:1" from the call, saying "Call me about the roof"
    And mw talk wait told the Governor "Recorded for after 21:00 UTC" re "direct:1"

  Scenario: With Ideas only on, a message of his is kept and answered, and does not end the wait
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait has a time limit of 1200 milliseconds
    And mw talk wait is armed
    When a "message" record to the Mayor is indexed
    Then mw talk wait is still waiting
    When the wait comes to its time limit
    Then the wait did not print "new postern message"
    And Ideas only kept "direct:1" from the general, saying "message text"
    And mw talk wait told the Governor "Recorded for after 21:00 UTC" re "direct:1"

  Scenario: A message the hook has already kept is not answered a second time
    Given Ideas only is on until 9 hours on for mw talk wait
    And the hook has already kept "direct:1"
    And mw talk wait has a time limit of 1200 milliseconds
    And mw talk wait is armed
    When a "message" record to the Mayor is indexed
    And the wait comes to its time limit
    Then the Governor was answered 0 times in all
    And Ideas only is keeping 1 idea

  Scenario: A message from anyone but the Governor still ends the wait while Ideas only is on
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait is armed
    When a "message" record from "stranger-key" to the Mayor is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "new postern message"
    And Ideas only is keeping 0 ideas

  Scenario: The stream's ideas-only event ends the mode in the note, and the next turn is the Mayor's again
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait is armed
    When the backend says the Governor's Ideas only has ended
    Then the note of Ideas only comes to say it is off
    When the Governor's turn 4 of talk "talk-8" saying "Back again" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-8 turn 4"
    And the Governor was answered 0 times in all

  Scenario: The stream's ideas-only event for another key changes nothing
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait is armed
    When the backend says the Ideas only of "another-key" has ended
    And the Governor's turn 4 of talk "talk-8" saying "Still away" is indexed
    Then mw talk wait is still waiting

  Scenario: With Ideas only off the turn ends the wait as ever
    Given Ideas only is off for mw talk wait
    And mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "Hello" is indexed
    Then mw talk wait ends within 1 second
    And the wait printed "talk talk-7 turn 3"
    And the Governor was answered 0 times in all

  # The end of Ideas only: the wait gives the ideas back, once, and ends printing
  # them, so that the Mayor's harness wakes to them. It does so on the stream's
  # ideas-only event, and on the clock passing the mode's end when no event comes.

  Scenario: The stream's ideas-only event ends the mode: one card is sent and the wait ends printing the ideas
    Given Ideas only is on until 9 hours on for mw talk wait
    And the hook has already kept "direct:9"
    And mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "An idea: a quieter dashboard" is indexed
    Then the wait has kept 2 ideas
    When the backend says the Governor's Ideas only has ended
    Then mw talk wait ends within 2 seconds
    And the wait printed "Ideas only ended"
    And the wait printed "2 ideas"
    And the wait printed "An idea: a quieter dashboard"
    And the wait printed "direct:9"
    And one card titled "Your ideas from Ideas only (2)" was sent to the Governor by the wait
    And Ideas only is keeping 0 ideas

  Scenario: The time the ideas took to be kept does not count against the wait ending on the event
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait is armed
    When the Governor's turn 3 of talk "talk-7" saying "An idea: a quieter dashboard" is indexed
    Then the wait has kept 1 idea
    When 1500 milliseconds go by on the host running mw talk wait
    And the backend says the Governor's Ideas only has ended
    Then mw talk wait ends within 2 seconds
    And the wait printed "Ideas only ended"

  Scenario: The clock passing the end of Ideas only ends the wait the same way, with no event
    Given Ideas only is on until 9 hours on for mw talk wait
    And the hook has already kept "direct:9"
    And mw talk wait is armed
    When the clock of Ideas only passes 9 hours and 1 second on for mw talk wait
    Then mw talk wait ends within 2 seconds
    And the wait printed "Ideas only ended"
    And the wait printed "1 idea"
    And the wait printed "direct:9"
    And one card titled "Your ideas from Ideas only (1)" was sent to the Governor by the wait
    And Ideas only is keeping 0 ideas

  Scenario: An end with nothing held says so and the wait goes on
    Given Ideas only is on until 9 hours on for mw talk wait
    And mw talk wait has a time limit of 1200 milliseconds
    And mw talk wait is armed
    When the backend says the Governor's Ideas only has ended
    Then mw talk wait is still waiting
    When the wait comes to its time limit
    Then the wait printed "Ideas only ended: nothing held"
    And no card was sent to the Governor by the wait

  Scenario: A wait armed after the end gives back what was left held
    Given Ideas only is off for mw talk wait
    And the hook has already kept "direct:9"
    And mw talk wait is armed
    Then mw talk wait ends within 2 seconds
    And the wait printed "Ideas only ended"
    And one card titled "Your ideas from Ideas only (1)" was sent to the Governor by the wait
