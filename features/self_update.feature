Feature: a host keeps the mw it runs level with the factory rig's main
  A host rebuilds mw when it lands a millwright story itself, and so a host that
  did not land the change kept running an old mw until it landed one (mw-gq6.183).
  On every tick a host whose millwright checkout is clean and behind origin's main
  fast-forwards it and runs the command the host names for the rig after a landing
  (its [after_landing] table, `make build` with the host's own Go), and says so
  once in the tick's line, naming the new version. A checkout that is dirty, on
  another branch or has commits of its own is left exactly as it is, and the tick
  says why. A build that fails keeps the old bin/mw, is said in the line, and is
  tried again by the next tick. A host that names no command builds nothing and
  its checkout is not touched.

  The dispatch tick does the same first, so that a host that dispatches and runs
  no Millhand tick (the desktop) keeps its mw level too (mw-gq6.184). A host that
  runs both ticks builds a commit once: the first tick remembers it.

  Background:
    Given a millwright rig whose origin's main has moved on since this host's checkout

  Scenario: A clean checkout behind origin's main is fast-forwarded and rebuilt
    Given the host's build command makes bin/mw
    When the host's tick looks at its mw
    Then the checkout is at origin's main
    And the build ran once, in the checkout, at origin's main
    And bin/mw was rebuilt
    And the tick's line says: self-update: millwright <old> → <new>, built

  Scenario: A tick that finds the checkout level says nothing and builds nothing more
    Given the host's build command makes bin/mw
    And the host's tick has looked at its mw
    When the host's tick looks at its mw
    Then the build ran once, in the checkout, at origin's main
    And the tick's line says nothing

  Scenario: A dirty checkout is not touched
    Given the host's build command makes bin/mw
    And the checkout has an uncommitted change
    When the host's tick looks at its mw
    Then the checkout is still at its old commit, with its uncommitted change
    And the build did not run
    And the tick's line says: self-update: millwright checkout left alone: it has 1 uncommitted path(s): README.md

  Scenario: A checkout with commits of its own is not touched
    Given the host's build command makes bin/mw
    And the checkout has a commit of its own
    When the host's tick looks at its mw
    Then the checkout is still at its own commit
    And the build did not run
    And the tick's line says: self-update: millwright checkout left alone: main here has commits of its own

  Scenario: A failed build keeps the old binary and is tried again
    Given the host's build command prints "boom" and fails
    When the host's tick looks at its mw
    Then bin/mw is still the old one
    And the tick's line says: the old mw is kept
    And the tick's line says: boom
    When the host's build command makes bin/mw
    And the host's tick looks at its mw
    Then bin/mw was rebuilt
    And the tick's line says: self-update: millwright

  Scenario: A host that names no build command is left alone
    Given the host names no build command
    When the host's tick looks at its mw
    Then the checkout is still at its old commit
    And the tick's line says nothing

  Scenario: A dispatch tick on a host with no Millhand tick rebuilds as well
    Given the host's build command makes bin/mw
    When the host's dispatch tick looks at its mw
    Then the checkout is at origin's main
    And the build ran once, in the checkout, at origin's main
    And bin/mw was rebuilt
    And the tick's line says: self-update: millwright <old> → <new>, built

  Scenario: A dispatch tick does not build what the Millhand tick already built
    Given the host's build command makes bin/mw
    And the host's tick has looked at its mw
    When the host's dispatch tick looks at its mw
    Then the build ran once, in the checkout, at origin's main
    And the tick's line says nothing

  Scenario: A dispatch tick's dry run leaves the checkout alone
    Given the host's build command makes bin/mw
    When the host's dispatch tick looks at its mw as a dry run
    Then the checkout is still at its old commit
    And the build did not run

  Scenario: A self-update to a main whose formulas differ installs them in the vault
    Given the host is home and its vault holds the rig's old formulas
    And the host's build command makes bin/mw
    When the host's tick looks at its mw
    Then the vault holds the rig's new formulas
    And the vault has one new commit: Install formulas from millwright <new>
    And the vault's origin has that commit
    And the tick's line says: self-update: millwright formulas installed in the vault from <new>

  Scenario: Formulas already equal make no vault commit
    Given the host is home and its vault already holds the rig's new formulas
    And the host's build command makes bin/mw
    When the host's tick looks at its mw
    Then the vault has no new commit
    And the tick's line says nothing of formulas

  Scenario: Uncommitted changes under the vault's formulas leave them alone
    Given the host is home and its vault holds the rig's old formulas
    And the vault has an uncommitted change under .beads/formulas
    And the host's build command makes bin/mw
    When the host's tick looks at its mw
    Then the vault has no new commit
    And the vault's uncommitted change is still there
    And the tick's line says: self-update: millwright formulas left alone: the vault has 1 uncommitted path(s) under .beads/formulas

  Scenario: A host that is not home leaves the vault's formulas alone
    Given the host is not home and its vault holds the rig's old formulas
    And the host's build command makes bin/mw
    When the host's tick looks at its mw
    Then the vault has no new commit
    And the tick's line says nothing of formulas
