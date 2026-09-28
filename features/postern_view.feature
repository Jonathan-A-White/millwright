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
