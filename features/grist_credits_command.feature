Feature: mw grist credits, the API key's switch and ledger looked up and changed by hand
  mw grist credits (also status, and --json) says whether the mill bills grists
  to the Anthropic API key: the switch, whether the key file is there (never any
  of the key), the spent mark, this UTC calendar month's grists billed to the key
  and their total_cost_usd against $200, and the time of the last one. on and off
  set [grist] api_credits in the config through the writer mw config set uses.
  key reads one line from stdin and writes the key file 0600, on the home only.
  clear removes the spent mark when the month's credits renew.

  Background:
    Given a credits workbench on the host "laptop", the home being "laptop"
    And the credits clock reads 2026-10-20T12:00:00Z

  Scenario: With nothing set up it says the switch is off, no key, no mark, nothing billed
    When mw grist credits is run
    Then mw grist credits printed the line "api credits: off (grists are billed to the subscription)"
    And mw grist credits printed the line "key file: no, <key file>"
    And mw grist credits printed the line "spent mark: none"
    And mw grist credits printed the line "this month (2026-10): 0 grists billed to the key, $0.00 of $200.00"
    And mw grist credits printed the line "last grist billed to the key: none"

  Scenario: With the switch on, a key and grists billed this month it sums them and never shows the key
    Given [grist] api_credits is true in the config
    And the key file holds "sk-ant-secret-key-1" at mode 0600
    And the mill's record holds a grist billed to "api" at 2026-10-02T08:00:00Z costing 1.50 dollars
    And the mill's record holds a grist billed to "api" at 2026-10-19T09:30:00Z costing 2.25 dollars
    And the mill's record holds a grist billed to "subscription" at 2026-10-19T10:00:00Z costing 9.00 dollars
    And the mill's record holds a grist billed to "api" at 2026-09-30T23:59:59Z costing 40.00 dollars
    When mw grist credits status is run
    Then mw grist credits printed the line "api credits: on (grists are billed to the API key while it has credit)"
    And mw grist credits printed the line "key file: yes, <key file>"
    And mw grist credits printed the line "this month (2026-10): 2 grists billed to the key, $3.75 of $200.00"
    And mw grist credits printed the line "last grist billed to the key: 2026-10-19T09:30:00Z"
    And "sk-ant-secret-key-1" is in nothing mw grist credits printed

  Scenario: A key file others can read is present but not used, and the line says why
    Given the key file holds "sk-ant-secret-key-1" at mode 0644
    When mw grist credits is run
    Then mw grist credits printed a line starting "key file: yes, <key file>, but not used: "
    And "sk-ant-secret-key-1" is in nothing mw grist credits printed

  Scenario: A spent mark is shown with the time the key is tried again
    Given the key was found spent at 2026-10-18T10:00:00Z
    When mw grist credits is run
    Then mw grist credits printed a line starting "spent mark: 2026-10-18T10:00:00Z (the key is tried again after 2026-10-19T10:00:00Z"

  Scenario: --json prints the same as one object
    Given [grist] api_credits is true in the config
    And the key file holds "sk-ant-secret-key-1" at mode 0600
    And the key was found spent at 2026-10-18T10:00:00Z
    And the mill's record holds a grist billed to "api" at 2026-10-19T09:30:00Z costing 2.25 dollars
    When mw grist credits is run with --json
    Then the JSON printed says api_credits is true, key_present is true, grists is 1, cost_usd is 2.25, budget_usd is 200
    And the JSON printed says spent_at is "2026-10-18T10:00:00Z" and last_api is "2026-10-19T09:30:00Z"
    And "sk-ant-secret-key-1" is in nothing mw grist credits printed

  Scenario: on sets api_credits true, says what changed and the way back
    Given the config holds a [grist] table with a daily_limit of 5
    When mw grist credits on is run
    Then the config has api_credits = true in its [grist] table, and daily_limit = 5 still
    And mw grist credits printed a line starting "old: unset"
    And mw grist credits printed a line starting "way back: cp "
    And mw grist credits printed a line starting "api credits are now on"

  Scenario: off sets api_credits false
    Given [grist] api_credits is true in the config
    When mw grist credits off is run
    Then the config has api_credits = false in its [grist] table
    And mw grist credits printed a line starting "api credits are now off"
    And mw grist credits printed a line starting "way back: cp "

  Scenario: on when it is already on changes nothing
    Given [grist] api_credits is true in the config
    When mw grist credits on is run
    Then mw grist credits printed the line "api credits are already on: nothing changed"
    And the config was not changed

  Scenario: key writes the key file at mode 0600 and never shows the key
    When mw grist credits key is run with the line "sk-ant-secret-key-2"
    Then the key file holds "sk-ant-secret-key-2" at mode 0600
    And mw grist credits printed a line starting "the API key is written to <key file>, mode 0600"
    And "sk-ant-secret-key-2" is in nothing mw grist credits printed
    And "sk-ant-secret-key-2" is in no file of the config or the credits state

  Scenario: key replaces a key file others could read with one only the user can
    Given the key file holds "sk-ant-old-key" at mode 0644
    When mw grist credits key is run with the line "sk-ant-secret-key-2"
    Then the key file holds "sk-ant-secret-key-2" at mode 0600

  Scenario: key refuses an empty line
    When mw grist credits key is run with the line ""
    Then mw grist credits was refused saying "the line was empty"
    And there is no key file

  Scenario: key refuses a line that does not start with sk-ant-, without repeating it
    When mw grist credits key is run with the line "sk-wrong-not-an-anthropic-key"
    Then mw grist credits was refused saying "does not start with sk-ant-"
    And "sk-wrong-not-an-anthropic-key" is in nothing mw grist credits printed
    And there is no key file

  Scenario: key is refused on a host that is not home, before it reads a line
    Given the credits workbench moves to the host "cloud1"
    When mw grist credits key is run with the line "sk-ant-secret-key-2"
    Then mw grist credits was refused saying "is not home"
    And no line was read from stdin
    And there is no key file

  Scenario: clear removes the spent mark
    Given the key was found spent at 2026-10-18T10:00:00Z
    When mw grist credits clear is run
    Then mw grist credits printed a line starting "the spent mark (2026-10-18T10:00:00Z) is removed"
    And there is no spent mark

  Scenario: clear with no mark says so
    When mw grist credits clear is run
    Then mw grist credits printed the line "there is no spent mark: nothing changed"
