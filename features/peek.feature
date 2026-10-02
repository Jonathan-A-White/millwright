Feature: Peeking at a running Builder
  The Mayor must never guess from a tmux target name whether a Builder is still
  there. `mw peek <story>` prints, for the story's session on whichever host
  works it: the host, the session, when it was launched and for how long, where
  the formula has got to, and the tail of what the harness has recorded. On
  another host it reaches the session by ssh and prints what that host's mw
  prints. It writes nothing.

  Background:
    Given the peeking host is "laptop", with the rig "millwright" checked out
    And the peeked story "mw-3evcnk.1" is pathed to "laptop"

  Scenario: A running Builder shows its host, session, elapsed time, step and transcript tail
    Given the peeked story was claimed 23 minutes ago and its session is running
    And the peeked story's formula is poured with the steps "Understand", "Write the test", "Implement" and the first is closed
    And the harness has recorded 30 lines of transcript for the story
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek says "host: laptop"
    And the peek says "session: mw-3evcnk_1 (running)"
    And the peek says "launched: 2026-10-02T21:40:00Z, 23m ago"
    And the peek says "step: Write the test (2 steps still open)"
    And the peek shows the last 20 of the 30 transcript lines
    And the peek wrote nothing to the tracker

  Scenario: A print-mode run with no steps tracked says so
    Given the peeked story was claimed 23 minutes ago and its session is running
    And the harness has recorded 3 lines of transcript for the story
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek says "step: print-mode run, steps not tracked"
    And the peek shows the last 3 of the 3 transcript lines

  Scenario: With no harness transcript the session's own pane is shown
    Given the peeked story was claimed 5 minutes ago and its session is running
    And the session's pane has printed 25 lines
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek shows the last 20 of the 25 pane lines

  Scenario: A finished Builder says it finished and how long it ran
    Given the peeked story was claimed 23 minutes ago and its session is running
    And the harness has recorded 3 lines of transcript for the story
    And the session has exited with status 0
    And the peeked story was closed 15 minutes ago
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek says "session: mw-3evcnk_1 (exited, status 0)"
    And the peek says "finished: the story is closed, after 8m"
    And the peek shows the last 3 of the 3 transcript lines

  Scenario: A claimed story whose session is gone says it is gone
    Given the peeked story was claimed 23 minutes ago and it has no session
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek says "session: mw-3evcnk_1 (gone)"

  Scenario: A story on another host is read over ssh
    Given the peeked story "mw-3evcnk.1" is pathed to "desktop"
    And the host "desktop" prints for the story:
      """
      host: desktop
      session: mw-3evcnk_1 (running)
      """
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek succeeds
    And the peek says "host: desktop"
    And the peek asked the host "desktop" and not this one's session

  Scenario: A story on another host that cannot be reached is a plain refusal
    Given the peeked story "mw-3evcnk.1" is pathed to "desktop"
    And the host "desktop" cannot be reached, saying "no way to reach desktop"
    When the Mayor peeks at "mw-3evcnk.1"
    Then the peek is refused, saying: no way to reach desktop

  Scenario: A story nobody filed is a plain refusal
    When the Mayor peeks at "mw-nope"
    Then the peek is refused, saying: reading mw-nope
    And the peek wrote nothing to the tracker
