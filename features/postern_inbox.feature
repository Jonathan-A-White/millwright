Feature: mw postern inbox
  mw postern inbox reads the postern's message records addressed to this
  host's key, decrypts them, and prints them newest first: class, from and
  text. Reading marks them read, by moving a cursor kept in a bd kv note,
  never an event of its own. --unread-count prints only how many are unread,
  without reading them, so a notifier can poll it without consuming anything.

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
