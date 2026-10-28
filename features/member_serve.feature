# mw member serve: the member's own dispatcher. Credentials cannot cross users,
# so a dispatch timer under the holder's user cannot use the member's Codex
# sign-in; the member runs their own, under their own user.

Feature: mw member serve
  Run as the member, mw member serve installs the holder's own dispatch units
  (mw-dispatch.service and mw-dispatch.timer) under the member's user manager,
  with a dispatch.env whose PATH names where mw, bd, git, tmux and codex are, and
  enables the timer. It needs the member's factory and linger, and refuses
  without them. --way-back prints how to remove it and runs nothing.

  Background:
    Given a throwaway home for mw member
    And mw member init has made "luke" with the vault "luke-vault"
    And the tools mw, bd, git, tmux and codex are found

  Scenario: The dispatch units are written in the holder's shape and the timer is enabled
    When mw member serve runs
    Then member serve succeeds
    And the member's mw-dispatch.service and mw-dispatch.timer are the holder's units
    And systemctl read the units again and enabled and started mw-dispatch.timer

  Scenario: The PATH line names where each tool is
    When mw member serve runs
    Then member serve succeeds
    And the member's dispatch.env has a PATH line naming the directories of mw, bd, git, tmux and codex

  Scenario: A dispatch.env the member already has is left alone
    Given the member already has a dispatch.env holding "PATH=/their/bin:/usr/bin"
    When mw member serve runs
    Then member serve succeeds
    And the member's dispatch.env still holds "PATH=/their/bin:/usr/bin"
    And the report says the dispatch.env was left as it was

  Scenario: Linger that is not set is refused, and the root hand step is printed
    Given loginctl says linger is not set
    When mw member serve runs
    Then member serve is refused, saying "loginctl enable-linger luke"
    And no dispatch unit or dispatch.env was written
    And systemctl was not run to enable anything

  Scenario: A tool that is missing is refused, and nothing is written
    Given the tool codex cannot be found
    When mw member serve runs
    Then member serve is refused, saying "codex"
    And no dispatch unit or dispatch.env was written
    And systemctl was not run to enable anything

  Scenario: A home with no member factory is refused
    Given the member's config file is removed
    When mw member serve runs
    Then member serve is refused, saying "no member factory here"
    And no dispatch unit or dispatch.env was written

  Scenario: A second run is refused and touches nothing
    Given mw member serve has run
    When mw member serve runs
    Then member serve is refused, saying "already serving"

  Scenario: --way-back prints the removal and runs nothing
    Given mw member serve has run
    When mw member serve --way-back runs
    Then member serve succeeds
    And the way back says to disable mw-dispatch.timer and to remove the two unit files
    And the units are still there
    And systemctl was not run again
