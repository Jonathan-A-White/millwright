Feature: mw postern inbox
  mw postern inbox reads the postern's message records addressed to this
  host's key, decrypts them, and prints them newest first: class, from, txid,
  channel and when, then the text. A message's channel is the bead a
  decision-needed question (or its reply) names, the bead or named channel its
  own plaintext wrapper names, or "general" (Factory) when it names neither.
  Under each message from the Governor, it says how to answer inside that
  post's thread: mw postern send --re <txid>, with the channel's flag. Reading marks
  them read, by moving a cursor kept in a bd kv note, never an event of its
  own. --unread-count prints only how many are unread, without reading them,
  so a notifier can poll it without consuming anything. A verified message
  from the Governor whose channel is a bead's lands as a comment on that bead
  instead of printing in the inbox, once per txid; a named channel, or a
  sender who is not the Governor, is left in the inbox as before.

  Background:
    Given a throwaway postern key
    And a postern record of class "message" addressed to this key
    And a postern record of class "decision-needed" addressed to this key
    And a postern record of class "message" addressed to another key

  Scenario: inbox decrypts what is addressed to this key and skips the rest, newest first
    When mw postern inbox is run
    Then reading succeeds
    And 2 messages are printed
    And the first message printed is classed "decision-needed"
    And the second message printed is classed "message"

  Scenario: unread count is 2, then 0 after a read
    When mw postern inbox --unread-count is run
    Then the unread count is 2
    When mw postern inbox is run
    And mw postern inbox --unread-count is run
    Then the unread count is 0

  Scenario: the cursor is a note, and reading records no event
    When mw postern inbox is run
    Then the postern inbox cursor is saved as a note
    And no story state was set

  Scenario: a reply naming a bead the tracker knows is recorded on the bead and mails the Mayor
    Given bead "mw-abc.1" is known to the tracker
    And bead "mw-abc.1" has an open question, txid "question-txid"
    And a postern reply for bead "mw-abc.1" with answer "A" and txid "answer-txid" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-abc.1" is commented an ANSWER with txid "answer-txid" from "governor-pubkey-hex" saying "A"
    And bead "mw-abc.1"'s question note is cleared
    And mail "Answer: mw-abc.1: A" was sent to mayor

  Scenario: a reply naming a bead the tracker does not know is printed, and nothing is written
    Given a postern reply for bead "mw-unknown.1" with answer "A" and txid "answer-txid" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "mw-unknown.1"
    And bead "mw-unknown.1" has no comment
    And no mail was sent for the reply

  Scenario: a plain text message still prints as before
    Given a plain text postern record with text "hello" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "hello"

  Scenario: inbox prints the txid of each message
    Given a postern record of class "message" addressed to this key with txid "record-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "record-txid"

  Scenario: a payload's claimed sender the envelope does not back is flagged, and never recorded as a reply
    Given bead "mw-abc.2" is known to the tracker
    And bead "mw-abc.2" has an open question, txid "spoof-question-txid"
    And a postern reply for bead "mw-abc.2" with answer "A" and txid "spoof-txid" addressed to this key claiming to be from "impostor-pubkey-hex"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "from governor-pubkey-hex (payload claimed impostor-pubkey-hex)"
    And bead "mw-abc.2" has no comment
    And no mail was sent for the reply

  Scenario: a transaction signed by a key the envelope does not back is flagged, and never recorded as a reply
    Given bead "mw-abc.3" is known to the tracker
    And bead "mw-abc.3" has an open question, txid "signer-question-txid"
    And a postern reply for bead "mw-abc.3" with answer "A" and txid "signer-txid" addressed to this key signed by "impostor-pubkey-hex"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "from governor-pubkey-hex (signer claimed impostor-pubkey-hex)"
    And bead "mw-abc.3" has no comment
    And no mail was sent for the reply

  Scenario: a verified sender who is the Governor, with the signer checked, is named as the Governor
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern record of class "message" addressed to this key signed by "governor-pubkey-hex"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "from the Governor  txid"

  Scenario: a verified sender who is not the Governor, with the signer checked, is printed as their hex key
    Given mw postern inbox trusts "some-other-governor-key" as the Governor's key
    And a postern record of class "message" addressed to this key signed by "governor-pubkey-hex"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "from governor-pubkey-hex  txid"

  Scenario: a verified sender the backend cannot yet supply a signer for is flagged unchecked
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern record of class "message" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "from the Governor (signer unchecked)  txid"

  Scenario: a message with no channel named prints channel general
    Given a plain text postern record with text "hello" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "channel general"

  Scenario: a message in a bead's channel prints that bead as its channel
    Given a postern record of class "message" addressed to this key, in the channel of bead "mw-abc.1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "channel mw-abc.1"
    And it printed "message text"

  Scenario: a message in a named channel prints that channel quoted
    Given a postern record of class "message" addressed to this key, in channel "roadmap"
    When mw postern inbox is run
    Then reading succeeds
    And it printed the named channel "roadmap"

  Scenario: a General post from the Governor says how to answer inside its thread
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" with text "hello there" and txid "direct:post1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "answer in its thread: mw postern send --re direct:post1"

  Scenario: a reply in a thread is answered with the post it answers
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" with text "and another thing" and txid "direct:reply1" answering "direct:post1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "answer in its thread: mw postern send --re direct:post1"
    And it did not print "--re direct:reply1"

  Scenario: a post in a named channel is answered with that channel's flag
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" in channel "roadmap" with text "topic update" and txid "direct:topic1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed the answer command for the named channel "roadmap" re "direct:topic1"

  Scenario: a message from someone who is not the Governor is given no answer line
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "some-other-pubkey-hex" with text "hello" and txid "direct:other1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "hello"
    And it did not print "answer in its thread"

  Scenario: a decision-needed question's own bead is its channel, without an explicit channel field
    Given a postern question for bead "mw-abc.2" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "channel mw-abc.2"

  Scenario: a Governor's message in a bead's channel lands as a comment on that bead, not in the inbox
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.1" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-thread.1" with text "ship it" and txid "gov-txid-1"
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-thread.1" is commented by the Governor saying "ship it"
    And it did not print "ship it"

  Scenario: the same txid is never commented on a bead twice
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.2" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-thread.2" with text "status" and txid "dup-txid"
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-thread.2" with text "status" and txid "dup-txid"
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-thread.2" has 1 comment

  Scenario: a bead-channel message from a sender who is not the Governor stays in the inbox
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.3" is known to the tracker
    And a postern message from "some-other-pubkey-hex" in the channel of bead "mw-thread.3" with text "not the boss" and txid "other-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "not the boss"
    And bead "mw-thread.3" has no comment

  Scenario: a Governor's message in a named channel stays in the inbox, not commented on any bead
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" in channel "roadmap" with text "topic update" and txid "topic-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "topic update"

  Scenario: the Governor taps Release on the epic's approval question, and its held stories are released
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And epic "mw-epic.1" has 2 held stories
    And epic "mw-epic.1" has an open question offering "Release, Hold", txid "question-txid"
    And a postern reply for bead "mw-epic.1" with answer "Release" and txid "tap-txid" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And epic "mw-epic.1"'s held stories are released
    And bead "mw-epic.1" is commented a RELEASED with txid "tap-txid"
    And mail "Released: mw-epic.1" was sent to mayor

  Scenario: any other answer to the same question releases nothing
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And epic "mw-epic.2" has 2 held stories
    And epic "mw-epic.2" has an open question offering "Release, Hold", txid "question-txid"
    And a postern reply for bead "mw-epic.2" with answer "Hold" and txid "hold-txid" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-epic.2" has 1 comment

  Scenario: a Release tap from a signer who is not the Governor releases nothing
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And epic "mw-epic.3" has 2 held stories
    And epic "mw-epic.3" has an open question offering "Release, Hold", txid "question-txid"
    And a postern reply from "some-other-pubkey-hex" for bead "mw-epic.3" with answer "Release" and txid "other-txid" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-epic.3" has 1 comment

  Scenario: the Governor sends a screenshot with a caption, and it is downloaded, decrypted and its path recorded
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.4" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-thread.4" with text "check this out" and txid "img-txid" carrying a screenshot
    When mw postern inbox is run
    Then reading succeeds
    And it printed "check this out"
    And the decrypted image is written under the attachment directory
    And bead "mw-thread.4"'s last comment names the decrypted image's path

  Scenario: a post with several files: each is saved and named
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" with text "two shots" and txid "many-txid" carrying two files
    When mw postern inbox is run
    Then reading succeeds
    And it printed "two shots"
    And both files of the post are written and printed under the attachment directory

  Scenario: a voice note on a host that has no transcriber says it was not heard, in the inbox and on the bead
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.5" is known to the tracker
    And a postern voice note from "governor-pubkey-hex" in the channel of bead "mw-thread.5" with txid "voice-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "voice note, not transcribed: postern_transcribe_cmd is not set"
    And bead "mw-thread.5"'s last comment contains "voice note, not transcribed: postern_transcribe_cmd is not set"
