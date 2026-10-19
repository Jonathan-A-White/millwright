Feature: mw member init
  A member is another person on the Governor's machine who has their own Linux
  user, and so their own factory: mw member init, run as that user, makes it.
  It writes the member's config file and beads.env, a Dolt user unit on the port
  chosen, a fresh vault laid from the starter kit with its beads database made on
  that server, and adds a Family section to the Mayor's charter that names the
  licence holder. It never overwrites a member's factory, and mw member remove
  is the way back: it stops and disables the unit and deletes nothing.

  Background:
    Given a throwaway home for mw member
    And a dolt that is found and a port that is free

  Scenario: A member's factory is made
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init succeeds
    And the member's config file names the vault "luke-vault", the host "laptop" and the postern backend "http://10.88.0.2:8788"
    And the member's beads.env points bd at 127.0.0.1 port 3308 with a generated password that only its owner can read
    And the member's Dolt unit serves port 3308 and was enabled and started
    And the member's vault is a git repository holding the starter kit's seat files
    And the beads database was made in the vault with the prefix "luke" on port 3308
    And the vault's first commit carries no attribution

  Scenario: The Mayor's charter names the licence holder
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then the Mayor's charter has a Family section after Who you serve
    And the Family section names "Jonathan (the Governor)"
    And the Family section says every epic is released only on the holder's approval, a decision card tapped from their account
    And the Family section says the holder's key has the cockpit role here

  Scenario: A second run is refused and writes nothing
    Given mw member init has made "luke" with the vault "luke-vault"
    When mw member init makes "luke" with the vault "second-vault", the Dolt port 3309, the holder "Someone Else" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying "already a member factory at"
    And there is no member directory "second-vault"
    And the member's config file still names the vault "luke-vault"

  Scenario: The report prints what was done and the way back
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then the report names each file it wrote
    And the report names mw member remove as the way back, which deletes nothing

  Scenario: Linger that is not set is named as a hand step
    Given loginctl says linger is not set
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init succeeds
    And the report names loginctl enable-linger as a hand step

  Scenario: Linger that is set is not named
    Given loginctl says linger is set
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init succeeds
    And the report does not name loginctl enable-linger

  Scenario: A vault directory with something in it is refused, and nothing is written
    Given a member directory "busy" holding the file "notes.txt"
    When mw member init makes "luke" with the vault "busy", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying "busy is not empty"
    And no member config file, beads.env or Dolt unit was written
    And systemctl was not run

  Scenario: A port that is taken is refused, and nothing is written
    Given the Dolt port is taken
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying port 3308 is taken
    And there is no member directory "luke-vault"
    And no member config file, beads.env or Dolt unit was written

  Scenario: A missing Dolt is refused, and nothing is written
    Given no dolt can be found
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying dolt was not found and --dolt names it
    And there is no member directory "luke-vault"

  Scenario: A name, a port or a holder that will not do is refused
    When mw member init makes "Luke Smith" with the vault "luke-vault", the Dolt port 3308, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying the name will not do
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 80, the holder "Jonathan (the Governor)" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying the port will not do
    When mw member init makes "luke" with the vault "luke-vault", the Dolt port 3308, the holder "" and the backend "http://10.88.0.2:8788"
    Then member init is refused, saying the holder is needed
    And no member config file, beads.env or Dolt unit was written

  Scenario: mw member remove stops and disables the unit and deletes nothing
    Given mw member init has made "luke" with the vault "luke-vault"
    When mw member remove runs
    Then member remove succeeds
    And systemctl disabled and stopped the Dolt unit
    And the member's config file, beads.env, Dolt unit file and vault are all still there
