Feature: mw memory
  The Mayor places, replaces and retires the typed facts of a rig kept as
  facts in the Builder's seat. Each verb writes files, prints each file it
  wrote, and never deletes one.

  Background:
    Given the memory clock says "2026-10-10"
    And the Builder keeps the rig "millwright" as facts

  Scenario: add writes a current fact, its slug the sentence's first five words
    When the Mayor adds a fact to the rig "millwright":
      | kind     | gotcha                                         |
      | subject  | bd                                             |
      | source   | mw-1                                           |
      | sentence | Point every bd call at the vault with -C.      |
    Then the memory command succeeded
    And the memory command printed the fact file "point-every-bd-call-at" of the rig "millwright"
    And the fact file "point-every-bd-call-at" of the rig "millwright" holds:
      """
      ---
      subject: bd
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-10-10
      ---

      Point every bd call at the vault with -C.
      """

  Scenario: add takes the slug it is given
    When the Mayor adds a fact to the rig "millwright":
      | kind     | decision                      |
      | subject  | gate                          |
      | source   | mayor:2026-10-10              |
      | slug     | gate-is-make-check            |
      | sentence | The gate is make check.       |
    Then the memory command succeeded
    And the fact file "gate-is-make-check" of the rig "millwright" holds:
      """
      ---
      subject: gate
      kind: decision
      status: current
      source: mayor:2026-10-10
      since: 2026-10-10
      ---

      The gate is make check.
      """

  Scenario: supersede writes a new fact and sets both links
    Given the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-09-01
      ---

      Tests need no socket.
      """
    When the Mayor supersedes the fact "old-tmux" of the rig "millwright":
      | source   | mw-2                          |
      | slug     | new-tmux                      |
      | sentence | Tests need their own socket.  |
    Then the memory command succeeded
    And the memory command printed the fact file "new-tmux" of the rig "millwright"
    And the memory command printed the fact file "old-tmux" of the rig "millwright"
    And the fact file "new-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: current
      source: mw-2
      since: 2026-10-10
      supersedes: old-tmux
      ---

      Tests need their own socket.
      """
    And the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: superseded
      source: mw-1
      since: 2026-09-01
      superseded-by: new-tmux
      ---

      Tests need no socket.
      """

  Scenario: retire keeps the sentence and says why
    Given the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-09-01
      ---

      Tests need no socket.
      """
    When the Mayor retires the fact "old-tmux" of the rig "millwright" because "tmux is gone"
    Then the memory command succeeded
    And the memory command printed the fact file "old-tmux" of the rig "millwright"
    And the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: retired
      source: mw-1
      since: 2026-09-01
      retired: 2026-10-10
      reason: tmux is gone
      ---

      Tests need no socket.
      """

  Scenario: recheck flags a fact in doubt and appends why to its reason
    Given the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-09-01
      ---

      Tests need no socket.
      """
    When the Mayor rechecks the fact "old-tmux" of the rig "millwright" saying "tmux 3.5 may differ"
    Then the memory command succeeded
    And the fact file "old-tmux" of the rig "millwright" holds:
      """
      ---
      subject: tmux
      kind: gotcha
      status: recheck
      source: mw-1
      since: 2026-09-01
      reason: tmux 3.5 may differ
      ---

      Tests need no socket.
      """

  Scenario: add refuses an unknown rig
    When the Mayor adds a fact to the rig "ghost":
      | kind     | gotcha          |
      | subject  | bd              |
      | source   | mw-1            |
      | sentence | Anything at all.|
    Then the memory command was refused saying "no rig \"ghost\""
    And the rig "ghost" has no folder in the Builder's seat

  Scenario: add refuses a slug that already has a file
    Given the fact file "taken" of the rig "millwright" holds:
      """
      ---
      subject: bd
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-09-01
      ---

      A fact.
      """
    When the Mayor adds a fact to the rig "millwright":
      | kind     | gotcha       |
      | subject  | bd           |
      | source   | mw-2         |
      | slug     | taken        |
      | sentence | Another one. |
    Then the memory command was refused saying "already exists"
    And the fact file "taken" of the rig "millwright" holds:
      """
      ---
      subject: bd
      kind: gotcha
      status: current
      source: mw-1
      since: 2026-09-01
      ---

      A fact.
      """

  Scenario: add refuses a sentence with a newline
    When the Mayor adds a fact to the rig "millwright" a sentence of two lines
    Then the memory command was refused saying "no newline"
    And the rig "millwright" has no fact files

  Scenario: add refuses an unknown kind
    When the Mayor adds a fact to the rig "millwright":
      | kind     | rumour       |
      | subject  | bd           |
      | source   | mw-1         |
      | sentence | A fact.      |
    Then the memory command was refused saying "unknown kind \"rumour\""
    And the rig "millwright" has no fact files

  Scenario: add refuses the slug of a fact flagged recheck
    Given the fact file "doubt" of the rig "millwright" holds:
      """
      ---
      subject: bd
      kind: gotcha
      status: recheck
      source: mw-1
      since: 2026-09-01
      ---

      A fact in doubt.
      """
    When the Mayor adds a fact to the rig "millwright":
      | kind     | gotcha       |
      | subject  | bd           |
      | source   | mw-2         |
      | slug     | doubt        |
      | sentence | Another one. |
    Then the memory command was refused saying "flagged to recheck: supersede or retire it"

  Scenario: supersede and retire refuse an unknown slug
    When the Mayor supersedes the fact "nothing" of the rig "millwright":
      | source   | mw-2     |
      | sentence | A fact.  |
    Then the memory command was refused saying "no fact \"nothing\""
    When the Mayor retires the fact "nothing" of the rig "millwright" because "gone"
    Then the memory command was refused saying "no fact \"nothing\""

  Scenario: supersede and retire refuse a fact already retired
    Given the fact file "gone" of the rig "millwright" holds:
      """
      ---
      subject: bd
      kind: gotcha
      status: retired
      source: mw-1
      since: 2026-09-01
      retired: 2026-10-01
      reason: obsolete
      ---

      A fact long gone.
      """
    When the Mayor supersedes the fact "gone" of the rig "millwright":
      | source   | mw-2     |
      | sentence | A fact.  |
    Then the memory command was refused saying "already retired"
    When the Mayor retires the fact "gone" of the rig "millwright" because "again"
    Then the memory command was refused saying "already retired"
    And the rig "millwright" has 1 fact file

  Scenario: list --status recheck shows only the facts in doubt
    Given the rig "millwright" holds facts for listing
    When the Mayor lists the rig "millwright" with the status "recheck"
    Then the memory command succeeded
    And the memory listing is:
      """
      doubt  recheck  gotcha  [bd]  2026-09-05  mw-5
      render 72/8000 bytes
      """

  Scenario: list puts current facts first by subject
    Given the rig "millwright" holds facts for listing
    When the Mayor lists the rig "millwright"
    Then the memory listing is:
      """
      alpha  current  gotcha  [bd]  2026-09-03  mw-3
      zeta  current  decision  [gate]  2026-09-02  mw-2
      doubt  recheck  gotcha  [bd]  2026-09-05  mw-5
      old  superseded  gotcha  [tmux]  2026-09-01  mw-1
      render 72/8000 bytes
      """

  Scenario: list --oldest orders by since ascending and ends with the size line
    Given the rig "millwright" holds facts for listing
    When the Mayor lists the rig "millwright" oldest first
    Then the memory listing is:
      """
      old  superseded  gotcha  [tmux]  2026-09-01  mw-1
      zeta  current  decision  [gate]  2026-09-02  mw-2
      alpha  current  gotcha  [bd]  2026-09-03  mw-3
      doubt  recheck  gotcha  [bd]  2026-09-05  mw-5
      render 72/8000 bytes
      """

  Scenario: mw status tells the Mayor to retire or supersede once the facts pass the budget, and nothing once they are under
    Given the rig "millwright" has an about text of 7970 bytes
    When the Mayor adds a fact to the rig "millwright":
      | kind     | gotcha                |
      | subject  | bd                    |
      | source   | mw-1                  |
      | slug     | big                   |
      | sentence | A fact past the line. |
    And mw status reads the host for memory
    Then the memory status line reads "millwright 8019/8000 bytes: retire or supersede (Mayor)"
    When the Mayor retires the fact "big" of the rig "millwright" because "too big"
    And mw status reads the host for memory
    Then the memory status line is absent for the rig "millwright"
