Feature: mw grist key, the mill key looked up, and made only on request
  mw grist key reads like a lookup, so it is one: it prints the mill key's
  public key and fingerprint, and with no key there it makes nothing and says
  how to make one. --create makes the key, once, and only on the home host:
  on any other host it is refused unless --force-host is given too.

  Background:
    Given a throwaway mill key file

  Scenario: With no key there, it says so and how to make one, and makes nothing
    Given this host is "laptop" and the home is "laptop"
    When mw grist key is run
    Then mw grist key was refused saying "there is no mill key" and "mw grist key --create"
    And the mill key file does not exist

  Scenario: With a key there, it prints the public key and fingerprint
    Given this host is "laptop" and the home is "laptop"
    And the mill key was made
    When mw grist key is run
    Then mw grist key printed the public key and fingerprint, and never the private key
    And mw grist key made no new key

  Scenario: --create on the home host makes the key
    Given this host is "laptop" and the home is "laptop"
    When mw grist key is run with --create
    Then mw grist key printed the public key and fingerprint, and never the private key
    And the mill key file exists, mode 0600

  Scenario: --create on a host that is not home is refused
    Given this host is "cloud1" and the home is "laptop"
    When mw grist key is run with --create
    Then mw grist key was refused saying "not home" and "--force-host"
    And the mill key file does not exist

  Scenario: --create on a host whose home cannot be told is refused
    Given this host is "cloud1" and the vault has no home file
    When mw grist key is run with --create
    Then mw grist key was refused saying "cannot tell which host is home" and "--force-host"
    And the mill key file does not exist

  Scenario: --create --force-host makes the key on a host that is not home
    Given this host is "cloud1" and the home is "laptop"
    When mw grist key is run with --create and --force-host
    Then mw grist key printed the public key and fingerprint, and never the private key
    And the mill key file exists, mode 0600
