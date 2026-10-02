Feature: a card answered by the Governor's own acts
  A card the Mayor sent with --option '<text>|<bead>:<state>,...' remembers
  what each option expects of the beads. After mw postern inbox --apply
  applies one of the Governor's acts, a card whose one option's expectations
  all hold is answered with that option: his acts did what it asks. It is
  commented ANSWER (by his acts) on the bead, its question note cleared, the
  Mayor mailed, and a reply posted under the card. Nothing is released or held
  on such an answer: his acts already did it. A bead a seat changed answers
  nothing, and an option sent with no expectations answers nothing.

  Background:
    Given a throwaway postern key
    And mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And epic "mw-x" has 2 held stories
    And bead "mw-z" is known to the tracker

  Scenario: his release of the first story leaves the card open; of the second, answers it with his acts
    Given bead "mw-x" was asked, through mw postern send, with options "A: Release both|mw-x.1:open,mw-x.2:open, C: Hold|mw-x.1:held,mw-x.2:held, B: Wait"
    And a postern action "release" on bead "mw-x.1" from "governor-pubkey-hex" with txid "tx-r1"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x.1" now stands "open"
    And bead "mw-x"'s question note is still open
    And bead "mw-x" has 1 comment
    Given a postern action "release" on bead "mw-x.2" from "governor-pubkey-hex" with txid "tx-r2"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x.2" now stands "open"
    And bead "mw-x"'s last comment starts "ANSWER (by his acts) "
    And bead "mw-x"'s last comment contains "txid tx-r2: A: Release both"
    And bead "mw-x"'s question note is cleared
    And mail "Answer: mw-x: A: Release both (by his acts)" was sent to mayor
    And the Governor was told "Answered by your acts: A: Release both" under the card asking about "mw-x"
    And bead "mw-x" has 2 comments

  Scenario: a release by a seat, and no act of his, answers nothing
    Given bead "mw-x" was asked, through mw postern send, with options "A: Release both|mw-x.1:open,mw-x.2:open, C: Hold|mw-x.1:held,mw-x.2:held"
    When a seat releases "mw-x.1" and "mw-x.2"
    And mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x"'s question note is still open
    And bead "mw-x" has 1 comment
    And nothing was told to the Governor

  Scenario: an act on a bead the card does not name answers nothing, though an option's expectations hold
    Given bead "mw-x" was asked, through mw postern send, with options "C: Hold|mw-x.1:held,mw-x.2:held, B: Wait"
    And a postern priority 0 action on bead "mw-z" from the Governor with txid "tx-p0"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x"'s question note is still open
    And bead "mw-x" has 1 comment

  Scenario: two options holding at once answer nothing
    Given bead "mw-x" was asked, through mw postern send, with options "A: One story|mw-x.1:open, B: Also that story|mw-x.1:open"
    And a postern action "release" on bead "mw-x.1" from "governor-pubkey-hex" with txid "tx-r1"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x.1" now stands "open"
    And bead "mw-x"'s question note is still open
    And bead "mw-x" has 1 comment
    And nothing was told to the Governor

  Scenario: options sent with no expectations behave as they always did
    Given bead "mw-x" was asked, through mw postern send, with options "A: Release, B: Hold"
    And a postern action "release" on bead "mw-x.1" from "governor-pubkey-hex" with txid "tx-r1"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-x"'s question note is still open
    And bead "mw-x" has 1 comment
    And nothing was told to the Governor
