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

  # mw memory migrate: a rig kept as one memory file (and its archive) becomes
  # about.md and one typed fact per file, in one run.

  Scenario: migrate makes about.md and seven fact files from the memory file and its archive
    Given the rig "demo" keeps its memory in one file:
      """
      # Rig memory: demo

      Demo is a small rig. It keeps a parser and a build.

      ## Before you start
      - Decided by the Governor: the gate is `make check` and nothing lands without it (mw-dd.1).

      ## Where things are
      - The parser lives in app/parse.go and reads once.
      - Run the build with `make build` and then look in bin/.
      - Never edit the ledger by hand, the Mayor owns it [mw-ee].
      - Node 20 is needed from 2026-09-02 for the build.

      A stray paragraph that belongs nowhere.
      """
    And the rig "demo" keeps its archive in one file:
      """
      # Rig memory archive: demo

      Lines moved out of demo.md by the Mayor.

      ## Moved 2026-09-20 (after mw-ff)

      - Old build used `make all` before the gate. (mw-ff.2)

      ## Moved 2026-08-01 (stale notes)

      - The old parser was in app/old.go.
      """
    When the Mayor migrates the rig "demo"
    Then the memory command succeeded
    And the memory command printed "migrated the rig demo: about.md and 7 facts (5 current, 2 retired); 1 line not placed"
    And the file "about.md" of the rig "demo" holds:
      """
      Demo is a small rig. It keeps a parser and a build.
      """
    And the fact file "decided-by-the-governor-the" of the rig "demo" holds:
      """
      ---
      subject: make check
      kind: decision
      status: current
      source: mw-dd.1
      since: 2026-10-10
      ---

      Decided by the Governor: the gate is `make check` and nothing lands without it.
      """
    And the fact file "the-parser-lives-in-appparsego" of the rig "demo" holds:
      """
      ---
      subject: app/parse.go
      kind: gotcha
      status: current
      source: mayor:demo.md@abc1234
      since: 2026-10-10
      ---

      The parser lives in app/parse.go and reads once.
      """
    And the fact file "run-the-build-with-make" of the rig "demo" holds:
      """
      ---
      subject: make build
      kind: gotcha
      status: current
      source: mayor:demo.md@abc1234
      since: 2026-10-10
      ---

      Run the build with `make build` and then look in bin/.
      """
    And the fact file "never-edit-the-ledger-by" of the rig "demo" holds:
      """
      ---
      subject: general
      kind: gotcha
      status: current
      source: mw-ee
      since: 2026-10-10
      ---

      Never edit the ledger by hand, the Mayor owns it.
      """
    And the fact file "node-20-is-needed-from" of the rig "demo" holds:
      """
      ---
      subject: general
      kind: gotcha
      status: current
      source: mayor:demo.md@abc1234
      since: 2026-09-02
      ---

      Node 20 is needed from 2026-09-02 for the build.
      """
    And the fact file "old-build-used-make-all" of the rig "demo" holds:
      """
      ---
      subject: make all
      kind: gotcha
      status: retired
      source: mw-ff.2
      since: 2026-10-10
      retired: 2026-09-20
      reason: pruned: Moved 2026-09-20 (after mw-ff)
      ---

      Old build used `make all` before the gate.
      """
    And the fact file "the-old-parser-was-in" of the rig "demo" holds:
      """
      ---
      subject: app/old.go
      kind: gotcha
      status: retired
      source: mayor:demo-archive.md@abc1234
      since: 2026-10-10
      retired: 2026-08-01
      reason: pruned: Moved 2026-08-01 (stale notes)
      ---

      The old parser was in app/old.go.
      """
    And the rig "demo" has 7 fact files
    And the rig "demo" keeps no memory file and no archive file
    And a boot of the rig "demo" reads 5 current facts after its about text

  Scenario: migrate --dry-run prints the same seven facts and the line not placed, and writes nothing
    Given the rig "demo" keeps its memory in one file:
      """
      # Rig memory: demo

      Demo is a small rig. It keeps a parser and a build.

      ## Before you start
      - Decided by the Governor: the gate is `make check` and nothing lands without it (mw-dd.1).

      ## Where things are
      - The parser lives in app/parse.go and reads once.
      - Run the build with `make build` and then look in bin/.
      - Never edit the ledger by hand, the Mayor owns it [mw-ee].
      - Node 20 is needed from 2026-09-02 for the build.

      A stray paragraph that belongs nowhere.
      """
    And the rig "demo" keeps its archive in one file:
      """
      # Rig memory archive: demo

      Lines moved out of demo.md by the Mayor.

      ## Moved 2026-09-20 (after mw-ff)

      - Old build used `make all` before the gate. (mw-ff.2)

      ## Moved 2026-08-01 (stale notes)

      - The old parser was in app/old.go.
      """
    When the Mayor migrates the rig "demo" with --dry-run
    Then the memory command succeeded
    And the memory command printed exactly:
      """
      dry run: nothing is written
      about.md  51 bytes
      decided-by-the-governor-the  current  decision  [make check]  2026-10-10  mw-dd.1  Decided by the Governor: the gate is `make check` and nothing lands without it.
      the-parser-lives-in-appparsego  current  gotcha  [app/parse.go]  2026-10-10  mayor:demo.md@abc1234  The parser lives in app/parse.go and reads once.
      run-the-build-with-make  current  gotcha  [make build]  2026-10-10  mayor:demo.md@abc1234  Run the build with `make build` and then look in bin/.
      never-edit-the-ledger-by  current  gotcha  [general]  2026-10-10  mw-ee  Never edit the ledger by hand, the Mayor owns it.
      node-20-is-needed-from  current  gotcha  [general]  2026-09-02  mayor:demo.md@abc1234  Node 20 is needed from 2026-09-02 for the build.
      old-build-used-make-all  retired  gotcha  [make all]  2026-10-10  mw-ff.2  Old build used `make all` before the gate.  retired 2026-09-20 (pruned: Moved 2026-09-20 (after mw-ff))
      the-old-parser-was-in  retired  gotcha  [app/old.go]  2026-10-10  mayor:demo-archive.md@abc1234  The old parser was in app/old.go.  retired 2026-08-01 (pruned: Moved 2026-08-01 (stale notes))
      not placed: demo.md line 14: A stray paragraph that belongs nowhere.
      would migrate the rig demo: about.md and 7 facts (5 current, 2 retired); 1 line not placed
      """
    And the rig "demo" keeps its memory file
    And the rig "demo" keeps its archive file
    And the rig "demo" has no facts folder

  Scenario: a slug that is taken gets a numeric suffix
    Given the rig "demo" keeps its memory in one file:
      """
      # Rig memory: demo

      - Run the gate before you close a step (mw-1).
      - Run the gate before you close a step (mw-2).
      - Run the gate before you close a step (mw-3).
      """
    When the Mayor migrates the rig "demo"
    Then the memory command succeeded
    And the rig "demo" has 3 fact files
    And the fact file "run-the-gate-before-you" of the rig "demo" holds a fact with source "mw-1"
    And the fact file "run-the-gate-before-you-2" of the rig "demo" holds a fact with source "mw-2"
    And the fact file "run-the-gate-before-you-3" of the rig "demo" holds a fact with source "mw-3"

  Scenario: a head over 600 bytes is cut at a sentence end and says so
    Given the rig "demo" keeps a memory file with a head of 700 bytes
    When the Mayor migrates the rig "demo"
    Then the memory command succeeded
    And the memory command printed "warning: the head of demo.md is 700 bytes; about.md keeps the first 593, cut at a sentence end"
    And the file "about.md" of the rig "demo" has a text of 593 bytes

  Scenario: migrate refuses a rig that already has a facts folder
    Given the rig "demo" keeps its memory in one file:
      """
      # Rig memory: demo

      - A fact (mw-1).
      """
    And the Builder keeps the rig "demo" as facts
    When the Mayor migrates the rig "demo"
    Then the memory command was refused saying "already has a facts folder"
    And the rig "demo" keeps its memory file

  Scenario: migrate refuses a rig with no memory file
    When the Mayor migrates the rig "ghost"
    Then the memory command was refused saying "no memory file for the rig ghost"
    And the rig "ghost" has no facts folder

  Scenario: query finds a fact by its subject, current facts first
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "bd"
    Then the memory command succeeded
    And the memory listing is:
      """
      never-sync-in-tests  current  [bd]  Never run sync from a test. (mw-3)
      vault-flag  current  [bd]  Point every call at the vault with -C. (mw-2)
      """

  Scenario: query finds a fact by a word of its sentence, whatever the case, and follows it to related facts by subject
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "SYNC"
    Then the memory command succeeded
    And the memory listing is:
      """
      never-sync-in-tests  current  [bd]  Never run sync from a test. (mw-3)
      vault-flag  current  [bd]  Point every call at the vault with -C. (mw-2)  related
      """

  Scenario: query finds a fact by its slug
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "gate-make"
    Then the memory command succeeded
    And the memory listing is:
      """
      gate-make-check  current  [gate]  The gate is make check. (mayor:2026-09-01)
      """

  Scenario: query finds a fact by its source
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "mayor:2026"
    Then the memory command succeeded
    And the memory listing is:
      """
      gate-make-check  current  [gate]  The gate is make check. (mayor:2026-09-01)
      """

  Scenario: query matches a word or the start of one, not the middle of one
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "ocket"
    Then the memory command was refused saying "no fact matches"
    When the Builder queries the rig "millwright" for "sock"
    Then the memory command succeeded

  Scenario: query shows a retired fact's date and reason
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "wall"
    Then the memory command succeeded
    And the memory listing is:
      """
      gone-clock  retired  [clock]  The wall clock steps back. (mw-5)  retired 2026-10-01: wsl2 fixed
      """

  Scenario: query names the successor of a superseded fact and follows the chain to it
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "default"
    Then the memory command succeeded
    And the memory listing is:
      """
      old-socket  superseded  [tmux]  Tests share the default socket. (mw-4)  superseded by own-socket
      own-socket  current  [tmux]  Every test needs its own socket. (mw-10)  related
      """

  Scenario: query follows the chain from the successor back to what it replaced
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "mw-10"
    Then the memory command succeeded
    And the memory listing is:
      """
      own-socket  current  [tmux]  Every test needs its own socket. (mw-10)
      old-socket  superseded  [tmux]  Tests share the default socket. (mw-4)  superseded by own-socket  related
      """

  Scenario: query with two terms keeps the facts that match both
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "bd sync"
    Then the memory command succeeded
    And the memory listing is:
      """
      never-sync-in-tests  current  [bd]  Never run sync from a test. (mw-3)
      vault-flag  current  [bd]  Point every call at the vault with -C. (mw-2)  related
      """

  Scenario: query finds nothing and says so
    Given the rig "millwright" holds facts for querying
    When the Builder queries the rig "millwright" for "tmux wall"
    Then the memory command was refused saying "no fact matches"

  Scenario: query refuses a rig the seat has no folder for
    When the Builder queries the rig "ghost" for "bd"
    Then the memory command was refused saying "no rig \"ghost\""
