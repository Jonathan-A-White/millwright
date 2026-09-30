Feature: mw hands
  A step only the Governor's hands could take — a sudo line, a unit to
  enable, a file to move between hosts — is written down by the Mayor with
  mw hands add rather than as a line for him to type (postern's
  docs/protocol.md section 17). It is kept on the bead, exactly as it will
  run, commented there for the record, and the bead is labelled hitl so
  Postern shows it under his hands. Nothing runs until he approves it with
  his key. An id is used once per bead; replacing a step changes its hash,
  so any approval of the old one no longer matches.

  Background:
    Given a bead "mw-h.1" for the Governor's hands

  Scenario: The Mayor adds a step, and it is kept, commented and labelled
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    Then adding the step succeeds
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"
    And bead "mw-h.1"'s last comment is the HANDS STEP "linger" on "desktop" as "root"
    And bead "mw-h.1" carries the label "hitl"

  Scenario: A new step sends the Governor one push that opens Needs you
    Given a working push to the Governor
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    Then adding the step succeeds
    And exactly one push was sent, on the thread of "mw-h.1", saying "New hands step on mw-h.1: For his hands"
    And that push's class opens Needs you

  Scenario: A push that fails is reported, and the step is still kept
    Given a failing push to the Governor
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    Then adding the step succeeds
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"
    And the failed push is reported on stderr
    And bead "mw-h.1"'s last comment says the push failed

  Scenario: --no-push sends nothing
    Given a working push to the Governor
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite" with --no-push
    Then adding the step succeeds
    And no push was sent
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"

  Scenario: A second step of the same id is refused unless it replaces the first
    Given the Mayor added the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger other"
    Then adding the step is refused, naming --replace
    When the Mayor replaces the step "linger" on "mw-h.1" on "desktop" as "root" running "loginctl enable-linger other"
    Then adding the step succeeds
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger other"

  Scenario: A step that runs as neither the user nor root is refused
    When the Mayor adds the step "odd" to "mw-h.1" on "desktop" as "admin" running "true"
    Then adding the step is refused
    And bead "mw-h.1" keeps no steps

  Scenario: The list shows each step's hash and whether it ran
    Given the Mayor added the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And the step "linger" on "mw-h.1" ran on "desktop" with exit 0
    When the Mayor lists the hands steps of "mw-h.1"
    Then the list shows "linger" with its sha256 and "ran"

  Scenario: --after adds the blocker and the view marks the step waiting
    Given a bead "mw-h.2" titled "Fetch the key first"
    And a working push to the Governor
    And a view the add publishes
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite" after "mw-h.2"
    Then adding the step succeeds
    And bead "mw-h.1" waits on "mw-h.2"
    And exactly one push was sent, on the thread of "mw-h.1", saying "New hands step on mw-h.1: For his hands (waits on Fetch the key first)"
    And the published view's hands need on "mw-h.1" is not ready, waiting on "Fetch the key first"

  Scenario: --after a bead that is not there is refused, and nothing is kept
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite" after "mw-h.9"
    Then adding the step is refused
    And bead "mw-h.1" keeps no steps

  Scenario: --replace publishes the view with the new step's hash
    Given the Mayor added the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    And a view the add publishes
    When the Mayor replaces the step "linger" on "mw-h.1" on "desktop" as "root" running "loginctl enable-linger other"
    Then adding the step succeeds
    And the published view's hands need on "mw-h.1" carries the step "linger" running "loginctl enable-linger other" with its sha256

  Scenario: --no-view publishes nothing
    Given a view the add publishes
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite" with --no-view
    Then adding the step succeeds
    And no view was published
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"

  Scenario: A view the notifier is publishing is skipped with a warning, and the step is kept
    Given a view the add publishes
    And the notifier holds the view lock
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    Then adding the step succeeds
    And no view was published
    And stderr warns the view was skipped
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"

  Scenario: A view that fails to publish is reported, and the step is kept
    Given a view the add publishes
    And the view cannot be written
    When the Mayor adds the step "linger" to "mw-h.1" on "desktop" as "root" running "loginctl enable-linger jwhite"
    Then adding the step succeeds
    And stderr warns the view was not published
    And bead "mw-h.1" keeps the step "linger" running "loginctl enable-linger jwhite"
