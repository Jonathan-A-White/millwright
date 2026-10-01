Feature: mw home
  The factory has one home: the host that holds the beads server, the Mayor and
  the live Postern backend. The vault records it in a tracked file, `home`, of
  one line: the home host's name (desktop or laptop), then the UTC time and the
  actor of the last change. It is a file in the vault, not a bead, so that every
  mw sync brings it level and it can be read when the home's own database is dead.

  mw home prints the home, this host, and whether this host is home. mw home
  --check leaves with 0 when this host is home, 1 when it is not and 2 when it
  cannot tell: there is no file, or it is not one line of the kind above. What to
  do then is the caller's to decide. On stdout it prints nothing, except when
  this host is not home: then it prints the home's name alone, one line, for a
  caller that has to know where to go (postern's standby relays there); the
  explanation stays on stderr. mw only reads the file: nothing here moves the home.

  Scenario: mw home names the home, this host and that this host is home
    Given the home file of the vault reads "laptop 2026-09-29T00:10:00Z mw@laptop"
    And this host, for mw home, is "laptop"
    When mw home runs
    Then mw home prints:
      """
      home: laptop (changed 2026-09-29T00:10:00Z by mw@laptop)
      this host: laptop
      this host is home: yes
      """
    And mw home leaves with the status 0

  Scenario: mw home says this host is not home
    Given the home file of the vault reads "laptop 2026-09-29T00:10:00Z mw@laptop"
    And this host, for mw home, is "desktop"
    When mw home runs
    Then mw home prints:
      """
      home: laptop (changed 2026-09-29T00:10:00Z by mw@laptop)
      this host: desktop
      this host is home: no
      """
    And mw home leaves with the status 0

  Scenario: mw home --check leaves with 0 on the home host
    Given the home file of the vault reads "laptop 2026-09-29T00:10:00Z mw@laptop"
    And this host, for mw home, is "laptop"
    When mw home --check runs
    Then mw home leaves with the status 0
    And mw home prints nothing

  Scenario: mw home --check leaves with 1 on the other host
    Given the home file of the vault reads "laptop 2026-09-29T00:10:00Z mw@laptop"
    And this host, for mw home, is "desktop"
    When mw home --check runs
    Then mw home leaves with the status 1
    And mw home says "this host is not home"
    And mw home prints only the line "laptop"

  Scenario: mw home --check leaves with 2 when there is no home file
    Given the vault has no home file
    And this host, for mw home, is "laptop"
    When mw home --check runs
    Then mw home leaves with the status 2
    And mw home says "cannot tell which host is home: the vault has no home file"
    And mw home prints nothing

  Scenario: mw home --check leaves with 2 when the home file is not understood
    Given the home file of the vault reads "vps 2026-09-29T00:10:00Z mw@laptop"
    And this host, for mw home, is "laptop"
    When mw home --check runs
    Then mw home leaves with the status 2
    And mw home says "cannot tell which host is home"
    And mw home prints nothing

  Scenario: mw home without --check says it cannot tell and still leaves with 0
    Given the vault has no home file
    And this host, for mw home, is "laptop"
    When mw home runs
    Then mw home prints:
      """
      home: unknown (the vault has no home file)
      this host: laptop
      this host is home: unknown
      """
    And mw home leaves with the status 0
