# millwright

A personal software factory: one human directs a small set of AI-occupied
seats that turn conversations into tracked work and tracked work into commits,
across several rigs and two hosts, on a tight fuel budget.

`mw` is the factory's command line. Today it knows its own version, files the
Mayor's plans with `mw file`, releases the ones approved later with
`mw release`, reads and writes stories through beads, runs
sessions in tmux, keeps the two hosts level with `mw sync`, starts a fresh
Builder session for each ready story with `mw dispatch`, and closes each
finished story out with `mw next` — which lands it, ledgers what it burned,
closes it and dispatches whatever is ready next. Two commands only look:
`mw status` reports what a host is doing, and `mw sweep` marks the claimed
stories whose session has gone or gone quiet.

## Getting started

```sh
export PATH=$PATH:/usr/local/go/bin
make build
bin/mw version
make test
make lint
```

Go 1.27. The only dependencies are [cobra](https://github.com/spf13/cobra) for
the command line and [godog](https://github.com/cucumber/godog) for the
features.

## Layout

| Directory              | What lives there                                                 |
| ---------------------- | ---------------------------------------------------------------- |
| `domain/`              | Value types — Story, Path. Pure: standard library only, no I/O.   |
| `application/`         | Use cases and the ports they reach the world through.             |
| `application/apptest/` | In-memory stand-ins for those ports, for any package's tests.     |
| `infrastructure/`      | Adapters behind those ports: beads, tmux, the harness, the disk.  |
| `cmd/mw/`              | The cobra command tree for the `mw` binary.                       |
| `features/`            | Gherkin features, run by godog from `features/features_test.go`.  |
| `docs/`                | Architecture decision records and research notes.                 |

## Vocabulary

[`CONTEXT.md`](CONTEXT.md) holds the factory's glossary, and it is law: Seat,
Session, Story, Path, Formula, Rig, Host and Fuel each mean one thing here.
Read it before naming anything. Sessions working this rig should also read
[`CLAUDE.md`](CLAUDE.md).

## Running a session

`application.Runner` is the port a session is run through: start a command in a
named session with a terminal attached, type into it, read what it has printed,
ask whether it is still running, wait for it to end. `infrastructure/tmux` is
the adapter — one tmux session per story, named after the story's id with the
punctuation tmux reads as a target replaced (`mw-gq6.4` becomes `mw-gq6_4`), so
that a person can `tmux attach -t mw-gq6_4` and watch any story being worked.
tmux is told to keep the window when the command exits, so that the exit status
and the last of the output are still there to be read afterwards.

## Filing a plan

```sh
bin/mw file plans/0001-walking-skeleton.json            # filed, and held
bin/mw file plans/0001-walking-skeleton.json --approve  # filed and released
```

A plan is JSON: one epic with the default Path its stories inherit, and the
stories, each with its acceptance criteria, its estimate, whatever it overrides
of that Path, and the keys of the stories it needs done first. `mw file` reads
the whole plan before it writes any of it — every story must resolve to a Path,
carry acceptance criteria, and wait only on stories the plan has, never on
itself or in a circle — and reports every reason it will not file a plan, not
just the first. A plan that cannot be filed leaves the tracker untouched: half
a plan in the tracker looks like work somebody meant.

What is filed is an epic carrying the default Path as metadata and its success
criteria as a section of its description (where `bd lint` looks for them), and
under it the stories, in an order where none comes before what it waits on,
each with its acceptance criteria and estimate in their own fields, its Path
overrides as metadata, and what it waits on as a blocked-by dependency.

Every story is filed **held** — beads' `deferred` status — so that nothing can
be dispatched from a plan nobody has approved. `mw file` prints the tree it
filed, with each story's Path and what it waits on, and then asks. `--approve`
answers yes without asking; at a terminal, only a plain `y` or `yes` is a yes;
with nobody there, the plan stays held. Releasing sets every story back to open,
and beads then keeps back the ones still waiting on another, so the stories that
wait on nothing are exactly what a dispatcher can take. See
`features/file_plan.feature`.

## Releasing a plan filed earlier

```sh
bin/mw release mw-gq6   # print the epic's tree, release what is still held
```

The Governor is usually not at the machine when a plan is filed, and filing it
again would file a second copy of it, so `mw release <epic-id>` is the other
half of `mw file`: it reads the epic back out of the tracker, prints the same
tree — every story with its Path, what it *still* waits on (a story it waits on
that is already finished is no longer a wait), and what the tracker says it is —
and then releases the stories that are still held. Running it is the approval,
so nobody is asked anything.

The tree is printed as the epic was found, before anything is released, and the
line under it says what changed. Nothing but a held story is touched: one
already taken, already finished or already released is left exactly as it was,
so releasing an epic twice does no more than releasing it once. An epic the
tracker does not have is a plain refusal, and nothing is written. See
`features/release.feature`.

## Booting a session into a seat

A session is primed by `application.SeatBoot`: it reads the seat from the vault
— `seats/<seat>/charter.md`, always, and `seats/<seat>/rigs/<rig>.md` when the
seat has worked this rig before — writes the boot file to
`runs/<story-id>/boot.md`, and asks the harness for the command line that
starts the session. Nothing else the vault holds is read at boot, because a
fresh session pays for every line of it (ADR 0003). `infrastructure/claude` is
the harness adapter: a headless `claude --print --output-format json` primed
with `--append-system-prompt-file`, its result redirected to
`runs/<story-id>/result.json` beside the boot file, and `BEADS_ACTOR`, `MW_SEAT`
and `MW_STORY` in its environment so that the work is signed by the seat rather
than by the session — `builder@<host>`, which is not the name mw's own writes
carry (see *Who mw writes as*). Assembling launches nothing and spends no fuel. See
`features/seat_boot.feature`.

## Dispatching a story

```sh
bin/mw dispatch --dry-run   # what it would start, writing nothing at all
bin/mw dispatch             # claim, cut, pour, boot, start
```

`mw dispatch` is the one command that spends fuel, so everything it does before
spending any is reversible. In order, once: `mw sync`, so that this host sees
the other host's claims before it makes its own; what this host already has in
flight, which is what the cap counts; what is ready here, which `mw` puts in
order itself — the most urgent priority first (P0 before P4), and of equal
priority the story filed longest ago — so that when the cap is smaller than what
is ready, the right stories wait. That is how the Mayor re-prioritises: change a
story's priority, and the next dispatch takes it earlier or later. Then, per
story, in that order, up to the cap:

1. **claim** it — from here everything is undone if anything fails;
2. **fetch** the rig's origin and **cut** `mw/<story-id>` at
   `<rig>/../.mw-worktrees/<story-id>`, from `origin/<target branch>` rather
   than from a local branch the other host may have moved past;
3. **pour** the story's formula into step beads, and record the molecule's id on
   the story;
4. **boot** the seat — the boot file, with the poured steps in it, so that a
   session never has to ask the tracker what its own steps are;
5. **start** the session through the runner;
6. **record** `run=running` on the story, saying which session, worktree and
   branch it is being worked in.

A failure at 2, 3, 4 or 5 removes the worktree, gives the claim back and writes
the reason on the story: the story is left exactly as ready as it was found.
A failure at 6 is reported and nothing is undone — the session is alive and
spending fuel, and a claim given back under a live session is how one story gets
worked twice. A story whose Path names another host, or no host at all, is never
claimed here, and neither is one whose rig this host has not checked out.

`--dry-run` prints what it would start, in that same order, and writes nothing:
nothing is synced, claimed, fetched, cut, poured or started. See `features/dispatch.feature`.

## Closing a story out

```sh
bin/mw next mw-gq6.8                 # check, land, ledger, close, dispatch again
bin/mw next mw-gq6.8 --no-dispatch   # close it out and stop there
```

`mw next` runs when a story's session ends: the command line `mw dispatch` starts
the session with chains it on with `;`, not `&&`, so it runs whether the session
finished, failed, ran dry or died. The baton is mw's, never the session's — a
session that died before spawning its successor would stall the chain silently
(ADR 0004) — and no hook has to be configured for it.

It reads what the session reported in `runs/<story-id>/result.json`. A session
that did not finish, or left no result at all, is written on the story, marked
`run=blocked` and left alone: nothing is merged, nothing is closed, the claim is
not given back and the worktree is kept, because it is the evidence. One ledger
line is still appended, saying it did not land and what it burned getting there.

A session that did finish is checked before anything is landed:

1. the branch must hold **commits** that `origin/<target>` does not;
2. none of those commits may be **signed by a machine** (below);
3. every **step** of the story's poured formula must be closed;
4. the **rig's own tests** must pass in the story's worktree. A test command
   the shell could not run at all (exit 126 or 127 — a toolchain that is not on
   the PATH `mw` was started with) is reported as that, with the command's own
   last lines, and not as a failing test; the story is blocked either way.

Then, under the rig's **merge slot**, `mw/<story-id>` is merged into the target
branch as the remote has it — in a throwaway detached worktree, so neither the
rig's checkout nor the story's worktree is disturbed. If that was not a
fast-forward the tests are run **again** on the merged result, because nothing
has ever tested that combination: the story's tests passed on the story's branch
and the other host's passed on its own. The push is never forced; a push the
remote refuses because the other host got there first is fetched, merged and
pushed again, a bounded number of times.

Only then: the worktree and its branch go, one line is appended to the seat's
ledger, that line and the seat's memory of the rig are committed in the vault,
the story is closed with the reason, `mw sync` brings the hosts level so that
the other host sees a closed story rather than a claimed one, and whatever is
ready here is dispatched.

The commit is exactly two paths — `seats/<seat>/ledger.md` and
`seats/<seat>/rigs/<rig>.md` — named explicitly, never `git add -A` and never
`commit -a`, under a plain message naming the story and signed by nobody. They
are the only files a story is allowed to write in the vault: mw wrote the first
and the session may have written the second, so a close-out that left them
uncommitted would stop its own sync and every later one. Anything else
uncommitted in the vault is still somebody else's to commit, and the sync's
vault half still refuses because of it, naming the file — the story is landed and closed all the
same, because none of that is undone. A close-out that lands nothing commits the
line it wrote saying so, for the same reason.

### Nothing here is signed by a machine

A seat outlives every session that occupies it, so the seat signs the work and
the model never does. That is enforced in three places, so that it does not
depend on any one of them:

- the session `mw` starts is given `--settings` with
  `attribution.commit` and `attribution.pr` set to the empty string and
  `attribution.sessionUrl` to `false`, which is how Claude Code's settings
  reference says to hide the `Co-Authored-By` trailer and the "Generated with"
  line it would otherwise add. It is passed as a JSON string rather than a file,
  so nothing is written into the rig's worktree for a session to commit by
  accident (*What a session may run without asking* is the other half of that
  same document);
- the Builder's kickoff prompt says it in words;
- `mw next` reads the messages of every commit it would land
  (`origin/<target>..mw/<story-id>`) before it merges anything, and refuses the
  branch if any line of any of them, ignoring case, starts with
  `Co-Authored-By:` or holds "generated with". This factory has one human, so
  there is no innocent second author. The story is marked `run=blocked`, a
  comment names the offending commit by its short hash and quotes the line, one
  "not landed" line goes in the ledger, the worktree and branch are kept, and
  `mw next` leaves with 1. Nothing is merged, so the branch is still the
  session's to amend.

The **merge slot** is an advisory lock (`flock`) on a file beside the rig's
worktrees, one per rig per host. It is a lock rather than a file somebody writes
their name in because the kernel holds it: an `mw` that is killed, runs out of
memory or has its terminal closed under it gives the slot back the moment the
process ends, so there is no stale slot to break by hand. A race between the two
*hosts* is not settled here at all — it is settled where it has to be, by the
remote refusing the second push.

The ledger line is one row of the seat's table: the date, the story, the
outcome, the model and effort it was worked at, the fuel it burned, and a note
of which host ran it. The fuel comes from the harness's own result JSON —
`usage.input_tokens`, `usage.output_tokens`, `usage.cache_read_input_tokens` and
`usage.cache_creation_input_tokens` totalled and broken out, `num_turns`,
`total_cost_usd` (a list-price equivalent, not a bill on a subscription) and
`duration_ms`. The ledger is opened for append and never rewritten: there is no
code path in mw that can change a line a seat has already written. It is read
back for two things only — a report of what a seat has burned, and a close-out
run again, asking whether this story's line is in it already. See
`features/next.feature`.

If the landing succeeded and the **close** did not, the story is landed and
still open: the work is on the target branch, the ledger line is written, and
nothing of it is undone. `mw next` says so — `OPEN the story is landed but still
open` — leaves with 1, and dispatches nothing. Running `mw next <story-id>`
again closes it and carries on: the run that landed the story recorded
`run=landed` the moment the push succeeded, so a later run knows there is
nothing to merge, test or push, and it adds no second ledger line, because the
seat's ledger already names the story.

### What a session may run without asking

The settings `mw` passes inline say one more thing:
`permissions.allow: ["Bash(bd *)"]` — every invocation of `bd`, the one program
a session must reach to work its story at all.

A session runs in `auto` mode with `--permission-prompts none`, so a second
model, the permission classifier, judges each command, and nobody is there to
answer if it says no. It has refused a session's own `bd close` as a write to an
external system — at random, on both hosts — and with nobody to ask, that
refusal is final: the story's steps stay open and `mw next` will not land it. An
allow rule is resolved before the classifier is asked, and a rule naming one
program stays in effect in `auto` mode (only broad ones, like `Bash(*)` or a
wildcarded interpreter, are suspended there).

It is the whole of `bd` rather than a list of subcommands, by the Governor's
decision: beads is this factory's own tracker, a session is *supposed* to write
to it, and a list would have to be kept in step with bd forever. It lives here,
in the rig, so both hosts get the same rule and nobody's personal Claude
settings are touched. Nothing else is allowed: everything a session does besides
`bd` still goes to the classifier.

One consequence worth knowing, because it is how Claude Code matches: a rule
must match **each subcommand** of a compound command on its own. `bd show x |
head -5; cat CONTEXT.md` is not covered by this rule — the `head` and the `cat`
are judged as usual, and a chain can be refused whole. Run `bd` on its own line.

### Who mw writes as

`mw` names itself on **every** `bd` it runs: `--actor mw@<host>`, from the
`host` key of the config file. It is not read from `$BEADS_ACTOR`, because the
same act is run from several environments that carry several names — a claim
from the dispatcher's shell or a timer, the close from inside the Builder
session that worked the story, which is signed `builder@<host>`. bd lets only
the actor that claimed a story close it, so a claim and a close under two names
is a story that lands and cannot be closed. A host with no `host` set is a plain
refusal before any `bd` is started.

`mw` is neither the Mayor nor the Builder on purpose: what `mw dispatch` and
`mw next` write down was decided by the machinery, not by a seat, and the
factory's history has to be able to tell the two apart. A session's own writes
are still signed by its seat — the harness sets `BEADS_ACTOR=<seat>@<host>`.

A story claimed by hand is closed by hand under the name that claimed it:

```sh
bd --actor root close mw-gq6.30 --reason "landed by hand"   # claimed as root
bd reclaim mw-gq6.30                                        # or take it over first
```

### What a host is told

`~/.config/mw/config.toml`, with `MW_VAULT`, `MW_HOST`, `MW_CAP` and
`MW_HOST_SILENT_HOURS` and `MW_STALE_HOURS` ahead of it:

```toml
vault = "/root/millwright-vault"   # the one beads database and the seats
host  = "vps"                      # which of the factory's hosts this is
cap   = 1                          # sessions running here at once (default 1)
host_silent_hours = 2              # how long another host may go unsynced (default 2)
stale_hours = 2                    # how long a session may print nothing new before mw sweep calls it stuck (default 2)

[rigs]
millwright = "/root/millwright"    # where each rig is checked out here

[tests]
millwright = "make test"           # how a close-out asks this rig if it is green
```

A rig a story names but this host has no checkout of is said so plainly, and the
story is left for the host that has it. A rig `[tests]` does not name is checked
with `make test`.

## Keeping two hosts level

`mw sync` is the one command that brings this host level with the other, by
hand, from the dispatcher or on a timer. In order: the vault's files, with a
plain `git pull --rebase` and a push of what this host has and the other does
not; then one `bd sync` for the factory's single beads database; then a note of
when this host was last level, under `host.<name>.last_sync` in beads' key-value
store, so that either host can say how stale the other is.

It never migrates, never forces and never retries. A vault holding uncommitted
work stops the vault's half and only that half, naming the files on one line:
committing them belongs to whoever wrote them, and the only vault files mw
commits for itself are the two a close-out writes (above). The beads half runs
anyway, so a `mw sync` on a timer keeps the hosts level in beads while one
forgotten edit waits for whoever made it, and `mw sync` leaves with 5 — a status
of its own, so that a timer reading nothing but the number can tell a waiting
edit from a fault. Nothing is recorded under `host.<name>.last_sync` then: a
host whose vault half never ran is not level, and the note may not say it was. A
rebase that cannot finish is undone, so the vault is left as it was found. `bd
sync`'s exit code is surfaced as it is and becomes mw's own: 2 (a merge conflict
beads will not resolve) and 4 (a working set only a person can clear) stop mw
with a plain message and are never retried or auto-resolved, and a beads halt is
what mw reports even when the vault was blocked too.

Everything that must not run on a stale vault still does not: `mw dispatch` and
`mw next` sync before they act, and a blocked vault half stops them exactly as
before — nothing is claimed, nothing is dispatched, and the reason names the
files.

Ledgers are appended to by both hosts and edited by neither, so the vault's
`.gitattributes` must carry `seats/*/ledger.md merge=union` — two hosts' appends
then merge by keeping every line instead of conflicting. `mw sync` writes that
line if it is missing and says so; committing it is a seat's job, and until
someone does, only this host is covered. See `features/sync.feature`.

## Seeing what a host is doing

```sh
bin/mw status
```

`mw status` takes no arguments and reads the vault, the tracker, the runner's
session names and the Builder's ledger. It writes to none of them. It is the report for a phone: what this host is doing right now, in
five parts.

- **RUNNING** — the stories this host has claimed, each with the name of the
  tmux session to attach to. A story recorded `run=stopped` or `run=stuck` says
  `NOT RUNNING` rather than pretending, and a claimed story whose poured formula
  still has a step open says its close-out is blocked.
- **READY** — what this host could take now.
- **BLOCKED** — what is waiting, each with only the work it is still waiting
  for: a wait that has finished is not listed.
- **OTHER HOSTS** — every other host a story is pathed to, and what it holds.
- **FUEL today** — the tokens the Builder's ledger charged on lines dated today,
  and 0 when the seat has no ledger yet.

A story filed with only its overrides is shown with the epic's defaults filled
in. The command only reads: no claim, no write to a bead, no note, no ledger
line and no session is started, so it costs no tokens. Every line fits 60
columns; a longer title is cut short with an ellipsis rather than wrapped.

The OTHER HOSTS part is the whole of the factory's safety net for a host that
has gone quiet. There is no failover: a host that stops syncing does not hand
its work back. Each host is shown with when it last recorded itself level (the
`host.<name>.last_sync` note `mw sync` leaves, so it is always a sync cycle
behind), and the stories pathed to it that are ready or already claimed. A host
is **ASLEEP** when that note is older than `host_silent_hours` — two by default,
so that the lag alone does not call a host asleep — set in the config file or by
`MW_HOST_SILENT_HOURS`, whole hours and at least 1. It is also `ASLEEP, never
synced` when it has left no note at all, and `ASLEEP, last sync unreadable` when
its note is not a time; neither is an error, since a host that cannot say when
it was last level is the one to look at. The stories of a sleeping host are
marked **stranded**: nothing here will move them and no other host will take
them. Under it `mw status` prints the one line that brings a story here:

```sh
bd update <id> --set-metadata host=vps
```

`mw status` only ever says this. Re-pathing a story is a person's act, never a
report's. The config keys it reads are `vault`, `host` and `host_silent_hours`
(*What a host is told*). See `features/status.feature`.

## Finding sessions that have stopped

```sh
bin/mw sweep
```

`mw sweep` takes no arguments and is for a host to run on itself, by hand or on
a timer, to find the stories it has claimed whose session is no longer doing
anything. It reads the stories this host has claimed, asks the runner whether
each one's tmux session is still there, and reads the last twenty lines that
session printed. It costs no tokens and starts no session.

- A claimed story whose session is **gone** is stuck at once.
- A session that is still there but has printed **nothing new** for longer than
  the stale threshold is stuck too. Every session is owed one full threshold
  before it is called that: the first sweep to see a session's output only
  records it, and a session whose output has changed since has its clock reset.
- A stuck story is commented on once, saying what was found, and recorded
  `run=stuck`, which `mw status` then shows as `NOT RUNNING`. A story that
  `mw next` or an earlier sweep already recorded gone is left alone, so sweeping
  twice comments once.

The threshold is `stale_hours` in the config file or `MW_STALE_HOURS`: whole
hours, at least 1, two by default. Along with `vault` and `host`, that is all
`mw sweep` reads from the config.

The only things sweep writes are those comments and state on the story's own
bead: `run`, and the fingerprint of the session's output and when it was first
seen (`sweep-output` and `sweep-output-since`), which is how a sweep with no
daemon remembers anything between runs. It never kills or restarts a session,
never gives a claim back, and never touches a worktree, git or the ledger:
settling a stuck claim is a separate command. One story's trouble — a session
that cannot be asked about, a write that fails — is reported on a `!` line and
the rest are still examined. See `features/sweep.feature`.

## The Path

A story is worked by a **Path**: the rig it is worked in, the branch it
targets, and the harness, model, effort, formula and host of the session that
works it. An epic carries default path values and each story may override any
of them; `Story.PathFrom` overlays the two and rejects what is not a path — no
rig, no target branch, or a harness, model or effort the factory does not
know. See `features/path_validation.feature`.
