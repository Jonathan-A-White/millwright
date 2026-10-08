Feature: mw signs every postern request with the v2 scheme
  Every call mw makes to the postern backend carries
  "Authorization: Postern2 <pubkey>:<nonce>:<sig>", the signature covering the
  request's method, its target exactly as sent (query included), the SHA-256
  of its body and the nonce (postern's docs/api.md, Authentication). A nonce the
  backend refuses with reason "nonce" acted on nothing, so mw asks for a fresh
  challenge and tries once more; a second refusal is the error.

  Scenario: A POST with a body is signed over its method, target, body and nonce
    Given a postern backend that checks every signed request against the v2 message
    When mw delivers a message record to the postern backend
    Then the delivery succeeds
    And the postern backend saw 1 signed request, verified as v2

  Scenario: A GET with a query and no body is signed over its target and the empty body
    Given a postern backend that checks every signed request against the v2 message
    When mw reads the postern backend's messages since 3
    Then the read succeeds
    And the postern backend saw 1 signed request, verified as v2
    And the signed request was "GET /api/messages?since=3&limit=200"

  Scenario: A nonce refusal is retried once from a fresh challenge
    Given a postern backend that checks every signed request against the v2 message
    And the postern backend refuses its first 1 signed requests with the reason "nonce"
    When mw delivers a message record to the postern backend
    Then the delivery succeeds
    And the postern backend saw 2 signed requests, verified as v2
    And the postern backend issued 2 challenges

  Scenario: A second nonce refusal is the error
    Given a postern backend that checks every signed request against the v2 message
    And the postern backend refuses its first 2 signed requests with the reason "nonce"
    When mw delivers a message record to the postern backend
    Then the delivery fails naming the 401
    And the postern backend saw 2 signed requests, verified as v2
