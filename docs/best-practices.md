# Best practices for an AI software factory

The design lessons of Millwright, written for someone building a factory of their
own: at another company, on other tools, perhaps with none of Millwright's code.
Each practice is a rule in one line, why it holds (the failure it prevents), how
Millwright does it, and what to do if you have less: one host instead of several,
no blockchain, no phone app, a smaller budget.

The words with a fixed meaning (Governor, Seat, Session, Story, Path, Formula, Rig,
Host, Fuel, Bead) are defined in [`CONTEXT.md`](../CONTEXT.md). The Governor is the
one human the factory serves; at a company it may be a team lead or a small group
that speaks with one voice. To stand a factory up, follow
[Setting up a vault](setting-up-a-vault.md); this page is the why behind it.

## Contents

1. [Seats outlive sessions](#1-seats-outlive-sessions)
2. [One story, one fresh session](#2-one-story-one-fresh-session)
3. [What is read at every boot stays tiny](#3-what-is-read-at-every-boot-stays-tiny)
4. [The tracker is the only state](#4-the-tracker-is-the-only-state)
5. [Every story has a path before it starts](#5-every-story-has-a-path-before-it-starts)
6. [One writer per file or table](#6-one-writer-per-file-or-table)
7. [Holds and dependencies are the damper](#7-holds-and-dependencies-are-the-damper)
8. [Every automatic action has a damper and an off switch](#8-every-automatic-action-has-a-damper-and-an-off-switch)
9. [Spend fuel where judgement is](#9-spend-fuel-where-judgement-is)
10. [A deterministic tool for everything clerical](#10-a-deterministic-tool-for-everything-clerical)
11. [The tool carries the baton, not the session](#11-the-tool-carries-the-baton-not-the-session)
12. [An append-only event log, a follower, cursors and lanes](#12-an-append-only-event-log-a-follower-cursors-and-lanes)
13. [Hands steps are written with their way back before they run](#13-hands-steps-are-written-with-their-way-back-before-they-run)
14. [A doctor with probes, cures, dampers and ways back](#14-a-doctor-with-probes-cures-dampers-and-ways-back)
15. [Acceptance is run, not read](#15-acceptance-is-run-not-read)
16. [Verify every landing before the next release](#16-verify-every-landing-before-the-next-release)
17. [The Governor decides; the seat finds facts](#17-the-governor-decides-the-seat-finds-facts)
18. [Hand off early, at a seam, and never two at once](#18-hand-off-early-at-a-seam-and-never-two-at-once)
19. [No blame, and no record is ever rewritten](#19-no-blame-and-no-record-is-ever-rewritten)

---

## 1. Seats outlive sessions

**Rule.** Give every standing responsibility a seat (a charter, a handoff, a ledger)
and treat each AI session in it as a disposable occupant.

**Why.** A session ends: its context fills, it crashes, the plan's limit stops it.
If what it knew lived only in its context, the next one starts from nothing and
relearns the same lessons at full price, or contradicts what the last one promised.
A seat is the office; the session is whoever sits in it today.

**How Millwright does it.** Each seat is a folder in the private vault
(`seats/<seat>/`, [ADR 0003](adr/0003-seats-are-folders-in-the-vault-read-by-need.md)):
`charter.md` (who the seat is, what it may, must and must never do; only the
Governor changes it), `handoffs/` (the newest one primes the next session, in the
shape of `handoffs/TEMPLATE.md`), `ledger.md` (append-only, one line per story) and
the memory the seat keeps of each rig. `mw seat up <seat>` boots a session from
them. The four seats and the cheap Clerks that are not seats are in
[`template/seats/README.md`](../template/seats/README.md).

**If you have less.** Start with one seat, the one you talk to: a charter file
under a page long and a handoff file it rewrites at the end of every sitting. Add a
ledger the first time you wonder what was done last week.

## 2. One story, one fresh session

**Rule.** Each unit of work is sized to fit one fresh context window, and is
worked by a new session that ends when the story does.

**Why.** Price grows faster than linearly with context length, and a long-lived
session drifts: old instructions, stale file contents and half-finished ideas pile
up and get acted on. A small fixed priming cost per story is cheaper than an
unbounded context, and a story that cannot fit is a story that should be split.

**How Millwright does it.** [ADR 0002](adr/0002-one-story-one-fresh-session.md).
`mw dispatch` cuts a worktree and branch per story, pours its formula into step
beads and starts one Builder session for it; the session ends with the story. The
Mayor writes stories to fit comfortably inside a window (aiming well under its
limit) and splits rather than extends.

**If you have less.** Even by hand: open a new session per story, paste in the
charter and the story, and close it when the story is done. Never "continue" a
session into a second story.

## 3. What is read at every boot stays tiny

**Rule.** Split a seat's files by when they are read: always, by need, or never at
boot; and give the always-read ones a byte budget.

**Why.** Every byte loaded at boot is paid on every session for ever. Memories
accrete; a seat document that grows a paragraph per incident ends up costing more
than the work, and its important lines drown.

**How Millwright does it.** The charter is always read; the Builder's memory of a
rig only for that rig, with an 8000-byte budget
([`template/seats/builder/rigs/README.md`](../template/seats/builder/rigs/README.md));
the ledger and postmortems never at boot. The Mayor's procedures sheet stays under
8 KB. A Builder may not edit its rig memory: it proposes at most two typed facts in its
closing comment, and the Mayor places, supersedes and retires them. This repository's own
[`docs/codemap.md`](codemap.md) has a size cap a lint check enforces.

**If you have less.** One rule is enough: a charter that fits on one screen, and a
"lessons" file nobody loads unless asked.

## 4. The tracker is the only state

**Rule.** Everything the factory knows about its work lives in one tracker: the
stories, their order, their claims, their run state, and the notes the tooling
keeps between runs.

**Why.** State scattered across chat history, sessions' memories, local files and a
person's head disagrees with itself, and nothing can tell which copy is right. With
one source of truth, any session on any host can pick up where another stopped, and
"what is next" is a query.

**How Millwright does it.** One beads database for every rig, in the vault
(`bd`). Epics, stories, formula steps, mail between seats and maps are all beads. A
story's run state is a label on it; the tooling's own memory between runs is kept
in beads' key-value notes, not in files on one host. Ports in
[`docs/codemap.md`](codemap.md) (`WorkTracker`, `TrackerNotes`) are the only way
the application reaches it.

**If you have less.** Any tracker with dependencies and a scriptable command line
will do, even one SQLite file. What matters is that there is one, and that a
session asks it rather than remembering.

## 5. Every story has a path before it starts

**Rule.** Before a story can start, it names where and how it is worked: rig,
target branch, harness, model, effort, formula and host.

**Why.** A story that leaves those to the session wastes fuel deciding them, picks
differently each time, and lands on the wrong branch. Deciding them up front makes
the cost and the risk of each story visible when it is approved.

**How Millwright does it.** The Path, set by the Mayor in the plan it files
(`mw file plan.json`; [*Filing a plan*](../README.md#filing-a-plan)). A story with
no rig or no target branch is refused at dispatch. The formula (a beads formula,
see [`docs/formulas.md`](formulas.md)) is poured into step beads when the story
starts, and the session closes each step as it goes.

**If you have less.** A short header at the top of each story, `repo / branch /
model / steps`, filled in by whoever writes the story, is most of the value.

## 6. One writer per file or table

**Rule.** For every file, table or record, exactly one actor writes it (or appends
under a lock), and every write says who made it.

**Why.** Two writers to one thing produce lost updates, merge conflicts and records
nobody can attribute. Two copies of the same seat acting at once contradict each
other in front of the Governor. When a write cannot say who made it, the history
cannot be read afterwards.

**How Millwright does it.**
- `bd` holds a single-writer lock, and the rule for seats is one `bd` call at a time.
- During a spoken talk only the Deputy writes beads; the Mayor reads.
- Two Mayors never act at once: the acting file names the one seat holder, and a
  handover moves it at a stated point in the event log (practice 18).
- The event log is appended under an exclusive file lock and numbered by the home
  alone.
- Ledgers are append-only; two hosts' appends merge by union, never by edit.
- `mw` commits only the vault files it wrote itself, and signs everything as
  `mw@<host>`, never as a seat ([ADR 0005](adr/0005-mw-acts-under-its-own-name.md),
  [*Who mw writes as*](../README.md#who-mw-writes-as)).

**If you have less.** On one host, a lock file around the tracker and a separate
identity for your scripts (a distinct git author and tracker user) cover most of it.

## 7. Holds and dependencies are the damper

**Rule.** Everything is filed held, order is written as dependencies, and nothing
starts until a person releases it.

**Why.** A factory that can start work can start too much of it: a wrong plan run
in parallel burns fuel on stories that all have to be thrown away. Holding by
default makes approval a deliberate act; dependencies let the tooling work out
what may run together without a session deciding.

**How Millwright does it.** `mw file` files an epic and its stories held (deferred)
with their dependencies; `mw release <epic>` (or `--approve` at filing) is the
approval ([*Releasing a plan filed earlier*](../README.md#releasing-a-plan-filed-earlier)).
Serial, parallel and hybrid modes are only dependency shapes. Each host has a
`cap` on sessions at once. On a bad landing the Mayor keeps the rest of its chain
held, and releases further only after verifying (practice 16).

**If you have less.** Keep a "ready" column you move cards into by hand, and never
let more than one story run at a time until you have watched several land.

## 8. Every automatic action has a damper and an off switch

**Rule.** Anything that acts on its own has a minimum wait between tries, a cap on
tries before it gives up and asks a person, and one command that stops it.

**Why.** Automation that retries without limit turns one fault into a storm:
restarted services that bounce the network, alarms every minute, sessions spawned
in a loop. Speed is worth nothing until the brakes are proven.

**How Millwright does it.** The dispatch timer is stopped with one
`systemctl --user disable --now mw-dispatch.timer`
([*Running a host on a timer*](../README.md#running-a-host-on-a-timer)); a host's
`cap` bounds sessions however often dispatch runs; every doctor cure has a damper
(practice 14); nudges and alarms are rate-limited per condition; the follower runs
each job at most once at a time; a `pause-host` control word stops a host's
dispatch passes until `resume-host`.

**If you have less.** A cron job with a lock file and a counter file is a damper.
Write the stop command at the top of the script that starts things.

## 9. Spend fuel where judgement is

**Rule.** Fresh context per story, the cheapest model that does each piece of work
well, and the expensive one only where judgement is the work, with the reason
written down.

**Why.** Fuel is the binding constraint. A strong model doing lookups, or a long
session dragging its context through clerical turns, spends the budget that the
hard decisions needed.

**How Millwright does it.** The model and effort are part of each story's path, so
they are chosen per story, not per seat; a stronger model is chosen with its reason
on the bead and stepped back down when the reason ends. Seats hand clerical
errands to Clerks (a cheaper model with a numbered question list and a word limit).
`mw seat context` says how full a session is and when to hand off
([*Asking a seat how full its session is*](../README.md#asking-a-seat-how-full-its-session-is)).
Every close-out records the fuel the session burned in the ledger.

**If you have less.** Default every story to your mid-priced model and write one
line on any story that gets the expensive one. Read your plan's usage page weekly.

## 10. A deterministic tool for everything clerical

**Rule.** If a step can be done by a program, a program does it, at zero tokens.

**Why.** A model doing clerical work is slow, costly and occasionally wrong in ways
that look right. A program is free per run, does it the same way every time, and
can be tested. Sessions then spend their fuel only on what needs judgement.

**How Millwright does it.** `mw` is that program: `dispatch` (claim, worktree,
pour, start), `next` (check, land, ledger, close), `status`, `brief` (a bead tree
summarised without a model), `sweep` (stale claims), `sync`, `mail`, `doctor`, and
the follower. [ADR 0001](adr/0001-build-our-own-harness-on-proven-components.md)
keeps it small: what an existing component does is used, not rebuilt. The map of
every command is [`docs/codemap.md`](codemap.md).

**If you have less.** Every time a session does the same clerical thing twice,
write a ten-line script for it and tell the seat to call the script.

## 11. The tool carries the baton, not the session

**Rule.** When a session ends, the tooling (not the session) closes the story out
and decides what starts next.

**Why.** A session that is meant to start its successor will sometimes die, run dry
or forget first, and the chain stalls silently. Choosing what is next is a free
graph query, and only the tooling sees the whole frontier, so it can start
independent stories side by side.

**How Millwright does it.** [ADR 0004](adr/0004-mw-carries-the-baton-not-the-session.md).
The command that starts a Builder chains `mw next` after it with `;`, so it runs
whether the session finished, failed or died
([*Closing a story out*](../README.md#closing-a-story-out)). A session that did not
finish is marked blocked and its worktree kept as evidence. `mw sweep` finds
claims whose session went away without a close-out
([*Finding sessions that have stopped*](../README.md#finding-sessions-that-have-stopped)).

**If you have less.** A wrapper script around your session command:
`run-session; close-out-and-pick-next`. Never put "then start the next one" in the
model's instructions.

## 12. An append-only event log, a follower, cursors and lanes

**Rule.** Every change of state is one numbered event in one append-only log; one
cheap loop follows it and wakes whatever cares; each reader keeps its own cursor.

**Why.** Polling costs fuel when a session does it and lag when a timer does. A
numbered log lets anything (a seat, a screen, a phone) catch up from where it left
off, dedupe by number, and replay. Defining the state machines once means every
screen agrees on what a state is.

**How Millwright does it.** [`docs/events.md`](events.md): the bead, card, talk and
job machines are defined once, and a transition they do not allow is refused. The
home numbers every event; `mw events follow` (the follower) reads the tracker's
audit, writes the log, springs the dispatch and Millhand jobs, and nudges a seat
whose subscribed kinds arrive. A seat's cursor is a sequence number, which is also
how a handover is made exact. Events ship in batches by lane: `normal`, `emergency`
(alone and at once) and `fallback` (direct only, re-sent later).
[*mw events*](../README.md#mw-events-follow-emit-tail-wait) has the commands.

**If you have less.** No chain and no phone: keep the log and the follower and drop
the lanes; every batch is direct, or there are no batches at all
([*Swapping the chain*](swapping-the-chain.md) says how). Even a JSON-lines
file and a loop that tails it, with each reader storing its last line number, gives
you the wake-ups without the polling.

## 13. Hands steps are written with their way back before they run

**Rule.** A step only a person's hands can take (root, a unit, a file between
machines) is written down as the exact command, with how to undo it, before anyone
runs it.

**Why.** A command typed from a chat message is not recorded, cannot be reviewed,
and has no known undo when it goes wrong. Written first, it can be read, approved
exactly as it stands, and reversed.

**How Millwright does it.** `mw hands add <bead> --id <id> --host <host> --as
user|root --way-back '<commands>' -- '<commands>'` records the step on the bead and
comments it exactly as it will run. The Governor approves it by signing its hash;
the step runs only if it still hashes to what was signed, the approval is fresh and
has not run before, and on the host named. A root step goes through a small helper
that trusts only a key installed by hand. Every run is recorded and reported
([*Steps for his hands*](../README.md#steps-for-his-hands)).

**If you have less.** No phone app and no signing: a comment on the story with the
command in a code block and a "way back:" line, which the person runs by hand and
answers "ran it, exit 0". The discipline is in the writing, not the machinery.

## 14. A doctor with probes, cures, dampers and ways back

**Rule.** Known faults of a host are cured by a table of checks, each a read-only
probe, a cure, a damper and a way back, run without AI; what it cannot cure is
escalated once.

**Why.** Hosts fail the same few ways again and again (a network that needs a
reconnect, a unit that needs a reload, a disk filling with temporary files). A
session diagnosing them from scratch each time costs fuel and acts inconsistently;
a cure without a damper can make things worse; a cure without a way back cannot be
undone.

**How Millwright does it.** `mw doctor` ([*mw doctor*](../README.md#mw-doctor)):
each check's probe reports ok, faulty or cannot-tell; a cure runs only on faulty
and within its damper; the way back is logged beside every cure and printed by
`--dry-run`. A check it cannot cure leaves a note in the tracker, and the Millhand
seat is woken once per note to look by hand. "Cannot tell" is never treated as
faulty, so a dead internet is not blamed on a tunnel.

**If you have less.** One shell script with a function per known fault, each
printing what it would do under `--dry-run`, and a log line per run. Add a check
only after the same fault has been cured by hand twice.

## 15. Acceptance is run, not read

**Rule.** A story is done when its acceptance criteria pass when run, and the
tooling runs them again before it lands anything.

**Why.** Code that looks right is not code that works, and a session's word that
it ran the tests is not the tests passing. Two branches that each passed can fail
together.

**How Millwright does it.** Every story's acceptance criteria are written to be
checked by running something. `mw check <story>` makes, from inside the session,
the checks `mw next` makes before landing: commits on the branch, none signed by
a machine, every formula step closed, the rig's tests passing. `mw next` then
merges under the rig's merge slot and, when the merge was not a fast-forward, runs
the tests again on the merged result
([*Closing a story out*](../README.md#closing-a-story-out)).

**If you have less.** A pre-merge CI job that runs the tests on the merged result,
and a rule that a story's last step is pasting the command and its output.

## 16. Verify every landing before the next release

**Rule.** After a story lands, someone checks it did what was asked before anything
that depends on it is released.

**Why.** A bad landing that is built on multiplies: every later story inherits the
fault, and unpicking them costs more than the original work. The close-out commit
and a green test run say the branch merged, not that the want was met.

**How Millwright does it.** The Mayor reads the landing's mail and runs its
acceptance criteria before releasing further down the chain; a verified landing is
recorded as a comment beginning `VERIFIED`, which the event log reads as the bead's
`verified` state. A story the Governor can see ends with a *HOW TO CHECK IT*
section (numbered steps, the exact labels on screen, what right looks like), and
the Governor's word to verify is quoted. On a bad landing the rest of the chain
stays held.

**If you have less.** Release one story at a time, and look at each result yourself
before releasing the next.

## 17. The Governor decides; the seat finds facts

**Rule.** Decisions belong to the person the factory serves; finding the facts that
inform them belongs to the seat, which asks one question at a time with its own
recommendation.

**Why.** A seat that decides on the Governor's behalf builds the wrong thing
confidently. A seat that asks the Governor to look things up wastes the scarcest
attention in the factory. A pile of questions at once gets skimmed answers.

**How Millwright does it.** The Mayor's charter
([`template/seats/mayor/charter.md`](../template/seats/mayor/charter.md)): grill
until the want is sharp, one question at a time, each with a recommended answer;
never answer its own grilling questions; where the Governor is away and the choice
is cheap to reverse, proceed and label it *provisional, Governor to confirm*.
Charters, fences, money and rig requirements change only on the Governor's word.

**If you have less.** Put "ask one question at a time, with your recommendation"
and "never decide X, Y, Z for me" in the one charter you have.

## 18. Hand off early, at a seam, and never two at once

**Rule.** A seat hands off at the end of a piece of work or before its context is
full, in a short written handoff, and the old session stops answering at a stated
point the new one starts from.

**Why.** A session handed off at its limit is already degraded and writes a poor
handoff. A handoff that carries rules gets longer every time. Two sessions in one
seat for even a minute answer the same message twice, differently.

**How Millwright does it.** The handoff template keeps it to one screen: what to do
first, what was decided, where things stand, what is new; a standing rule goes to
the procedures sheet instead. `mw seat context` says `handoff` at the configured
limit. `mw seat handover --at <N>` writes a `handover` event at sequence N: the
old session answers up to N, the successor from N on, and each one's wait says so
([`docs/events.md`](events.md)).

**If you have less.** A handoff file with those four headings, and the rule that
you close the old window before you start talking to the new one.

## 19. No blame, and no record is ever rewritten

**Rule.** When something goes wrong, write what happened truthfully and propose the
smallest fix; never edit a closed record, only append a dated correction.

**Why.** Blame teaches sessions to hide failures, and a hidden failure costs far
more than a reported one. One falsified or quietly edited record makes every record
suspect, and distrust is paid for in fuel: everything has to be checked again.

**How Millwright does it.** Every charter says nobody is blamed: a misstep is a fact
about a seat or the machinery. Ledgers are append-only; closed beads, charters and
ledgers are never edited by a session; a truthful "this failed, here is the output"
is a good outcome for a Builder, and a story reported done that is not done is the
only bad one. A session that did not finish still gets its ledger line, saying it
did not land.

**If you have less.** Make the closing note of every story say plainly what was
verified and how, and what was left undone; never edit an old one.
