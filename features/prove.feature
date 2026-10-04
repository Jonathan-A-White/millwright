Feature: mw prove shows the chain's proof of a stamped commit
  A landed commit is stamped on testnet by the chain-stamp job. Whoever holds the
  rig and the commit asks mw prove for the stamp: the txid, the block it is in
  and when, the preimage the stamp's commitment is made of, and where to read it
  on an explorer. A commit that was never stamped is an error.

  Scenario: A stamped commit in a block is proved
    Given the rig "millwright" has a commit "7b4430c1f2d9a8e3" that was stamped as "abc123txid"
    And the chain has "abc123txid" in block 1650123 at "2026-10-04 09:07:30"
    When I prove "millwright" "7b4430c"
    Then the proof succeeded
    And the proof says "abc123txid"
    And the proof says "1650123"
    And the proof says "2026-10-04 09:07:30 UTC"
    And the proof says "7b4430c1f2d9a8e3"
    And the proof says "https://test.whatsonchain.com/tx/abc123txid"

  Scenario: A commit that was never stamped is an error
    Given the rig "millwright" has a commit "7b4430c1f2d9a8e3" that was stamped as "abc123txid"
    When I prove "millwright" "deadbeef"
    Then the proof failed with "no stamp for millwright deadbeef"

  Scenario: A stamp still in the mempool has no block yet
    Given the rig "millwright" has a commit "7b4430c1f2d9a8e3" that was stamped as "abc123txid"
    And the chain has "abc123txid" in the mempool
    When I prove "millwright" "7b4430c1f2d9a8e3"
    Then the proof succeeded
    And the proof says "in the mempool, no block yet"
    And the proof says "abc123txid"
