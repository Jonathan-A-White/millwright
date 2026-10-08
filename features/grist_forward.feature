Feature: mw grist grind forwards a grist whose grind says forward to the Mayor
  An app's grind may say "forward": "mayor" instead of naming a model. The mill
  then runs no session: it opens the grist's text and pictures, keeps the
  pictures under its state directory (one directory for each grist, named in the
  mail), sends one mail to the mayor with the text, the sender's key and the
  pictures' paths, and answers the app {"status":"sent"} so it can say Sent.
  The factory's limits on attachments apply as for any grist. A grind that
  forwards to anyone but the mayor is refused.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And the mill keeps its mail and the pictures it forwards under its state directory

  Scenario: Feedback with two pictures lands in the mayor's mail, pictures kept, and the app is answered Sent
    Given the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the phone sends a "cairn" "feedback" grist, version "1", saying "The sweep button is hidden behind the keyboard." with 2 photos
    When the mill grinds
    Then the mill answered 1, refused 0, failed 0, and left 0 waiting
    And no grind was run
    And one mail went to "mayor" titled "Feedback from cairn: The sweep button is hidden behind the keyboard."
    And that mail has the text "The sweep button is hidden behind the keyboard.", the sender's key and the paths of 2 kept pictures
    And the 2 kept pictures are the photos as they were sent
    And the app's decrypted reply says "answered" with the answer {"status":"sent"}
    And the grist's photos were deleted from the backend
    And the mill's record holds one line for the grist with no tokens

  Scenario: The subject takes the first 80 characters of the text
    Given the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the phone sends a "cairn" "feedback" grist, version "1", saying "0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789" with 0 photos
    When the mill grinds
    Then one mail went to "mayor" titled "Feedback from cairn: 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 0123456789 012"

  Scenario: A forwarded grist is a run of the kind forward, with no tokens
    Given the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the mill keeps its runs under its state directory
    And the mill's clock moves a second at each look
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Too slow." with 1 photo
    When the mill grinds
    And mw grist runs lists the runs
    Then the list has one line for the grist with its kind "forward", its model "", more than 0 seconds, and "answered"
    And the run's answer.json says "answered"

  Scenario: A grind that forwards to anyone but the mayor is refused, and sends no mail
    Given the app's grind "feedback" forwards to "other" and takes up to 6 pictures
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Hello." with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And the answer says "refused" because "The app's grind forwards to someone the mill does not forward to."
    And the mill sent no mail
    And no grind was run

  Scenario: A grist over the attachment cap is refused, and sends no mail
    Given the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the factory allows at most 1 photos a grist
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Two pictures." with 2 photos
    When the mill grinds
    Then the mill answered 0, refused 1, failed 0, and left 0 waiting
    And the answer says "refused" because "This grist carries 2 photos; its grind takes at most 1."
    And the mill sent no mail
    And no kept pictures exist

  Scenario: A grist the mill cannot mail is failed, so the app can send it again
    Given the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the mayor's mail cannot be sent
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Hello." with 1 photo
    When the mill grinds
    Then the mill answered 0, refused 0, failed 1, and left 0 waiting
    And the answer says "failed" because "The factory could not pass this on; send it again."
    And no grind was run
