Feature: mw mail
  Mail is beads: a message is a bead of type mail, addressed to a recipient,
  with its subject as the title and its body as the description. mw mail send
  writes one, signed by the seat in $MW_SEAT and by no one else; mw mail inbox
  lists the unread messages of a seat; mw mail read prints one and marks it
  read, which takes it out of the inbox. inbox and read take the mailbox from
  $MW_SEAT too, or from --as.

  Scenario: A message sent to a seat is in its inbox and in no other seat's
    Given MW_SEAT is "builder@laptop"
    When mail is sent to "mayor" with the subject "Ready for review" and the body "The branch is up."
    Then sending mail succeeds
    And mail says it went to "mayor"
    And the inbox of "mayor" lists a message from "builder@laptop" with the subject "Ready for review"
    And the inbox of "governor" is empty
    And the inbox of "builder@laptop" is empty

  Scenario: Reading a message prints it whole and takes it out of the inbox
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "Ready for review" and the body:
      """
      The branch is up.

      Please look at the second commit.
      """
    And MW_SEAT is "mayor"
    When the message with the subject "Ready for review" is read
    Then reading mail succeeds
    And the mail printed says "From: builder@laptop"
    And the mail printed says "To: mayor"
    And the mail printed says "Date: 2026-09-19T09:00:00Z"
    And the mail printed says "Subject: Ready for review"
    And the mail printed says the body:
      """
      The branch is up.

      Please look at the second commit.
      """
    And the inbox of "mayor" is empty

  Scenario: A message that has been read can be read again, and stays read
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "Ready for review" and the body "The branch is up."
    And MW_SEAT is "mayor"
    When the message with the subject "Ready for review" is read
    And the message with the subject "Ready for review" is read
    Then reading mail succeeds
    And the mail printed says "Subject: Ready for review"
    And the inbox of "mayor" is empty

  Scenario: Sending with no MW_SEAT is refused, naming MW_SEAT, and nothing is written
    Given MW_SEAT is not set
    When mail is sent to "mayor" with the subject "Who am I?" and the body "Nobody."
    Then mail is refused, naming "MW_SEAT"
    And the mailbox recorded no writes

  Scenario: Sending with an MW_SEAT that is only blanks is refused too
    Given MW_SEAT is "  "
    When mail is sent to "mayor" with the subject "Who am I?" and the body "Nobody."
    Then mail is refused, naming "MW_SEAT"
    And the mailbox recorded no writes

  Scenario: Sending with no subject is refused and nothing is written
    Given MW_SEAT is "builder@laptop"
    When mail is sent to "mayor" with the subject "" and the body "Nothing to say."
    Then mail is refused, naming "subject"
    And the mailbox recorded no writes

  Scenario: A message with no body says so
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "Just a nudge" and no body
    And MW_SEAT is "mayor"
    When the message with the subject "Just a nudge" is read
    Then reading mail succeeds
    And the mail printed says "(no body)"

  Scenario: The inbox is the seat's own, from MW_SEAT
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "For the Mayor" and the body "One."
    And mail was sent to "builder@laptop" with the subject "For the Builder" and the body "Two."
    When the inbox is listed
    Then listing the inbox succeeds
    And the inbox printed lists the subject "For the Builder"
    And the inbox printed does not list the subject "For the Mayor"

  Scenario: --as names another seat's mailbox
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "For the Mayor" and the body "One."
    And mail was sent to "builder@laptop" with the subject "For the Builder" and the body "Two."
    When the inbox is listed with --as "mayor"
    Then listing the inbox succeeds
    And the inbox printed lists the subject "For the Mayor"
    And the inbox printed does not list the subject "For the Builder"

  Scenario: An empty inbox says whose it is
    Given MW_SEAT is "mayor"
    When the inbox is listed
    Then listing the inbox succeeds
    And the inbox printed says "no unread mail for mayor"

  Scenario: The inbox lists the oldest message first, with who it is from
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "First" and the body "One."
    And mail was sent to "mayor" with the subject "Second" and the body "Two."
    When the inbox is listed with --as "mayor"
    Then listing the inbox succeeds
    And the inbox printed names "First" before "Second"
    And the inbox printed says "from builder@laptop"

  Scenario: Listing an inbox with no MW_SEAT and no --as is refused, naming MW_SEAT
    Given MW_SEAT is not set
    When the inbox is listed
    Then mail is refused, naming "MW_SEAT"

  Scenario: --as stands in for a missing MW_SEAT when listing
    Given MW_SEAT is "builder@laptop"
    And mail was sent to "mayor" with the subject "For the Mayor" and the body "One."
    And MW_SEAT is not set
    When the inbox is listed with --as "mayor"
    Then listing the inbox succeeds
    And the inbox printed lists the subject "For the Mayor"

  Scenario: Reading an id that is not mail is refused, naming it
    Given MW_SEAT is "mayor"
    When the message "mw-nope" is read
    Then mail is refused, naming "mw-nope"
    And the mailbox recorded no writes
