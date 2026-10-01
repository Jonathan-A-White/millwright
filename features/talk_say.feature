Feature: mw talk say
  mw talk say is the Mayor's answer in a talk: it encrypts the words to the
  Governor as postern's docs/protocol.md section 20 turn plaintext and hands
  the record straight to the postern backend (section 9). The record's class is
  talk and it carries no summary, so no word of the answer is ever pushed or
  logged. It prints the txid and the milliseconds it took, which the Governor's
  eight-second budget is spent against.

  Background:
    Given a throwaway Mayor postern key for talking
    And a throwaway Governor key for talking

  Scenario: an answer is a talk record the Governor can read, with no summary
    When mw talk say "Two stories landed." is run for talk "talk-7" turn 3
    Then the talk record was delivered directly, and nothing was broadcast
    And the delivered talk record's class is "talk"
    And the delivered talk record carries no summary
    And the delivered talk record is addressed to the Governor from the Mayor
    And the Governor decrypts the talk record's plaintext to talk "talk-7" turn 3 role "answer" saying "Two stories landed."
    And it prints the txid and the elapsed milliseconds

  Scenario: --holding sends a holding answer
    When mw talk say "One moment." is run holding for talk "talk-7" turn 3
    Then the Governor decrypts the talk record's plaintext to talk "talk-7" turn 3 role "holding" saying "One moment."

  Scenario: --end ends the talk
    When mw talk say "Goodbye." is run ending for talk "talk-7" turn 4
    Then the Governor decrypts the talk record's plaintext to talk "talk-7" turn 4 role "end" saying "Goodbye."

  Scenario: it refuses what cannot be a turn, and sends nothing
    When mw talk say "" is run for talk "talk-7" turn 3
    Then talk say is refused saying "what should it say"
    When mw talk say "Hi" is run for talk "" turn 3
    Then talk say is refused saying "--talk"
    When mw talk say "Hi" is run for talk "talk-7" turn 0
    Then talk say is refused saying "--turn"
    When mw talk say "Hi" is run holding and ending for talk "talk-7" turn 3
    Then talk say is refused saying "--holding and --end"
    And nothing was delivered for talking

  Scenario: it refuses when there is no Governor to answer
    Given the talk Governor's key is not set
    When mw talk say "Hi" is run for talk "talk-7" turn 3
    Then talk say is refused saying "postern_governor_key"
    And nothing was delivered for talking

  Scenario: a backend that will not take the record is reported
    Given the postern backend will not take a talk record
    When mw talk say "Hi" is run for talk "talk-7" turn 3
    Then talk say is refused saying "backend said no"
