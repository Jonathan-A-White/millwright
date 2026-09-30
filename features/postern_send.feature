Feature: mw postern send
  mw postern send builds a message record and sends it by postern_channel,
  printing the txid. The direct channel hands the record straight to the
  postern backend (postern's docs/protocol.md section 9); the chain channel,
  the default, signs a transaction spending the postern key's own testnet
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

  Scenario: Two sends in a row both succeed while the backend still lists what the first spent
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    When mw postern send "message" "First." is run
    Then sending succeeds
    Given the backend still lists what that send spent, and its change
    When mw postern send "message" "Second." is run
    Then sending succeeds
    And the second broadcast does not spend what the first one spent

  Scenario: A send with one confirmed coin splits its change into four coins
    Given the postern key holds a confirmed utxo of 5000 satoshis
    When mw postern send "message" "First." is run
    Then sending succeeds
    And the last broadcast pays its change to 4 outputs

  Scenario: Three sends in a row, with no block between, spend distinct coins
    Given the postern key holds a confirmed utxo of 5000 satoshis
    When mw postern send "message" "First." is run
    Then sending succeeds
    When mw postern send "message" "Second." is run
    Then sending succeeds
    When mw postern send "message" "Third." is run
    Then sending succeeds
    And there were 3 broadcasts
    And no outpoint was spent by two broadcasts
    And the second broadcast spends only the first one's change

  Scenario: With only its own unconfirmed change to spend, a send spends it
    Given the postern key holds an unconfirmed utxo of 4000 satoshis
    When mw postern send "message" "From change." is run
    Then sending succeeds
    And the last broadcast spends the utxo of 4000 satoshis

  Scenario: A send prefers a confirmed coin to an unconfirmed one
    Given the postern key holds an unconfirmed utxo of 9000 satoshis
    And the postern key also holds a confirmed utxo of 3000 satoshis
    When mw postern send "message" "Confirmed first." is run
    Then sending succeeds
    And the last broadcast spends the utxo of 3000 satoshis

  Scenario: Four confirmed coins are enough, so a send keeps one change output
    Given the postern key holds 4 confirmed utxos of 3000 satoshis
    When mw postern send "message" "Enough coins." is run
    Then sending succeeds
    And the last broadcast pays its change to 1 outputs

  Scenario: A question sent directly carries the summary "Answer:" and the bead's title
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "decision-needed" "Is it red or blue, secret words?" for bead "mw-abc.1" recommending "A" with options "A, B" is run
    Then sending succeeds
    And the delivered record's summary is "Answer: Pick the colour"
    And the delivered record's summary does not contain "secret words"

  Scenario: A landing threaded on a bead carries the summary "Check:" and the bead's title
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "landing" "Landed, secret words." threaded on bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Check: Pick the colour"
    And the delivered record's summary does not contain "secret words"

  Scenario: A message threaded on a bead carries the summary "Message on" and the bead's title
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "message" "Some secret words." threaded on bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Message on Pick the colour"
    And the delivered record's summary does not contain "secret words"

  Scenario: A bare message carries the summary "Message"
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." is run
    Then sending succeeds
    And the delivered record's summary is "Message"

  Scenario: A message on a topic thread carries the summary "Message"
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." on topic "roadmap" is run
    Then sending succeeds
    And the delivered record's summary is "Message"

  Scenario: An alarm carries no summary key
    Given the postern channel is "direct"
    When mw postern send "alarm" "Some secret words." is run
    Then sending succeeds
    And the delivered record has no summary key

  Scenario: Grist carries no summary key
    Given the postern channel is "direct"
    When mw postern send "grist" "Some secret words." is run
    Then sending succeeds
    And the delivered record has no summary key

  Scenario: A send whose bead title cannot be read carries the bead id in its place
    Given the postern channel is "direct"
    And the bead "mw-abc.1" exists
    When mw postern send "landing" "Landed." threaded on bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Check: mw-abc.1"

  Scenario: A message on a bead the tracker does not hold still sends, naming the bead id
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." threaded on bead "mw-gone.1" is run
    Then the delivered record's summary is "Message on mw-gone.1"

  Scenario: A title over 80 runes is cut to 80, an ellipsis as the 80th
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled with 100 letters exists
    When mw postern send "decision-needed" "Which?" for bead "mw-abc.1" recommending "A" with options "A, B" is run
    Then sending succeeds
    And the delivered record's summary is 80 runes, ending in an ellipsis

  Scenario: Every record of several attachments carries the summary
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    And a file "one.pdf" to attach
    And a file "two.pdf" to attach
    When mw postern send "message" "the reports" threaded on bead "mw-abc.1" attaching "one.pdf" and "two.pdf" is run
    Then sending succeeds
    And every delivered record's summary is "Message on Pick the colour"

  Scenario Outline: The chain channel never carries a summary
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "<class>" "Some secret words." threaded on bead "mw-abc.1" is run
    Then sending succeeds
    And the broadcast record has no summary key

    Examples:
      | class   |
      | landing |
      | message |

  Scenario: A question sent by the chain channel carries no summary key
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "decision-needed" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A, B" is run
    Then sending succeeds
    And the broadcast record has no summary key

  Scenario: A bare message by the chain channel carries no summary key
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    When mw postern send "message" "Ready." is run
    Then sending succeeds
    And the broadcast record has no summary key
