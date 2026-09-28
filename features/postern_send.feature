Feature: mw postern send
  mw postern send builds a message record and sends it by postern_channel,
  printing the txid. The direct channel, the default, hands the record
  straight to the postern backend (postern's docs/protocol.md section 9); the
  chain channel signs a transaction spending the postern key's own testnet
  balance to carry it, and broadcasts it. It refuses when there is no
  governor key to send to, when the class is not one mw knows, or — on the
  chain — when the key's balance would exceed the float cap mw enforces,
  naming the excess.

  --thread <bead-id> or --topic <name> wraps the message's plaintext with an
  explicit thread; a decision-needed question's own --bead is already its
  thread, so --thread and --topic are refused alongside one. A message in a
  bead's thread is written to that bead too, as the Mayor's side of the
  exchange. --attach sends a file, encrypted to the Governor and uploaded to
  the backend's blob store, announced in the message.

  Background:
    Given a throwaway postern key for sending
    And the postern governor key is "governor-pubkey-hex"
    And the postern float cap is 100000 satoshis
    And the postern channel is "chain"

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

  Scenario: --thread wraps the message with a bead thread, and writes it to the bead
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "thread-txid"
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Ready for review." threaded on bead "mw-abc.1" is run
    Then sending succeeds
    And the broadcast record's plaintext is threaded on bead "mw-abc.1" with text "Ready for review."
    And bead "mw-abc.1" is commented "MAYOR via postern, txid thread-txid: Ready for review."

  Scenario: --topic wraps the message with a named topic thread
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    When mw postern send "message" "Ready for review." on topic "roadmap" is run
    Then sending succeeds
    And the broadcast record's plaintext is on topic "roadmap" with text "Ready for review."

  Scenario: --thread and --topic cannot both be set
    Given the postern key's balance is 1000 satoshis
    When mw postern send "message" "Ready for review." threaded on bead "mw-abc.1" and on topic "roadmap" is run
    Then it is refused, saying --thread and --topic cannot both be set

  Scenario: --thread is refused with a decision-needed question, whose own bead is already the thread
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-abc.1" exists
    When mw postern send "decision-needed" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A" threaded on bead "mw-other.1" is run
    Then it is refused, saying --thread and --topic are refused with a decision-needed question

  Scenario: The direct channel delivers the record without spending anything
    Given the postern channel is "direct"
    And the postern key's balance is 150000 satoshis
    And the postern backend will report the txid "direct:abc"
    When mw postern send "message" "Ready for review." is run
    Then sending succeeds
    And it prints "direct:abc"
    And the record was delivered directly, and nothing was broadcast

  Scenario: A question is asked, and recorded on its bead, by the direct channel too
    Given the postern channel is "direct"
    And the postern backend will report the txid "direct:q1"
    And the clock reads 1758700000 for sending
    And the bead "mw-abc.1" exists
    When mw postern send "decision-needed" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A, B" is run
    Then sending succeeds
    And bead "mw-abc.1" is commented the QUESTION with txid "direct:q1", "Ship it?" recommending "A" with options "A, B"
    And bead "mw-abc.1"'s question note holds the txid "direct:q1"

  Scenario: --attach sends a file encrypted to the Governor, announced in the message
    Given the postern channel is "direct"
    And a file "report.pdf" to attach
    When mw postern send "message" "the report" attaching "report.pdf" is run
    Then sending succeeds
    And the delivered message announces the uploaded "application/pdf" file, captioned "the report"

  Scenario: A file over 8 MiB is refused before anything is sent
    Given the postern channel is "direct"
    And a file "huge.png" of 9 MiB to attach
    When mw postern send "message" "too big" attaching "huge.png" is run
    Then it is refused, and nothing was uploaded or sent
