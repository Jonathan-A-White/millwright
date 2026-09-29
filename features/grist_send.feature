Feature: mw grist send, the terminal sends a grist
  A key that is not the mill's can send the factory grist from a terminal
  (postern's docs/protocol.md section 19): mw grist send proves the given key
  to the backend, seals each photo and the app's request to the mill's public
  key, which the backend names at GET /api/me, uploads the photos as blobs and
  posts the grist. With --wait it pages the backend for the answer to that
  grist, opens it, prints it as JSON, and leaves with 0 when it is answered,
  1 when it is refused or failed, and 2 when none came in time.

  Background:
    Given a backend that names the mill key
    And a key file holding the phone's key

  Scenario: A grist with one photo is sent sealed to the mill, and its answer printed
    Given the mill has answered the grist "direct:the-grist" as "answered"
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and waits 2 minutes
    Then mw grist send left with 0
    And the backend took 1 blob and 1 grist, all sealed to the mill key
    And the grist is from the phone's key to the mill key and names "cairn" "sweep" "1.1" with the photo and the request
    And mw grist send printed the answer with the status "answered"

  Scenario: A refused answer is printed with its reason, and mw leaves with 1
    Given the mill has answered the grist "direct:the-grist" as "refused"
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and waits 2 minutes
    Then mw grist send left with 1
    And mw grist send said "This phone's licence does not open the app named in the grist."
    And mw grist send printed the answer with the status "refused"

  Scenario: A failed answer is printed with its reason, and mw leaves with 1
    Given the mill has answered the grist "direct:the-grist" as "failed"
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and waits 2 minutes
    Then mw grist send left with 1
    And mw grist send said "The grind's answer did not match its schema; send it again."

  Scenario: No answer before --wait leaves with 2
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and waits 2 minutes
    Then mw grist send left with 2
    And mw grist send said "no answer to direct:the-grist"
    And the backend took 1 blob and 1 grist, all sealed to the mill key

  Scenario: Without --wait the grist is sent and its id printed, and no answer is read
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and does not wait
    Then mw grist send left with 0
    And mw grist send printed only the id "direct:the-grist"

  Scenario: An answer to another grist is not this one's
    Given the mill has answered the grist "direct:another" as "answered"
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and waits 2 minutes
    Then mw grist send left with 2

  Scenario: A backend that names no mill key takes nothing
    Given the backend names no mill key
    When mw grist send sends a "cairn" "sweep" grist with 1 photo and does not wait
    Then mw grist send left with 1
    And mw grist send said "names no mill key"
    And the backend took nothing
