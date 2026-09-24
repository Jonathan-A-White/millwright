Feature: mw postern key
  mw postern key holds the Mayor's postern payment key: a generated testnet
  secp256k1 key, kept in a plain file outside the vault and its backups,
  host-local like .mayor-acting. init makes it once and refuses to overwrite
  one that is already there; show prints its public half and never the
  private key.

  Background:
    Given a throwaway postern key file

  Scenario: init generates a key file that only its owner can read
    When mw postern key init is run
    Then it succeeds
    And the postern key file holds a testnet WIF key, mode 0600

  Scenario: init refuses to overwrite a key that is already there
    Given a postern key already exists
    When mw postern key init is run
    Then it is refused, saying the key already exists
    And the postern key file still holds the key it had before

  Scenario: show prints the public key and the testnet address
    Given a postern key already exists
    When mw postern key show is run
    Then it succeeds
    And it prints the key's compressed public key
    And it prints the key's testnet address
    And it never prints the private key

  Scenario: show is refused when there is no key yet
    When mw postern key show is run
    Then it is refused, saying there is no postern key
