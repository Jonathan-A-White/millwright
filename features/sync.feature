Feature: Keeping the vault and beads in step between hosts
  mw sync is the one command that brings this host level with the other one. It
  runs by hand, from the dispatcher, or on a timer, and always in the same
  order: the vault's files first, then the beads database, and last a note of
  when this host was level, so that the other host can tell how stale this one
  is. It never migrates and it never forces. A sync that cannot finish stops
  and says why in plain words rather than resolving anything itself.

  Background:
    Given a vault shared by both hosts
    And this host is "vps"
    And the vault marks ledgers as append-only

  Scenario: Both hosts appending different ledger lines survive one sync
    Given the other host appended a line to the Mayor's ledger and pushed it
    And this host appended its own line to the Mayor's ledger
    When this host syncs
    Then the sync succeeds
    And the Mayor's ledger on this host holds, in this order:
      | the other host worked mw-gq6.9 |
      | this host worked mw-gq6.11     |
    And no conflict is left in the vault
    And the other host sees both lines once it pulls

  Scenario: The other host's work is pulled and this host's is pushed
    Given the other host appended a line to the Mayor's ledger and pushed it
    And this host appended its own line to the Mayor's ledger
    When this host syncs
    Then the sync succeeds
    And the sync reports 1 commit pulled and 1 commit pushed

  Scenario: A sync with nothing to do changes nothing
    When this host syncs
    Then the sync succeeds
    And the sync reports nothing pulled and nothing pushed
    And the vault is where it was on both hosts
    And the beads database was synced once

  Scenario: A successful sync records when this host was last level
    When this host syncs
    Then the sync succeeds
    And the beads database holds the time of the sync under host.vps.last_sync

  Scenario: An unresolvable beads conflict stops the sync
    Given bd sync will exit 2
    When this host syncs
    Then the sync fails, and mw stops with a non-zero exit
    And the failure says, in plain words:
      | merge conflict |
      | by hand        |
    And the beads database was synced once
    And nothing is recorded under host.vps.last_sync

  Scenario: A stuck working set stops the sync
    Given bd sync will exit 4
    When this host syncs
    Then the sync fails, and mw stops with a non-zero exit
    And the failure says, in plain words:
      | stuck        |
      | nothing was pushed |
    And the beads database was synced once
    And nothing is recorded under host.vps.last_sync

  Scenario: A vault that does not mark its ledgers yet is marked before anything merges
    Given the vault does not mark ledgers as append-only
    And the other host appended a line to the Mayor's ledger and pushed it
    And this host appended its own line to the Mayor's ledger
    When this host syncs
    Then the sync succeeds
    And the vault marks seats/*/ledger.md as merge=union
    And the sync reports that the mark was added
    And no conflict is left in the vault

  Scenario: Uncommitted vault work stops the sync before anything moves
    Given the other host appended a line to the Mayor's ledger and pushed it
    And this host has an uncommitted change to the Mayor's ledger
    When this host syncs
    Then the sync fails, and mw stops with a non-zero exit
    And the failure says, in plain words:
      | uncommitted           |
      | seats/mayor/ledger.md |
    And the beads database was never synced
    And the vault is where it was on this host
