Feature: mw verify confirm records a proposal as verified by his use
  A seat confirms what mw verify match proposed with one command: the VERIFIED
  comment in the standing form, the demo closed on it, its card cleared and one
  reply in the card's thread. mw verify list prints what still waits.

  Background:
    Given a confirm fixture with the demo "c-demo" proposed from "direct:ccc" showing "the green tick after Save"
    And the demo "c-demo" has a card asked under "direct:card"

  Scenario: confirming a demo closes it and clears its card
    When mw verify confirm runs on "c-demo" with "direct:ccc" as the Mayor
    Then the confirm bead "c-demo" has a first comment opening VERIFIED in the standing form naming "direct:ccc" and "the green tick after Save"
    And the confirm bead "c-demo" was closed
    And the card of "c-demo" is cleared and "direct:card" got one reply

  Scenario: a refused confirm changes nothing
    When mw verify confirm runs on "c-demo" with "direct:other" as the Mayor
    Then the confirm is refused saying "no PROPOSED comment"
    And the confirm bead "c-demo" was not closed
    And no confirm comment was written

  Scenario: the list shows the proposal with its confirm line until it is confirmed
    When mw verify list runs
    Then the confirm output says "mw verify confirm c-demo --txid direct:ccc"
    When mw verify confirm runs on "c-demo" with "direct:ccc" as the Mayor
    And mw verify list runs
    Then the confirm output says "No proposals waiting"
