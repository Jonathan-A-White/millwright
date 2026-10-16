Feature: mw config export-boost writes the vault's hosts/boost-config.toml from the home's config
  contrib/boost-bootstrap.sh gives a new Boost the [tests] and [changed_specs] lines of
  the rigs it clones from the vault's hosts/boost-config.toml (mw-gq6.453). That file was
  kept in step with the home's ~/.config/mw/config.toml by hand, and a line added on the
  home and not in the file made the next Boost gate that rig with make test. `mw config
  export-boost`, run on the home, rewrites the file from the home's two tables, and
  nothing else of the config, and commits that one file in the vault.

  Background:
    Given the home's config file for the export holding:
      """
      vault = "/home/jwhite/millwright-vault"
      cap = 5

      [rigs]
      argus = "/home/jwhite/argus"

      [tests]
      millwright = "make test && scripts/check-codemap.sh"
      argus = "npm ci --no-audit --no-fund && npm run lint && npm run build"

      [changed_specs]
      lampas = "npm run build && TZ=UTC npx playwright test --project=shots {specs}"

      [after_landing]
      millwright = "make build"

      [heavy_net]
      postern = true
      """

  Scenario: on the home it writes only [tests] and [changed_specs], and commits that file alone
    Given the export runs on the home
    When mw config export-boost runs
    Then mw config export-boost succeeded
    And the vault's hosts/boost-config.toml holds, after its header:
      """
      [tests]
      argus = "npm ci --no-audit --no-fund && npm run lint && npm run build"
      millwright = "make test && scripts/check-codemap.sh"

      [changed_specs]
      lampas = "npm run build && TZ=UTC npx playwright test --project=shots {specs}"
      """
    And the export committed "hosts/boost-config.toml" and nothing else
    And mw config export-boost printed "[tests] argus: missing"

  Scenario: it names each line it brings level: missing, different and gone from the home
    Given the export runs on the home
    And the vault's hosts/boost-config.toml for the export holding:
      """
      [tests]
      argus = "npm test"
      millwright = "make test && scripts/check-codemap.sh"
      legend = "npm test"
      """
    When mw config export-boost runs
    Then mw config export-boost succeeded
    And mw config export-boost printed "[tests] argus: differs"
    And mw config export-boost printed "[tests] legend: not on the home"
    And mw config export-boost printed "[changed_specs] lampas: missing"
    And mw config export-boost did not print "[tests] millwright"

  Scenario: a second export finds the file level, writes nothing and commits nothing
    Given the export runs on the home
    And mw config export-boost has run once
    When mw config export-boost runs
    Then mw config export-boost succeeded
    And mw config export-boost printed "already level"
    And the export committed nothing more

  Scenario: --dry-run says what it would write and writes nothing
    Given the export runs on the home
    When mw config export-boost runs dry
    Then mw config export-boost succeeded
    And mw config export-boost printed "dry run: nothing is written"
    And mw config export-boost printed "[tests] argus: missing"
    And the vault has no hosts/boost-config.toml
    And the export committed nothing

  Scenario: on a host that is not the home it is refused and writes nothing: a Boost's tables are a copy
    Given the export runs on a host that is not the home
    When mw config export-boost runs
    Then mw config export-boost was refused, saying "not the home"
    And the vault has no hosts/boost-config.toml
    And the export committed nothing

  Scenario: on a home with no config file it is refused, and the vault's file is left as it was
    Given the export runs on the home
    And the vault's hosts/boost-config.toml for the export holding:
      """
      [tests]
      argus = "npm test"
      """
    And the home has no config file for the export
    When mw config export-boost runs
    Then mw config export-boost was refused, saying "nothing was exported"
    And the export committed nothing
