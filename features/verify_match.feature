Feature: mw verify match proposes what the Governor's use shows met
  One bounded agent call reads the open demos' and the unverified landings'
  check text beside his saved messages and pictures. A match at or over the
  confidence becomes a PROPOSED comment on its bead and one mail to the Mayor;
  nothing closes, and no comment opens with the word VERIFIED.

  Background:
    Given a verify fixture with the demos "d-green" and "d-blue"
    And a verify landing "s-lamp" closed a day ago whose closing comment says how to check it
    And a saved message of his "direct-aaa" and a saved picture "direct-bbb"

  Scenario: a match over the confidence is proposed and the one under it is only logged
    Given the verify stand-in answers "d-green" from "direct:aaa" showing "the green tick after Save" at 0.9
    And the verify stand-in answers "d-blue" from "direct:bbb" showing "a blue shape, maybe" at 0.5
    When mw verify match runs
    Then the verify bead "d-green" has one comment opening PROPOSED that names "direct:aaa" and "the green tick after Save"
    And the verify bead "d-blue" has no new comment
    And the Mayor has one verify mail saying "Proposed: 1 verified by his use" with the line "mw verify confirm d-green --txid direct:aaa"
    And the verify run's record keeps the answer for "d-blue" as left out
    And the verify stand-in was called once
    And no verify bead was closed

  Scenario: a dry run prints what it would match and calls no agent
    When mw verify match runs as a dry run
    Then the verify output names "d-green", "d-blue", "s-lamp", "direct:aaa" and "direct:bbb" and the dir's size
    And the verify stand-in was not called
    And no verify comment was written
    And the Mayor has no verify mail

  Scenario: a picture sent before the landing is dropped and counted, one sent after it still proposes
    Given a verify landing "s-late" closed 90 minutes ago whose closing comment says how to check it
    And the verify stand-in answers "s-late" from "direct:bbb" showing "the picture of the new reading" at 0.9
    And the verify stand-in answers "s-late" from "direct:aaa" showing "he used the new reading" at 0.9
    When mw verify match runs
    Then the verify bead "s-late" has one comment opening PROPOSED that names "direct:aaa" and "he used the new reading"
    And the verify run's record keeps the answer for "s-late" from "direct:bbb" as dropped for being older than the landing
    And the verify output says "1 dropped"
