Feature: mw report send
  mw report send builds a summary of the week from the same numbers as mw
  report, in at most 14 lines of 60 columns, and posts it as a plain message to
  the Governor's Factory channel: QUALITY, SPEED, FUEL and CHEAP WINS in that
  order, each number with its arrow against the week before. It goes out from
  the home alone, at most once a UTC day, the day remembered on this host;
  --force sends again, and --dry-run prints the summary and sends nothing.

  Background:
    Given a summary factory whose clock reads "2026-10-11T12:00:05Z"
    And the summary home file says "laptop 2026-10-01T00:00:00Z mayor"
    And this summary host is "laptop"
    And the summary ledger holds the rows:
      | day        | id   | outcome                                   | tokens |
      | 2026-10-05 | w-1  | landed on main                            | 1000   |
      | 2026-10-06 | w-2  | landed on main                            | 1000   |
      | 2026-10-08 | w-3  | landed on main                            | 1000   |
      | 2026-10-09 | w-4  | landed on main                            | 1000   |
      | 2026-10-10 | w-5  | refused for tests-fail                    | 0      |
      | 2026-09-28 | p-1  | landed on main                            | 2000   |
      | 2026-09-30 | p-2  | landed on main                            | 2000   |
    And the summary benchmarks:
      | story | day        | actual | ratio |
      | w-1   | 2026-10-05 | 600    | 1.2   |
      | w-3   | 2026-10-08 | 600    | 1.2   |
      | p-1   | 2026-09-28 | 900    | 1.5   |
      | p-2   | 2026-09-30 | 900    | 1.5   |
    And the postern backend will report the summary txid "txid-one"

  Scenario: On the home it posts one plain message to the Factory channel and prints its txid
    When mw report send runs
    Then the summary was posted once, as a plain message with no class and no bead
    And the posted text starts with "Factory, 7 days to 2026-10-11"
    And the posted text holds QUALITY, SPEED, FUEL and CHEAP WINS in that order
    And the posted text has the line "QUALITY landed 4 ↑ better, refused 1 (20% ↑ worse)"
    And the posted text has the line "escaped bugs 0"
    And the posted text has the line "FUEL tokens/landing 1,000 ↓ better"
    And the posted text is at most 14 lines of 60 columns
    And the summary prints the txid "txid-one"

  Scenario: A second run on the same UTC day sends nothing and says when it sent
    Given mw report send has run
    And the summary clock reads "2026-10-11T18:30:00Z"
    When mw report send runs
    Then the summary was posted once, as a plain message with no class and no bead
    And the summary says "already sent today (2026-10-11) at 12:00:05Z, txid-one"
    And the summary says "--force sends again"

  Scenario: The next UTC day it sends again
    Given mw report send has run
    And the summary clock reads "2026-10-12T12:00:05Z"
    When mw report send runs
    Then the summary was posted 2 times

  Scenario: --force sends again the same day
    Given mw report send has run
    When mw report send runs with --force
    Then the summary was posted 2 times

  Scenario: --dry-run prints the summary and sends and remembers nothing
    When mw report send runs with --dry-run
    Then nothing was posted
    And the summary says "Factory, 7 days to 2026-10-11"
    And the summary says "CHEAP WINS"
    And the summary remembers no day

  Scenario: --dry-run still prints on a day already sent
    Given mw report send has run
    When mw report send runs with --dry-run
    Then the summary was posted once, as a plain message with no class and no bead
    And the summary says "Factory, 7 days to 2026-10-11"

  Scenario: On a host that is not home it says why and sends nothing
    Given this summary host is "desktop"
    When mw report send runs
    Then nothing was posted
    And the summary says "laptop is home, not desktop"
    And the summary remembers no day
