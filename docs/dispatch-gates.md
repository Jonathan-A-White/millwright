# Dispatch gates

Every reason `mw dispatch` leaves a ready story unclaimed, in the order a story
meets them. A story passed over stays open and unclaimed (`mw dispatch --dry-run`
prints the reason beside it); none of these spends one of its attempts. The list
is `application.DispatchGates`; each gate is marked `// gate: <name>` in
`Dispatch.run`, and a test holds the three together. A new gate is added to all
three.

| Gate | Holds the story when |
|-|-|
| `paused` | the host is paused by a pause-host event: nothing at all is done |
| `hitl` | the story is labelled hitl, or demo with no files key (his own demo): the Governor must be present, the Mayor works it |
| `parent` | the bead has child beads that are not closed: a parent is never a session's to take, its children are the work (mw-gq6.1086) |
| `held-for` | the story carries a `held-for-<event>` label (say `held-for-jonah`): it is parked behind an event and is not claimed, however open or ready its status, until the label is removed (mw-gq6.753) |
| `no-path` | the story's path cannot be read |
| `no-host` | the path names no host |
| `other-host` | the path names another host |
| `harness-hold` | the harness stopped a session here (usage limit, rate limit, overloaded or failing API) and the time it named has not come (below) |
| `no-room` | the host has no room: load, or a grist tutor in use |
| `queued` | its work is done and it is labelled `run:queued` for its rig's batch, yet it is open (a hold and un-hold, or an open-steps give-back, left it so): `mw land` takes it, a new attempt would redo done work (mw-bo813m2) |
| `landed` | its last run landed it and only the close was lost |
| `blocked` | it waits on a story that is not closed |
| `rig-not-checked-out` | the rig is not under `[rigs]` in the config file |
| `smoke-hold` | the rig's last grist smoke failed and the story does not name a failing example in its smoke_examples, title or description |
| `metered-network` | the rig installs dependencies and the network is metered |
| `no-grist-key` | the story's acceptance or description runs `mw grist eval` or `mw grist smoke`, or its path carries `smoke_examples`, or its files key names a path or glob under `grinds/`, and the host has no readable grist test key (`grist_test_key_file`); it waits for a host that has one (mw-gq6.741, mw-gq6.779) |
| `formula-not-installed` | the path's formula is not installed in the vault |
| `no-sweep` | a bug filed after the sweep rule has no line beginning `Sweep:` in its acceptance (below) |
| `tester-no-landed-story` | a Tester story's description has no `Landed story: <id>` line: its session could not file a finding (mw-gq6.594) |
| `attempts-exhausted` | it was started `max_attempts` times already |
| `handed-back` | its Builder handed it back (a comment opening `HANDED BACK`) and no later comment opening `RE-PATHED:` says what changed (below) |
| `needs-files-key` | it names no files key and its rig's file says `files_expected = true`: `mw path set <id> files=<csv>` ends it, and it holds no other story back (mw-gq6.599); the first pass to meet it comments on the story and mails the Mayor that command, once, and `mw status` DAMPERS names it from the moment it is open (mw-gq6.795, application/pathnotice.go) |
| `shared-files` | it names a file a running story of the rig names or has changed; since when is kept in the note `host.<host>.files_held`. A files key naming a directory or a glob over more than 20 files is refused by `mw path set` and `mw file` (globs are counted too, mw-gq6.633), so none is held behind a crowd |
| `testers-per-rig` | it is a Tester story and its rig runs `testers_per_rig` (the `[dampers]` key, default 1) Tester stories already, on any host |
| `drained` | the host's cap is 0, a host drained for a restart: the pass still syncs and closes out, logs `ok: capped at 0`, and starts nothing; every ready story it would take is passed over under this gate, not `cap` (mw-gq6.1044) |
| `cap` | the host runs as many sessions as it may |

A refused story's kept claim (`run=blocked`) holds its files at `shared-files` until a retry or a hold, so a ready story that names one waits for as long as the refusal does, and a refused story whose `cause_fix` is that story waits on it in a cycle. `mw status` says so (READY, OTHER HOSTS, and a DAMPERS line for a cycle or a wait over an hour, mw-gq6.941); the cure is `mw retry <id> --hold`, run on the host that holds the claim.

<!-- toc -->

- [The sweep gate](#the-sweep-gate)
- [The bug recheck](#the-bug-recheck)
- [The handed-back gate](#the-handed-back-gate)
- [The held-for gate](#the-held-for-gate)
- [The harness-hold gate](#the-harness-hold-gate)
- [The smoke-hold gate, and the gates that cannot hold their own fix](#the-smoke-hold-gate-and-the-gates-that-cannot-hold-their-own-fix)
- [The starved-rig alarm](#the-starved-rig-alarm)
- [Who runs the rig's tests, and under which lock](#who-runs-the-rigs-tests-and-under-which-lock)

<!-- /toc -->

## The sweep gate

When a bug is seen, the story looks for the whole class, not only the case seen.
A story of type bug, titled `[bug]` or labelled `bug`, filed on or after
2026-10-15 (`domain.SweepRuleFrom`) on any rig is not dispatched unless a line of
its acceptance criteria begins `Sweep:` (any case, after an optional number or
bullet) and says what is checked over the whole class. The escape for a true
one-off is `Sweep: none, because <reason>`; `Sweep: none` with no reason does not
pass. Older bugs are not asked.

The first time a dispatch passes a bug over for it, it comments on the bead and
mails the Mayor, once per bead. `mw status` lists such bugs under
BUGS WITHOUT A SWEEP, and nothing when there are none. `mw file` refuses a plan
with such a bug in it, and `mw tester file` such a bug finding, with the same
words, before anything is filed (mw-gq6.447).

## The bug recheck

Before a bug is claimed, a dispatch with a free slot re-runs the check the bug
names (a suite spec, a `Check:` line, a `mw grist eval ... --example` line, a spec
in the title) on its rig's main, and closes the bug with no session when it
passes. It does not recheck a bug about a flake: one whose title or description
says flake, flakes, flaked, flaking or flaky, whose title or description or
acceptance asks `N of M` or `in a row`, or a flaky file's bug (` is flaky on
main`). One pass proves nothing about those, and a session takes them instead
(mw-gq6.784).

## The handed-back gate

A story the Builder handed back (its newest hand-back comment opens
`HANDED BACK`) is not dispatched, and `mw retry` refuses it, until a comment
opening `RE-PATHED:` follows: the same Builder given the same story hands it back
the same way. `mw retry <id> --repathed "<what changed>"` writes that comment and
retries. `mw status` lists such stories under HANDED BACK with the date of the
hand-back.

## The held-for gate

A story labelled `held-for-<event>` (`held-for-jonah`, or the id of the epic or demo
whose close ends the event) is parked: it is not dispatched however its status reads.
The label is the hold, not the status, because a turn-cap hand-back reopens a story
at once and the next tick, a dozen seconds later, would take it again before the
Mayor had parked it (mw-gq6.753). Whoever ends the event removes the label. The gate
stops a new claim only: a session already running when the label is put on runs on.
The starved-rig alarm does not count these stories.

## The harness-hold gate

A session the harness stopped for something that is not the work's is held by
its close-out under its own reason (`usage-limit`, `rate-limit`, `overloaded`,
`server-error`, `api-unreachable`, `login-expired`), the claim kept. Until the time the stop names (the reset a usage
limit names, or a quarter hour after the session ended for the rest) the host
claims nothing new; once it has come, a dispatch gives the stopped story back
itself, its stopped attempt not counted, and it is started again. `mw retry` of
such a story does not count the attempt either, and keeps the hold. The hold is
kept in the host's note `host.<host>.harness_hold`.

An expired Claude login (`Failed to authenticate: OAuth session expired and could
not be refreshed`) holds the host for `LoginExpiredWait` (12 hours): the close-out
writes the hold note itself and mails the Mayor once per host while that note
stands, not per story, and `mw status` names the host as held for the login with
the one command that clears it (`bd kv set host.<host>.harness_hold ""`) once the
Governor has logged in again. A dispatch that finds the note cleared gives the
stopped story back at once, its attempt not counted (mw-gq6.781).

A usage limit can stop the harness before it writes its JSON: the close-out
then reads the limit from what the harness printed in its place, or from the
session's pane, and keeps the stop it read in the run (`harness-stop.txt`,
numbered by attempt as the result is) for dispatch, `mw retry` and `mw status`.
All of them read the limit's notice with the one reader, `HarnessStopOf` and
`HarnessStopOfWords` (mw-gq6.471).

## The smoke-hold gate, and the gates that cannot hold their own fix

A failed grist smoke holds the open stories of its rig, except the fix: a story
whose `smoke_examples` metadata, title or description names a failing example (as
`<app>/<kind>/<example>`, `<kind>/<example>` or the bare example name standing on its own;
`application.NamesSmokeExample`, the same reading the smoke's "already named" match uses,
mw-gq6.773) runs over the hold, and `mw dispatch` says so (`runs over
the grist smoke hold of <rig>: ...`). `mw grist smoke <app> --filed <bead>` with
no example names the examples the last smoke failed on, and `mw status` names the
beads that run over a hold (mw-gq6.579). Closing the bead ends it.

The other gates cannot hold a fix behind the very failure it is to mend, each
for its own reason: `shared-files` and `testers-per-rig` wait only for stories
already running, which are past every gate and end on their own; `harness-hold`,
`no-room` and `paused` wait for the host, not for a story; `no-sweep`,
`tester-no-landed-story`, `handed-back` and `attempts-exhausted` read the story's own bead; `blocked` waits
on a dependency a person filed. The false-hold guard is `mw release`'s, not
dispatch's, and no smoke touches it.

## The starved-rig alarm

When a pass leaves a rig with ready stories passed over (the Governor's own and
the `held-for` stories excepted) and none running or started, the host's dispatch notes it in
`host.<host>.starved`: the rig, when it was first so held, the hold and why. A
rig held that way for over an hour (`application.StarvedAfter`) with a story
ready and none running is an alarm: `mw doctor` (check `starved-rig`) mails the
Mayor once per episode, and `mw status` prints the same line first under DAMPERS.
It clears when a story of the rig starts. The hold is named from the gate that
passed most of the rig's stories over, by `application.StarvedHolds`; a new gate
is mapped there too, and a test says so.

| Gate | Hold name |
|-|-|
| `hitl` | Governor's own story |
| `parent` | parent with open children (not counted in the alarm, as `hitl` and `held-for` are not) |
| `held-for` | held for an event |
| `no-path` | unreadable path |
| `no-host` | no host |
| `other-host` | another host (asleep?) |
| `harness-hold` | harness stop |
| `no-room` | no room |
| `queued` | waits for its batch |
| `landed` | lost close |
| `blocked` | blocked |
| `rig-not-checked-out` | rig not checked out |
| `smoke-hold` | smoke hold |
| `metered-network` | metered network |
| `no-grist-key` | no grist key |
| `formula-not-installed` | formula not installed |
| `no-sweep` | no sweep |
| `tester-no-landed-story` | tester names no landed story |
| `attempts-exhausted` | attempts exhausted |
| `handed-back` | handed back |
| `needs-files-key` | needs files key |
| `shared-files` | files overlap |
| `testers-per-rig` | damper |
| `drained` | the host's cap is 0, a host drained for a restart: the pass still syncs and closes out, logs `ok: capped at 0`, and starts nothing; every ready story it would take is passed over under this gate, not `cap` (mw-gq6.1044) |
| `cap` | cap |
| `pour-refused` | pour refused |
| `claim-lost` | claim lost |

## Who runs the rig's tests, and under which lock

One gate-weight run goes at a time on a host, whoever asked for it: the host's gate
lock (`application.GateLock`, `hostlock.Gate`, mw-gq6.388). A Builder's whole
`mw check` takes it too (`Check.GateLock`, mw-gq6.906).

The numbers that decided it, from the vault's bench lines (`runs/*/result.json`,
`gate_seconds`, `running_count`) and the Builders' check logs (`runs/*/check-N.log`)
of the 7 days to 2026-11-03, desktop, 43 gates: the 38 gates no Builder's check
finished inside took 419 s on average (median 223 s); the 5 gates a Builder's check
finished inside took 1,472 s (median 1,140 s). A check that finished inside a gate
is a floor of the checks that overlapped one, since a check's log is written when it
ends. By stories running when the gate began, the 3 and 4 running gates average
744 s and 750 s against 203 s for 1 running (mw-gq6.903); the two with a check
inside average 1,746 to 1,890 s. The sample is small; the direction is the same in
every row.

A whole check takes, in this order, the rig's merge slot (shared with the other
checks, alone for a landing) and then the gate lock, for the rig's tests and again
for the changed specs, and gives both back when they end. The order is the
close-out's: `mw next` holds the slot alone while it waits for the lock, so a check
that held the lock and waited for the slot would wait for a close-out that waits for
it. Every wait of a whole check is one deadline, counted from the start of the check:
the slot, the lock and a calm host before the tests, and after them the slot and lock
the changed specs take again and the second wait for a calm host (the run again of
tests that failed while the host was busy). Each gets what the earlier ones left
(mw-gq6.915, mw-gq6.918); a deadline the tests have spent leaves a wait of nothing,
which is one try, not a wait. The tests themselves take as long as they take, and are
not under it.

How long the deadline is depends on whether the call can outlast a Builder's tool
call, which is cut off at 600 seconds with no words (mw-gq6.939); that is the harness's default cap, which
`BASH_MAX_TIMEOUT_MS` raises (see *A rig's longer call* below):

- **Foreground, no `--wait`** (what the formulas tell a Builder to run): the deadline is
  `CheckPartSlotWait`, 9 minutes. A wait that runs out keeps the check's place in the
  queue for `CheckKeepPlace`, 10 minutes, and ends the check in one line with exit 0
  and nothing run: what is still held, by whom and since when, *your place N of M in
  the queue is kept for 10 minutes: run the same command again to go on waiting*
  (`Check.KeepPlace`, `CheckReport.Waiting`). A busy host has no queue, so its line
  says no place is kept. The Builder runs the same command again in a fresh Bash call,
  as many times as the session's time allows, and hands back (a comment naming the
  holder) only when the session's turn cap nears. The gate itself is not under the
  deadline, so a check that won the gate lock after a long wait could start a gate
  the call cannot finish: it is then cut off at 600 seconds with the lock held
  (mw-gq6.940). So when the lock is won and the rig's usual gate on this host (the
  median of the last gates in the vault's bench lines, `GateExpectedFor`) is more than
  what is left of `CheckCallBudget`, 540 seconds from the check's start, the check
  gives the lock back, keeps its place and ends in one line, exit 0: *the gate needs
  about G and only R is left in this call; your place N of M in the queue is kept for
  10 minutes: run the same command again to start it* (`Check.CallBudget`,
  `QueuePlace.Late`); run at once it takes the place up and starts the gate, saying
  *gate expected about G, R left in this call*. A rig with no figure, or a gate longer
  than a whole call, is started as it always was. The same test is made again once the
  wait for a calm host that follows the lock is over (mw-gq6.945), with the time left
  then, since a calm wait of several minutes can eat the fit the lock's win had: a
  gate that no longer fits is not started, the lock is given back, the place is put
  back in the queue (`PlaceKeeper`, `Next.AfterCalm`) and the same line is said. The
  changed specs' run after the gate and the rerun after a failure on a far-over host
  are not under this test: neither has a figure of its own to compare.
- **`--wait` or `--background`**: the deadline is `mw check --wait` (40 minutes by
  default; mw-gq6.938: nine minutes handed a Builder back three times in 45 minutes
  behind two long gates). A wait that outlasts it ends in one line saying the wait
  limit was reached, naming the holder, and that the Builder hands back at once and
  does not run it again (`Check.HandBackAtOnce`): no place is kept. `--background`
  detaches the check with `--wait` 40m (the log is the story's `check-N.log`).
- **`mw check --part N`** is run one call per part to fit the 600 seconds and keeps
  `CheckPartSlotWait` and the kept place whether or not `--wait` is given; its line is
  that part's name, *did not start*, and the same words. A part that passed keeps how
  long it took, by part number and host, in the story's run
  (`runs/<story>/check-<attempt>-part-<n>.took`, `application.PartTime`, read back from
  every story's run by `vault.PartTimes`). A part that needs the merge slot and wins it
  with less of `CheckCallBudget` left than its usual time on this host (the median of
  its last ten passing runs, `GatePartExpectedFor`, `Check.PartExpected`) is not
  started, as a whole check's gate is not: it gives the slot back (`QueuePlace.Late`,
  honoured by `rig.Slots` as by `hostlock.Gate`), keeps its place and ends in one line,
  *part N of M (name) did not start: the merge slot of <rig> is free, but the part needs
  about P and only R is left in this call; your place ... is kept for 10 minutes: run the
  same command again to start it* (an error, like the part's other did-not-start lines,
  with nothing run). A part with no figure yet, one whose time is longer than a whole
  call, or one that needs no merge slot (nothing to wait for, so the whole call is left),
  is started as it always was (mw-gq6.944).

**A rig's longer call** (mw-t5shinh.14). The 600 seconds are the harness's default: Claude Code reads
`BASH_MAX_TIMEOUT_MS` as the cap on a foreground Bash call. A rig's file may say `builder_call = "55m"`
(10m to 55m, `domain.EpicRequirements.BuilderCall`); a story's session on that rig is started with the
variable set to it (`claude.WithBuilderCalls`; a seat's session is not given it). A foreground
`mw check` reads the variable from its own environment (`application.CheckCallLimitsFor`): when it
is above 600000 the call budget (`Check.CallBudget`) is the call less 50 seconds, and the waits
(`Check.PartSlotWait`: slot, gate lock, calm host, a part or the specs still running) may spend it;
unset, empty, not a number or no larger than 600000, every value is the one above (540 s, 9 minutes).
The kept place stays 10 minutes.

A place is kept by the queue itself (`infrastructure/ticketq`): the ticket of a waiter
that asked (`application.QueuePlace`, put on the wait's context) is not removed when
its wait ends at the deadline, but marked `keep <until>`. A ticket whose process is
gone is otherwise cleared by the next walk of the queue; a kept one counts as live
until its keep time passes, for the holder of the lock and for `mw next`'s own walk, so
it stands in front of the tickets taken after it and a close-out behind it waits for it,
for up to 10 minutes. The same story's next call (key `check <story>`) takes it up again:
the same ticket, so the same place, not the back of the queue. A place not taken up is
dropped when its keep time passes. The calm host is not queued for: the lock a check
waited for is held while it waits for a calm host, and given back when that wait runs
out, so the next call queues for the lock again at the back.

While it waits it says every minute who holds the lock now and since when. It is a wait
inside one call, not a loop that spends the Builder's turns. The wait for a calm host
that follows the lock (mw-gq6.819) is taken only once the lock is held, so the load
waited on is not another gate's. `mw check --no-slot` takes neither the slot nor the
lock.

What the Builder does when a foreground check ends in that line is to run the same
command again; a check that reached its `--wait` limit is not run again. When a wait
ended a check by another route (an older mw, `KeepPlace` unset): run `mw check
<story>` again; if the same wait ends it again, comment on the story naming the
holder or the load and hand back. What the run again does with the tests depends on
where the hand-back came:

- the changed specs' slot or lock (after the tests passed at a clean tip): the pass is
  kept in `runs/<story>/check-N-tests.rec` with the commit and the command, so the
  next `mw check` at that commit does not run the tests again and takes neither slot,
  lock nor calm host for them; it goes to the specs, with a fresh deadline. A new
  commit, or uncommitted work, makes the tests run again;
- the second calm wait (tests failed while the host was far over its cores): nothing
  is kept, because the first failure is not counted, and the run again is what the
  next `mw check` makes first, after its own wait for a calm host.

`mw suite` and so the nightly suite (which starts it) takes the gate lock too, for each
command line it runs, and not before its wait for a calm host (`Suite.GateLock`,
mw-gq6.914 and mw-gq6.917). It takes no merge slot: it runs on a throwaway worktree and
touches no target branch. Its order is: the wait for a calm host (`--calm-bound`, up to
2 hours) without the lock; then the lock, held while the worktree is cut and the rig's
tests run, and given back; then the lock again for the changed specs, given back; then
the lock again for each re-run of a failing file. A close-out's gate or a Builder's check
that comes while the suite runs therefore waits behind the one command line in hand, not
behind the suite: the lock is first come first served, so when the line ends the gate
that waited has the lock and the suite's next line waits behind it. A suite waits for the
lock as long as the lock's own cap, `hostlock.GateCap` (3 hours), saying every minute
whom it waits for and since when, and if it cannot take it again it ends with nothing
kept, naming the command lines it had run and who holds the lock: it does not run beside
a gate. Before mw-gq6.914 the suite waited only for a calm host and ran anyway, and the
nightly Lampas suite was among the load sources of the Laptop at 40 to 80 of 20 cores
(handoff 726). The numbers above are its reason: a gate with another run beside it takes
three to four times as long.

The numbers behind the order and the rule, from the vault's `runs/suite/lampas` of the 21
nights 2026-10-13 to 2026-11-02: the whole suite ran 16 to 131 minutes (median 36), from
08:00 to 09:44 UTC. Taking the lock before the calm wait made a close-out wait for that and
for the wait too (up to 2 hours more); 7 of the Laptop's 111 gates of the 7 days to
2026-11-03 fell in 08:00 to 12:00 UTC, and stories are worked there then, so the nightly
suite has the Laptop's gates and Builders to share it with. The wait for a calm host is made without
the lock, which means the load it sees may be a gate's; that is the lesser cost, for a
gate runs 3 to 25 minutes, the suite takes the lock only after it, and the lock is not
the suite's before that gate has ended. The rig's runner takes one shell line, so the
suite cannot yield inside a line: the longest a close-out waits behind a suite is the
longest single line, the rig's tests (its vitest run alone was 9 to 45 minutes in the 10
nights 10/20 to 11/02, inside a line that also installs, type-checks, lints and builds) or
its changed specs (Playwright's own count 9 to 33 minutes), and not the 36 minute median
or 131 minute longest of the whole suite. A Builder's whole `mw check` hands back after
9 minutes with one line naming the holder (`suite lampas`), since when, and the command to
run again; the second hand-back in an attempt says the same and to comment on the story
and hand back if the lock is held a third time. So a line of up to 27 minutes is waited
out within one check's three calls, and a longer one ends with the Builder's comment, not
with turns spent. The result file's `gate_waited_seconds` is the part of `duration_seconds`
the suite spent waiting to take the lock again, and its `lines` are the command lines run, in
order (the tests, each re-run alone, the changed specs, each re-run alone), each with its `command` and
`seconds` run, not counting the wait for the lock: the longest of them is what a close-out
can wait behind, and the figures above that rest on vitest's and Playwright's own counts can
be checked against it from the nights kept after 2026-11-04 (a result kept before has no
`lines`, and is read as one with none). The nightly `mw timing` waits for a calm
host first and then takes the lock in the same way (the suite's lock is given back between
its lines, so timing can run between them), and runs only after the suite's result of the
day is kept when the rig asks for both. `Suite.Recheck` (the bug recheck) takes no lock
itself: its callers hold it (`rerunUnderGate`).

What a re-run alone runs (mw-gq6.924): for a failing file of the `[tests]` line, the one step
of that line that runs unit tests (`npm test`, `pnpm test`, `yarn test`, `vitest`, `jest`,
with any `VAR=value` before it) with the file given to it (`TZ=UTC npm test -- <file>`), in
the tree the whole run installed in, and none of the line's other steps. Before it, the file
was added to the end of the whole line, so it landed on the last step (`npm run build <file>`)
and `npm test` ran every file again: Lampas's suite of 2026-11-03 ran its 828 files four
times, 3.9 of 5.5 hours, and its 'failed alone' was another whole run under load. A rig
whose `[tests]` line has no such step (a make target, a wrapper script) has its failing
file marked `recheck void`, with the reason, run again by nothing and filed as nothing; a
re-run whose output says it ran more than one test file (vitest's `Test Files ... (N)`,
jest's `Test Suites: ... N total`) is `recheck void` too, so that `alone` is said only of a
run that was alone. A failing spec of `[changed_specs]` is run with that command and just
the spec, as before.

`mw check --part N` does not follow the rule. Its parts are named by a host for a rig
whose whole gate outruns a foreground call, each sized to fit one; a part that only
runs tools in the worktree takes no merge slot at all (`PartNeedsSlot`, mw-gq6.814),
so that a landing does not hold it back, and putting it behind a gate that holds the
lock for 9 to 25 minutes would reverse that with no measurement of parts to say it
should: the bench above is of the whole check of an unsplit rig, and the parts' records
keep no times. Measuring parts is reported to the Mayor to file as a follow-up.

A part does wait for a calm host, as a whole check does (`Check.calmForPart`,
mw-gq6.987): before it starts, and before its slot, up to the same nine minutes (what
the wait leaves is what the slot is waited for in), saying every minute that it is
waiting. A host still busy at the end of it hands the part back in one line naming the
load and `mw check <story> --part N`, counted once for the attempt when repeated
(`check-<attempt>-part-<n>-calm.slot`); a part that then no longer fits what is left of
its call is not started either. Before this a part started at any load, and on the
desktop's three or four suites at once (load 25 to 30 of 16 cores) outran the call.

A part that fails while the host is at twice its cores or more (read before it began or
after it ended, `GateFarOver`) is run once more after the same wait for a calm host, as a
whole check's tests are (`Check.runPartAgain`, mw-gq6.1060, after mw-gq6.228): only the
second run counts and is the part's record, and the output says the part ran twice. A
wait that ends with the host still busy, or with too little of the call left, hands the
part back (first failure not counted, nothing kept) with the command to run it again. A
failure read between the cores and twice them counts at once. A part run by its own
process (`PartWorker`) judges by the load after it only.

The nightly `mw timing` run does the same (`NightlyTiming.runRig`, mw-gq6.1063): its tests,
failed with the host at twice its cores or more (read after the run), are run once more after
the wait for a calm host that a part makes (`Next.waitToRunAgain`, the one function both
call), and only the second run counts: a pass files nothing, a second failure files the
second run's failing files, and the result (`ran_twice`) and the output say it ran twice. A
host still far over its cores at the end of the wait skips the night with one line, files
nothing and keeps nothing, and the next night tries again. A failure read between the cores
and twice them is filed at once.

The run of the failing tests on main alone (`Next.mainAlone`, mw-gq6.1064) does the same:
a failure read with the host at twice its cores or more (before the run or after it) waits
for the host to calm (`Next.waitToRunAgain` again) and runs the named tests on main once
more, and the second run's result stands, with a note that it ran twice. A host still far
over its cores at the end of the wait does not call main red: the tests are labelled
"not run" with the load as the reason, and the branch's failure stands as if main were
unknown. A failure on a calm host, or a busy one under twice its cores, is main's at once.

Every place mw runs the rig's tests, and what it takes:

| Where | Merge slot | Gate lock | Follows the rule |
|-|-|-|-|
| `mw next` close-out, gate in the worktree | no (taken after it) | yes | yes |
| `mw next` close-out, gate on the merged result | yes, alone | yes, kept to the end of the landing | yes |
| `mw land` batch gate (and its changed specs) | yes, alone, yielded while it waits for the lock or calm | yes | yes |
| `mw check`, whole, rig not split | yes, shared | yes (after the slot) | yes |
| `mw check`, changed specs | yes, shared | yes | yes |
| `mw check --part N` | only a part that is not slot-free, taken after the calm wait | no | yes, before the part and before its slot, within the same 9 minutes, ending in one line naming the load and `--part N` (mw-gq6.987); a part that failed far over its cores is run once more after the same wait (mw-gq6.1060) |
| `mw check --no-slot` | no | no | no, the escape hatch |
| `mw dispatch` bug recheck and `mw next` fixed-on-main recheck (`rerunUnderGate`) | no | yes, a short wait, else "not re-checked" | yes |
| nightly `mw timing` | no | yes | yes; tests that failed while the host was far over its cores are run once more after the same wait, and the second run counts (mw-gq6.1063) |
| the run of the failing tests on main alone (`mainAlone`) | no | yes, the close-out's own | yes; a failure read far over the cores is run once more after the same wait (mw-gq6.1064) |
| `mw suite` and the nightly suite it is started by | no (it runs on a throwaway worktree of the rig's origin) | yes, taken after the wait for a calm host, for one command line at a time (the tests, the changed specs, each re-run of a failing file) and given back between them; a lock it cannot take ends the suite with nothing kept (`Suite.GateLock`, mw-gq6.914, mw-gq6.917) | yes |
| a landing's after-landing command (deploy, shots) | its own after-landing slot | no | no: it is not the rig's tests; the shots run after the landing's gate has ended |
| `mw grist eval` and `mw grist smoke` | no | no | not the rig's tests: they run the mill, under its own grind slots (`hostlock.GrindSlots`) |
