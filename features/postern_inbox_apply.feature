Feature: mw postern inbox --apply
  The Governor's one-tap actions go straight to beads, at zero tokens, and
  the Mayor is told afterwards (postern's docs/protocol.md section 13). The
  postern backend's on-message hook runs mw postern inbox --apply: every
  message since the cursor that the Governor verifiably sent and this host
  knows how to apply — a reply, a comment in a bead's thread, an action — is
  applied at once, each at most once per txid, commented on its bead and
  mailed to the Mayor. It moves no cursor and prints no message's text. The
  Mayor's own read then shows an applied message as one line, and never
  applies it twice. An action this host does not know is left as text.

  Background:
    Given a throwaway postern key
    And mw postern inbox trusts "governor-pubkey-hex" as the Governor's key
    And epic "mw-act" has 2 held stories
    And bead "mw-act.3" is known to the tracker

  Scenario: The Governor's release tap on a held story is applied at once, and the cursor does not move
    Given a postern action "release" on bead "mw-act.1" from "governor-pubkey-hex" with txid "tx-release"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-act.1" now stands "open"
    And bead "mw-act.1"'s last comment reads "RELEASED by the Governor via postern, txid tx-release"
    And mail "Released: mw-act.1" was sent to mayor
    And the txid "tx-release" is marked applied
    And the postern inbox cursor has not moved
    And it did not print "action"

  Scenario: An action is applied once, however many passes see it
    Given a postern action "verified" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-verified"
    When mw postern inbox --apply is run
    And mw postern inbox --apply is run
    Then bead "mw-act.3" has 1 comment
    And bead "mw-act.3"'s last comment reads "VERIFIED by the Governor via postern (tx-verified)"

  Scenario: The Mayor's read shows an applied message as one line and never applies it twice
    Given a postern action "release" on bead "mw-act.2" from "governor-pubkey-hex" with txid "tx-once"
    When mw postern inbox --apply is run
    And mw postern inbox is run
    Then it printed "applied release mw-act.2 txid tx-once"
    And bead "mw-act.2" has 1 comment
    And the postern inbox cursor is saved as a note

  Scenario: A hold on an open, unclaimed story holds it
    Given a postern action "hold" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-hold"
    When mw postern inbox --apply is run
    Then bead "mw-act.3" now stands "deferred"
    And bead "mw-act.3"'s last comment reads "HELD by the Governor via postern, txid tx-hold"

  Scenario: A hold on a story already claimed is refused, and the Mayor is told why
    Given bead "mw-act.3" is claimed by "mw@laptop"
    And a postern action "hold" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-claimed"
    When mw postern inbox --apply is run
    Then bead "mw-act.3" now stands "in_progress"
    And bead "mw-act.3" has no comment
    And mail "Not applied: hold mw-act.3" was sent to mayor

  Scenario: A priority tap sets the bead's priority
    Given a postern priority 0 action on bead "mw-act.3" from the Governor with txid "tx-p0"
    When mw postern inbox --apply is run
    Then bead "mw-act.3" now has priority 0
    And bead "mw-act.3"'s last comment reads "PRIORITY 0 set by the Governor via postern, txid tx-p0"

  Scenario: An action from anyone but the Governor is not applied
    Given a postern action "release" on bead "mw-act.1" from "someone-else-pubkey-hex" with txid "tx-other"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "deferred"
    And no mail was sent for the reply

  Scenario: An action this host does not know is left for the Mayor to read as text
    Given a postern action "dance" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-dance"
    When mw postern inbox --apply is run
    And mw postern inbox is run
    Then it printed "dance"
    And bead "mw-act.3" has no comment
