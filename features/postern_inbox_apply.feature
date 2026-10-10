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
    Then it printed "already applied earlier: applied release mw-act.2 txid tx-once"
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

  Scenario: The Governor's message in a bead's channel is mailed to the Mayor with the command that answers it
    Given a postern message from "governor-pubkey-hex" in the channel of bead "mw-act.3" with text "ship it" and txid "direct:ch1"
    When mw postern inbox --apply is run
    Then mail "Governor on mw-act.3: ship it" was sent to mayor saying "answer in its thread: mw postern send --bead-channel mw-act.3 --re direct:ch1"

  # The demo card's Looks good tap sends those two words into the demo's
  # channel; on a demo bead, a message that is only those words closes it.
  Scenario: Looks good in a demo's channel closes the demo, quoting him, and the Mayor is mailed
    Given a demo story "mw-demo.1" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.1" with text "Looks good" and txid "direct:lg1"
    When mw postern inbox --apply is run
    Then bead "mw-demo.1" now stands "closed"
    And bead "mw-demo.1" was closed with the reason "Demo accepted on the Governor's 'Looks good' (txid direct:lg1)"
    And bead "mw-demo.1"'s last comment contains "The Governor by postern"
    And bead "mw-demo.1"'s last comment contains "Looks good"
    And bead "mw-demo.1"'s last comment contains "(txid direct:lg1)"
    And mail "Closed: mw-demo.1 on his Looks good" was sent to mayor
    And the txid "direct:lg1" is marked applied

  Scenario: A demo story known by its title is closed too
    Given a story "mw-demo.2" titled "Demo: the card shows" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.2" with text "Looks good" and txid "direct:lg2"
    When mw postern inbox --apply is run
    Then bead "mw-demo.2" now stands "closed"

  Scenario Outline: Case, spaces and one final full stop or exclamation mark do not matter
    Given a demo story "mw-demo.3" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.3" with text "<text>" and txid "direct:lg3"
    When mw postern inbox --apply is run
    Then bead "mw-demo.3" now stands "closed"
    And mail "Closed: mw-demo.3 on his Looks good" was sent to mayor

    Examples:
      | text           |
      | looks good.    |
      |   LOOKS GOOD!  |
      | Looks  Good    |

  Scenario Outline: Anything more than the words takes the ordinary path, and the demo stays open
    Given a demo story "mw-demo.4" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.4" with text "<text>" and txid "direct:lg4"
    When mw postern inbox --apply is run
    Then bead "mw-demo.4" now stands "open"
    And bead "mw-demo.4" is commented by the Governor saying "<text>"
    And mail "Governor on mw-demo.4: <text>" was sent to mayor

    Examples:
      | text                         |
      | Looks good, but the colour is off |
      | This looks good              |
      | Looks good!!                 |

  Scenario: Looks good on a bead that is not a demo does not close it
    Given a postern message from "governor-pubkey-hex" in the channel of bead "mw-act.3" with text "Looks good" and txid "direct:lg5"
    When mw postern inbox --apply is run
    Then bead "mw-act.3" now stands "open"
    And mail "Governor on mw-act.3: Looks good" was sent to mayor

  Scenario: Looks good on a demo already closed takes the ordinary path
    Given a demo story "mw-demo.6" is known to the tracker
    And bead "mw-demo.6" is already closed
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.6" with text "Looks good" and txid "direct:lg6"
    When mw postern inbox --apply is run
    Then mail "Governor on mw-demo.6: Looks good" was sent to mayor
    And bead "mw-demo.6" was not closed with the reason "Demo accepted on the Governor's 'Looks good' (txid direct:lg6)"

  Scenario: A second pass over the same Looks good does nothing
    Given a demo story "mw-demo.7" is known to the tracker
    And a postern message from "governor-pubkey-hex" in the channel of bead "mw-demo.7" with text "Looks good" and txid "direct:lg7"
    When mw postern inbox --apply is run
    And mw postern inbox --apply is run
    Then bead "mw-demo.7" has 1 comment
    And bead "mw-demo.7" now stands "closed"

  Scenario: Looks good from anyone but the Governor does not close the demo
    Given a demo story "mw-demo.8" is known to the tracker
    And a postern message from "someone-else-pubkey-hex" in the channel of bead "mw-demo.8" with text "Looks good" and txid "direct:lg8"
    When mw postern inbox --apply is run
    Then bead "mw-demo.8" now stands "open"

  Scenario: A voice note from the Governor is heard on this host, written on its bead, and sent back to him
    Given the postern inbox hears voice notes as "ship the storage engine as planned"
    And a postern voice note from "governor-pubkey-hex" in the channel of bead "mw-act.3" with txid "direct:voice"
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

  Scenario: a step whose bead waits on an open story is not run, and he is told what it waits on
    Given bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And bead "mw-act.3" waits on the open story "mw-blk" titled "Install the unit"
    And the Governor approves the hands step "echo" on "mw-act.3" with txid "tx-wait"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" did not run
    And bead "mw-act.3"'s last comment starts "NOT RUN step echo (approved by the Governor via postern, txid tx-wait): it waits on Install the unit (mw-blk). Approve it again once that is done."
    And the Governor was told "it waits on Install the unit (mw-blk). Approve it again once that is done." in bead "mw-act.3"'s thread
    And mail "Not run: mw-act.3 echo" was sent to mayor

  Scenario: a step that a newer one took the place of is not run, and he is told which
    Given bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And the hands step "echo" on "mw-act.3" is superseded by "mw-act.9"
    And the Governor approves the hands step "echo" on "mw-act.3" with txid "tx-old"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" did not run
    And bead "mw-act.3"'s last comment starts "NOT RUN step echo (approved by the Governor via postern, txid tx-old): it is superseded by mw-act.9, a newer step that took its place, so it never runs."
    And the Governor was told "Not run: replaced by mw-act.9; tap that one." in bead "mw-act.3"'s thread
    And mail "Not run: mw-act.3 echo" was sent to mayor

  Scenario: the same step runs once the blocker closes and he approves again
    Given bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And bead "mw-act.3" waits on the open story "mw-blk" titled "Install the unit"
    And the Governor approves the hands step "echo" on "mw-act.3" with txid "tx-early"
    When mw postern inbox --apply is run
    And the blocker "mw-blk" is closed
    And the Governor approves the hands step "echo" on "mw-act.3" again, with txid "tx-again"
    And mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" ran with exit 0
    And bead "mw-act.3"'s last comment starts "RAN step echo on desktop as user, exit 0 (approved by the Governor via postern, txid tx-again)"

  Scenario: an approval older than the limit is refused
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And the Governor approves the hands step "echo" on "mw-act.3" 6 minutes before the clock, with txid "tx-late"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" did not run
    And bead "mw-act.3"'s last comment starts "NOT RUN step echo (approved by the Governor via postern, txid tx-late): the approval is 6m0s old, over the 5m0s an approval is good for"

  Scenario: an approval signed before the step was added is refused
    Given the postern inbox clock reads "2026-09-28T11:02:00Z"
    And bead "mw-act.3" has the hands step "echo" on "desktop" as "user" running "echo done"
    And the Governor approves the hands step "echo" on "mw-act.3" 3 minutes before the clock, with txid "tx-before"
    When mw postern inbox --apply is run
    Then the hands step "echo" on "mw-act.3" did not run
    And bead "mw-act.3"'s last comment starts "NOT RUN step echo (approved by the Governor via postern, txid tx-before): you approved it at 2026-09-28T10:59:00Z, before the step was added at 2026-09-28T11:00:00Z"

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

  # The three answers to a chase need (protocol section 11 and 13): a bead
  # waiting on others for three working days is chased. The clock reads Monday
  # 28 September, 12:00 UTC, and the wait began Wednesday 23 September.
  Scenario: An ask_done tap does what mw ask done does, and is echoed
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And the home's event log is kept
    And live epic "mw-ask" has story "mw-ask.1" waiting on others since "2026-09-23T12:00:00Z"
    And a postern action "ask_done" on bead "mw-ask.1" from "governor-pubkey-hex" with txid "tx-done"
    When mw postern inbox --apply is run
    Then reading succeeds
    And bead "mw-ask.1" is no longer waiting on others
    And the note "ask.waiting.mw-ask.1" is not set
    And the postern view raises no chase need on "mw-ask.1"
    And bead "mw-ask.1"'s last comment reads "WAITING ENDED by the Governor via postern, txid tx-done: the other side delivered"
    And the events tail shows an action_applied event on "mw-ask.1" with detail "tx-done"
    And the txid "tx-done" is marked applied

  Scenario: A chase tap starts the wait again from now, so the next chase falls due three working days on
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And the home's event log is kept
    And live epic "mw-ask" has story "mw-ask.1" waiting on others since "2026-09-23T12:00:00Z"
    And the postern view raises a chase need on "mw-ask.1"
    And a postern action "chase" on bead "mw-ask.1" from "governor-pubkey-hex" with txid "tx-chase"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the note "ask.waiting.mw-ask.1" reads "2026-09-28T12:00:00Z"
    And bead "mw-ask.1" is still waiting on others
    And bead "mw-ask.1"'s last comment reads "CHASED by the Governor via postern, txid tx-chase: the wait starts again from 2026-09-28T12:00:00Z"
    And the events tail shows an action_applied event on "mw-ask.1" with detail "tx-chase"
    And the postern view raises no chase need on "mw-ask.1"
    When the postern inbox clock moves to "2026-09-30T12:00:00Z"
    Then the postern view raises no chase need on "mw-ask.1"
    When the postern inbox clock moves to "2026-10-01T12:00:00Z"
    Then the postern view raises a chase need on "mw-ask.1"

  Scenario: A keep_waiting tap puts the next chase three working days on and leaves the waiting mark
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And the home's event log is kept
    And live epic "mw-ask" has story "mw-ask.1" waiting on others since "2026-09-23T12:00:00Z"
    And the postern view raises a chase need on "mw-ask.1"
    And a postern action "keep_waiting" on bead "mw-ask.1" from "governor-pubkey-hex" with txid "tx-keepw"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the note "ask.waiting.mw-ask.1" reads "2026-09-28T12:00:00Z"
    And bead "mw-ask.1" is still waiting on others
    And bead "mw-ask.1"'s last comment reads "KEEP WAITING by the Governor via postern, txid tx-keepw: no chase until 2026-10-01"
    And the events tail shows an action_applied event on "mw-ask.1" with detail "tx-keepw"
    And the postern view raises no chase need on "mw-ask.1"
    When the postern inbox clock moves to "2026-10-01T12:00:00Z"
    Then the postern view raises a chase need on "mw-ask.1"

  Scenario Outline: A <action> tap on a bead not waiting on others is refused, and the Mayor is told why
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And the home's event log is kept
    And a postern action "<action>" on bead "mw-act.3" from "governor-pubkey-hex" with txid "<txid>"
    When mw postern inbox --apply is run
    Then reading succeeds
    And the note "ask.waiting.mw-act.3" is not set
    And bead "mw-act.3" has no comment
    And mail "Not applied: <action> mw-act.3" was sent to mayor saying "it is not waiting on others"
    And the events tail shows no events

    Examples:
      | action       | txid  |
      | ask_done     | tx-nd |
      | chase        | tx-nc |
      | keep_waiting | tx-nk |

  Scenario: A chase answer from anyone but the Governor does nothing
    Given the postern inbox clock reads "2026-09-28T12:00:00Z"
    And live epic "mw-ask" has story "mw-ask.1" waiting on others since "2026-09-23T12:00:00Z"
    And a postern action "chase" on bead "mw-ask.1" from "someone-else-pubkey-hex" with txid "tx-chase-other"
    When mw postern inbox --apply is run
    Then the note "ask.waiting.mw-ask.1" reads "2026-09-23T12:00:00Z"
    And bead "mw-ask.1" has no comment

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

  Scenario: A close tap on an epic with an open (released, unclaimed) child closes nothing and names the child
    Given epic "mw-act" has an open, unclaimed story "mw-act.4"
    And a postern action "close" on bead "mw-act" from "governor-pubkey-hex" with txid "tx-close-open"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "deferred"
    And bead "mw-act.2" now stands "deferred"
    And bead "mw-act.4" now stands "open"
    And bead "mw-act" now stands "open"
    And bead "mw-act" has no comment
    And nothing under epic "mw-act" was closed with the reason "Closed by the Governor via postern (tx-close-open)"
    And mail "Not applied: close mw-act" was sent to mayor saying "mw-act.4"

  Scenario: A close tap from anyone but the Governor does nothing
    Given a postern action "close" on bead "mw-act.1" from "someone-else-pubkey-hex" with txid "tx-close-other"
    When mw postern inbox --apply is run
    Then bead "mw-act.1" now stands "deferred"
    And no mail was sent for the reply
