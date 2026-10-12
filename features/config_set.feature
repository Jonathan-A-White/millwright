Feature: mw config set changes one key of the config file, with a backup and the way back
  The Mayor's classifier refuses a hand edit of ~/.config/mw/config.toml, and three
  refusals in a row once parked a window for a day. `mw config set <table>.<key>
  <value>` is the one narrow, auditable command that edits it instead: it copies the
  file to a dated backup beside it, changes exactly one key, and prints the old value,
  the new one, the backup and the whole command that puts the file back.
  `mw config get <table>.<key>` reads a key.

  Background:
    Given a config file holding:
      """
      vault = "/home/jwhite/millwright-vault"
      cap   = 5 # let it rip; way back: 2

      [rigs]
      legend = "/home/jwhite/legend"
      millwright = "/home/jwhite/millwright"

      [after_landing]
      # what runs after a landing
      millwright = "make build"

      [tests]
      millwright = "make test"
      """
    And the config clock reads 2026-10-12T14:03:09Z

  Scenario: set on an existing key changes that one line and keeps every other byte
    When mw config set is run for "after_landing.millwright" with "make build && make install"
    Then mw config set succeeded
    And the config file differs from before in exactly one line
    And the config line "millwright = \"make build && make install\"" is in the table "after_landing"
    And the config set printed: old: "make build"
    And the config set printed: new: "make build && make install"

  Scenario: set keeps a comment written after the old value
    Given a config file holding:
      """
      [cloud]
      cap = 5 # let it rip; way back: 2
      """
    When mw config set is run for "cloud.cap" with "6"
    Then mw config set succeeded
    And the config file reads:
      """
      [cloud]
      cap = 6 # let it rip; way back: 2
      """

  Scenario: set writes a dated backup that equals the old file
    When mw config set is run for "tests.millwright" with "make test && scripts/check-codemap.sh"
    Then mw config set succeeded
    And the backup config.toml.bak-2026-10-12T140309Z exists and equals the old file
    And the config set printed: backup: <the config>.bak-2026-10-12T140309Z

  Scenario: the printed way back restores the old file
    When mw config set is run for "after_landing.millwright" with "make"
    And the printed way back is run
    Then the config file equals the old file

  Scenario: set adds a missing key at the end of its table
    When mw config set is run for "rigs.argus" with "/home/jwhite/argus"
    Then mw config set succeeded
    And the config file differs from before in exactly one line
    And the config line "argus = \"/home/jwhite/argus\"" is in the table "rigs"
    And the config set printed: old: unset

  Scenario: set on a missing table appends the table
    When mw config set is run for "doctor.tunnel_host" with "root@allmymind.org"
    Then mw config set succeeded
    And the config file is the old file followed by a blank line and:
      """
      [doctor]
      tunnel_host = "root@allmymind.org"
      """
    And the config set printed: old: unset

  Scenario: a number or a true is written bare, an array as typed
    When mw config set is run for "after_landing.cap" with "7"
    Then the config line "cap = 7" is in the table "after_landing"
    When mw config set is run for "after_landing.watch" with "true"
    Then the config line "watch = true" is in the table "after_landing"
    When mw config set is run for "after_landing.list" with '["a", "b"]'
    Then the config line "list = [\"a\", \"b\"]" is in the table "after_landing"

  Scenario: a dotted table is found by its whole name
    Given a config file holding:
      """
      [cloud]
      provider = "vultr"

      [cloud.caps]
      desktop = 2
      """
    When mw config set is run for "cloud.caps.desktop" with "3"
    Then the config file reads:
      """
      [cloud]
      provider = "vultr"

      [cloud.caps]
      desktop = 3
      """

  Scenario: a key with no table is refused and nothing is written
    When mw config set is run for "nokey" with "x"
    Then mw config set was refused, saying: needs a table
    And the config directory is as it was

  Scenario: a value that would break the file is refused and nothing is written
    When mw config set is run for "after_landing.millwright" with '"make build'
    Then mw config set was refused, saying: would not parse
    And the config directory is as it was

  Scenario: a file that does not parse is refused and nothing is written
    Given a config file holding:
      """
      [rigs
      legend = "/x"
      """
    When mw config set is run for "rigs.legend" with "/y"
    Then mw config set was refused, saying: does not parse
    And the config directory is as it was

  Scenario: the same value is refused as unchanged and no backup is made
    When mw config set is run for "after_landing.millwright" with "make build"
    Then mw config set was refused, saying: unchanged
    And the config directory is as it was

  Scenario: --dry-run prints the same and writes nothing
    When mw config set is dry-run for "after_landing.millwright" with "make"
    Then mw config set succeeded
    And the config set printed: old: "make build"
    And the config set printed: new: "make"
    And the config set printed: backup: <the config>.bak-2026-10-12T140309Z
    And the config set printed: way back: cp <the config>.bak-2026-10-12T140309Z <the config>
    And the config directory is as it was

  Scenario: get prints a string without its quotes
    When mw config get is run for "after_landing.millwright"
    Then mw config get printed: make build

  Scenario: get of a key that is not there fails
    When mw config get is run for "after_landing.nothing"
    Then mw config get failed, saying: unset
