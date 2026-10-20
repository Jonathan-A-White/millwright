Feature: mw grist request links a bead to an app's request and tells the sender it was filed
  A feedback grist the mill forwarded to the Mayor is a request. The Mayor files
  a bead for it and runs mw grist request <bead> <txid>: the bead takes the
  request (its grist's txid) and the app as metadata, and the mill sends the key
  that sent the request a sealed grist record, "request-status" with the status
  "filed" and no re (postern's docs/protocol.md section 19, "Request status").
  The mill keeps the sender's key with the forwarded run, and the forward mail
  ends with this command, ready, for the Mayor to fill the bead in. A txid that
  was never a forwarded grist is refused, and nothing is sent.

  After the grind pass on the home host, mw grist status sends the same key each
  later change of the linked bead, once: building when the bead is in progress,
  landed when it is closed with a landing (the version and commit of the rig's
  "Version <v> (<bead>)" commit, or for a rig that lands in batches the version
  commit of the batch whose records name the bead, "Version <v> (batch <n> of
  <rig>)", or for a rig that writes no version the commit of the story itself,
  with no version, and a url only when the rig's file in the vault names
  public_url; a closed bead with no landing is named in a note of each pass), declined when the Mayor marked it with mw grist request
  decline and closed it (the reason's text from a menu, and an optional line),
  adopted-as-setting when the Mayor marked it with mw grist request adopt. What
  was sent is kept in a state file for the bead, so a second pass sends nothing.

  Background:
    Given a mill on the host "laptop"
    And the app "cairn" is checked out here, its main at commit "0123456789abcdef0123456789abcdef01234567" with the grind "sweep"
    And a phone whose licence opens "cairn"
    And the mill keeps its mail and the pictures it forwards under its state directory
    And the mill keeps its runs under its state directory
    And the app's grind "feedback" forwards to "mayor" and takes up to 6 pictures
    And the bead "mw-x.1" is filed

  Scenario: The forward mail's command line comes just before the app's vision, and the run keeps the sender's key
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    Then the forward mail's last line before the vision section is the command "mw grist request <bead>" and the grist's txid
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

  Scenario: A linked bead going in progress is told building, once
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" goes in progress
    When the status pass runs
    Then the status pass sent one "building" status for that grist and "mw-x.1"
    And the mill keeps a state file for "mw-x.1" under its requests directory
    When the status pass runs
    Then the status pass sent no status

  Scenario: A bead still open is told nothing beyond filed
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    When the status pass runs
    Then the status pass sent no status

  Scenario: A bead closed with a landing is told landed with the version, the commit and the url, once
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the rig "cairn" names the public url "https://github.com/Jonathan-A-White/cairn"
    And the bead "mw-x.1" goes in progress
    And the status pass runs
    And the bead "mw-x.1" is closed after landing in version "0.1.86" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field   | value                                                                                    |
      | version | 0.1.86                                                                                   |
      | commit  | fedcba9876543210fedcba9876543210fedcba98                                                 |
      | url     | https://github.com/Jonathan-A-White/cairn/commit/fedcba9876543210fedcba9876543210fedcba98 |
    When the status pass runs
    Then the status pass sent no status

  Scenario: A rig that names no public url gets a landed status with no url
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" is closed after landing in version "0.1.86" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field   | value                                    |
      | version | 0.1.86                                   |
      | commit  | fedcba9876543210fedcba9876543210fedcba98 |

  Scenario: A bead landed in a batch is told landed with the batch's version, once
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the rig "cairn" names the public url "https://github.com/Jonathan-A-White/cairn"
    And the bead "mw-x.1" goes in progress
    And the status pass runs
    And the bead "mw-x.1" is closed after landing in batch 128 of "cairn" in version "0.1.373" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field   | value                                                                                    |
      | version | 0.1.373                                                                                  |
      | commit  | fedcba9876543210fedcba9876543210fedcba98                                                 |
      | url     | https://github.com/Jonathan-A-White/cairn/commit/fedcba9876543210fedcba9876543210fedcba98 |
    And the status pass noted nothing
    When the status pass runs
    Then the status pass sent no status

  Scenario: A bead already stuck on filed and landed in a batch before this was mended is told landed once
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" is closed after landing in batch 7 of "cairn" in version "0.1.300" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field   | value                                    |
      | version | 0.1.300                                  |
      | commit  | fedcba9876543210fedcba9876543210fedcba98 |
    When the status pass runs
    Then the status pass sent no status

  Scenario: A bead landed in a rig that writes no version is told landed with the commit and no version, once
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the rig "cairn" names the public url "https://github.com/Jonathan-A-White/cairn"
    And the bead "mw-x.1" goes in progress
    And the status pass runs
    And the bead "mw-x.1" is closed after landing with no version at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field  | value                                                                                    |
      | commit | fedcba9876543210fedcba9876543210fedcba98                                                 |
      | url    | https://github.com/Jonathan-A-White/cairn/commit/fedcba9876543210fedcba9876543210fedcba98 |
    And the status pass noted nothing
    When the status pass runs
    Then the status pass sent no status

  Scenario: A bead whose commit is in a rig that writes a version is not told landed until its version commit is there
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the rig "cairn" writes the version in "package.json"
    And the bead "mw-x.1" is closed after landing with no version at the commit "fedcba9876543210fedcba9876543210fedcba98"
    When the status pass runs
    Then the status pass sent no status
    And the status pass noted "mw-x.1"

  Scenario: A bead closed with no landing is told nothing, and the pass notes the bead
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" is closed
    When the status pass runs
    Then the status pass sent no status
    And the status pass noted "mw-x.1"

  Scenario: A bead marked declined is told so when it is closed, with the reason's text
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    When mw grist request declines "mw-x.1" for the reason "outside"
    Then mw grist request left without an error
    And the bead "mw-x.1" is not closed
    When the status pass runs
    Then the status pass sent no status
    When the bead "mw-x.1" is closed
    And the status pass runs
    Then the status pass sent one "declined" status for that grist and "mw-x.1" carrying:
      | field  | value                                      |
      | reason | This is outside what this app is for.      |
    When the status pass runs
    Then the status pass sent no status

  Scenario: A decline may carry a line of the Mayor's own after the reason's text
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    When mw grist request declines "mw-x.1" for the reason "not-now" with the note "Ask again after the next release."
    And the bead "mw-x.1" is closed
    And the status pass runs
    Then the status pass sent one "declined" status for that grist and "mw-x.1" carrying:
      | field  | value                                                                         |
      | reason | This is not something the factory will build now. Ask again after the next release. |

  Scenario: A reason that is not on the menu is refused with the menu
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    When mw grist request declines "mw-x.1" for the reason "because"
    Then mw grist request was refused saying "outside, already-there, not-now"
    When the bead "mw-x.1" is closed after landing in version "0.1.86" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    And the status pass runs
    Then the status pass sent one "landed" status for that grist and "mw-x.1" carrying:
      | field   | value                                    |
      | version | 0.1.86                                   |
      | commit  | fedcba9876543210fedcba9876543210fedcba98 |

  Scenario: A bead that is not linked cannot be declined or adopted
    When mw grist request declines "mw-x.1" for the reason "outside"
    Then mw grist request was refused saying "not linked"
    When mw grist request adopts "mw-x.1" as the setting "bible-walk"
    Then mw grist request was refused saying "not linked"

  Scenario: A bead marked adopted is told adopted-as-setting with the setting once it is closed with a landing
    Given the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" is closed after landing in version "0.1.86" at the commit "fedcba9876543210fedcba9876543210fedcba98"
    And the status pass runs
    When mw grist request adopts "mw-x.1" as the setting "bible-walk"
    Then mw grist request left without an error
    When the status pass runs
    Then the status pass sent one "adopted-as-setting" status for that grist and "mw-x.1" carrying:
      | field   | value                                    |
      | setting | bible-walk                               |
      | version | 0.1.86                                   |
      | commit  | fedcba9876543210fedcba9876543210fedcba98 |
    When the status pass runs
    Then the status pass sent no status

  Scenario: The dispatch tick on the home host sends the statuses after its grind pass
    Given "laptop" is home
    And [grist] is configured
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" goes in progress
    And mw dispatch runs on "laptop" with a cap of 1 and the request status pass
    Then the status pass sent one "building" status for that grist and "mw-x.1"

  Scenario: A dispatch tick on a host that is not home sends no statuses
    Given "desktop" is home
    And [grist] is configured
    And the phone sends a "cairn" "feedback" grist, version "1", saying "Please add a quiz." with 0 photos
    When the mill grinds
    And mw grist request links "mw-x.1" to that grist
    And the bead "mw-x.1" goes in progress
    And mw dispatch runs on "laptop" with a cap of 1 and the request status pass
    Then the status pass sent no status
