Feature: the follower matches a picture of his as soon as it saves it
  When the follower's pass applies a message of the Governor's and saves a
  picture with it, one mw verify match runs for its txid, apart from the pass,
  which never waits on it; a match that finds his use of a demo leaves one
  PROPOSED comment on it. Nothing closes.

  Background:
    Given a realtime fixture with the demo "d-green"

  Scenario: a picture applied by the follower's pass is matched while the pass is already over
    Given he sent the picture "direct:aaa" with the words "the green tick"
    And the realtime stand-in answers "d-green" from "direct:aaa" showing "the green tick after Save" at 0.9 and waits
    When the follower's pass runs
    Then the pass has returned while the realtime stand-in is still waiting
    And the realtime stand-in was called once
    When the realtime stand-in is let go
    Then the realtime bead "d-green" has one comment opening PROPOSED that names "direct:aaa" and "the green tick after Save"
    And the realtime follower said "verify match direct:aaa: 1 proposed"
    And no realtime bead was closed

  Scenario: words without a picture start no match
    Given he sent the words "just words" with no picture as "direct:bbb"
    When the follower's pass runs
    Then the realtime stand-in was not called
