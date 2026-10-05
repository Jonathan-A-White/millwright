Feature: mw stamp queues a chain stamp of a rig's head by hand
  The stamps of landings and vault pushes are queued on their own. A head that
  was never landed or pushed by mw has none, and mw stamp queues one: the head
  of origin's default branch, or a commit named, with no story and the title
  "Head of <rig>: <subject>". A commit that is already stamped, sent or still
  waiting, is refused. --all stamps every rig whose head has no stamp.

  Scenario: A rig's head is queued
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    When I stamp the rig "bsv-kit"
    Then the stamp succeeded
    And the stamp output says "queued"
    And 1 stamp is queued: rig "bsv-kit" commit "8fb17a28c1" with no story and title "Head of bsv-kit: Subject of 8fb17a28c1"

  Scenario: A commit named is queued instead of the head
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    And the rig "bsv-kit" has an older commit "11aa22bb33"
    When I stamp the rig "bsv-kit" at the commit "11aa22bb33"
    Then the stamp succeeded
    And 1 stamp is queued: rig "bsv-kit" commit "11aa22bb33" with no story and title "Head of bsv-kit: Subject of 11aa22bb33"

  Scenario: An already stamped commit is refused
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    And the head of "bsv-kit" was stamped as "abc123txid"
    When I stamp the rig "bsv-kit"
    Then the stamp failed with "already stamped: abc123txid"
    And 0 stamps are queued

  Scenario: A commit already waiting in the queue is refused
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    When I stamp the rig "bsv-kit"
    And I stamp the rig "bsv-kit"
    Then the stamp failed with "already stamped: queued, not yet sent"
    And 1 stamp is queued: rig "bsv-kit" commit "8fb17a28c1" with no story and title "Head of bsv-kit: Subject of 8fb17a28c1"

  Scenario: A rig that is not in the config is refused
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    When I stamp the rig "nowhere"
    Then the stamp failed with "no rig nowhere in the config's [rigs] table"

  Scenario: --all skips the rigs whose head is already stamped
    Given the rigs "bsv-kit" and "cairn" have heads "8fb17a28c1" and "088a208b77"
    And the head of "bsv-kit" was stamped as "abc123txid"
    When I stamp every rig
    Then the stamp succeeded
    And the stamp output says "skipped bsv-kit: already stamped: abc123txid"
    And 1 stamp is queued: rig "cairn" commit "088a208b77" with no story and title "Head of cairn: Subject of 088a208b77"
