Feature: mw postern send
  mw postern send builds a message record and sends it by postern_channel,
  printing the txid. The direct channel hands the record straight to the
  postern backend (postern's docs/protocol.md section 9); the chain channel,
  the default, signs a transaction spending the postern key's own testnet
  balance to carry it, and broadcasts it. It refuses when there is no
  governor key to send to, when the class is not one mw knows, or — on the
  chain — when the key's balance would exceed the float cap mw enforces,
  naming the excess.

  --bead-channel <bead-id> or --channel <name> wraps the message's plaintext
  with an explicit channel (a bead's, or a named one; Factory is the default);
  a decision-needed question's own --bead is already its channel, so both are
  refused alongside one. --re <txid> answers inside that post's thread, in the
  channel a flag names or, with none, the root's own channel (a root mw has
  not seen is refused, never sent to Factory). A message in a bead's channel is written to
  that bead too, as the Mayor's side of the exchange. --attach sends a file, encrypted to the Governor and uploaded to
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

  Scenario: an --option ending '|<bead>:<state>,...' keeps its expectations in the note and shows its text alone
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "question-txid"
    And the clock reads 1758700000 for sending
    And the bead "mw-x" exists
    When mw postern send "decision-needed" "Release them?" for bead "mw-x" recommending "A: Release both" with options "A: Release both|mw-x.1:open,mw-x.2:open, C: Hold|mw-x.1:held,mw-x.2:held, B: Wait" is run
    Then sending succeeds
    And the broadcast record is postern's question for bead "mw-x", "Release them?" recommending "A: Release both" with options "A: Release both, C: Hold, B: Wait"
    And bead "mw-x" is commented the QUESTION with txid "question-txid", "Release them?" recommending "A: Release both" with options "A: Release both, C: Hold, B: Wait"
    And bead "mw-x"'s question note expects "mw-x.1:open,mw-x.2:open" of the option "A: Release both"
    And bead "mw-x"'s question note expects "mw-x.1:held,mw-x.2:held" of the option "C: Hold"
    And bead "mw-x"'s question note expects nothing of the option "B: Wait"

  Scenario: an --option naming a state there is none of is refused, naming the states, and nothing is sent
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-x" exists
    When mw postern send "decision-needed" "Release them?" for bead "mw-x" recommending "A" with options "A|mw-x.1:bogus" is run
    Then it is refused, saying "open, landed, verified, closed or held"
    And it is refused, and nothing was uploaded or sent

  Scenario: --bead is refused without --class decision-needed
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A" is run
    Then it is refused, saying --bead is only accepted with --class decision-needed

  Scenario: --bead-channel wraps the message with a bead channel, and writes it to the bead
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "thread-txid"
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Ready for review." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the broadcast record's plaintext is in the channel of bead "mw-abc.1" with text "Ready for review."
    And bead "mw-abc.1" is commented "MAYOR via postern, txid thread-txid: Ready for review."

  Scenario: --channel wraps the message with a named channel
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    When mw postern send "message" "Ready for review." in channel "roadmap" is run
    Then sending succeeds
    And the broadcast record's plaintext is in channel "roadmap" with text "Ready for review."

  Scenario: --channel and --bead-channel cannot both be set
    Given the postern key's balance is 1000 satoshis
    When mw postern send "message" "Ready for review." in the channel of bead "mw-abc.1" and in channel "roadmap" is run
    Then it is refused, saying --channel and --bead-channel cannot both be set

  Scenario: --bead-channel is refused with a decision-needed question, whose own bead is already its channel
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-abc.1" exists
    When mw postern send "decision-needed" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A" in the channel of bead "mw-other.1" is run
    Then it is refused, saying --channel and --bead-channel are refused with a decision-needed question

  Scenario: --re answers inside a post's thread in a bead's channel, and writes it to the bead
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the postern backend will report the txid "answer-txid"
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Yes, merged." in the channel of bead "mw-abc.1" answering "direct:post-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in the channel of bead "mw-abc.1" with text "Yes, merged." answering "direct:post-txid"
    And bead "mw-abc.1" is commented "MAYOR via postern, txid answer-txid: Yes, merged."

  Scenario: --re answers inside a post's thread in a named channel
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    When mw postern send "message" "Noted." in channel "roadmap" answering "direct:post-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in channel "roadmap" with text "Noted." answering "direct:post-txid"

  Scenario: --re with no channel flag answers in the channel of a root mw has seen in a bead's channel
    Given the postern channel is "direct"
    And the bead "mw-x" exists
    And mw has seen the post "direct:root-txid" in the channel of bead "mw-x"
    And the postern backend will report the txid "direct:answer-txid"
    When mw postern send "message" "On it." answering "direct:root-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in the channel of bead "mw-x" with text "On it." answering "direct:root-txid"
    And bead "mw-x" is commented "MAYOR via postern, txid direct:answer-txid: On it."

  Scenario: --re with no channel flag answers in the named channel of a root mw has seen there, given bare or prefixed
    Given the postern channel is "direct"
    And mw has seen the post "direct:root-txid" in channel "roadmap"
    When mw postern send "message" "Noted." answering "root-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in channel "roadmap" with text "Noted." answering "root-txid"

  Scenario: --re with no channel flag is refused for a root mw has not seen, never sent to Factory
    Given the postern channel is "direct"
    When mw postern send "message" "Lost." answering "direct:unknown-txid" is run
    Then it is refused, saying --re needs the root's channel and naming --bead-channel and --channel
    And it is refused, and nothing was uploaded or sent

  Scenario: --re with a channel flag goes where the flag says, whatever channel the root was seen in
    Given the postern channel is "direct"
    And the bead "mw-y" exists
    And mw has seen the post "direct:root-txid" in channel "roadmap"
    When mw postern send "message" "Over here." in the channel of bead "mw-y" answering "direct:root-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in the channel of bead "mw-y" with text "Over here." answering "direct:root-txid"

  Scenario: --re with no channel flag answers a root mw itself sent, wherever it went
    Given the postern channel is "direct"
    And the bead "mw-z" exists
    And the postern backend will report the txid "direct:card-txid"
    When mw postern send "message" "The card." in the channel of bead "mw-z" is run
    And mw postern send "message" "More." answering "direct:card-txid" is run
    Then sending succeeds
    And the broadcast record's plaintext is in the channel of bead "mw-z" with text "More." answering "direct:card-txid"

  Scenario: --bead without --class decision-needed says where a bead's channel is posted to
    Given the postern key's balance is 1000 satoshis
    And the bead "mw-abc.1" exists
    When mw postern send "message" "Ship it?" for bead "mw-abc.1" recommending "A" with options "A" is run
    Then it is refused, saying --bead-channel <id> posts in a bead's channel

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

  Scenario: A landing in a bead's channel carries the summary "Check:" and the bead's title
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "landing" "Landed, secret words." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Check: Pick the colour"
    And the delivered record's summary does not contain "secret words"

  Scenario: A message in a bead's channel carries the summary "Message on" and the bead's title
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "message" "Some secret words." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Message on Pick the colour"
    And the delivered record's summary does not contain "secret words"

  Scenario: A bare message carries the summary "Message"
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." is run
    Then sending succeeds
    And the delivered record's summary is "Message"

  Scenario: A message in a named channel carries the summary "Message"
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." in channel "roadmap" is run
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
    When mw postern send "landing" "Landed." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record's summary is "Check: mw-abc.1"

  Scenario: A message on a bead the tracker does not hold still sends, naming the bead id
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." in the channel of bead "mw-gone.1" is run
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
    When mw postern send "message" "the reports" in the channel of bead "mw-abc.1" attaching "one.pdf" and "two.pdf" is run
    Then sending succeeds
    And every delivered record's summary is "Message on Pick the colour"

  Scenario Outline: The chain channel never carries a summary
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "<class>" "Some secret words." in the channel of bead "mw-abc.1" is run
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

  Scenario: A direct message in a named channel names it in the clear
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." in channel "general" is run
    Then sending succeeds
    And the delivered record names the clear key "channel" as "general"
    And the delivered record has no "bead" key

  Scenario: A direct message in a bead's channel names the bead in the clear
    Given the postern channel is "direct"
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "message" "Some secret words." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the delivered record names the clear key "bead" as "mw-abc.1"
    And the delivered record has no "channel" key

  Scenario: A direct message in Factory names no channel
    Given the postern channel is "direct"
    When mw postern send "message" "Some secret words." is run
    Then sending succeeds
    And the delivered record has no "channel" key
    And the delivered record has no "bead" key

  Scenario: The chain channel names neither channel nor bead
    Given the postern key's balance is 1000 satoshis
    And the postern key holds a spendable utxo of 5000 satoshis
    And the bead "mw-abc.1" titled "Pick the colour" exists
    When mw postern send "message" "Some secret words." in the channel of bead "mw-abc.1" is run
    Then sending succeeds
    And the broadcast record has no "channel" key
    And the broadcast record has no "bead" key
