Feature: What a story has really changed is recorded as it works and noted when it lands
  The files key a story is filed with is the Mayor's guess. mw next, beside the
  Builder's session, records the files the story's branch has changed on the story
  (files_changed), where the other host's dispatch reads them; and a landing says
  which files it changed that the key did not name, so the Mayor can widen the keys
  of stories like it (mw-gq6.406).

  Background:
    Given a factory whose vault holds a builder charter and a ledger
    And a rig "millwright" checked out from a bare origin of its own
    And a plan "mw-gq6" whose stories are worked on "vps" and target "main"

  Scenario: The heartbeat records the files the branch has changed on the story
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the terminal holds a running session for "mw-gq6.1"
    When the heartbeat of "mw-gq6.1" ticks once
    Then the story "mw-gq6.1" records "mw-gq6.1.md" as changed

  Scenario: The heartbeat of a story whose branch changed nothing records nothing
    Given the story "mw-gq6.1" has been worked in its own worktree, committing nothing
    And the terminal holds a running session for "mw-gq6.1"
    When the heartbeat of "mw-gq6.1" ticks once
    Then the story "mw-gq6.1" records no files as changed

  Scenario: A landing that changed files outside the files key says which
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" expects to change "docs/*.md"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the story "mw-gq6.1" carries a comment quoting: FILES OUTSIDE THE KEY: mw-gq6.1.md

  Scenario: A landing that stayed inside the files key says nothing of it
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the story "mw-gq6.1" expects to change "mw-gq6.1.md"
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the story "mw-gq6.1" carries no comment quoting: FILES OUTSIDE THE KEY

  Scenario: A landing of a story with no files key says nothing of it
    Given the story "mw-gq6.1" has been worked in its own worktree
    And the session of "mw-gq6.1" reported a plain success
    When mw closes out "mw-gq6.1"
    Then the story "mw-gq6.1" is closed
    And the story "mw-gq6.1" carries no comment quoting: FILES OUTSIDE THE KEY
