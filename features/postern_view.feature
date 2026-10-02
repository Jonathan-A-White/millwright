Feature: mw postern view
  mw postern view builds the live view of the factory the Governor's app
  shows (postern's docs/protocol.md section 11): every live epic, every bead
  under one at any depth — a closed one only when it closed within the week,
  the rest counted in its epic's done_earlier — and everything waiting on the
  Governor, most blocking first, then oldest. It seals the view — gzip, then
  BRC-78 to the Governor's key, then base64 — and writes it where the postern
  backend serves it. It is cheap enough to run every half minute: one tracker
  call per live epic for its children, and comments read only for a question
  or a landing that needs them.

  Background:
    Given the view's epic "mw-v" is live
    And the view's bead "mw-v.1" under "mw-v" is labelled "hitl"
    And the view's bead "mw-v.2" under "mw-v" waits on "mw-v.1"
    And the view's bead "mw-v.3" under "mw-v" landed an hour ago
    And the view's bead "mw-v.4" under "mw-v" has a postern question open since two hours ago
    And the view's bead "mw-v.5" under "mw-v" landed a month ago

  Scenario: The view lists the epic's beads and what waits on the Governor, most blocking first
    When the live view is built
    Then the view's needs are "hands:mw-v.1, question:mw-v.4, verify:mw-v.3"
    And the view's need on "mw-v.1" blocks 1 bead
    And the view's bead "mw-v.2" waits on "mw-v.1"

  Scenario: A bead closed a month ago is only counted in its epic's done_earlier
    When the live view is built
    Then the view has no bead "mw-v.5"
    And the view's epic "mw-v" has 1 done earlier

  Scenario: The written view is sealed, and opens from gzip to the view that was built
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    When the live view is run and written
    Then the written view opens, from gzip, to a v 2 view holding "mw-v.1"
    And the view read no comments of "mw-v.1" or "mw-v.2"

  Scenario: A hands need carries its steps, each with the hash his approval binds and how it ran
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the view's bead "mw-v.1" has the hands step "restart" on "vps" as "user" running "systemctl --user restart mw-dispatch"
    And the view's hands step "linger" on "mw-v.1" ran with exit 0
    When the live view is built
    Then the view's hands need on "mw-v.1" carries the step "linger" with its sha256, run with exit 0

  # A Verify card is his only when the story's closing comment tells him how
  # to check it (HOW TO CHECK IT); a landing without that waits on the Mayor.
  Scenario: A landed story with HOW TO CHECK IT is a Verify card for the Governor carrying the steps
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Done and green. HOW TO CHECK IT, for the Governor: 1. Open the app and tap Needs you. 2. The card for mw-v.6 reads Verify, with a Verified button.\nFor the rig memory: nothing"
    When the live view is built
    Then the view's verify need on "mw-v.6" says "1. Open the app and tap Needs you. 2. The card for mw-v.6 reads Verify, with a Verified button."
    And the view's need "verify" on "mw-v.6" waits for "you"
    And the view's verify need on "mw-v.6" offers "Verified"

  Scenario: A landed story without HOW TO CHECK IT waits on the Mayor
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Internal: nothing for the Governor to look at."
    When the live view is built
    Then the view's need "verify" on "mw-v.6" waits for "mayor"
    And the view's verify need on "mw-v.6" is waiting on "the Mayor to check the landing"
    And the view's verify need on "mw-v.6" offers nothing
    And the view's need "verify" on "mw-v.3" waits for "mayor"

  Scenario: A landing whose HOW TO CHECK IT is the Internal line waits on the Mayor
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Done. HOW TO CHECK IT: Internal: nothing for the Governor to look at.\nFor the rig memory: nothing"
    When the live view is built
    Then the view's need "verify" on "mw-v.6" waits for "mayor"
    And the view's verify need on "mw-v.6" is waiting on "the Mayor to check the landing"
    And the view's verify need on "mw-v.6" offers nothing

  Scenario: A later comment that only mentions HOW TO CHECK IT does not replace the steps
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Done. HOW TO CHECK IT, for the Governor: 1. Open the app and tap Needs you.\nFor the rig memory: nothing"
    And the view's bead "mw-v.6" has the comment "HOW TO CHECK IT is in the closing comment)."
    When the live view is built
    Then the view's verify need on "mw-v.6" says "1. Open the app and tap Needs you."
    And the view's need "verify" on "mw-v.6" waits for "you"

  Scenario: A HOW TO CHECK IT heading with a qualifier before its colon still counts
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Done. HOW TO CHECK IT, for the Governor (after the backend is swapped and the new site is live):\n\n1. Open the app and tap Needs you.\nFor the rig memory: nothing"
    When the live view is built
    Then the view's verify need on "mw-v.6" says "1. Open the app and tap Needs you."
    And the view's need "verify" on "mw-v.6" waits for "you"
    And the view's verify need on "mw-v.6" offers "Verified"

  Scenario: A HOW TO CHECK IT heading with no colon still counts
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Done.\nHOW TO CHECK IT, for the Governor\n\n1. Open the app and tap Needs you.\nFor the rig memory: nothing"
    When the live view is built
    Then the view's verify need on "mw-v.6" says "1. Open the app and tap Needs you."
    And the view's need "verify" on "mw-v.6" waits for "you"
    And the view's verify need on "mw-v.6" offers "Verified"

  Scenario: A VERIFIED comment still clears both
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "HOW TO CHECK IT: 1. Open the app."
    And the view's bead "mw-v.6" has the comment "VERIFIED by the Governor via postern"
    And the view's bead "mw-v.7" under "mw-v" landed two hours ago with the Mayor's comment "Internal: nothing for the Governor to look at."
    And the view's bead "mw-v.7" has the comment "VERIFIED by the Mayor"
    When the live view is built
    Then the view's needs on "mw-v.6" are "none"
    And the view's needs on "mw-v.7" are "none"

  Scenario: The memory answers a second run with no comment read
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "HOW TO CHECK IT: 1. Open the app and tap Needs you."
    When the live view is built
    And the view's reads of the comments of "mw-v.6" are counted
    And the live view is built
    Then the view has read the comments of "mw-v.6" no more than counted
    And the view's verify need on "mw-v.6" says "1. Open the app and tap Needs you."

  Scenario: A closed hands bead leaves Needs you with no Verify card
    Given the view's hands bead "mw-v.7" under "mw-v" closed an hour ago
    When the live view is built
    Then the view's needs on "mw-v.7" are "none"
    And the view's needs are "hands:mw-v.1, question:mw-v.4, verify:mw-v.3"

  Scenario: A hands need leaves the view once its one step has run with exit 0
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the view's hands step "linger" on "mw-v.1" ran with exit 0
    When the live view is built
    Then the view's needs are "question:mw-v.4, verify:mw-v.3"

  Scenario: A hands need stays when its step ran with a non-zero exit
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the view's hands step "linger" on "mw-v.1" ran with exit 1
    When the live view is built
    Then the view's needs are "hands:mw-v.1, question:mw-v.4, verify:mw-v.3"
    And the view's hands need on "mw-v.1" carries the step "linger" with its sha256, run with exit 1

  Scenario: A hands need stays when its step has not run
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    When the live view is built
    Then the view's needs are "hands:mw-v.1, question:mw-v.4, verify:mw-v.3"

  # A Release card names each story it holds, with when it was filed; a held
  # story still behind an open bead is the factory's to wait on, not his.
  Scenario: a Release card lists its held stories with their filing times
    Given the view's epic "mw-rel" is live
    And the view's held story "mw-rel.1" under "mw-rel" was filed 2 days ago
    And the view's held story "mw-rel.2" under "mw-rel" was filed 1 day ago
    When the live view is built
    Then the view's needs on "mw-rel" are "approve:mw-rel"
    And the view's approve need on "mw-rel" says "2 held stories wait for your Release: mw-rel.1 Story mw-rel.1, filed 26 Sep 12:00 UTC; mw-rel.2 Story mw-rel.2, filed 27 Sep 12:00 UTC"
    And the view's approve need on "mw-rel" offers "Release"

  Scenario: a story held behind an open hitl bead makes no Release card
    Given the view's epic "mw-r5s2i" is live
    And the view's hitl bead "mw-r5s2i.1" under "mw-r5s2i" was filed 2 days ago
    And the view's held story "mw-r5s2i.2" under "mw-r5s2i" waits on "mw-r5s2i.1"
    When the live view is built
    Then the view's needs on "mw-r5s2i" are "none"
    And the view's need "approve" on "mw-r5s2i.2" waits for "factory"
    And the view's approve need on "mw-r5s2i.2" says "held; waits on Story mw-r5s2i.1"
    And the view's approve need on "mw-r5s2i.2" offers nothing

  Scenario: a Release card holds only the stories whose blockers are closed
    Given the view's epic "mw-mix" is live
    And the view's hitl bead "mw-mix.1" under "mw-mix" was filed 2 days ago
    And the view's held story "mw-mix.2" under "mw-mix" waits on "mw-mix.1"
    And the view's held story "mw-mix.3" under "mw-mix" was filed 1 day ago
    When the live view is built
    Then the view's approve need on "mw-mix" says "1 held story waits for your Release: mw-mix.3 Story mw-mix.3, filed 27 Sep 12:00 UTC"
    And the view's need "approve" on "mw-mix.2" waits for "factory"

  Scenario: an epic released before says so
    Given the view's epic "mw-again" is live
    And the view's bead "mw-again" has the comment "RELEASED by the Governor via postern, txid abc123: 2 held stories, ready: mw-again.1" dated 2 days ago
    And the view's held story "mw-again.3" under "mw-again" was filed 1 day ago
    When the live view is built
    Then the view's approve need on "mw-again" says "1 held story waits for your Release: mw-again.3 Story mw-again.3, filed 27 Sep 12:00 UTC. You released this epic on 26 Sep 12:00 UTC; this story was filed after."

  # A held story left over a week, or a hands step left over three days, is
  # no longer a plain approve or hands card: it is one stale card that says
  # the facts and asks Keep or Close (protocol section 11).
  Scenario: An epic whose oldest held story is 8 days old gives one stale need and no approve need
    Given the view's epic "mw-old" is live
    And the view's held story "mw-old.1" under "mw-old" was filed 8 days ago
    And the view's held story "mw-old.2" under "mw-old" was filed 2 days ago
    And the view's bead "mw-old" has the comment "Waiting on the Governor's word about the new formula, which is a long sentence."
    When the live view is built
    Then the view's needs on "mw-old" are "stale:mw-old"
    And the view's stale need on "mw-old" says "approve: Epic mw-old; 2 held stories; waiting 8 days; newest comment: Waiting on the Governor's word about the new formula, which is a long sentence."
    And the view's stale need on "mw-old" has the options "Keep, Close" and has been stale since "2026-09-27T12:00:00Z"

  Scenario: An epic whose oldest held story is 6 days old still gives approve
    Given the view's epic "mw-fresh" is live
    And the view's held story "mw-fresh.1" under "mw-fresh" was filed 6 days ago
    When the live view is built
    Then the view's needs on "mw-fresh" are "approve:mw-fresh"

  Scenario: A hitl bead 4 days old gives stale in place of hands, naming the step not run
    Given the view's epic "mw-hnd" is live
    And the view's hitl bead "mw-hnd.1" under "mw-hnd" was filed 4 days ago
    And the view's bead "mw-hnd.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the view's bead "mw-hnd.1" has the comment "Please run the step when you are at the desk" dated 3 days ago
    When the live view is built
    Then the view's needs on "mw-hnd.1" are "stale:mw-hnd.1"
    And the view's stale need on "mw-hnd.1" says "hands: Story mw-hnd.1; step linger not run; waiting 4 days; newest comment 25 Sep 2026: Please run the step when you are at the desk"

  Scenario: A hitl bead 2 days old still gives hands
    Given the view's epic "mw-hnd" is live
    And the view's hitl bead "mw-hnd.1" under "mw-hnd" was filed 2 days ago
    When the live view is built
    Then the view's needs on "mw-hnd.1" are "hands:mw-hnd.1"

  Scenario: A newest comment is cut to its first 200 characters
    Given the view's epic "mw-hnd" is live
    And the view's hitl bead "mw-hnd.1" under "mw-hnd" was filed 4 days ago
    And the view's bead "mw-hnd.1" has a comment of 300 letters
    When the live view is built
    Then the view's stale need on "mw-hnd.1" quotes 200 letters of its newest comment

  Scenario: A keep note ahead of now hides the stale, approve and hands needs of a bead
    Given the view's epic "mw-kept" is live
    And the view's held story "mw-kept.1" under "mw-kept" was filed 8 days ago
    And the view's hitl bead "mw-kept.2" under "mw-kept" was filed 4 days ago
    And the view's bead "mw-kept" is kept until 10 days ahead
    And the view's bead "mw-kept.2" is kept until 10 days ahead
    When the live view is built
    Then the view's needs on "mw-kept" are "none"
    And the view's needs on "mw-kept.2" are "none"

  Scenario: A keep note ahead of now hides an approve need that is not yet stale
    Given the view's epic "mw-kept" is live
    And the view's held story "mw-kept.1" under "mw-kept" was filed 2 days ago
    And the view's bead "mw-kept" is kept until 10 days ahead
    When the live view is built
    Then the view's needs on "mw-kept" are "none"

  Scenario: A keep note in the past hides nothing
    Given the view's epic "mw-kept" is live
    And the view's held story "mw-kept.1" under "mw-kept" was filed 8 days ago
    And the view's hitl bead "mw-kept.2" under "mw-kept" was filed 4 days ago
    And the view's bead "mw-kept" is kept until 1 day ago
    And the view's bead "mw-kept.2" is kept until 1 day ago
    When the live view is built
    Then the view's needs on "mw-kept" are "stale:mw-kept"
    And the view's needs on "mw-kept.2" are "stale:mw-kept.2"

  # Every need says who it waits on: you, mayor or factory (protocol section 11).
  Scenario: A hands bead with no step waits on the Mayor
    Given the view's epic "mw-who" is live
    And the view's hitl bead "mw-who.1" under "mw-who" was filed 2 days ago
    When the live view is built
    Then the view's hands need on "mw-who.1" waits for "mayor"
    And the view's hands need on "mw-who.1" is not ready, waiting on "the Mayor to write the steps"

  Scenario: A hands bead with a BY HAND comment is his, with Done
    Given the view's epic "mw-who" is live
    And the view's hitl bead "mw-who.1" under "mw-who" was filed 2 days ago
    And the view's bead "mw-who.1" has the comment "BY HAND: open the router page and switch the guest network off."
    When the live view is built
    Then the view's hands need on "mw-who.1" waits for "you"
    And the view's hands need on "mw-who.1" is ready, saying "open the router page and switch the guest network off."

  Scenario: A hands step whose bead waits on an open story waits on the factory, naming it
    Given the view's epic "mw-who" is live
    And the view's story "mw-who.1" under "mw-who" is open
    And the view's hitl bead "mw-who.2" under "mw-who" waits on "mw-who.1"
    And the view's bead "mw-who.2" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    When the live view is built
    Then the view's hands need on "mw-who.2" waits for "factory"
    And the view's hands need on "mw-who.2" is not ready, waiting on "Story mw-who.1"

  Scenario: A hands bead whose only unrun step a newer one took the place of is not his to tap
    Given the view's epic "mw-who" is live
    And the view's hitl bead "mw-who.1" under "mw-who" was filed 2 days ago
    And the view's bead "mw-who.1" has the hands step "swap" on "desktop" as "user" running "echo swap"
    And the view's hands step "swap" on "mw-who.1" is superseded by "mw-who.9"
    When the live view is built
    Then the view's hands need on "mw-who.1" is not ready, waiting on "superseded by mw-who.9"

  Scenario: A hands bead with a step and nothing open before it is his
    Given the view's epic "mw-who" is live
    And the view's hitl bead "mw-who.1" under "mw-who" was filed 2 days ago
    And the view's bead "mw-who.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    When the live view is built
    Then the view's hands need on "mw-who.1" waits for "you"
    And the view's hands need on "mw-who.1" is ready

  Scenario: A question, a landing to verify and a stale card wait on him
    Given the view's epic "mw-old" is live
    And the view's held story "mw-old.1" under "mw-old" was filed 8 days ago
    And the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "HOW TO CHECK IT: 1. Open the app."
    When the live view is built
    Then the view's need "question" on "mw-v.4" waits for "you"
    And the view's need "verify" on "mw-v.6" waits for "you"
    And the view's need "stale" on "mw-old" waits for "you"

  Scenario: A host alarm waits on the factory
    Given the view's host "laptop" last synced 34 minutes ago with work pathed to it
    When the live view is built
    Then the view's alarm for "laptop" waits for "factory"

  Scenario: A story out of attempts waits on the Mayor
    Given the view's story "mw-v.6" under "mw-v" has used all 3 attempts
    When the live view is built
    Then the view's need "alarm" on "mw-v.6" waits for "mayor"
