Feature: mw report
  mw report sums the factory's quality over a window, from the ledger, the
  beads and the Tester's FINDINGS: what landed, what was refused and why, how
  many attempts a landing took, what caused the refusals, how many bugs were
  filed and how many findings the Tester made per landing. It prints one block
  per rig, then per model, then per host, in 60 columns, or all of it as JSON.
  After QUALITY come SPEED (the landings' time against par, and each host's
  gate) and FUEL (tokens and dollars per landing), and every number carries an
  arrow against the window of the same length before it. The report ends with
  CHEAP WINS: at most three one-line suggestions chosen by fixed rules over its
  numbers, quality rules first, or "CHEAP WINS: none seen". Before CHEAP WINS
  comes FACTORY CHANGES: each millwright landing of the last 30 days with the
  "Expect: <metric> <up|down>" line of its acceptance, the metric in the 7 days
  before and after the landing, and WORSE when it went the wrong way past a 10%
  band. The writes of mw report send tell the Mayor of a WORSE change once and
  keep one snapshot of the key metrics a day. Beside the bugs filed, QUALITY
  counts the escaped bugs: a [bug] story of a landing's rig filed on its day or
  in the 14 days after, naming its story id; FACTORY CHANGES shows each
  landing's own.

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

  Scenario: Escaped bugs are the bugs of the landing's rig filed within 14 days after it that name it
    Given the report stories:
      | id    | title              | rig    | host   | model  | attempts | filed      | description            |
      | l-1   | One                | lampas | laptop | opus   |          |            |                        |
      | e-0   | [bug] it broke     | lampas | laptop | sonnet |          | 2026-10-01 | after l-1 landed       |
      | e-1   | [bug] it broke too | lampas | laptop | sonnet |          | 2026-10-09 | since l-1, it is slow  |
      | e-2   | [bug] elsewhere    | orrery | laptop | sonnet |          | 2026-10-09 | after l-1 landed       |
      | e-3   | [bug] another      | lampas | laptop | sonnet |          | 2026-10-09 | after l-12 landed      |
      | e-4   | [bug] too late     | lampas | laptop | sonnet |          | 2026-10-10 | after l-1 landed       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker    |
      | 2026-09-25 | l-1 | landed on main | opus/high |
    When mw report runs with "--since 7d"
    Then the report prints "escaped bugs 1 ↔" under "opus: landed 0 ↔, refused 0 (0%)"
    And the report prints "escaped bugs 1 ↔" under "lampas: landed 0 ↔, refused 0 (0%)"
    And the report prints "escaped bugs 0" under "orrery: landed 0, refused 0 (0%)"

  Scenario: --json carries the escaped bugs of every block and the window before
    Given the report stories:
      | id    | title          | rig    | host   | model  | attempts | filed      | description      |
      | l-1   | One            | lampas | laptop | opus   |          |            |                  |
      | e-1   | [bug] it broke | lampas | laptop | sonnet |          | 2026-10-09 | after l-1 landed |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker    |
      | 2026-10-02 | l-1 | landed on main | opus/high |
    When mw report runs with "--since 7d --json"
    Then the report JSON has rigs.0.quality.escaped_bugs as "1"
    And the report JSON has rigs.0.previous.escaped_bugs as "0"
    And the report JSON has models.0.name as "opus"
    And the report JSON has models.0.quality.escaped_bugs as "1"

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

  Scenario: Speed is the median and p90 of actual over par, per rig, model and host
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | p-1 | One   | lampas | laptop | sonnet |          |       |
      | p-2 | Two   | lampas | laptop | sonnet |          |       |
      | p-3 | Three | lampas | laptop | sonnet |          |       |
      | p-4 | Four  | lampas | laptop | sonnet |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-10-08 | p-1 | landed on main | sonnet/high |
      | 2026-10-08 | p-2 | landed on main | sonnet/high |
      | 2026-10-09 | p-3 | landed on main | sonnet/high |
      | 2026-10-09 | p-4 | landed on main | sonnet/high |
    And the report benchmarks:
      | story | rig    | host   | day        | actual | ratio |
      | p-1   | lampas | laptop | 2026-10-08 | 480    | 0.8   |
      | p-2   | lampas | laptop | 2026-10-08 | 600    | 1.0   |
      | p-3   | lampas | laptop | 2026-10-09 | 720    | 1.2   |
      | p-4   | lampas | laptop | 2026-10-09 | 1500   | 2.5   |
    When mw report runs with "--since 7d"
    Then the SPEED "rig" block "lampas" prints "par x1.1 median, x2.5 p90"
    And the SPEED "model" block "sonnet" prints "par x1.1 median, x2.5 p90"
    And the SPEED "host" block "laptop" prints "par x1.1 median, x2.5 p90"
    And the SPEED "rig" block "lampas" prints "dispatch to landing 11m00s median"

  Scenario: A rig with gate benchmarks prints its gate usual and latest per host
    Given "lampas" landed 1 stories and refused 0 for "tests-fail" in the window
    And the report benchmarks:
      | story | rig    | host   | day        | gate |
      | g-1   | lampas | laptop | 2026-10-05 | 310  |
      | g-2   | lampas | laptop | 2026-10-06 | 310  |
      | g-3   | lampas | laptop | 2026-10-07 | 310  |
      | g-4   | lampas | laptop | 2026-10-09 | 542  |
      | g-5   | lampas | vps    | 2026-10-09 | 95   |
    When mw report runs with "--since 7d"
    Then the SPEED "rig" block "lampas" prints "laptop: gate usual 5m10s, latest 9m02s"
    And the SPEED "rig" block "lampas" prints "vps: gate usual 1m35s, latest 1m35s"
    And the SPEED "host" block "laptop" prints "lampas: gate usual 5m10s, latest 9m02s"

  Scenario: No benchmark in the window says there is no par yet
    Given "lampas" landed 3 stories and refused 0 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the SPEED "rig" block "lampas" prints "no par yet"
    And the SPEED "model" block "sonnet" prints "no par yet"
    And the SPEED "host" block "laptop" prints "no par yet"

  Scenario: Fuel is the tokens of the ledger and the dollars of the result files per landing
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | f-1 | One   | lampas | laptop | sonnet |          |       |
      | f-2 | Two   | lampas | laptop | sonnet |          |       |
      | f-3 | Three | orrery | vps    | opus   |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      | tokens |
      | 2026-10-08 | f-1 | landed on main | sonnet/high | 1000   |
      | 2026-10-08 | f-2 | landed on main | sonnet/high | 3000   |
      | 2026-10-09 | f-3 | landed on main | opus/high   | 50000  |
    And the report result files:
      | story | attempt | cost |
      | f-1   | 1       | 1.50 |
      | f-2   | 1       | 0.50 |
      | f-3   | 1       | 7.25 |
    When mw report runs with "--since 7d"
    Then the FUEL "rig" block "lampas" prints "tokens/landing 2,000"
    And the FUEL "rig" block "lampas" prints "$/landing 1.00"
    And the FUEL "rig" block "orrery" prints "tokens/landing 50,000"
    And the FUEL "model" block "opus" prints "$/landing 7.25"
    And the FUEL "model" block "sonnet" prints "$/landing 1.00"
    And the FUEL "host" block "vps" prints "$/landing 7.25"

  Scenario: A story landed on its second attempt sums both attempts
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | a-1 | One   | lampas | laptop | sonnet | 2        |       |
    And the report ledger holds the rows:
      | day        | id  | outcome                      | worker      | tokens |
      | 2026-10-08 | a-1 | not landed (tests-fail): red | sonnet/high | 500    |
      | 2026-10-09 | a-1 | landed on main               | sonnet/high | 1500   |
    And the report result files:
      | story | attempt | cost |
      | a-1   | 1       | 0.40 |
      | a-1   | 2       | 0.60 |
    When mw report runs with "--since 7d"
    Then the FUEL "rig" block "lampas" prints "tokens/landing 2,000"
    And the FUEL "rig" block "lampas" prints "$/landing 1.00"

  Scenario: Fuel with no figures says so
    Given "lampas" landed 2 stories and refused 0 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the FUEL "rig" block "lampas" prints "no fuel figures yet"

  Scenario: A refusal rate that fell is better and a landed count that held is level
    Given "lampas" landed 9 stories and refused 1 for "tests-fail" in the window
    And "lampas" landed 8 stories and refused 2 for "tests-fail" in the window before
    When mw report runs with "--since 7d"
    Then the QUALITY "rig" section prints the line "lampas: landed 9 ↑ better, refused 1 (10% ↓ better)"

  Scenario: A landed count within 5% of the one before is level
    Given "lampas" landed 12 stories and refused 0 for "tests-fail" in the window
    And "lampas" landed 12 stories and refused 0 for "tests-fail" in the window before
    When mw report runs with "--since 7d"
    Then the QUALITY "rig" section prints the line "lampas: landed 12 ↔, refused 0 (0% ↔)"

  Scenario: Speed and fuel carry arrows against the window before
    Given the report stories:
      | id   | title | rig    | host   | model  | attempts | filed |
      | q-1  | Now   | lampas | laptop | sonnet |          |       |
      | q-0  | Then  | lampas | laptop | sonnet |          |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      | tokens |
      | 2026-10-08 | q-1 | landed on main | sonnet/high | 1500   |
      | 2026-09-30 | q-0 | landed on main | sonnet/high | 1000   |
    And the report benchmarks:
      | story | rig    | host   | day        | actual | ratio |
      | q-1   | lampas | laptop | 2026-10-08 | 600    | 1.5   |
      | q-0   | lampas | laptop | 2026-09-30 | 600    | 1.0   |
    When mw report runs with "--since 7d"
    Then the SPEED "rig" block "lampas" prints "median ↑ worse, p90 ↑ worse"
    And the FUEL "rig" block "lampas" prints "tokens/landing 1,500 ↑ worse"

  Scenario: --json carries the previous figure and the arrow of a number
    Given "lampas" landed 9 stories and refused 1 for "tests-fail" in the window
    And "lampas" landed 8 stories and refused 2 for "tests-fail" in the window before
    When mw report runs with "--since 7d --json"
    Then the report JSON has rigs.0.arrows.refusal_rate.previous as "20"
    And the report JSON has rigs.0.arrows.refusal_rate.arrow as "↓"
    And the report JSON has rigs.0.arrows.refusal_rate.verdict as "better"
    And the report JSON has rigs.0.arrows.landed.arrow as "↑"

  Scenario: QUALITY comes first, then SPEED, then FUEL
    Given "lampas" landed 2 stories and refused 0 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the report puts QUALITY before SPEED before FUEL

  Scenario: Refusals nobody gave a cause are the first cheap win
    Given "lampas" landed 8 stories and refused 2 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the report prints the line "CHEAP WINS"
    And the report prints the line "1. 2 refusals have no cause: mw cause lampas-r1 lampas-r2"

  Scenario: Seven uncaused refusals name five stories and "+2 more", and --json carries all seven
    Given "lampas" landed 1 stories and refused 7 for "tests-fail" in the window
    When mw report runs with "--since 7d --json"
    Then the report JSON has cheap_wins.0.rule as "R1"
    And the report JSON has cheap_wins.0.text as "7 refusals have no cause: mw cause lampas-r1 lampas-r2 lampas-r3 lampas-r4 lampas-r5 +2 more"
    And the report JSON has cheap_wins.0.ids.0 as "lampas-r1"
    And the report JSON has cheap_wins.0.ids.6 as "lampas-r7"

  Scenario: Seven uncaused refusals print the "+2 more" on a wrapped line, none wider than the report
    Given "lampas" landed 1 stories and refused 7 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the report prints the line "1. 7 refusals have no cause: mw cause lampas-r1 lampas-r2"
    And the report prints the line "lampas-r3 lampas-r4 lampas-r5 +2 more"
    And no line of the report is wider than 60 columns

  Scenario: A refusal whose story has a cause is never named
    Given "lampas" landed 1 stories and refused 3 for "tests-fail" in the window
    And the report story "lampas-r2" has the cause "code"
    When mw report runs with "--since 7d --json"
    Then the report JSON has cheap_wins.0.text as "2 refusals have no cause: mw cause lampas-r1 lampas-r3"
    And the report JSON has cheap_wins.0.ids.1 as "lampas-r3"

  Scenario: A reason that is a third of a rig's refusals is a cheap win
    Given "lampas" landed 5 stories and refused 3 for "tests-fail" in the window
    And every refused story of "lampas" has the cause "code"
    When mw report runs with "--since 7d"
    Then the report prints the line "1. tests-fail is 100% of lampas refusals: gate check first"

  Scenario: A reason under three refusals is not a cheap win
    Given "lampas" landed 5 stories and refused 2 for "tests-fail" in the window
    And every refused story of "lampas" has the cause "code"
    When mw report runs with "--since 7d"
    Then the report prints the line "CHEAP WINS: none seen"

  Scenario: A story that used three attempts is a cheap win
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | w-1 | One   | lampas | laptop | sonnet | 3        |       |
      | w-2 | Two   | lampas | laptop | sonnet | 2        |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-10-08 | w-1 | landed on main | sonnet/high |
      | 2026-10-08 | w-2 | landed on main | sonnet/high |
    When mw report runs with "--since 7d"
    Then the report prints the line "1. w-1 used 3 attempts: split it smaller"

  Scenario: A story that ran out of attempts is a cheap win
    Given the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | x-1 | One   | lampas | laptop | sonnet | 3        |       |
    And the report ledger holds the rows:
      | day        | id  | outcome                      | worker      |
      | 2026-10-08 | x-1 | not landed (tests-fail): red | sonnet/high |
    And the report story "x-1" has the cause "code"
    When mw report runs with "--since 7d"
    Then the report prints the line "1. x-1 used 3 attempts: split it smaller"

  Scenario: A refusal rate up five points on the window before is a cheap win
    Given "lampas" landed 8 stories and refused 2 for "tests-fail" in the window
    And every refused story of "lampas" has the cause "code"
    And "lampas" landed 10 stories and refused 0 for "tests-fail" in the window before
    When mw report runs with "--since 7d"
    Then the report prints the line "1. lampas refusals up 20 points: mw report --rig lampas"

  Scenario: A kind whose par is far off is a cheap win
    Given "lampas" landed 1 stories and refused 0 for "tests-fail" in the window
    And the report benchmarks:
      | story | rig    | host   | kind  | day        | actual | ratio |
      | k-1   | lampas | laptop | rig   | 2026-10-04 | 1000   | 2.0   |
      | k-2   | lampas | laptop | rig   | 2026-10-05 | 1000   | 2.0   |
      | k-3   | lampas | laptop | rig   | 2026-10-06 | 1000   | 2.0   |
      | k-4   | lampas | laptop | rig   | 2026-10-07 | 1000   | 2.0   |
      | k-5   | lampas | laptop | rig   | 2026-10-08 | 1000   | 2.0   |
    When mw report runs with "--since 7d"
    Then the report prints the line "1. par for kind rig is off 100%: try a wider window"

  Scenario: A gate well over its usual is a cheap win
    Given "lampas" landed 1 stories and refused 0 for "tests-fail" in the window
    And the report benchmarks:
      | story | rig    | host   | day        | gate |
      | g-1   | lampas | laptop | 2026-10-05 | 310  |
      | g-2   | lampas | laptop | 2026-10-06 | 310  |
      | g-3   | lampas | laptop | 2026-10-07 | 310  |
      | g-4   | lampas | laptop | 2026-10-09 | 542  |
    When mw report runs with "--since 7d"
    Then the report prints the line "1. laptop gate on lampas 9m02s, usual 5m10s: check its load"

  Scenario: A gate under one and a half times its usual is not a cheap win
    Given "lampas" landed 1 stories and refused 0 for "tests-fail" in the window
    And the report benchmarks:
      | story | rig    | host   | day        | gate |
      | g-1   | lampas | laptop | 2026-10-05 | 310  |
      | g-2   | lampas | laptop | 2026-10-06 | 310  |
      | g-3   | lampas | laptop | 2026-10-07 | 310  |
      | g-4   | lampas | laptop | 2026-10-09 | 450  |
    When mw report runs with "--since 7d"
    Then the report prints the line "CHEAP WINS: none seen"

  Scenario: No rule holding prints that none were seen
    Given "lampas" landed 8 stories and refused 0 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then the report prints the line "CHEAP WINS: none seen"

  Scenario: Five rules holding print three lines in order, and --json carries all five
    Given "lampas" landed 4 stories and refused 3 for "tests-fail" in the window
    And "lampas" landed 10 stories and refused 0 for "tests-fail" in the window before
    And the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | v-1 | One   | lampas | laptop | sonnet | 3        |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-10-08 | v-1 | landed on main | sonnet/high |
    And the report benchmarks:
      | story | rig    | host   | kind | day        | gate | actual | ratio |
      | g-1   | lampas | laptop | rig  | 2026-10-05 | 310  |        |       |
      | g-2   | lampas | laptop | rig  | 2026-10-06 | 310  |        |       |
      | g-3   | lampas | laptop | rig  | 2026-10-07 | 310  |        |       |
      | g-4   | lampas | laptop | rig  | 2026-10-09 | 542  |        |       |
    When mw report runs with "--since 7d"
    Then the report prints the line "1. 3 refusals have no cause: mw cause lampas-r1 lampas-r2"
    And the report prints the line "lampas-r3"
    And the report prints the line "2. tests-fail is 100% of lampas refusals: gate check first"
    And the report prints the line "3. v-1 used 3 attempts: split it smaller"
    And the report prints no line starting "4."
    And the report prints no line starting "lampas refusals up"

  Scenario: --json carries every rule that held with its id
    Given "lampas" landed 4 stories and refused 3 for "tests-fail" in the window
    And "lampas" landed 10 stories and refused 0 for "tests-fail" in the window before
    And the report stories:
      | id  | title | rig    | host   | model  | attempts | filed |
      | v-1 | One   | lampas | laptop | sonnet | 3        |       |
    And the report ledger holds the rows:
      | day        | id  | outcome        | worker      |
      | 2026-10-08 | v-1 | landed on main | sonnet/high |
    And the report benchmarks:
      | story | rig    | host   | day        | gate |
      | g-1   | lampas | laptop | 2026-10-05 | 310  |
      | g-2   | lampas | laptop | 2026-10-06 | 310  |
      | g-3   | lampas | laptop | 2026-10-07 | 310  |
      | g-4   | lampas | laptop | 2026-10-09 | 542  |
    When mw report runs with "--since 7d --json"
    Then the report JSON has cheap_wins.0.rule as "R1"
    And the report JSON has cheap_wins.1.rule as "R2"
    And the report JSON has cheap_wins.2.rule as "R3"
    And the report JSON has cheap_wins.3.rule as "R4"
    And the report JSON has cheap_wins.4.rule as "R6"
    And the report JSON has cheap_wins.0.shown as "true"
    And the report JSON has cheap_wins.3.shown as "false"

  Scenario: The cheap wins fit 60 columns and write nothing
    Given "a-rig-with-a-name-that-goes-on-and-on-and-on-and-on" landed 3 stories and refused 3 for "tests-fail" in the window
    When mw report runs with "--since 7d"
    Then no line of the report is wider than 60 columns
    And the report wrote nothing to the tracker or the vault

  Scenario: FACTORY CHANGES compares a millwright landing's metric the week before and the week after
    Given "lampas" landed 4 and refused 1 on "2026-09-15"
    And "lampas" landed 2 and refused 2 on "2026-09-23"
    And the factory change "mw-c1" titled "Make refusals rarer" landed on "2026-09-20" with the acceptance "1. It works.\n2. Expect: refusals down"
    When mw report runs with "--since 7d"
    Then the report prints the line "FACTORY CHANGES (millwright landings, 30 days)"
    And the report prints the line "7 days before vs 7 after; WORSE past a 10% band"
    And the report prints "landed 2026-09-20, Expect: refusals down" under "Make refusals rarer (mw-c1)"
    And the report prints "20% → 50% (+150%): WORSE" under "Make refusals rarer (mw-c1)"
    And no line of the report is wider than 60 columns
    And the report wrote nothing to the tracker or the vault

  Scenario: A change that moved its metric the right way is better, not WORSE
    Given "lampas" landed 2 and refused 2 on "2026-09-15"
    And "lampas" landed 5 and refused 0 on "2026-09-23"
    And the factory change "mw-c2" titled "Fewer refusals" landed on "2026-09-20" with the acceptance "Expect: refusals down"
    When mw report runs with "--since 7d"
    Then the report prints "50% → 0% (-100%): better" under "Fewer refusals (mw-c2)"
    And the report prints no line starting "50% → 0% (-100%): WORSE"

  Scenario: A change less than a week old is too soon, one with no metric to move says so
    Given "lampas" landed 4 and refused 1 on "2026-10-05"
    And the factory change "mw-c3" titled "Brand new" landed on "2026-10-08" with the acceptance "Expect: refusals down"
    And the factory change "mw-c4" titled "A doc" landed on "2026-09-25" with the acceptance "Expect: none, because it is a doc"
    And the factory change "mw-c5" titled "Unmeasured" landed on "2026-09-26" with the acceptance "1. It works."
    When mw report runs with "--since 7d"
    Then the report prints "too soon: the week after ends 2026-10-15" under "Brand new (mw-c3)"
    And the report prints "landed 2026-09-25, Expect: none" under "A doc (mw-c4)"
    And the report prints the line "1 landed with no Expect named (--json lists them)"
    And the report prints no line starting "Unmeasured"

  Scenario: A factory change shows the bugs that escaped it, too soon until 14 days are over
    Given the factory change "mw-c7" titled "Older" landed on "2026-09-20" with the acceptance "Expect: none, because it is a test"
    And the factory change "mw-c8" titled "Newer" landed on "2026-10-05" with the acceptance "Expect: escaped bugs down"
    And the report stories:
      | id  | title          | rig        | host   | model  | attempts | filed      | description          |
      | e-1 | [bug] it broke | millwright | laptop | sonnet |          | 2026-09-22 | after mw-c7 landed   |
      | e-2 | [bug] it broke | millwright | laptop | sonnet |          | 2026-10-06 | since mw-c8          |
    When mw report runs with "--since 7d"
    Then the report prints "escaped bugs 1 in the 14 days after" under "Older (mw-c7)"
    And the report prints "landed 2026-10-05, Expect: escaped bugs down" under "Newer (mw-c8)"
    And the report prints "escaped bugs 1 so far, too soon: 14 days end 2026-10-19" under "Newer (mw-c8)"
    And no line of the report is wider than 60 columns

  Scenario: A landing older than 30 days or on another rig is not a factory change
    Given the factory change "mw-c6" titled "Long ago" landed on "2026-08-01" with the acceptance "Expect: refusals down"
    And "lampas" landed 1 and refused 0 on "2026-10-01"
    When mw report runs with "--since 7d"
    Then the report prints the line "none landed"
    And the report prints no line starting "Long ago"

  Scenario: A WORSE change writes one event to the Mayor, once
    Given "lampas" landed 4 and refused 1 on "2026-09-15"
    And "lampas" landed 2 and refused 2 on "2026-09-23"
    And the factory change "mw-c1" titled "Make refusals rarer" landed on "2026-09-20" with the acceptance "Expect: refusals down"
    When the writes of mw report send run 3 times
    Then the Mayor's inbox holds 1 message about "Make refusals rarer"

  Scenario: A change that is not WORSE writes nothing to the Mayor
    Given "lampas" landed 2 and refused 2 on "2026-09-15"
    And "lampas" landed 5 and refused 0 on "2026-09-23"
    And the factory change "mw-c2" titled "Fewer refusals" landed on "2026-09-20" with the acceptance "Expect: refusals down"
    When the writes of mw report send run 1 time
    Then the Mayor's inbox holds 0 messages about "Fewer refusals"

  Scenario: The key metrics are kept as one dated snapshot a day in the vault
    Given "lampas" landed 3 and refused 1 on "2026-10-09"
    When the writes of mw report send run 2 times
    And the report factory clock moves on a day
    And the writes of mw report send run 1 time
    Then the vault holds the metric snapshots of the days "2026-10-11,2026-10-12"

  Scenario: --since a story sets the 7 days before its landing beside the 7 days after
    Given the factory change "mw-gq6.425" titled "The gate runs in parallel" landed on "2026-09-20" with the acceptance "Expect: dispatch-to-landing down"
    And the report stories:
      | id    | title            | rig    | host   | model  | attempts | filed      |
      | x-0   | Too early        | lampas | laptop | sonnet |          |            |
      | b-1   | Before one       | lampas | laptop | sonnet | 2        |            |
      | b-2   | Before two       | lampas | laptop | sonnet |          |            |
      | b-3   | Before refused   | lampas | laptop | sonnet |          |            |
      | b-bug | [bug] it broke   | lampas | laptop | sonnet |          | 2026-09-17 |
      | a-1   | After one        | lampas | laptop | sonnet |          |            |
      | a-2   | After two        | lampas | laptop | sonnet |          |            |
      | a-3   | After three      | lampas | laptop | sonnet |          |            |
      | x-2   | Too late         | lampas | laptop | sonnet |          |            |
    And the report ledger holds the rows:
      | day        | id  | outcome                      | worker      | tokens |
      | 2026-09-12 | x-0 | landed on main               | sonnet/high | 9000   |
      | 2026-09-14 | b-1 | landed on main               | sonnet/high | 3000   |
      | 2026-09-15 | b-2 | landed on main               | sonnet/high | 1000   |
      | 2026-09-16 | b-3 | not landed (tests-fail): red | sonnet/high | 500    |
      | 2026-09-22 | a-1 | landed on main               | sonnet/high | 1000   |
      | 2026-09-23 | a-2 | landed on main               | sonnet/high | 1000   |
      | 2026-09-24 | a-3 | landed on main               | sonnet/high | 1000   |
      | 2026-09-28 | x-2 | landed on main               | sonnet/high | 9000   |
    And the report benchmarks:
      | story | rig    | host   | day        | actual | ratio |
      | b-1   | lampas | laptop | 2026-09-14 | 1200   | 2.0   |
      | b-2   | lampas | laptop | 2026-09-15 | 1800   | 3.0   |
      | a-1   | lampas | laptop | 2026-09-22 | 600    | 1.0   |
      | a-2   | lampas | laptop | 2026-09-23 | 600    | 1.0   |
      | a-3   | lampas | laptop | 2026-09-24 | 900    | 1.5   |
    And the report result files:
      | story | attempt | cost |
      | b-1   | 1       | 2.00 |
      | b-1   | 2       | 2.00 |
      | b-2   | 1       | 2.00 |
      | a-1   | 1       | 1.00 |
      | a-2   | 1       | 1.00 |
      | a-3   | 1       | 1.00 |
    When mw report runs with "--since mw-gq6.425"
    Then the report prints the line "mw-gq6.425 landed 2026-09-20"
    And the report prints the line "The gate runs in parallel"
    And the report prints the line "before: 7 days, 2026-09-13 to 2026-09-19"
    And the report prints the line "after: 7 days, 2026-09-21 to 2026-09-27"
    And the report puts QUALITY before SPEED before FUEL
    And the "landed" row of the landing reads "2" before, "3" after, change "+50% better"
    And the "refused" row of the landing reads "1" before, "0" after, change "-100% better"
    And the "refusal rate" row of the landing reads "33%" before, "0%" after, change "-100% better"
    And the "attempts/landing" row of the landing reads "1.50" before, "1.00" after, change "-33% better"
    And the "bugs/landing" row of the landing reads "0.50" before, "0.00" after, change "-100% better"
    And the "findings/landing" row of the landing reads "0.00" before, "0.00" after, change "level"
    And the "par median" row of the landing reads "x2.5" before, "x1.0" after, change "-60% better"
    And the "par p90" row of the landing reads "x3.0" before, "x1.5" after, change "-50% better"
    And the "dispatch to landing" row of the landing reads "25m00s" before, "10m00s" after, change "-60% better"
    And the "tokens/landing" row of the landing reads "2,000" before, "1,000" after, change "-50% better"
    And the "$/landing" row of the landing reads "$3.00" before, "$1.00" after, change "-67% better"
    And no line of the report is wider than 60 columns
    And the report wrote nothing to the tracker or the vault

  Scenario: --since a recent story takes the days after its landing that are over, and compares counts per day
    Given "lampas" landed 7 and refused 0 on "2026-10-03"
    And "lampas" landed 2 and refused 0 on "2026-10-10"
    And "lampas" landed 5 and refused 0 on "2026-10-11"
    And the factory change "mw-gq6.426" titled "Recent" landed on "2026-10-08" with the acceptance "Expect: refusals down"
    When mw report runs with "--since mw-gq6.426"
    Then the report prints the line "before: 7 days, 2026-10-01 to 2026-10-07"
    And the report prints the line "after: 2 days, 2026-10-09 to 2026-10-10"
    And the report prints the line "today counts once it is over"
    And the report prints the line "landed, refused and escaped bugs are compared per day"
    And the "landed" row of the landing reads "7" before, "2" after, change "level"

  Scenario: --since a story that landed yesterday has no day after it over yet
    Given "lampas" landed 7 and refused 0 on "2026-10-03"
    And the factory change "mw-gq6.427" titled "Yesterday" landed on "2026-10-10" with the acceptance "Expect: refusals down"
    When mw report runs with "--since mw-gq6.427"
    Then the report prints the line "after: no day over yet"
    And the "landed" row of the landing reads "7" before, "-" after, change "-"

  Scenario: --since a millwright commit finds the story it landed
    Given "lampas" landed 2 and refused 0 on "2026-09-15"
    And "lampas" landed 4 and refused 0 on "2026-09-22"
    And the factory change "mw-gq6.425" titled "The gate runs in parallel" landed on "2026-09-20" with the acceptance "Expect: dispatch-to-landing down"
    And the millwright commit "9e7d278" is "Merge branch 'mw/mw-gq6.425' into HEAD", made "2026-09-20T15:04:00Z"
    When mw report runs with "--since 9e7d278"
    Then the report prints the line "mw-gq6.425 landed 2026-09-20 (commit 9e7d278)"
    And the report prints the line "The gate runs in parallel"
    And the "landed" row of the landing reads "2" before, "4" after, change "+100% better"

  Scenario: --since a millwright commit that names no story is measured from the day it was made
    Given "lampas" landed 2 and refused 0 on "2026-09-15"
    And "lampas" landed 4 and refused 0 on "2026-09-22"
    And the millwright commit "abc1234" is "Tidy the docs", made "2026-09-20T15:04:00Z"
    When mw report runs with "--since abc1234"
    Then the report prints the line "commit abc1234 made 2026-09-20"
    And the report prints the line "Tidy the docs"
    And the "landed" row of the landing reads "2" before, "4" after, change "+100% better"

  Scenario: --since a story that never landed is refused
    Given "lampas" landed 2 and refused 0 on "2026-09-15"
    Then mw report with "--since mw-nope" is refused, saying "mw-nope has no landing in the builder ledger"

  Scenario: --since a story with --json carries the rows side by side
    Given "lampas" landed 2 and refused 0 on "2026-09-15"
    And "lampas" landed 4 and refused 0 on "2026-09-22"
    And the factory change "mw-gq6.425" titled "The gate runs in parallel" landed on "2026-09-20" with the acceptance "Expect: dispatch-to-landing down"
    When mw report runs with "--since mw-gq6.425 --json"
    Then the report JSON has landing.story as "mw-gq6.425"
    And the report JSON has landing.landed as "2026-09-20"
    And the report JSON has previous_window.since as "2026-09-13"
    And the report JSON has window.since as "2026-09-21"
    And the report JSON has landing.rows.0.measure as "landed"
    And the report JSON has landing.rows.0.before as "2"
    And the report JSON has landing.rows.0.after as "4"
