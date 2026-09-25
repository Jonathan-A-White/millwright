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
