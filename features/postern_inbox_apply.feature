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

  Scenario: A second verified tap on a bead already verified is refused, and no second comment is written
    Given a postern action "verified" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-verified-1"
    And a postern action "verified" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-verified-2"
    When mw postern inbox --apply is run
    Then bead "mw-act.3" has 1 comment
    And bead "mw-act.3"'s last comment reads "VERIFIED by the Governor via postern (tx-verified-1)"
    And mail "Verified: mw-act.3" was sent to mayor
    And mail "Not applied: verified mw-act.3" was sent to mayor

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

  Scenario: A voice note from the Governor is heard on this host, written on its bead, and sent back to him
    Given the postern inbox hears voice notes as "ship the storage engine as planned"
    And a postern voice note from "governor-pubkey-hex" threaded on bead "mw-act.3" with txid "direct:voice"
    When mw postern inbox --apply is run
    Then bead "mw-act.3"'s last comment reads "GOVERNOR (voice) via postern, txid direct:voice: ship the storage engine as planned"
    And the transcript "ship the storage engine as planned" was sent back to the Governor in bead "mw-act.3"'s thread, re "direct:voice"
    And mail "Voice: mw-act.3: ship the storage engine as planned" was sent to mayor
    And the txid "direct:voice" is marked applied

  Scenario: A hands step the Governor approved runs, and how it ran is written, sent back and mailed
    Given bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And the Governor approves the hands step "echo" on "mw-act.3" with txid "tx-run"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" ran with exit 0
    And bead "mw-act.3"'s last comment starts "RAN step echo on desktop as user, exit 0 (approved by the Governor via postern, txid tx-run)"
    And mail "Ran: mw-act.3 echo, exit 0" was sent to mayor
    And the txid "tx-run" is marked applied

  Scenario: An approval of a step that has changed since is refused, and the step does not run
    Given bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And the Governor approves the hands step "echo" on "mw-act.3" as it read before it changed, with txid "tx-stale"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" did not run
    And bead "mw-act.3"'s last comment starts "NOT RUN step echo (approved by the Governor via postern, txid tx-stale): the step changed since you approved it"
    And mail "Not run: mw-act.3 echo" was sent to mayor

  # keep and close answer a stale card (protocol section 11): keep hides the
  # bead from the view for a while, close finishes it. Both are applied only
  # as the Governor (section 13).
  Scenario: A keep tap writes the note and the comment, and the bead leaves the view
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And the stale epic "mw-old" has a held story filed 8 days ago
    And the postern view raises a stale need on "mw-old"
    And a postern keep action on bead "mw-old" for 14 days from the Governor with txid "tx-keep"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the note "postern.keep.mw-old" reads "2026-10-12T12:00:00Z"
    And bead "mw-old"'s last comment reads "KEPT by the Governor via postern (tx-keep) until 2026-10-12"
    And mail "Kept: mw-old" was sent to mayor
    And the txid "tx-keep" is marked applied
    And the postern view raises no stale need on "mw-old"

  Scenario: A keep tap that names no days keeps the bead for 30
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And a postern action "keep" on bead "mw-act.3" from "governor-pubkey-hex" with txid "tx-keep30"
    When mw postern inbox --apply is run
    Then the note "postern.keep.mw-act.3" reads "2026-10-28T12:00:00Z"
    And bead "mw-act.3"'s last comment reads "KEPT by the Governor via postern (tx-keep30) until 2026-10-28"

  Scenario: A keep tap from anyone but the Governor does nothing
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And a postern action "keep" on bead "mw-act.3" from "someone-else-pubkey-hex" with txid "tx-keep-other"
    When mw postern inbox --apply is run
    Then the note "postern.keep.mw-act.3" is not set
    And bead "mw-act.3" has no comment

  Scenario: A close tap on a held story closes it with the Governor's reason
    Given a postern action "close" on bead "mw-act.1" from "governor-pubkey-hex" with txid "tx-close"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "closed"
    And bead "mw-act.1" was closed with the reason "Closed by the Governor via postern (tx-close)"
    And mail "Closed: mw-act.1" was sent to mayor
    And the txid "tx-close" is marked applied

  Scenario: A close tap on an epic closes its two held stories, then the epic
    Given a postern action "close" on bead "mw-act" from "governor-pubkey-hex" with txid "tx-close-epic"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "closed"
    And bead "mw-act.2" now stands "closed"
    And bead "mw-act" now stands "closed"
    And bead "mw-act.1" was closed with the reason "Closed by the Governor via postern (tx-close-epic)"
    And bead "mw-act.2" was closed with the reason "Closed by the Governor via postern (tx-close-epic)"
    And bead "mw-act" was closed with the reason "Closed by the Governor via postern (tx-close-epic)"
    And mail "Closed: mw-act" was sent to mayor

  Scenario: A close tap on an epic with an in_progress story closes nothing and names the story
    Given bead "mw-act.2" is claimed by "mw@laptop"
    And a postern action "close" on bead "mw-act" from "governor-pubkey-hex" with txid "tx-close-busy"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "deferred"
    And bead "mw-act.2" now stands "in_progress"
    And bead "mw-act" now stands "open"
    And bead "mw-act" has no comment
    And mail "Not applied: close mw-act" was sent to mayor saying "mw-act.2"

  Scenario: A close tap from anyone but the Governor does nothing
    Given a postern action "close" on bead "mw-act.1" from "someone-else-pubkey-hex" with txid "tx-close-other"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "deferred"
    And no mail was sent for the reply
