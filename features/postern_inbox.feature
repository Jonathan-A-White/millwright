Feature: mw postern inbox
  mw postern inbox reads the postern's message records addressed to this
  host's key, decrypts them, and prints them newest first: class, from, txid,
  thread and when, then the text. A message's thread is the bead a
  decision-needed question (or its reply) names, the bead or topic its own
  plaintext wrapper names, or "general" when it names neither. Reading marks
  them read, by moving a cursor kept in a bd kv note, never an event of its
  own. --unread-count prints only how many are unread, without reading them,
  so a notifier can poll it without consuming anything. A verified message
  from the Governor whose thread is a bead lands as a comment on that bead
  instead of printing in the inbox, once per txid; a topic thread, or a
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

  Scenario: a message with no thread prints the general thread
    Given a plain text postern record with text "hello" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "thread general"

  Scenario: a message threaded on a bead prints that bead as its thread
    Given a postern record of class "message" addressed to this key, threaded on bead "mw-abc.1"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "thread mw-abc.1"
    And it printed "message text"

  Scenario: a message on a named topic prints that topic as its thread
    Given a postern record of class "message" addressed to this key, on topic "roadmap"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "thread roadmap"

  Scenario: a decision-needed question's own bead is its thread, without an explicit thread field
    Given a postern question for bead "mw-abc.2" addressed to this key
    When mw postern inbox is run
    Then reading succeeds
    And it printed "thread mw-abc.2"

  Scenario: a Governor's message in a bead thread lands as a comment on that bead, not in the inbox
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.1" is known to the tracker
    And a postern message from "governor-pubkey-hex" threaded on bead "mw-thread.1" with text "ship it" and txid "gov-txid-1"
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-thread.1" is commented by the Governor saying "ship it"
    And it did not print "ship it"

  Scenario: the same txid is never commented on a bead twice
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.2" is known to the tracker
    And a postern message from "governor-pubkey-hex" threaded on bead "mw-thread.2" with text "status" and txid "dup-txid"
    And a postern message from "governor-pubkey-hex" threaded on bead "mw-thread.2" with text "status" and txid "dup-txid"
    When mw postern inbox is run
    Then reading succeeds
    And bead "mw-thread.2" has 1 comment

  Scenario: a bead-threaded message from a sender who is not the Governor stays in the inbox
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And bead "mw-thread.3" is known to the tracker
    And a postern message from "some-other-pubkey-hex" threaded on bead "mw-thread.3" with text "not the boss" and txid "other-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "not the boss"
    And bead "mw-thread.3" has no comment

  Scenario: a Governor's message on a named topic stays in the inbox, not commented on any bead
    Given mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And a postern message from "governor-pubkey-hex" on topic "roadmap" with text "topic update" and txid "topic-txid"
    When mw postern inbox is run
    Then reading succeeds
    And it printed "topic update"
