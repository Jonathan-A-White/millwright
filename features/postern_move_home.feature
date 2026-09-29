Feature: mw postern inbox --apply moves the home on the Governor's tap
  The Governor's one tap in Postern sends a message of class move-home naming
  the host to make home (postern's docs/protocol.md section 18). The backend's
  on-message hook, mw postern inbox --apply, runs mw home move on the host it
  names, and only when the record's signer, the key the backend vouches for, is
  the Governor's and the message is under 30 minutes old: anything else is
  refused and nothing runs. The move is --planned when the old home answers
  ssh, and --old-home-dead when it does not; its start and its result are
  written on the home move's bead. On a host that is not home the pass applies
  nothing but a move-home, so a boost's hook never records anything twice.

  Background:
    Given a throwaway postern key
    And mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And this host is "desktop" and the vault's home file names "laptop"
    And the home move is written on bead "mw-home"

  Scenario: A move-home the Governor signed, naming this host, runs mw home move once, planned while the old home answers
    Given the old home answers ssh
    And a move-home to "desktop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-move"
    When mw postern inbox --apply is run
    Then reading succeeds
    And mw home move ran once, as "desktop --planned"
    And bead "mw-home" has 2 comments
    And bead "mw-home"'s first comment starts "MOVE-HOME to desktop started: mw home move desktop --planned (the Governor via postern, txid tx-move)"
    And bead "mw-home"'s last comment starts "MOVE-HOME to desktop ran: mw home move desktop --planned, exit 0 (the Governor via postern, txid tx-move)"
    And mail "Home move to desktop: exit 0" was sent to mayor
    And the txid "tx-move" is marked applied

  Scenario: With the old home silent, the move is --old-home-dead
    Given the old home does not answer ssh
    And a move-home to "desktop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-dead"
    When mw postern inbox --apply is run
    Then mw home move ran once, as "desktop --old-home-dead"

  Scenario: A move-home seen by a second pass is not run again
    Given the old home does not answer ssh
    And a move-home to "desktop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-twice"
    When mw postern inbox --apply is run
    And mw postern inbox --apply is run
    Then mw home move ran once, as "desktop --old-home-dead"

  Scenario: The same message signed by another key runs nothing and is recorded as refused
    Given the old home does not answer ssh
    And a move-home to "desktop" signed by "someone-else-pubkey-hex", sent 5 minutes ago, with txid "tx-forged"
    When mw postern inbox --apply is run
    Then reading succeeds
    And mw home move did not run
    And the txid "tx-forged" is marked refused, saying "not signed by the Governor's key"

  Scenario: A move-home whose signer the backend cannot vouch for runs nothing and is recorded as refused
    Given an unsigned move-home to "desktop", sent 5 minutes ago, with txid "tx-unsigned"
    When mw postern inbox --apply is run
    Then mw home move did not run
    And the txid "tx-unsigned" is marked refused, saying "not signed by the Governor's key"

  Scenario: A move-home naming the other host runs nothing here
    Given a move-home to "laptop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-other-host"
    When mw postern inbox --apply is run
    Then reading succeeds
    And mw home move did not run
    And the txid "tx-other-host" is not marked
    And bead "mw-home" has no comment

  Scenario: A move-home older than 30 minutes is a replay, and is refused
    Given a move-home to "desktop" signed by "governor-pubkey-hex", sent 31 minutes ago, with txid "tx-old"
    When mw postern inbox --apply is run
    Then mw home move did not run
    And the txid "tx-old" is marked refused, saying "older than 30 minutes"

  Scenario: On a boost, the Governor's other words are not applied
    Given epic "mw-act" has 1 held stories
    And a postern action "release" on bead "mw-act.1" from "governor-pubkey-hex" with txid "tx-release"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-act.1" now stands "deferred"
    And bead "mw-act.1" has no comment
    And no mail was sent for the reply
    And the txid "tx-release" is not marked

  Scenario: On the home, the Governor's other words are applied as before
    Given this host is "laptop" and the vault's home file names "laptop"
    And epic "mw-act" has 1 held stories
    And a postern action "release" on bead "mw-act.1" from "governor-pubkey-hex" with txid "tx-release"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "open"
    And the txid "tx-release" is marked applied

  Scenario: On the home, a move-home naming it is already done, and runs nothing
    Given this host is "laptop" and the vault's home file names "laptop"
    And a move-home to "laptop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-already"
    When mw postern inbox --apply is run
    Then mw home move did not run
    And the txid "tx-already" is marked refused, saying "laptop is already home"

  Scenario: A boost whose tracker cannot be read still hears a move-home and runs it
    Given the old home does not answer ssh
    And the tracker cannot be reached
    And a move-home to "desktop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-dark"
    When mw postern inbox --apply is run
    Then reading succeeds
    And mw home move ran once, as "desktop --old-home-dead"

  Scenario: A boost whose tracker cannot be read starts a move-home once, however many passes see it
    Given the old home does not answer ssh
    And the tracker cannot be reached
    And a move-home to "desktop" signed by "governor-pubkey-hex", sent 5 minutes ago, with txid "tx-dark-twice"
    When mw postern inbox --apply is run
    And mw postern inbox --apply is run
    Then mw home move ran once, as "desktop --old-home-dead"
