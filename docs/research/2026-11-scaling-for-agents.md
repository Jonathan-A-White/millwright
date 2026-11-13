# Research: boundaries, deletions and measures that keep the factory cheap to change

Answers mw-6ww.175.1, the first ticket of the map mw-6ww.175 (the Governor's ask of 2026-11-13 on
Postern: do we have the machinery to find wise boundaries, to delete code and to know what is
used, so that the products and the factory scale for work done by sessions). Research only: no
code changed, no bead written, nothing in the vault edited. Read on the Laptop on 2026-11-13:
the millwright, postern and lampas checkouts at their main heads (ed08b4e3, 090229b0, and
Lampas's as it moved from ae02aec0 to af1e44fb4 during the reading), the Builder ledger, `metrics/factory.jsonl`, the beads dump of 2026-11-12 and the
session transcripts kept on this host. Four read-only search helpers surveyed the code; every
number in the tables below was computed by the lead, and how is in the last section.

<!-- toc -->

- [The short answer](#the-short-answer)
- [Scorecard](#scorecard)
  - [1. Landing rate](#1-landing-rate)
  - [2. Lead time](#2-lead-time)
  - [3. Change-fail rate](#3-change-fail-rate)
  - [4. Time to restore](#4-time-to-restore)
  - [5. First-try landings](#5-first-try-landings)
  - [6. Tokens per landing](#6-tokens-per-landing)
  - [7. Hot files over the allowance](#7-hot-files-over-the-allowance)
  - [8. Removed share](#8-removed-share)
  - [Left out, and why](#left-out-and-why)
- [Hotspots](#hotspots)
  - [What the list says](#what-the-list-says)
  - [What a hot file costs](#what-a-hot-file-costs)
  - [A limit of the test-caller count](#a-limit-of-the-test-caller-count)
- [Boundaries](#boundaries)
  - [Millwright](#millwright)
  - [Postern](#postern)
  - [Lampas](#lampas)
  - [Across rigs](#across-rigs)
- [Deletions](#deletions)
- [Usage](#usage)
  - [What we can tell today](#what-we-can-tell-today)
  - [The cheapest way to know more, with nothing leaving his devices](#the-cheapest-way-to-know-more-with-nothing-leaving-his-devices)
- [Top ten stories](#top-ten-stories)
  - [Cheap wins](#cheap-wins)
  - [Big investments](#big-investments)
  - [Next in line, not in the ten](#next-in-line-not-in-the-ten)
- [Method and limits](#method-and-limits)

<!-- /toc -->

## The short answer

**Is it necessary?** Yes, and the ledger can show it. In the last 30 days a landing that
touched one of its rig's five hottest files cost about one and a half times the tokens of a
landing of the same size that did not (millwright size S: 2.68M against 1.73M; size M: 5.05M
against 3.09M; Lampas size M: 12.7M against 7.3M; the smallest gap is Lampas size S, 4.26M
against 3.44M). A third of millwright's and Postern's landings touch one of
those five files. That premium is the price of the code's shape, paid about eighteen times a
day across the three rigs.

**What we have.** More than half the machinery: the factory report (landings, refusals by
reason, escaped bugs, speed, fuel, first-try share), the common-errors register, the
test-caller count on every path (`mw path set`), the `shared-files` dispatch gate, the rig
memory, a module map in Postern and in Lampas, and five shared kits.

**What is missing.** Three things, all cheap to add:

1. **Nothing reads the code's own shape.** No report names the hot files, so the Mayor learns
   that `application/next.go` has 149 test callers only when a story trips the warning.
2. **Code only arrives.** In 30 days millwright removed 8 production lines for every 100 it
   added and deleted no code file; nothing counts or celebrates a removal. Classic dead code is
   nearly absent in millwright (14 unreachable functions outside the test fakes), so the
   deletions worth having are whole features nobody uses, and those need usage evidence.
3. **We do not know what is used.** `mw` keeps no record of its own runs. Lampas already counts
   screens and features on the device but shows the counts to nobody. Postern counts nothing.

**What to do.** Ten stories, at the end: eight cheap wins the Mayor can file, and two big
investments for one card to the Governor. The order is measure first (so the rest can be
judged), then the cheapest cuts to the hot files, then the checks that stop decay coming back.

**How we would know.** On the files a story changed: tokens per landing falls toward the level
of landings that touch no hot file, the count of hot files over the test-caller allowance
falls, and the removed share of lines rises. Those are measures 6, 7 and 8 of the scorecard.

## Scorecard

Eight measures. The first six are computed today by `mw report` or can be read from the beads
dump; the last two are new and need only git. Values are for the 30 days to 2026-11-13 unless a
row says otherwise.

| # | Measure | millwright | postern | lampas |
|-|-|-|-|-|
| 1 | Landing rate (landings a day) | 26.7 (801) | 4.6 (139) | 40.9 (1,226) |
| 2 | Lead time (dispatch to landing, median) | 30m17s | 58m31s | 2h35m |
| 3 | Change-fail rate (escaped bugs per landing) | 17% (134) | 27% (38) | 25% (309; 15% without the Tester's 126) |
| 4 | Time to restore (bug filed to bug closed, median) | 1.0h | 2.1h | 5.0h; main red 60 min median |
| 5 | First-try landings (sonnet / opus) | 93% / 89% | 84% / 86% | 91% / 87% |
| 6 | Tokens per landing | 3,452,658 ($1.19) | 4,863,484 ($1.80) | 7,048,687 ($1.89) |
| 7 | Hot files over the allowance | 22 of 111 | 22 of 49 | 69 of 138 |
| 8 | Removed share (lines removed per 100 added) | 8 | 20 | 21 |

### 1. Landing rate

- **Definition.** Stories landed on the target branch per day.
- **Source.** DORA's deployment frequency (Forsgren, Humble, Kim, *Accelerate*, 2018; the yearly
  State of DevOps reports).
- **How mw computes it.** It does: `mw report --rig <rig> --since 30d`, QUALITY, "landed N",
  from the Builder ledger.
- **Reading it here.** It follows how much the Mayor files more than it follows the code's
  health, so read it beside the others, never alone.

### 2. Lead time

- **Definition.** Median wall time from a story's dispatch to its landing.
- **Source.** DORA's lead time for changes.
- **How mw computes it.** It does: SPEED, "dispatch to landing", from the ledger. The whole wait
  (bead created to bead closed, queue included) is in the beads dump: median 1.4h, 2.6h and
  10.6h for the three rigs, over the stories closed in the 30 days to 2026-11-12.
- **Reading it here.** Lampas's 2h35m is mostly its gate (usual 34 minutes to 2 hours by host).

### 3. Change-fail rate

- **Definition.** Escaped bugs per landing: `[bug]` stories of the landing's rig, filed on its
  day or in the 14 days after, that name its story id; a Builder's sweep finds are left out.
- **Source.** DORA's change failure rate.
- **How mw computes it.** It does: QUALITY, "escaped bugs N (Tester M)", from the ledger and
  the beads. For a rig that lands in batches the batch rows of `metrics/factory.jsonl` add a
  second reading: 106 of Lampas's 503 batch landings since 2026-10-14 ended `fixed`, that is,
  went red and needed the fixer (21%).

### 4. Time to restore

- **Definition.** How long a failure stays: for a batch-landing rig, minutes main was red per
  red batch; for every rig, the time from a `[bug]` bead's creation to its close.
- **Source.** DORA's failed deployment recovery time (once "time to restore service").
- **How mw computes it.** Red minutes are already in the batch rows (`red_minutes`): Lampas
  median 60, p90 116, over the 60 batches that recorded any. The bug time is `closed_at` less
  `created_at` over the `[bug]` beads closed in the window: median 1.0h (p90 8.7h) for
  millwright, 2.1h (12.9h) for Postern, 5.0h (2.3 days) for Lampas. `mw report` prints neither
  today; both are one line each to add.

### 5. First-try landings

- **Definition.** The share of landed stories that landed on their first session, with no
  refusal before it.
- **Source.** First-pass yield, from lean manufacturing (the share of units that pass with no
  rework); DORA's 2024 report adds the kindred "rework rate".
- **How mw computes it.** It does: MODELS, "first try", per model, from the ledger. Attempts per
  landing is its twin: 1.10, 1.22 and 1.12.
- **Reading it here.** Postern is the outlier (84%, 25% of sessions refused), and its refusals'
  causes are mostly tests (26 of 46), which is the health report's ground, not this map's.

### 6. Tokens per landing

- **Definition.** Tokens the Builder ledger charged for a story, every attempt summed, per
  landed story.
- **Source.** Unit cost, the FinOps Foundation's "cost per unit of value"; for sessions it is
  also the nearest thing we hold to "the context a change needs".
- **How mw computes it.** It does: FUEL, "tokens/landing" and "$/landing".
- **Reading it here.** This is the measure the code's shape moves most directly: see the cost
  table under Hotspots.

### 7. Hot files over the allowance

- **Definition.** Of the production files changed in 10 or more commits in 90 days, how many
  have more test callers than a size M story allows (20). A file over the line cannot be named
  by an S or M story without the warning, so every change to it is big by rule.
- **Source.** Tornhill's hotspots (*Your Code as a Crime Scene*, 2015; *Software Design
  X-Rays*, 2018): change frequency times size finds the few files where work and risk gather.
  The test-caller count stands in for coupling.
- **How mw computes it.** New, from git and the checkout only: `git log --since="90 days ago"
  --no-merges --numstat` gives the commits per file, and `RigFiles.TestCallers`
  (infrastructure/rig/rigfiles.go:137) gives the callers, the same count `mw path set` prints.
- **Today.** 22 of 111, 22 of 49 and 69 of 138. Over the XL allowance (50): 13, 10 and 32. Its
  twin: 32%, 34% and 20% of the last 30 days' landings touched one of the rig's five hottest
  files (merge commits on main, files of each merge against its first parent).

### 8. Removed share

- **Definition.** Production lines removed per 100 added in the window, with the count of code
  files deleted beside it.
- **Source.** Lehman's laws of software evolution (1980): a system in use grows and its
  structure decays unless work is done to reduce it. Removal is that work, and no DORA measure
  sees it.
- **How mw computes it.** New, from git only: the same numstat, summed over code files that are
  not tests; `git log --diff-filter=D --name-only` for the files.
- **Today.** millwright +115,941 and -9,586 lines (8 per 100), no code file deleted; Postern
  +14,779 and -2,969 (20), 9 files deleted; Lampas +108,892 and -23,198 (21), 57 files deleted.
  Net growth in 30 days: +106,355, +11,810 and +85,694 lines.

### Left out, and why

- **Module depth as one number** (Ousterhout, *A Philosophy of Software Design*, 2018). No
  honest single figure exists. It is used below as the test of each seam (how small is the
  interface against what it hides), with the counts that matter named seam by seam.
- **Cyclomatic complexity.** The hotspot table carries Tornhill's indentation count beside the
  line count; it did not change the ranking, so size is enough.
- **Test coverage, and survey measures (SPACE, DevEx).** Coverage is not what hurts here, and
  there is one person to survey; his time is already in the report's HIS TIME.

## Hotspots

Churn is the number of commits (merges left out) that changed the file in the 90 days to
2026-11-13. All three rigs are younger than that (first commits 2026-09-18, 2026-09-24 and
2026-10-08), so the window is each rig's whole life. Lines are non-blank lines today. Score is
commits times lines. Indent is Tornhill's indentation count, a stand-in for complexity. Test
callers is the count `mw path set` uses. Test files and test fakes are ranked apart, below.

| # | Rig | File | Commits | Lines | Score | Indent | Test callers |
|-|-|-|-|-|-|-|-|
| 1 | millwright | application/next.go | 170 | 4,221 | 717,570 | 4,800 | 149 |
| 2 | millwright | infrastructure/config/config.go | 122 | 3,982 | 485,804 | 3,449 | 23 |
| 3 | millwright | application/dispatch.go | 131 | 3,178 | 416,318 | 4,472 | 93 |
| 4 | millwright | application/status.go | 119 | 3,311 | 394,009 | 4,429 | 65 |
| 5 | millwright | application/postern.go | 83 | 2,492 | 206,836 | 2,470 | 114 |
| 6 | lampas | src/Reader.tsx | 189 | 915 | 172,935 | 2,442 | 356 |
| 7 | lampas | src/data/db.ts | 92 | 1,309 | 120,428 | 2,693 | 712 |
| 8 | millwright | application/posternview.go | 46 | 2,107 | 96,922 | 2,746 | 45 |
| 9 | millwright | application/batchland.go | 51 | 1,681 | 85,731 | 2,134 | 19 |
| 10 | millwright | application/worktracker.go | 83 | 953 | 79,099 | 686 | 150 |
| 11 | lampas | src/Talk.tsx | 85 | 925 | 78,625 | 3,158 | 2 |
| 12 | millwright | cmd/mw/postern.go | 68 | 1,033 | 70,244 | 2,167 | 0 |
| 13 | millwright | application/gristgrind.go | 41 | 1,670 | 68,470 | 1,910 | 13 |
| 14 | millwright | infrastructure/beads/beads.go | 51 | 1,289 | 65,739 | 1,306 | 39 |
| 15 | millwright | application/sync.go | 43 | 1,485 | 63,855 | 1,267 | 58 |

No Postern file reaches the fifteen: its biggest production file has 886 lines and none has
1,000. Its own top five, and Lampas's next five, so that each rig's seams can be seen:

| Rig | File | Commits | Lines | Score | Test callers |
|-|-|-|-|-|-|
| postern | server/internal/api/handlers.go | 40 | 886 | 35,440 | 23 |
| postern | src/cockpit/TalkLineScreen.tsx | 41 | 691 | 28,331 | 8 |
| postern | src/cockpit/Conversation.tsx | 38 | 589 | 22,382 | 19 |
| postern | src/todo/TodoBoard.tsx | 19 | 956 | 18,164 | 0 |
| postern | src/data/db.ts | 38 | 461 | 17,518 | 239 |
| lampas | src/IdeaSheet.tsx | 64 | 894 | 57,216 | 11 |
| lampas | src/services/talk.ts | 86 | 663 | 57,018 | 77 |
| lampas | src/WordSheet.tsx | 62 | 895 | 55,490 | 19 |
| lampas | src/data/grammar/questions.ts | 42 | 1,174 | 49,308 | 196 |
| lampas | src/settings/registry.ts | 66 | 643 | 42,438 | 64 |

### What the list says

- **Millwright's heat is four hub files.** `next.go`, `dispatch.go`, `status.go` and
  `config.go` are each over 3,000 lines and were changed in 119 to 170 commits. 23 of
  millwright's 737 production files have 1,000 lines or more and hold 22% of its lines; Postern
  has none and Lampas three.
- **Lampas's heat is registries, not size.** Two short files that every feature must edit do
  not rank by score but are changed more than almost anything: `src/data/repositories/index.ts`
  (141 lines, 113 commits) and `src/preface.ts` (120 lines, 121 commits). `src/nav/route.ts`
  (63 commits) and `src/settings/rows.ts` (63) are the same kind.
- **The test side has its own hot files.** `application/apptest/faketracker.go` (2,645 lines,
  115 commits), `infrastructure/config/config_test.go` (2,762, 105),
  `features/steps/next_steps.go` (1,769, 83) in millwright; `features/steps/talk-screen.steps.tsx`
  (2,896, 53) in Postern. Each mirrors a production hub and is changed with it.

### What a hot file costs

Each merge commit on main in the last 30 days was matched to its story's rows in the Builder
ledger (tokens of every attempt summed) and to the story's size in the beads dump.

| Rig | Landings that touched a top-five file | Tokens, median | Landings that did not | Tokens, median |
|-|-|-|-|-|
| millwright, all sizes | 156 | 3.88M | 321 | 2.09M |
| millwright, size S | 42 | 2.68M | 125 | 1.73M |
| millwright, size M | 33 | 5.05M | 62 | 3.09M |
| postern, all sizes | 17 | 5.40M | 35 | 2.96M |
| lampas, size S | 41 | 4.26M | 321 | 3.44M |
| lampas, size M | 76 | 12.70M | 210 | 7.27M |

The premium holds inside each size, so it is not only that such stories are bigger. It is a
correlation all the same: a story that reaches a hub also changes more files (10 against 5 at
the median in millwright), and no split of a file changes that by itself.

### A limit of the test-caller count

For a TypeScript file the count matches a test that says any exported name as a word
(infrastructure/rig/rigfiles.go:186-195). `src/Reader.tsx` scores 356 because 356 test files
contain the word Reader; none imports it. The repositories barrel scores 650, and 213 test
files import it. For a screen that end-to-end tests reach without importing, the word match is
the only signal there is, so this is a deliberate over-count and not a fault; but it means the
count ranks Lampas's and Postern's files by how common their names are as well as by how many
tests lean on them. Read those two columns as upper bounds.

## Boundaries

The test of a seam, after Ousterhout: it is worth cutting when a small interface hides a lot,
and here also when it takes a file off the list that every story must edit. Each candidate has
its evidence and a verdict. None of them is a new service: the only network seam the three rigs
share is Postern's backend, and what it needs is a firmer contract (B5), not more parts.

### Millwright

**B1. The seams already inside the four hub files.** Verdict: cut now, by moving code between
files of the same package, which changes no caller.

- `application/status.go` (3,590 lines): `StatusReport.String` and the other rendering run
  from line 2331 to about 3227, a quarter of the file, and take only a `StatusReport` value
  that `Status.Run` (status.go:910) produces.
- `application/next.go` (4,542 lines): the report type and its text (next.go:579-747 and
  4349-4542) never touch `Next`; the lease and heartbeat (1021-1190), the refusal checks
  (1974-2620) and the ledger line (3929-4180) are each a block of their own.
- `application/dispatch.go` (3,390 lines): the report's outcome types (307-545) and rendering
  (3232-3390); `run` itself is one method of 645 lines (dispatch.go:866).
- `infrastructure/config/config.go` (4,288 lines, 124 exported functions): the doctor and watch
  settings (3391-3935) have one consumer, `cmd/mw/doctor.go`; the TOML reading (3936-4106) is
  separate from every getter.
- The line ranges inside next.go, dispatch.go and config.go are a helper's reading; the first
  declaration at each start was checked.

**B2. The hubs as lists of small parts.** Verdict: the big investment for millwright; pilot on
the refusal checks.

- `Next` has 73 fields (application/next.go:238-576), 47 of them ports by a helper's count. It
  is built at three sites (cmd/mw/next.go:96, cmd/mw/next.go:248, cmd/mw/land.go:133) and
  mirrored by a test context of 78 fields (features/steps/next_steps.go:34). A new check at
  close-out edits all of them and the hub itself, which is why next.go has 149 test callers.
- `Dispatch` has 48 fields (application/dispatch.go:72-295), `Status` 69
  (application/status.go:204-481).
- The model to copy is already in the rig: the dispatch gates are a table of named reasons
  (application/dispatchgates.go:45), and the doctor's probes are files of their own under
  `infrastructure/doctor`. A close-out check that is a small type with its own ports, listed in
  one place, would be added without opening next.go.

**B3. `WorkTracker`.** Verdict: leave the port whole; stop it widening.

- 43 methods (application/worktracker.go:747-1011); 58 non-test files of `application/` name
  it, 53 holding it whole as a field (helper's count). Its fake is 2,837 lines
  (application/apptest/faketracker.go) and the hottest test file in the rig.
- Narrow role interfaces already exist beside it (worktracker.go:267-287 and 429-447,
  `IceNotes` at ice.go:48, `TrackerNotes` at status.go:179). The cheap rule: a new use case
  takes a role interface, never the whole port. Splitting the 43 would touch 150 test files for
  no change a session would feel.

**B4. Configuration.** Verdict: split the file by table now (part of B1); read the file once
per run later, if SHAPE still shows it hot.

- Every getter re-reads the file through `setting` (config.go:3936) and a hand-written parser
  (`valueIn`, config.go:3985). `cmd/mw` calls `config.X(` 558 times across 90 files (helper's
  count).
- Of the 50 root keys mw knows (domain/configtoml.go:280), 15 are set in this host's config
  file and 35 are set nowhere (helper's count, names only).

**B5. Sub-packages out of `application/`.** Verdict: not yet.

- `application/` is one Go package: 301 non-test files, 123,265 lines, 1,706 exported functions
  and types in one namespace.
- By a helper's count the grist family (22 files, 11,622 lines) is the most separable: 14
  outside files use its names. Postern, talk and grist would have to move together; the seat
  family cannot, because `Vault` (application/seatboot.go:257) is used by about 35 outside files.
- A package split of this size is costly, and a flat Go package costs a session less than a
  hub file does. Do B1 and B2 first and let the measures say whether this is still wanted.

**B6. The step definitions.** Verdict: small win, when a story is next in there.

- `features/steps` holds 188 Go files and 61,743 lines, with a context struct per feature.
  The same helpers are written again per context: `nothingWritten` in 8 files (the first at
  features/steps/cause_steps.go:98, note_steps.go:125, member_steps.go:511), `refusedSaying` or
  `isRefusedSaying` in 12, `theClockReads` in 7. One embedded base would hold them.

### Postern

**B7. The wire contract, kept in step by hand in three codebases.** Verdict: cut now; it is the
one boundary between rigs that fails silently.

- Message classes: the backend lists 11 (postern server/internal/record/envelope.go:11-23), the
  PWA 10, without `grist` (postern src/data/db.ts:36), millwright 5 for send
  (application/postern.go:554) with the rest spread over other files.
- Event kinds: the PWA knows 9 (postern src/model/events.ts:21), millwright 16
  (domain/events/events.go:37-54).
- Expect states: the PWA's list says it follows `domain/card.go ExpectStates` and lacks `held`
  (postern src/model/cards.ts:12 against millwright domain/card.go:27).
- The shared test vectors are copied by hand and have drifted: millwright's
  `infrastructure/postern/testdata/protocol-vectors.json` says it was copied at postern commit
  fee65de and differs from postern's `docs/fixtures/protocol-vectors.json` from line 30.
- Nothing mechanical ties the three: no shared schema and no generated code; `docs/protocol.md`
  is prose.

**B8. Layers that are written down but not held.** Verdict: cut now, with a rule that can only
tighten.

- `docs/module-map.md` says cockpit sits on model, services and ui, over data, with a pure
  model. The only import rule in force is for the chain (postern eslint.config.js:21).
- Against it today, by a helper's count: model imports services in 16 lines (for example
  src/model/needs.ts:5-8, src/model/conversation.ts:9-10), model imports ui
  (src/model/tree.ts:7), data imports services (src/data/repositories/drafts-repo.ts:2), ui
  imports services (src/ui/primitives.tsx:6), and cockpit and todo import each other
  (src/todo/posternStore.ts:9-12, src/cockpit/BeadScreen.tsx:25).

**B9. Inside the hot files.** Verdict: lift when next touched; no story of its own.

- `server/internal/api/handlers.go` (969 lines): by a helper's reading, blobs (813-920), the
  chain relay (693-756) and push (757-812) each call only the shared write helpers. Its routes
  live in two tables that must agree (handlers.go:224-252 and licencemap.go:9-39).
- `src/data/db.ts`: by a helper's count the row types (3-282) are imported by 63 files and
  hold no code; they are why the file has 239 test callers.

### Lampas

**B10. The registries every feature must edit.** Verdict: the big investment for the products;
pilot on settings. The pattern carries to every PWA rig.

- A new setting is listed by hand three times: `SETTINGS` (src/settings/registry.ts:420, a
  215-line array), `CONTROLS` (src/settings/controls/index.ts:29) and the rows
  (src/settings/rows.ts), though each setting already has a file of its own under
  `src/settings/definitions/`.
- A new screen edits the `Route` union (src/nav/route.ts:18), `ROUTE_TABLE` (route.ts:20), the
  chain in `routeOf` and the chain in `src/App.tsx`.
- A new repository adds its names to the barrel (src/data/repositories/index.ts, 113 commits in
  90 days), which 157 source files and 213 test files import.
- By mw's own count these make small features big before a line is written: route.ts has 106
  test callers and rows.ts 54, over the XL allowance of 50.
- The rig already gathers files by glob where it chose to (src/maps/places.ts:5,
  src/hardSounds/lessons.ts:11), so a setting or a screen that registers itself from its own
  file is a known move here.

**B11. The big screens.** Verdict: lift when next touched.

- `ReaderBody` is one component of about 760 lines (src/Reader.tsx:164-922) with 19 `useState`
  and 18 `useEffect` (helper's count). `src/Talk.tsx` holds seven offer components (113-410)
  and `Turn` (414-537) that take props only and belong in `src/talk/`, which already has 29
  files. `src/IdeaSheet.tsx:195-460` and `src/WordSheet.tsx:315-547` are stateless blocks.
- These are leaves: only `src/App.tsx` imports Reader. Their cost is size, not fan-in.

**B12. Layer inversions.** Verdict: the same tightening rule as B8.

- Data imports a type from a screen (src/data/answerWord.ts:9 and src/nav/links.ts:8, from
  `../WordSheet`), data imports settings (src/data/chapter.ts:11-12), the shared ui imports a
  feature (src/ui/ShareSheet.tsx:6 imports `../AskTutor`), and a service imports from talk,
  tutor and reader (src/services/talk.ts:20-44).

### Across rigs

**B13. Kit code copied instead of imported.** Verdict: cut now; each is a deletion.

- Postern's `src/ui/useAlive.ts` (19 lines) is pwa-kit's `packages/alive/src/index.ts` (18
  lines), and Postern already depends on pwa-kit.
- Postern lists bsv-kit (package.json:27) and imports it in no file; 44 of its files still
  import the older `spell-forge-bsv` package (package.json:36). That is a move left half done:
  finish it or drop the unused line.
- By a helper's reading Postern's `src/services/push.ts` repeats pwa-kit's push package, and
  Lampas's `src/ui/tapGuard.ts` pwa-kit's tap; neither was compared line by line.

**B14. The kits are pinned by hand and lag.** Verdict: cut now; without it a fix made once in a
kit does not reach the rigs.

| Kit | Pins across rigs | Furthest behind the kit's head |
|-|-|-|
| pwa-kit | 5 commits in 5 rigs | legend 20, postern 16, spell-forge 11, lampas 9, trade-tracker 1 |
| bsv-kit | 5 commits in 6 rigs | postern 31, night-stack 25, lampas and spell-forge 6 |
| voice-kit | 2 commits in 7 rigs | postern 78, the other six 32 |

**B15. The PWA shell, copied rig to rig and in no kit.** Verdict: a later investment, after
B14 makes kit lag visible.

- Identical in Lampas and Postern: `src/precacheGuard.ts`, `src/ui/focus.ts`.
- Drifted copies (changed lines, Lampas against Postern): `src/sw.ts` 304, `pwa-manifest.ts`
  49, `src/ui/visibleInterval.ts` 38, `build-version.ts` 20, `pwa-precache.ts` 17,
  `src/ui/scrollGuard.ts` 13, `src/markdown/Markdown.tsx` 136.
- By a helper's count the vite, vitest and playwright configs differ by 40 to 150 lines across
  all seven PWA rigs.

## Deletions

Nothing in the factory looks for code to remove, and nothing counts a removal: measure 8 is the
first number for it. Each candidate below names its evidence. "Safe" means a check proves
nothing reaches it; "ask first" means only the Governor or the Mayor knows whether it is kept
on purpose.

**D1. Millwright: 84 functions nothing reaches. Safe.** Reachability from `main` and from
every test (the method of `golang.org/x/tools/cmd/deadcode -test`) finds:

- 70 methods of the test fakes in `application/apptest`, 460 lines, for example
  `FakeShipArchive.Keep` (fakeeventlog.go:267), `FakeCloseOutMarks.Read` (fakecloseouts.go:43)
  and 8 in fakerigfiles.go.
- 13 production functions: `CheckGateHeldFileName` (application/checkgatelock.go:45),
  `Check.takeSlotWithin` (application/checkparts.go:394), `VaultFilesDispatchDoesNotRead`
  (application/dispatchblocked.go:94), `HomeElsewhereIn` (application/home.go:56),
  `MillhandTickReport.String` (application/millhandtick.go:91),
  `PosternInbox.recordThreadComment` (application/postern.go:1540), `TalkModelReport.String`
  (application/talkmodel.go:100), `ControlWords` (domain/events/control.go:30), `WithShell`
  (infrastructure/claude/claude.go:313 and infrastructure/codex/codex.go:63), `WithAfterShell`
  (infrastructure/rig/afterlanding.go:126), `WithCheckShell` (infrastructure/rig/checks.go:102)
  and `WithRemote` (infrastructure/rig/worktree.go:77).
- One test helper, `perStory` (application/dispatch_reads_test.go:193).
- A further 58 production functions (238 lines) are reached only by tests; most are the
  `With...` options the tests are built on, and they stay.

That is about 510 lines of 194,000: millwright has almost no dead code in this sense. Its
deletions are in D2 to D4.

**D2. Millwright: six commands no session ran. Ask first.** In the Bash calls of 4,530
transcripts (2026-10-14 to 2026-11-12), and in no seat file, script, formula or timer, a helper
found no run of `mw ask by` and `mw ask of` (cmd/mw/ask.go:28-29), `mw ask waiting`
(ask.go:58), `mw grist request decline` (cmd/mw/grist.go:258), `mw grist request adopt`
(grist.go:294) or `mw prompt show` (cmd/mw/prompt.go:100). They may be meant for the Governor's
own hand, which transcripts do not see; story 4 below would settle it.

**D3. Millwright: the codex harness. Ask first.** `infrastructure/codex` is 330 lines, wired
at cmd/mw/seat.go:156, cmd/mw/sync.go:205 and cmd/mw/version.go:121. Every one of the 4,208
stories that name a harness in the beads dump names claude. It may be kept as the way out when
Claude is not to be had.

**D4. Millwright: 35 settings never set. Ask first.** Of the 50 root keys
(domain/configtoml.go:280), 35 are set in no config file on this host or in the vault (helper's
count), `giveway_tmux_sockets`, `tick_recheck_seconds` and `dispatch_sync_tries` among them.
Each is a getter, a test and a paragraph of README. A knob nobody has turned in eight weeks is
a candidate to become a constant.

**D5. Postern: small and safe.**

- 8 exports nothing names: `storedBeadDetail` (src/services/beads.ts:38), `SESSION_MINUTES`
  (src/services/keySession.ts:18), `mayorKeyState` (src/services/me.ts:109), `API_BASE`
  (src/services/messages.ts:19), `isHomeHost` (src/services/standby.ts:12), `threadHref`
  (src/services/threads.ts:190), `TodoNotice` (src/todo/edits.ts:178), `EventKind`
  (src/model/events.ts:23). Each has one hit in `git grep -w` over src, tests, features,
  scripts and server: its own declaration.
- Two barrels no source file imports: `src/push/index.ts`, and `src/licence/index.ts` (one
  test imports it).
- The copy of pwa-kit's `useAlive` and the half-done move to bsv-kit, both in B13.
- Ask first: eight backend routes with no caller found in either checkout, the four under
  `/api/register/` (server/internal/api/handlers.go:260-269) and four under `/api/alerts`
  (handlers.go:246, licencemap.go:34). `docs/api.md` says the alerts routes serve other apps'
  keys.

**D6. Lampas: small and safe, and five to ask about.**

- 23 exports nothing names, each with one hit in `git grep -w` over src, tests, features and
  scripts. Among them `useEvent` (src/events/bus.ts:78), `noteGreekNo` and `clearGreekNo`
  (src/data/repositories/memory.ts:203 and 214), `isRunning` and `resumeOf`
  (src/pending/pendingAsks.ts:123 and 138), `pastedPictures` (src/talk/pictures.ts:54),
  `talkOpenNow` (src/tutor/door.ts:122), `lastAnswer` (src/services/clientTiming.ts:55),
  `GRAMMAR_MOVES` (src/data/grammar/move.ts:15), `ownCells`
  (src/data/paradigms/ownForms.ts:264), `canCurl` (src/reader/pageCurl.ts:136).
- A component nothing draws: `src/tutor/ReportScreen.tsx` (9 lines; every screen calls the hook).
- A record nothing reads: `lampas.lastError` in localStorage, written at src/reportError.ts:5-16.
- Ask first, five files only tests or feature steps import: `src/tutor/knows.ts` (223 lines),
  `src/approaches/bma-tutor.ts` (158), `src/services/fillers.ts` (34), `src/gate/Gate.tsx`
  (16), `src/script/depthSettings.ts` (9).
- A further 98 exports in Lampas and 29 in Postern are named only by tests; most are reset
  hooks for tests and stay.

**D7. What would keep removal going.** A check in each gate that fails on unreachable code (the
`deadcode` run for Go; an unused-export and unused-file check for the PWAs), so the list above
stays empty once cleared; the removed share printed with the report, so a removal is seen; and
a "Removed:" form of the closing comment's What's new line, so he reads of it.

## Usage

### What we can tell today

**Millwright.** `mw` keeps no record of its own runs: the root command has no hook
(cmd/mw/root.go) and `main` only maps the error to an exit status (cmd/mw/main.go:15). What
exists is indirect:

- The session transcripts on this host (4,530 files, 2026-10-14 to 2026-11-12) hold every Bash
  call a session made. A helper's count of those calls: `mw check` 46,776, `mw bd` 16,960,
  `mw events wait` 13,949, `mw path set` 11,459, `mw talk wait` 10,793, `mw postern send`
  10,711, `mw events tail` 10,245, `mw note` 9,668, `mw mail inbox` 8,427, `mw status` 7,982.
  Of 193 leaf commands, the six of D2 were never run.
- By a helper's reading of the unit files, eight timers and two services run `mw dispatch`,
  `mw doctor`, `mw millhand tick`, `mw postern mirror`, `mw usage read`, `mw report send`,
  `mw needs sweep`, `mw verify match`, `mw events follow` and the mail-notify script.
- What this misses: anything the Governor types himself, anything run through a script or a
  timer, and every host but this one. It also counts a resumed session's calls twice.

**Lampas.** It already counts, on the device and nowhere else:

- `startUsageLog` (src/tips/usageLog.ts:31, started at src/App.tsx:85) adds one to a per-day
  row for each kind of event on the app's bus and for each screen opened, keeping only the kind
  or the route, never what was read or typed. The rows live in the IndexedDB store `usage`
  (src/data/db.ts:1339).
- `src/tips/summary.ts:77-81` already works out the screens, features and settings never used.
- No screen shows any of it. It leaves the device in one case only: inside a Tips request to
  his own Postern backend, when the Tips setting is on (src/tips/offer.ts:58-72, by a helper's
  reading).

**Postern.** The PWA records no screen view or tap (a helper's grep for analytics, telemetry,
usage and the like found none). The backend writes one line per request to its own standard
error: `accepted METHOD path why=...` (server/internal/api/handlers.go:371), `posted ...`
(handlers.go:478) and `refused ...` (handlers.go:521), with no body. So which routes are called,
how often and by which key prefix can already be counted on his own host from lines it keeps.

### The cheapest way to know more, with nothing leaving his devices

1. **Millwright: one line per run.** At cmd/mw/main.go:15, append the command path, exit
   status and duration (no arguments, which can hold private text) to a file under
   `~/.local/state/mw/`. A line of the report then lists the commands not run in 30 days. This
   is story 4; it turns D2 from a guess into a fact within a month.
2. **Lampas: show the counts it has.** A page that lists what was opened and what never was,
   from the `usage` store, with a Copy button. He reads it on the phone and pastes it into
   Postern if he wants the Mayor to see it. This is story 8.
3. **Postern: count the lines it already writes,** then take the same counter Lampas has. A
   script over the backend's own lines gives routes per day and routes never called (it settles
   D5's eight). Lifting `usageLog.ts` and `repositories/usage.ts` into pwa-kit gives Postern,
   and every later PWA, the same page as Lampas for the cost of one import.

The rule worth writing down with them: usage is counted where it happens, kept there, shown to
him there, and moved only by his hand.

## Top ten stories

Ranked by bang for buck. Size is by test callers, the count `mw path set` prints for the files
the story would name (XS 5, S 10, M 20, L 35, XL 50); where the count is over the line but no
caller changes, the story carries `callers_ok` and the size is the work's. The first eight are
cheap wins; the last two are big investments and go to the Governor as one card.

| # | Story | Rig | Size | Kind |
|-|-|-|-|-|
| 1 | mw report prints SHAPE | millwright | M | cheap win |
| 2 | Split status.go along its own seam; hold file length | millwright | S, callers_ok | cheap win |
| 3 | Unreachable code fails the gate, after today's 84 are removed | millwright | S, callers_ok | cheap win |
| 4 | A line for every mw run; the report names what never runs | millwright | S | cheap win |
| 5 | Postern's contract in one file, tested from three sides | postern, millwright | S and S | cheap win |
| 6 | Layer rules that can only tighten | postern, lampas | S and S | cheap win |
| 7 | Kit lag on mw doctor; Postern drops its copies of kit code | millwright, postern | S and S | cheap win |
| 8 | Lampas shows what it has counted | lampas | M | cheap win |
| 9 | Lampas: a setting, then a screen, registers itself | lampas | L, in stages | big investment |
| 10 | Millwright: close-out checks as a list, one file each | millwright | XL, in stages | big investment |

### Cheap wins

**1. mw report prints SHAPE: the hot files, the files over the allowance and what was removed.**

- Why: measures 7 and 8 exist nowhere today, and every other story here is judged by them. It
  also gives the Mayor the hot list when he sizes a story, and makes a removal visible.
- Rig: millwright. Size: M (application/factoryreport.go has 17 test callers; the rest are new
  files).
- Draft acceptance: `mw report --rig millwright --since 30d` prints a block headed SHAPE with
  the five hottest production files (commits times lines) and each one's test callers, a line
  "hot files over the allowance N of M", and a line "lines added A, removed R, code files
  deleted K"; `mw report --json` carries the same under `shape`; `go test ./application -run
  TestFactoryReportShape` passes against a fixture checkout. Expect: none.

**2. Split application/status.go along its own seam, and hold file length from then on.**

- Why: the cheapest cut to a hub. The rendering (status.go:2331 to about 3227) takes only a
  `StatusReport`; moving it changes no caller and no behaviour. The same move then applies to
  next.go, dispatch.go and config.go (B1), one story each, once this one has shown no harm.
- Rig: millwright. Size: S of work; 65 test callers, so `callers_ok` ("moves inside one
  package; no declared name changes"). Land it while no other story holds status.go.
- Draft acceptance: `wc -l application/status.go` prints under 2,500; `git diff main --stat`
  names only `application/status*.go`, the new script and its test; `make test` and `make lint`
  pass; `scripts/check-file-length.sh` passes, and its test shows it fail for a non-test Go
  file over 2,500 lines that is not on its list, and for a list that grew. Expect:
  tokens/landing down.

**3. Unreachable code fails the gate, after today's 84 functions are removed.**

- Why: the first removal the factory can celebrate (about 510 lines, D1), and the check that
  keeps the list empty, so removal stops being a campaign.
- Rig: millwright. Size: S of work; application/apptest/faketracker.go alone has 280 test
  callers, so `callers_ok` ("each removed function is unreachable from main and from every
  test, by the check this story adds").
- Draft acceptance: `go run golang.org/x/tools/cmd/deadcode -test -tags beads_integration ./...`
  prints nothing and `make lint` runs it; `make test` passes; `git diff main --shortstat` shows
  more lines removed than added.

**4. A line for every mw run, kept on the host; the report names what never runs.**

- Why: the factory's own usage, for a dozen lines of code (Usage, step 1). It settles D2 and
  shows which of 193 commands earn their keep.
- Rig: millwright. Size: S (cmd/mw/main.go has no test callers; the writer is a new adapter).
- Draft acceptance: after `mw version`, the last line of `~/.local/state/mw/commands.jsonl` is
  JSON with `"command":"mw version"`, an exit status and a duration, and no arguments; `go test
  ./cmd/mw -run TestEveryRunIsRecorded` passes, with a case showing an argument is not kept;
  SHAPE prints "not run in 30 days: N" and the names.

**5. Postern's contract in one file, tested from three sides.**

- Why: B7. Three codebases keep the same lists by hand; three have drifted already, and a
  drifted list fails silently.
- Rig: postern, then millwright. Size: S (postern src/model/cards.ts has 8 test callers, the
  tests and the file are new) and S (millwright: a test, a copied file and a script, all new).
- Draft acceptance, postern: `docs/fixtures/contract.json` lists the message classes, event
  kinds, expect states, need kinds and action words; `npx vitest run
  tests/unit/contract.test.ts` and `go test ./server/internal/record -run TestContract` pass,
  and each fails when a value is added on its side only. Millwright: `go test ./... -run
  TestPosternContract` passes against its copy; `scripts/check-postern-fixtures.sh ~/postern`
  exits 0 when the copies match postern's and names the file and line when they do not (today:
  protocol-vectors.json, line 30).

**6. Layer rules that can only tighten, for Postern and Lampas.**

- Why: B8 and B12. Both rigs wrote their layers down and neither holds them. Listing today's
  inversions and refusing new ones costs a day and stops the decay.
- Rig: postern and lampas, one story each. Size: S (eslint.config.js has no test callers; the
  rule and its list are new files).
- Draft acceptance: `npx eslint src` passes; the rule's own test shows an import from
  `src/model` into `src/cockpit` (Postern) or from `src/data` into a screen (Lampas) refused;
  a test fails when the list of allowed inversions is longer than the count this story
  recorded.

**7. Kit lag on mw doctor; Postern drops its copies of kit code.**

- Why: B13 and B14. A kit is a boundary only if its fixes reach the rigs; today Postern is 78
  commits behind on voice-kit and nothing says so.
- Rig: millwright (the line), then postern (the removal). Size: S (a new probe; cmd/mw/doctor.go
  has no test callers) and S (postern src/ui/useAlive.ts has 1).
- Draft acceptance, millwright: `mw doctor` prints one line for each rig and kit that is
  behind, as "postern: voice-kit 78 behind", and `go test ./infrastructure/doctor -run
  TestKitLag` passes against two fixture checkouts. Postern: `test ! -e src/ui/useAlive.ts`;
  `git grep -c "spell-forge-bsv\|bsv-kit" package.json` shows one of the two packages, not
  both; the gate passes.

**8. Lampas shows what it has counted.**

- Why: Usage, step 2. The counts and the never-used lists exist; this is the page.
- Rig: lampas. Size: M if it is a section of an existing screen (src/SettingsScreen.tsx has 15
  test callers, src/tips/summary.ts 19). A route or a settings row of its own would name
  route.ts (106) or rows.ts (54) and be over XL by the count, which is story 9's point.
- Draft acceptance: a Playwright spec opens two screens, then the page, and finds both under
  what was opened and a third under "never opened"; a unit test shows the page reads only the
  `usage` store and makes no request.

### Big investments

**9. Lampas: a setting, then a screen, registers itself.**

- Why: B10. Lampas lands 41 stories a day at 7.0M tokens each, the dearest of the three, and a
  size M story that touches a hot file there costs 12.7M against 7.3M. Its hottest files after
  the Reader are lists every feature appends to. The same move then serves every PWA rig.
- Rig: lampas. Size: L, in stages. Settings first: registry.ts (64 test callers), rows.ts (54)
  and controls/index.ts (7) are over XL together, so the first story moves the callers onto
  one helper, as mw's own warning asks; then routes; the repositories barrel is frozen by a
  lint rule and emptied later.
- Draft acceptance (settings stage): a unit test puts a definition file in a fixture directory
  and finds it in `SETTINGS` and `CONTROLS` with no edit to registry.ts or controls/index.ts;
  the gate passes with the settings screen's order unchanged (its existing specs); 30 days on,
  SHAPE no longer lists registry.ts among Lampas's hot files.

**10. Millwright: close-out checks as a list, one file each.**

- Why: B2. `next.go` is the hottest file in the factory (170 commits, 149 test callers, a
  73-field struct built at three sites), and it is the landing path of every rig, so its cost
  is paid by all of them. A check that is a small type with its own ports is added without
  opening the hub.
- Rig: millwright. Size: XL by the count, so in stages, each behind unchanged features: first
  the mechanical split of next.go (as story 2); then the refusal checks
  (application/next.go:1974-2620) behind one interface; then the after-landing steps. The same
  shape then fits `Status`'s sections and `Dispatch`'s gates.
- Draft acceptance (refusal stage): `go test ./application -run
  TestACloseOutCheckIsAddedWithoutEditingNext` passes; the field count of `Next` (an awk count
  in the test) falls from 73 to under 50; `features/next*.feature` are unchanged and green;
  `make test` and `make lint` pass. Expect: tokens/landing down.
- The risk that makes it big: a mistake here stops every landing. Each stage should land
  alone, with the factory watched for an hour after.

### Next in line, not in the ten

- The PWA shell into pwa-kit (B15) and the usage counter into pwa-kit (Usage, step 3), after
  story 7 makes kit lag visible.
- An unused-export and unused-file check in the Postern and Lampas gates, with D5 and D6
  cleared first.
- Read the config file once per run (B4), if SHAPE still lists config.go after its split.
- One base for the step definitions' repeated helpers (B6).
- The four "ask first" lists (D2, D3, D4 and the routes and files of D5 and D6), as one card.

## Method and limits

- **Hotspots, shape and removal.** `git log --since="90 days ago" --no-merges --numstat` per
  checkout, renames followed to the new name; code is `.go`, `.ts`, `.tsx`, `.js`, `.mjs`,
  `.py` and `.sh` outside `public/`, `data/`, `dist/` and `node_modules/`; a test is a
  `_test.go` file, anything under `tests/` or `features/`, and `application/apptest`.
- **Test callers.** A scratch program outside the rig called `rig.Files{}.TestCallers` for one
  file at a time, so each figure is what `mw path set` would print for a story naming that file
  alone.
- **Scorecard 1 to 6.** `mw report --rig <rig> --since 30d` (it reads and writes nothing), the
  batch rows of `metrics/factory.jsonl` since 2026-10-14, and the beads dump of 2026-11-12.
- **The cost table.** Merge commits on main in 30 days (486, 53 and 1,148), each matched by its
  branch name to the story's ledger rows and to its size in the dump. Fast-forward landings
  have no merge commit and are not in it.
- **Unreachable code.** For Go, rapid type analysis from every `main` and every test, as
  `deadcode -test` does, written against the copy of golang.org/x/tools already in the module
  cache; a function is listed only if no build of it is reached. For TypeScript, a script
  resolved every relative import and every `import.meta.glob`, and looked for each exported
  name as a word in every other file; a name shared with another symbol hides a dead one, so
  those lists are a floor. Every name printed in D5 and D6 was checked again with `git grep -w`.
- **Helpers.** Four read-only search helpers read the three checkouts, the kits and the
  transcripts. Where a figure is theirs and was not recounted, the text says "helper's count" or
  "helper's reading"; the line numbers quoted from them were opened and checked for the
  millwright structs and interfaces, the Postern contract, the Lampas registries and the usage
  code. The transcript counts are one helper's single pass and were not repeated.
- **Not done.** No test, build or gate of Postern or Lampas was run. The other rigs
  (spell-forge, trade-tracker, legend and the rest) were read only for kit pins and copied
  files. Nothing here measures the Governor's own time.
