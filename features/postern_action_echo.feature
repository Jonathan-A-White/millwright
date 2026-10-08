Feature: every one-tap action applied from the inbox is echoed in the events tail
  When mw postern inbox --apply applies one of the Governor's taps (Release,
  Hold, Keep, Close, Verified), it appends an action_applied event on the bead
  whose detail is the tap's txid, so the app that sent the tap reads from the
  events tail that it took. A tap that is refused did nothing and is echoed by
  no event.

  Background:
    Given a throwaway postern key
    And mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And the home's event log is kept

  Scenario: a release tap is echoed with its txid
    Given epic "mw-x" has 2 held stories
    And a postern action "release" on bead "mw-x.1" from "governor-pubkey-hex" with txid "tx-r1"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the events tail shows an action_applied event on "mw-x.1" with detail "tx-r1"

  Scenario Outline: a <action> tap is echoed with its txid
    Given epic "mw-x" has 2 open stories
    And a postern action "<action>" on bead "mw-x.1" from "governor-pubkey-hex" with txid "<txid>"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the events tail shows an action_applied event on "mw-x.1" with detail "<txid>"

    Examples:
      | action   | txid   |
      | hold     | tx-h1  |
      | keep     | tx-k1  |
      | close    | tx-c1  |
      | verified | tx-v1  |

  Scenario: a refused tap is echoed by no event
    Given epic "mw-x" has 2 open stories
    And a postern action "release" on bead "mw-x.1" from "governor-pubkey-hex" with txid "tx-r1"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the events tail shows no events
