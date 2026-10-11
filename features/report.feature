Feature: mw report
  mw report sums the factory's quality over a window, from the ledger, the
  beads and the Tester's FINDINGS: what landed, what was refused and why, how
  many attempts a landing took, what caused the refusals, how many bugs were
  filed and how many findings the Tester made per landing. It prints one block
  per rig, then per model, then per host, in 60 columns, or all of it as JSON.
  The window before the one asked for is computed too, for the arrows to come.

  Background:
    Given a report factory whose clock reads "2026-10-11T12:00:00Z"

  Scenario: Refusals are counted by reason and given their causes
    Given "lampas" landed 8 stories and refused 2 for "tests-fail" in the window
    And the report story "lampas-r1" has the cause "code"
    And the report story "lampas-r2" has the cause "tooling"
    When mw report runs with "--since 7d"
    Then the report prints the line "lampas: landed 8, refused 2 (20%)"
    And the report prints "tests-fail 2" under "lampas: landed 8, refused 2 (20%)"
    And the report prints "causes code 1 · tooling 1 · unset 0" under "lampas: landed 8, refused 2 (20%)"

  Scenario: Attempts per landing is the mean of the attempts the landings took
    Given the report stories:
      | id    | title | rig    | host   | model  | attempts | filed |
      | l-1   | One   | lampas | laptop | sonnet | 2        |       |
      | l-2   | Two   | lampas | laptop | sonnet | 1        |       |
      | l-3   | Three | lampas | laptop | sonnet | 1        |       |
      | l-4   | Four  | lampas | laptop | sonnet |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome            | worker      |
      | 2026-10-08 | l-1 | landed on main     | sonnet/high |
      | 2026-10-08 | l-2 | landed on main     | sonnet/high |
      | 2026-10-09 | l-3 | landed on main     | sonnet/high |
      | 2026-10-09 | l-4 | landed on main     | sonnet/high |
    When mw report runs with "--since 7d"
    Then the report prints "attempts per landing 1.25" under "lampas: landed 4, refused 0 (0%)"

  Scenario: Bugs filed in the window are counted per landing
    Given "lampas" landed 8 stories and refused 0 for "tests-fail" in the window
    And the report stories:
      | id    | title            | rig    | host   | model  | attempts | filed      |
      | b-1   | [bug] first      | lampas | laptop | sonnet |          | 2026-10-09 |
      | b-2   | [bug] second     | lampas | laptop | sonnet |          | 2026-10-10 |
      | b-old | [bug] long ago   | lampas | laptop | sonnet |          | 2026-09-01 |
      | f-1   | a feature        | lampas | laptop | sonnet |          | 2026-10-09 |
    When mw report runs with "--since 7d"
    Then the report prints "bugs filed 2 (0.25/landing)" under "lampas: landed 8, refused 0 (0%)"

  Scenario: Tester findings are counted per landing from the landed story's comments
    Given "lampas" landed 8 stories and refused 0 for "tests-fail" in the window
    And the report story "lampas-l1" carries the comment "FINDINGS:\n- [bug] the list does not scroll\n- [bug] the button is cut off\n- [taste] the colour is dull"
    When mw report runs with "--since 7d"
    Then the report prints "Tester findings 3 (0.38/landing)" under "lampas: landed 8, refused 0 (0%)"

  Scenario: Per model and per host blocks follow the per rig block
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | m-1 | One   | lampas | laptop | sonnet |          |       |
      | m-2 | Two   | lampas | vps    | opus   |          |       |
      | m-3 | Three | orrery | vps    | opus   |          |       |
      | m-4 | Four  | orrery | vps    | opus   |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome                       | worker      |
      | 2026-10-08 | m-1 | landed on main                | sonnet/high |
      | 2026-10-08 | m-2 | landed on main                | opus/high   |
      | 2026-10-09 | m-3 | landed on main                | opus/high   |
      | 2026-10-09 | m-4 | not landed (tests-fail): red  | opus/high   |
    When mw report runs with "--since 7d"
    Then the "rig" section prints the line "lampas: landed 2, refused 0 (0%)"
    And the "rig" section prints the line "orrery: landed 1, refused 1 (50%)"
    And the "model" section prints the line "sonnet: landed 1, refused 0 (0%)"
    And the "model" section prints the line "opus: landed 2, refused 1 (33%)"
    And the "host" section prints the line "laptop: landed 1, refused 0 (0%)"
    And the "host" section prints the line "vps: landed 2, refused 1 (33%)"

  Scenario: --rig and --host narrow every block
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | n-1 | One   | lampas | laptop | sonnet |          |       |
      | n-2 | Two   | lampas | vps    | opus   |          |       |
      | n-3 | Three | orrery | vps    | opus   |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-10-08 | n-1 | landed on main | sonnet/high |
      | 2026-10-08 | n-2 | landed on main | opus/high   |
      | 2026-10-09 | n-3 | landed on main | opus/high   |
    When mw report runs with "--since 7d --rig lampas --host vps"
    Then the "rig" section prints the line "lampas: landed 1, refused 0 (0%)"
    And the "rig" section prints no line starting "orrery"
    And the "model" section prints the line "opus: landed 1, refused 0 (0%)"
    And the "model" section prints no line starting "sonnet"
    And the "host" section prints no line starting "laptop"

  Scenario: Rows older than --since are left out
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | o-1 | Old   | lampas | laptop | sonnet |          |       |
      | o-2 | New   | lampas | laptop | sonnet |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-09-20 | o-1 | landed on main | sonnet/high |
      | 2026-10-09 | o-2 | landed on main | sonnet/high |
    When mw report runs with "--since 2026-10-05"
    Then the report prints the line "lampas: landed 1, refused 0 (0%)"

  Scenario: A ledger row whose story is gone counts under the rig unknown
    Given the report ledger holds the rows:
      | day        | id      | outcome                         | worker      |
      | 2026-10-08 | mw-gone | landed on main                  | sonnet/high |
      | 2026-10-08 | mw-lost | not landed (tests-fail): red    | sonnet/high |
    When mw report runs with "--since 7d"
    Then the "rig" section prints the line "unknown: landed 1, refused 1 (50%)"
    And the "model" section prints the line "sonnet: landed 1, refused 1 (50%)"

  Scenario: --json prints the window, the window before and the same numbers
    Given "lampas" landed 8 stories and refused 2 for "tests-fail" in the window
    And the report ledger also holds a landed row for "lampas" on "2026-09-30"
    When mw report runs with "--since 7d --json"
    Then the report JSON has window.since as "2026-10-04"
    And the report JSON has previous_window.since as "2026-09-27"
    And the report JSON has rigs.0.name as "lampas"
    And the report JSON has rigs.0.quality.landed as "8"
    And the report JSON has rigs.0.quality.refused as "2"
    And the report JSON has rigs.0.previous.landed as "1"

  Scenario: Every line is at most 60 columns, a long rig name too
    Given "a-rig-with-a-name-that-goes-on-and-on-and-on-and-on" landed 3 stories and refused 1 for "merge-conflict" in the window
    When mw report runs with "--since 7d"
    Then no line of the report is wider than 60 columns
