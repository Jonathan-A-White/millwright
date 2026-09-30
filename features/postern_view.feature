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
    When the live view is run and written
    Then the written view opens, from gzip, to a v 2 view holding "mw-v.1"
    And the view read no comments of "mw-v.1" or "mw-v.2"

  Scenario: A hands need carries its steps, each with the hash his approval binds and how it ran
    Given the view's bead "mw-v.1" has the hands step "linger" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the view's bead "mw-v.1" has the hands step "restart" on "vps" as "user" running "systemctl --user restart mw-dispatch"
    And the view's hands step "linger" on "mw-v.1" ran with exit 0
    When the live view is built
    Then the view's hands need on "mw-v.1" carries the step "linger" with its sha256, run with exit 0

  Scenario: A verify need says what landed, that the Mayor checked it, and that the tap is optional
    Given the view's bead "mw-v.6" under "mw-v" landed two hours ago with the Mayor's comment "Landing checked: the gate passes on main and the card reads right. Nothing else to do."
    When the live view is built
    Then the view's verify need on "mw-v.6" says "Landed 28 Sep 10:00 UTC: Story mw-v.6. Checked by the Mayor: the gate passes on main and the card reads right. Tap Verified if you have looked; optional, clears by itself 29 Sep 10:00 UTC."

  Scenario: A verify need on a landing the Mayor has not checked says not yet
    When the live view is built
    Then the view's verify need on "mw-v.3" says "Landed 28 Sep 11:00 UTC: Story mw-v.3. Checked by the Mayor: not yet. Tap Verified if you have looked; optional, clears by itself 29 Sep 11:00 UTC."

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
