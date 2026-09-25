Feature: mw postern send
  mw postern send builds a message record, signs a transaction spending the
  postern key's own testnet balance to carry it, and broadcasts it through the
  postern backend, printing the txid. It refuses when there is no governor key
  to send to, when the key's balance would exceed the float cap mw enforces on
  every send, naming the excess, or when the class is not one mw knows.

  Background:
    Given a throwaway postern key for sending
    And the postern governor key is "governor-pubkey-hex"
    And the postern float cap is 100000 satoshis

  Scenario: send builds, signs and broadcasts, printing the txid the backend returns
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "abc123txid"
    When mw postern send "message" "Ready for review." is run
    Then sending succeeds
    And it prints "abc123txid"

  Scenario: send refuses over the float cap, naming the excess
    Given the postern key's balance is 150000 satoshis
    When mw postern send "message" "Ready for review." is run
    Then it is refused, naming the excess of 50000

  Scenario: send refuses a class it does not know
    Given the postern key's balance is 1000 satoshis
    When mw postern send "urgent" "Ready for review." is run
    Then it is refused, saying "urgent" is not a class postern knows

  Scenario: send with no --class defaults to class message
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the clock reads 1758700000 for sending
    When mw postern send "Ship it?" is run with no --class
    Then sending succeeds
    And the broadcast record is postern's payload, classed "message", stamped 1758700000

  Scenario: send refuses when there is no governor key to send to
    Given the postern governor key is ""
    And the postern key's balance is 1000 satoshis
    When mw postern send "message" "Ready for review." is run
    Then it is refused, saying postern_governor_key is not set

  Scenario: the record carries postern's own payload, in its field order
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the clock reads 1758700000 for sending
    When mw postern send "decision-needed" "Ship it?" is run
    Then sending succeeds
    And the broadcast record is postern's payload, classed "decision-needed", stamped 1758700000

  Scenario: --bead, --recommend and --option send a question and record it on the bead
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "question-txid"
    And the clock reads 1758700000 for sending
    And the bead "mw-abc.1" exists
    When mw postern send "decision-needed" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A, B" is run
    Then sending succeeds
    And the broadcast record is postern's question for bead "mw-abc.1", "Ship it?" recommending "A" with options "A, B"
    And bead "mw-abc.1" is commented the QUESTION with txid "question-txid", "Ship it?" recommending "A" with options "A, B"
    And bead "mw-abc.1"'s question note holds the txid "question-txid"

  Scenario: --bead is refused without --class decision-needed
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A" is run
    Then it is refused, saying --bead is only accepted with --class decision-needed
