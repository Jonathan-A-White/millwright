Feature: mw grist request links a bead to an app's request and tells the sender it was filed
  A feedback grist the mill forwarded to the Mayor is a request. The Mayor files
  a bead for it and runs mw grist request <bead> <txid>: the bead takes the
  request (its grist's txid) and the app as metadata, and the mill sends the key
  that sent the request a sealed grist record, "request-status" with the status
  "filed" and no re (postern's docs/protocol.md section 19, "Request status").
  The mill keeps the sender's key with the forwarded run, and the forward mail
  ends with this command, ready, for the Mayor to fill the bead in. A txid that
  was never a forwarded grist is refused, and nothing is sent.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And the mill keeps its mail and the pictures it forwards under its state directory
    And the mill keeps its runs under its state directory
    And the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the bead "mw-x.1" is filed

  Scenario: The forward mail ends with the ready command line, and the run keeps the sender's key
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    Then the forward mail's last line is the command "mw grist request <bead>" and the grist's txid
    And the run of that grist keeps the phone's key as its sender

  Scenario: A forwarded grist is linked to the bead and its sender is told it was filed
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    Then mw grist request left without an error
    And the bead "mw-x.1" has the metadata request as that grist's txid and request_app "cairn"
    And the mill sent one request status after its answer, a grist record from the mill key sealed to the phone
    And that status matches the protocol's "filed" vector for that grist and "mw-x.1" at the mill's clock

  Scenario: A txid the mill never saw is refused and nothing is sent
    When mw grist request links "mw-x.1" to the txid "direct:nothing-like-it"
    Then mw grist request was refused saying "not a grist the mill forwarded"
    And the bead "mw-x.1" has no request metadata
    And the mill sent no request status

  Scenario: A grist that was ground, not forwarded, is refused and nothing is sent
    Given the phone sends a "cairn" "sweep" grist, version "1.1", with 1 photo
    And the grind answers with a sweep result
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    Then mw grist request was refused saying "not a grist the mill forwarded"
    And the bead "mw-x.1" has no request metadata
    And the mill sent no request status

  Scenario: A bead that is not there is refused and nothing is sent
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-nope.9" to that grist
    Then mw grist request was refused saying "mw-nope.9"
    And the mill sent no request status
