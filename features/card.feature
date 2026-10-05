Feature: mw card
  A live card is the Mayor's numbered list for the Governor that keeps itself
  current: each item has its text, the beads it links to and what it expects
  of a bead (open, landed, verified, closed, answered or held), and the card says
  which event kinds and beads it subscribes to, so the app can tick an item off
  when the expected event arrives. mw card send posts a card record and
  mw card update a card-update record naming the card by its txid, each sealed
  to the Governor as a message is. An item that gives no expectation has one
  derived from its ask. mw prompt run --card sends the Mayor's answer to a
  saved prompt as a card that records the prompt's name.

  Scenario: A card is a sealed card record with its items, links, expectations and subscriptions
    When the Mayor sends the card "Top 5" with the items:
      | item                                       |
      | Approve mw-a.1: the plan\|mw-a.1\|         |
      | Check the map\|mw-b,mw-c\|mw-b:landed      |
      | Read the brief                             |
    Then the card is delivered as one record of class "card" from the Mayor to the Governor, with no summary
    And the sealed card is titled "Top 5"
    And sealed item 1 reads "Approve mw-a.1: the plan", links "mw-a.1" and expects "mw-a.1" "answered"
    And sealed item 2 reads "Check the map", links "mw-b,mw-c" and expects "mw-b" "landed"
    And sealed item 3 reads "Read the brief", links "" and expects nothing
    And the sealed card subscribes to the kinds "card_answered,bead_changed" and the beads "mw-a.1,mw-b,mw-c"
    And the card's txid is printed first

  Scenario Outline: An item with no expectation has one derived from its ask
    When the Mayor sends the card "Asks" with the items:
      | item   |
      | <item> |
    Then sealed item 1 reads "<item>", links "" and expects "<bead>" "<state>"

    Examples:
      | item                 | bead   | state    |
      | VERIFIED on mw-v.1   | mw-v.1 | verified |
      | Looks good on mw-g   | mw-g   | closed   |
      | Approve mw-p.2       | mw-p.2 | answered |
      | Answer mw-q          | mw-q   | answered |
      | Release mw-r.3       | mw-r.3 | open     |

  Scenario: A bad expectation state is refused and nothing is sent
    When the Mayor sends the card "Top 5" with the items:
      | item                     |
      | Check it\|mw-a\|mw-a:done |
    Then the card is refused, saying "open, landed, verified, closed, answered or held"
    And no card record is delivered

  Scenario: An update is a sealed card-update record naming its card
    When the Mayor updates the card "direct:abc" adding the item "6. Release mw-f|mw-f|", linking item 2 to "mw-b.1" and ticking item 1
    Then the card is delivered as one record of class "card-update" from the Mayor to the Governor, with no summary
    And the sealed update is re "direct:abc", adds item 6 expecting "mw-f" "open", links item 2 to "mw-b.1" and ticks item 1

  Scenario: A prompt's answer sent as a card records the prompt's name
    Given the backend holds the prompt "top5" for a card
    When the Mayor runs the prompt "top5" as the card "Top 5" with the item "Approve mw-a"
    Then the sealed card records the prompt "top5"

  Scenario Outline: An item that can never tick is refused and nothing is sent
    When the Mayor sends the card "Top 5" with the items:
      | item                                 |
      | Check it\|mw-epic\|mw-epic:<state> |
    Then the card is refused, saying "mw-epic is an epic: an epic is never landed or verified; expect closed"
    And no card record is delivered
    And the list of sent cards has 0 entries

    Examples:
      | state    |
      | verified |
      | landed   |

  Scenario: A bead that cannot be read is refused
    When the Mayor sends the card "Top 5" with the items:
      | item                           |
      | Check it\|mw-nope\|mw-nope:verified |
    Then the card is refused, saying "cannot read mw-nope"
    And no card record is delivered

  Scenario: An epic expected closed and a story expected verified are accepted
    When the Mayor sends the card "Top 5" with the items:
      | item                             |
      | Close it\|mw-epic\|mw-epic:closed |
      | Check it\|mw-b\|mw-b:verified     |
    Then the card is delivered as one record of class "card" from the Mayor to the Governor, with no summary

  Scenario: Every card sent is kept, and mw card list prints them newest first
    When the Mayor sends the card "First card" with the items:
      | item     |
      | Read it  |
    And the Mayor sends the card "Second card" with the items:
      | item     |
      | Look     |
    Then the list of sent cards has 2 entries
    And listing the cards prints "Second card" before "First card"
