Feature: The chain-stamp job broadcasts the stamps a landing queued
  A landing queues a stamp of the commit it landed. The follower's chain-stamp
  job takes each pending stamp, broadcasts it on testnet through the backend,
  records the txid beside the stamp in sent.jsonl and comments
  "STAMP <txid> for <commit> (testnet)" on the story. A stamp that cannot be
  broadcast stays pending and is tried again on the next pass, with no limit.

  Scenario: Each pending stamp is broadcast once, recorded as sent and commented on its story
    Given a stamp queue holding stamps for the stories "mw-a.1" and "mw-b.1"
    When the chain-stamp job runs
    Then 2 transactions were broadcast
    And no stamp is pending
    And sent.jsonl holds 2 stamps, each with the txid its broadcast returned
    And the story "mw-a.1" carries the comment "STAMP fake-txid-1 for commit-of-mw-a.1 (testnet)"
    And the story "mw-b.1" carries the comment "STAMP fake-txid-2 for commit-of-mw-b.1 (testnet)"

  Scenario: A backend that fails leaves the stamp pending and the job succeeds
    Given a stamp queue holding stamps for the stories "mw-a.1"
    And the backend refuses every broadcast, saying: connection refused
    When the chain-stamp job runs
    Then the job succeeded
    And 1 stamp is pending, with 1 failed try
    And sent.jsonl holds 0 stamps, each with the txid its broadcast returned
    And the story "mw-a.1" carries no comment

  Scenario: The next pass retries a stamp the last one could not send
    Given a stamp queue holding stamps for the stories "mw-a.1"
    And the backend refuses every broadcast, saying: connection refused
    And the chain-stamp job runs
    When the backend takes broadcasts again
    And the chain-stamp job runs
    Then no stamp is pending
    And sent.jsonl holds 1 stamps, each with the txid its broadcast returned
    And the story "mw-a.1" carries the comment "STAMP fake-txid-1 for commit-of-mw-a.1 (testnet)"
