Feature: mw talk call
  mw talk call is the Mayor's call-back: it encrypts a short line to the
  Governor as postern's docs/protocol.md section 21 ring plaintext and hands
  the record straight to the postern backend (section 9), as mw talk say does.
  The record's class is call and it carries no summary, so no word of it is
  pushed or logged by the backend. It prints the txid and the milliseconds it
  took.

  Background:
    Given a throwaway Mayor postern key for talking
    And a throwaway Governor key for talking

  Scenario: a call-back is a call ring record the Governor can read, with no summary
    When mw talk call "Back now: two landings." is run
    Then the talk record was delivered directly, and nothing was broadcast
    And the delivered talk record's class is "call"
    And the delivered talk record carries no summary
    And the delivered talk record is addressed to the Governor from the Mayor
    And the Governor decrypts the call record's plaintext to role "ring" saying "Back now: two landings."
    And the call record says when it was sent
    And it prints "txid direct:" and the elapsed milliseconds

  Scenario: --link carries bead ids in the record and never in the text
    When mw talk call "Back now." is run with links "mw-x.1" and "mw-x.2"
    Then the Governor decrypts the call record's plaintext to role "ring" saying "Back now."
    And the call record's links are "mw-x.1" and "mw-x.2"

  Scenario: it refuses what cannot be a ring, and sends nothing
    When mw talk call "" is run
    Then talk call is refused saying "what should it say"
    And nothing was delivered for talking

  Scenario: it refuses when there is no Governor to call
    Given the talk Governor's key is not set
    When mw talk call "Hi" is run
    Then talk call is refused saying "postern_governor_key"
    And nothing was delivered for talking

  Scenario: a backend that will not take the record is reported
    Given the postern backend will not take a talk record
    When mw talk call "Hi" is run
    Then talk call is refused saying "backend said no"
