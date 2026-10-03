Feature: Keeping the vault and beads in step between hosts
  mw sync is the one command that brings this host level with the other one. It
  runs by hand, from the dispatcher, or on a timer, and always in the same
  order: the vault's files first, then the beads database, carrying a note of
  when this host was level, so that the other host can tell how stale this one
  is. It never migrates and it never forces. A sync that cannot finish stops
  and says why in plain words rather than resolving anything itself. One thing
  does not stop all of it: uncommitted work in the vault blocks the vault's half
  only. The beads half runs anyway, so a timer keeps the hosts level in beads
  while somebody's edit waits for them, and mw leaves with a status of its own
  for that.

  All of that is beads_sync = remote, every host keeping a copy of its own. On
  the host that keeps the one database (backup) the tracker's remote is only a
  backup, pushed once one is due, and a halted backup stops nothing; on a host
  whose beads live in another host's database (shared) there is no remote
  cycle and no collection at all. In both the note of when this host was level
  is written on every sync, straight into the one database every host reads.

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

  Scenario: The note reaches the other host in the same sync that wrote it
    When this host syncs
    Then the sync succeeds
    And the other host's next sync reads the time of the sync under host.vps.last_sync

  Scenario: An unresolvable beads conflict stops the sync after its one retry
    Given bd sync will exit 2
    When this host syncs
    Then the sync fails, and mw stops with a non-zero exit
    And the failure says, in plain words:
      | merge conflict |
      | by hand        |
    And the beads database was synced twice, the conflict retried once
    And nothing is recorded under host.vps.last_sync

  Scenario: A beads conflict that clears on retry is level, with a notice
    Given bd sync will exit 2, then clear on the next try
    When this host syncs
    Then the sync succeeds
    And the sync reports that the conflict cleared on retry
    And the beads database was synced twice, the conflict retried once
    And the beads database holds the time of the sync under host.vps.last_sync

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

  Scenario: Uncommitted vault work leaves the vault alone, and beads sync anyway
    Given the other host appended a line to the Mayor's ledger and pushed it
    And this host has an uncommitted change to the Mayor's ledger
    When this host syncs
    Then the sync stops with the vault blocked
    And mw exits 5, which is neither a plain failure nor one of bd's own
    And the failure says, in plain words:
      | uncommitted           |
      | seats/mayor/ledger.md |
    And the failure is one line
    And the uncommitted change is still there, and nothing was pulled over it
    And the vault is where it was on this host
    And the beads database was synced once
    And nothing is recorded under host.vps.last_sync

  Scenario: A beads halt is what stops a sync whose vault half was blocked too
    Given this host has an uncommitted change to the Mayor's ledger
    And bd sync will exit 2
    When this host syncs
    Then the sync fails, and mw stops with a non-zero exit
    And the failure says, in plain words:
      | merge conflict |
    And mw exits 2
    And nothing is recorded under host.vps.last_sync

  Scenario: The host that keeps the one database records it was level, and backs up only once a backup is due
    Given this host keeps the one beads database, backed up every 30 minutes
    And its last backup of the beads database was 10 minutes ago
    When this host syncs
    Then the sync succeeds
    And the beads database holds the time of the sync under host.vps.last_sync
    And the beads database was never synced
    And the sync says no backup was due

  Scenario: The host that keeps the one database backs it up once the last backup is old enough
    Given this host keeps the one beads database, backed up every 30 minutes
    And its last backup of the beads database was 31 minutes ago
    When this host syncs
    Then the sync succeeds
    And the beads database was synced once
    And the beads database holds the time of the sync under host.vps.last_backup
    And the sync says a backup ran

  Scenario: A sync asked to leave the backup alone does not push one, though one is due
    Given this host keeps the one beads database, backed up every 30 minutes
    And its last backup of the beads database was 31 minutes ago
    And the sync is asked to leave the backup to another run
    When this host syncs
    Then the sync succeeds
    And the beads database holds the time of the sync under host.vps.last_sync
    And the beads database was never synced
    And the beads database was never asked to reclaim its disk space
    And the sync says the backup was left to another run

  Scenario: A backup that halts is said, and stops nothing on the host that keeps the one database
    Given this host keeps the one beads database, backed up every 30 minutes
    And bd sync will exit 2
    When this host syncs
    Then the sync succeeds
    And the sync says the backup halted on a merge conflict
    And the beads database holds the time of the sync under host.vps.last_sync
    And nothing is recorded under host.vps.last_backup

  Scenario: A host whose beads live in another host's database never syncs or collects them
    Given this host's beads live in another host's database
    When this host syncs
    Then the sync succeeds
    And the beads database holds the time of the sync under host.vps.last_sync
    And the beads database was never synced
    And the beads database was never asked to reclaim its disk space
