Feature: mw init
  mw init makes a fresh vault: the template laid into a directory that has
  nothing in it, one first commit that carries no attribution, and a beads
  database with the prefix it is given. It writes this host's config file only
  when there is none, and otherwise says what it would have written. It checks
  what it can before it writes anything, so a refusal leaves nothing behind.

  Background:
    Given a throwaway home for mw init

  Scenario: A fresh vault is made from the template
    When mw init makes the vault "fresh" with the prefix "tst"
    Then initialising succeeds
    And the vault holds the three seat charters
    And the vault's vision and ledger are the template's blank ones
    And the vault is a git repository with one commit
    And the beads database was made with the prefix "tst"
    And the report says what is owed: a private remote, git push and scripts/install-units.sh

  Scenario: An empty directory that is already there is used
    Given an empty directory "fresh"
    When mw init makes the vault "fresh" with the prefix "tst"
    Then initialising succeeds
    And the vault holds the three seat charters

  Scenario: A directory with something in it is refused, and nothing is written
    Given a directory "busy" holding the file "notes.txt"
    When mw init makes the vault "busy" with the prefix "tst"
    Then initialising is refused, saying "busy" is not empty
    And the directory "busy" holds only "notes.txt"
    And no beads database was made
    And no config file was written

  Scenario: A prefix beads would not take is refused, and nothing is written
    When mw init makes the vault "fresh" with the prefix "9 lives"
    Then initialising is refused, saying the prefix will not do
    And there is no directory "fresh"
    And no beads database was made

  Scenario: The first commit carries no attribution
    When mw init makes the vault "fresh" with the prefix "tst"
    Then initialising succeeds
    And the first commit's message has no Co-Authored-By and no Generated with line

  Scenario: The first commit is made even where git knows nobody
    When mw init makes the vault "fresh" with the prefix "tst"
    Then initialising succeeds
    And the vault is a git repository with one commit
    And the first commit was made by "mw@testhost"

  Scenario: The first commit is the person's own where git knows who they are
    Given git knows the name "Ada Lovelace" in the throwaway home
    When mw init makes the vault "fresh" with the prefix "tst"
    Then initialising succeeds
    And the first commit was made by "Ada Lovelace"

  Scenario: A config file for this host is written when there is none
    When mw init makes the vault "fresh" with the prefix "tst", the host "laptop" and the rigs:
      | rig        | dir            |
      | millwright | /work/mill     |
      | other      | /work/other    |
    Then initialising succeeds
    And the config file says:
      """
      vault = "<home>/fresh"
      host  = "laptop"
      cap   = 1

      [rigs]
      millwright = "/work/mill"
      other = "/work/other"
      """
    And the report says the config file was written

  Scenario: --join and --prefix are refused together, and nothing is written
    When mw init makes the vault "fresh" with the prefix "tst" and joins "https://example.test/vault.git"
    Then initialising is refused, saying --join and --prefix cannot both be given
    And there is no directory "fresh"
    And no beads database was made

  Scenario: Joining brings this host onto a vault that already exists, without making a new database
    Given a bare git remote holding a vault to join
    When mw init joins the vault as "joined"
    Then initialising succeeds
    And the vault holds the three seat charters
    And the beads database was picked up rather than made

  Scenario: bd bootstrap's dropped trailing newline is restored before the first sync
    Given a bare git remote holding a vault to join
    And bd bootstrap will drop the trailing newline from .beads/config.yaml
    When mw init joins the vault as "joined"
    Then initialising succeeds
    And .beads/config.yaml holds no uncommitted changes
    And the report says bd bootstrap's dropped trailing newline was restored

  Scenario: bd bootstrap changing .beads/config.yaml some other way is left alone
    Given a bare git remote holding a vault to join
    And bd bootstrap will rewrite .beads/config.yaml to "enabled: true"
    When mw init joins the vault as "joined"
    Then initialising succeeds
    And .beads/config.yaml holds uncommitted changes
    And the report does not mention a restored trailing newline

  Scenario: A config file that is already there is left as it is
    Given a config file that says:
      """
      vault = "/somewhere/else"
      host  = "vps"
      """
    When mw init makes the vault "fresh" with the prefix "tst", the host "laptop" and the rigs:
      | rig        | dir            |
      | millwright | /work/mill     |
    Then initialising succeeds
    And the config file still says exactly:
      """
      vault = "/somewhere/else"
      host  = "vps"
      """
    And the report shows the lines the config file would have had, with the host "laptop" and the rig "millwright"
    And the report does not say the config file was written
