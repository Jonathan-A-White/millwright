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
