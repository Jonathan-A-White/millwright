# millwright

A personal software factory: one human directs a small set of AI-occupied
seats that turn conversations into tracked work and tracked work into commits,
across several rigs and two hosts, on a tight fuel budget.

## Quick start

One line installs everything this needs and builds `mw`, on Debian or Ubuntu
(WSL Ubuntu included), as your own user:

```sh
url=https://raw.githubusercontent.com/Jonathan-A-White/millwright/main/scripts/install.sh
curl -fsSL "$url" | sh
```

Piping a script straight into a shell is worth a second thought. Download it and
read it first if you'd rather, then run it the same way:

```sh
curl -fsSL "$url" -o install.sh
less install.sh && sh install.sh
```

It never handles a secret, and ends by printing the hand steps that are still
yours, each with its command and a check:

- **`[claude]`** install the harness and log in to it
  install: `curl -fsSL https://claude.ai/install.sh | bash` · log in: `claude auth login` · check: `claude auth status`
- **`[github]`** GitHub credentials, for a private vault (or a deploy key on the
  vault's repo, cloned over ssh)
  run: `gh auth login && gh auth setup-git` · check: `gh auth status`
- **`[git-identity]`** tell git who you are
  run: `git config --global user.name "Your Name" && git config --global user.email "you@example.com"` · check: `git config --global user.name && git config --global user.email`
- **`[mw-init]`** set `mw` up on this host — see below
  run: `mw init` · check: `test -f ~/.config/mw/config.toml && echo ok`

Then `mw init --vault <dir> --prefix <prefix>` for a factory of your own, or `mw
init --join <git-url> --vault <dir>` to bring this host onto one that already
exists (*Making a fresh vault* and *Joining an existing vault*, below).

Next, `scripts/install-units.sh` puts this host's timers where systemd finds
them (*Running a host on a timer*, below). Which ones a host wants depends on
what it does here:

- a host that runs Builders wants `mw-dispatch`;
- the Mayor's home wants `mw-mail-notify` and `mw-health`;
- a host with a Millhand wants the two Millhand timers, `mw-millhand-tick` and
  `mw-millhand-review`.

`sh scripts/install-units.sh --enable <name>...` links and arms them; run it
with no name to see what is installed and active already.

Last, `mw seat up mayor` starts the Mayor's session, primed from its charter and
its newest handoff. A fresh vault has no handoff yet, so the very first
conversation is by hand instead: open `claude` in the vault, point it at
`seats/mayor/charter.md` and `seats/mayor/vision.md`, and talk — it ends by
writing the first handoff. Every session after that starts with `mw seat up
mayor`.

## Moving a host

Moving a seat's home — the Mayor's, say — to a new host:

1. **Join** on the new box first, so the seat has somewhere to land before it
   leaves the old one: `mw init --join <git-url> --vault <dir> --host <name>`,
   then `scripts/install-units.sh` there for whichever timers it now wants.
2. **Turn the old host's timers off first** — whichever it ran for the seat
   that is moving (`mw-mail-notify` and `mw-health` for the Mayor's home, the
   two Millhand timers for a Millhand): `systemctl --user disable --now
   <unit>.timer` for each. Two hosts must never run the same seat's timers at
   once.
3. **Move the designated-migrator note**, if the old host held it: the line in
   the vault's `CLAUDE.md` naming which one host runs `bd migrate` — every
   other clone stays on `bd bootstrap`, never `bd migrate`, forever.

What this does **not** move: any other timer the old host still runs (a
dispatcher's, another seat's), any rig only it has checked out, and anything
else it serves that millwright did not start. Those stay exactly where they
are; only the seat crosses over.

`mw` is the factory's command line. Today it knows its own version, files the
Mayor's plans with `mw file`, shows a filed plan with `mw show`, releases the ones
approved later with `mw release`, reads and writes stories through beads, runs
sessions in tmux, keeps the two hosts level with `mw sync`, starts a fresh
Builder session for each ready story with `mw dispatch`, and closes each
finished story out with `mw next` — which lands it, ledgers what it burned,
closes it and dispatches whatever is ready next. Five commands only look:
`mw show` prints a filed plan's tree, `mw check` runs a story's pre-landing
checks on its branch, `mw status` reports what a host is doing, `mw brief`
prints the live children of a bead for a seat to boot from, and `mw sweep`
marks the claimed stories whose session has gone or gone quiet.

## Installing

See *Quick start*, above, for the one line. It needs apt packages (git, tmux, jq,
ripgrep, python3, curl, ca-certificates, gh, make), Go and `bd` at the versions in
`scripts/pins.env` (each checked against the sha256 its release publishes). It
clones the rig to `~/millwright` (or `$MW_HOME`; run from inside a checkout, it
uses that one), runs `make build` and links `mw` into `~/.local/bin` (or
`$MW_BIN`); Go goes to `~/.local/go`. Every step is printed first and skipped
when already done, so running it again is safe. `sh scripts/install.sh
--dry-run` prints the steps and changes nothing. It never runs `sudo`: when
packages are missing it prints the one `apt-get install` command that needs
root and stops before changing anything; run that with `sudo`, then run the
line again. On any other system it prints what it would need and exits
non-zero.

## Getting started

```sh
export PATH=$PATH:/usr/local/go/bin
make build
bin/mw version
make test
make lint
```

Go at the version in `scripts/pins.env`, which is the `go` line of `go.mod` and which
`make lint` holds to it. The only dependencies are [cobra](https://github.com/spf13/cobra) for
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
and the last of the output are still there to be read afterwards. tmux does not
always record that status — pinned to one CPU it often reaps a command and never
notes how it ended — so a session whose command has ended with no status after a
few seconds is reported as `exited, status unknown`: not running, not finished,
and not a clean exit.

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

## Showing a plan filed earlier

```sh
bin/mw show mw-gq6      # print the epic's tree, and stop
```

The Governor approves a plan from a phone, later, and has to see what he is
approving before he says yes. `mw show <epic-id>` reads the epic back out of the
tracker and prints exactly the tree `mw release` prints, and then stops: it
writes nothing, so every held story is as held after it as before. There is no
`--dry-run` on `mw release`; this is the one command for looking.

An id the tracker has no epic under, or that names a bead that is not an epic (a
story, say), is a plain refusal. An epic filed *under* the epic is not a story:
the tree names it as an epic, with its state, and leaves its own stories to
`mw show` of its id — and `mw release` never releases it. See
`features/show.feature`.

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
carry (see *Who mw writes as*). It also carries
`CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`, so a Builder cannot background a
command and end its turn before it finishes, which lost uncommitted work more
than once (mw-gq6.90); a seat's own session (`SeatSession`, below) does not get
it, since a seat's mail watcher and waiters run in the background by design.
Assembling launches nothing and spends no fuel. See
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
   the story — unless the story already records a molecule whose root is still
   open (an earlier dispatch failed after pouring): that one is worked again, with
   only the steps still open, and nothing is poured;
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

A story the Governor must be present for carries the label `hitl` (`bd update
<id> --add-label hitl`; a story filed under an epic inherits the epic's labels
unless made with `--no-inherit-labels`). No dispatcher takes it, and, dry run
or not, it is passed over with that reason; the Mayor claims it and does it beside
the Governor, and a `hitl` story in progress does not count against the cap.

**A story is tried a bounded number of times.** Every session mw starts for a
story is an attempt, counted in the story's `attempts` metadata once the session
is running: a dispatch that fails before that (the fetch, the worktree, the
runner) is not one, and the rebase session `mw next` sends a conflicted story
back to is one. `max_attempts` in the config file (`MW_MAX_ATTEMPTS` ahead of it,
default 3) is how many a story gets. A ready story that has had them all is not
started: `mw dispatch` leaves it unclaimed, marks it `run=blocked` under the
reason code `attempts-exhausted`, writes one comment on it and mails the Mayor
once, with the count and the refusals `mw next` recorded on the story. A later
tick finds the mark (`attempts_exhausted` in the metadata) and says nothing more,
and the story does not use up a place under the cap. `mw status` shows
`attempts N` under a story that has had more than one.

The way back in is the Mayor's, and explicit: once the cause is dealt with, reset
the counter by hand, and nothing else does. A story `mw next` refused is still
claimed, so give it back too:

```sh
bd update <story-id> --set-metadata attempts=0                       # unclaimed, exhausted
bd update <story-id> --status open --assignee "" --set-metadata attempts=0   # a refused story, claimed
```

The next dispatch then starts it as a first attempt.

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

Once it is pushed, this host's own checkout of the rig is fast-forwarded onto
the landed commit — a checkout left behind is one whose rebuilt binary is the
old one. It is moved only when it is on the target branch, holds no uncommitted
work (untracked files count) and has no commits of its own; otherwise it is
left exactly as it was and the report's `rig` line says why.

A host that runs its own `mw` from a rig's checkout says so in its config, and a
moved checkout is then rebuilt by the landing itself: the command the
`[after_landing]` table names for the rig is run once, in the rig's checkout,
under a five-minute limit, after the checkout was moved and never otherwise (see
*What a host is told*). It changes nothing about the landing: the story is recorded
landed, ledgered and closed exactly as without it. A command that exits non-zero,
is not found or outlives its limit is one plain line — `after landing: make
build: exit status 2: <the tail of its output>` — in the report, the ledger line,
a comment on the story and the `Landed:` mail to the Mayor, so that they know
this host's binary is old; success is `after landing: make build: ok` in the
report and the mail. A rig the table does not name runs nothing.

Only then: the worktree and its branch go, one line is appended to the seat's
ledger, that line and the seat's memory of the rig are committed in the vault,
the story is closed with the reason, `mw sync` brings the hosts level so that
the other host sees a closed story rather than a claimed one, and whatever is
ready here is dispatched.

The commit is exactly three paths — `seats/<seat>/ledger.md`,
`seats/<seat>/rigs/<rig>.md` and `runs/<story>/result.json` — and a fourth,
`runs/<story>/landing-error.txt`, when a landing failed (below) — named explicitly,
never `git add -A` and never `commit -a`, under a plain message naming the story
and signed by nobody. They are the only files a story is allowed to write in the
vault: mw wrote the first and the run record, and the Mayor writes the second from
what Builders propose in their closing comments (a session never edits it, but a
stray edit is committed all the same), so a close-out that left them uncommitted
would stop its own sync and every later one. The run record is committed so that the evidence behind a ledger
line's fuel travels to the other host with the line; `runs/<story>/boot.md` beside
it is never committed. A run record that is not there is said so in the report,
and the other two are committed all the same. Anything else
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

A landing that fails — a push the remote refuses, a merge that will not go —
keeps the whole of git's error in `runs/<story>/landing-error.txt` and quotes it
in full, fenced, in the story's comment; the ledger row keeps its one line, the
first line of the error.

The **merge slot** is an advisory lock (`flock`) on a file beside the rig's
worktrees, one per rig per host. It is a lock rather than a file somebody writes
their name in because the kernel holds it: an `mw` that is killed, runs out of
memory or has its terminal closed under it gives the slot back the moment the
process ends, so there is no stale slot to break by hand. A race between the two
*hosts* is not settled here at all — it is settled where it has to be, by the
remote refusing the second push.

The ledger line is one row of the seat's table: the date, the story, the
outcome, the model and effort it was worked at, the fuel it burned, and a note
of which host ran it.

The fuel comes from the harness's own result JSON, and the fuel column names
which figure each number is, because the fields in that file do not all cover
the same span. The tokens are `modelUsage` — every model the session used, its
subagents included — totalled and broken out, and the column says **whole
session**; a file with no `modelUsage` gives up only its last turn's `usage`,
and the column says **last turn only** instead. Beside them are
`total_cost_usd` (a list-price equivalent for the whole session, not a bill on
a subscription) and the turn count and clock. A session that a background task
or a monitor woke leaves a result whose `num_turns` and `duration_ms` are the
last turn's alone — mw-gq6.33 ran fifteen minutes and its file says one turn
and 3.6 seconds — so on those lines the turns read **after the last wake-up**
and the clock is `duration_api_ms`, named **of API time**, which is the only
whole-session length the file holds. Why each field covers what it covers, with
the evidence from two real runs, is in
`docs/research/claude-code-result-json-fuel.md`.

The ledger is opened for append and never rewritten: there is no
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

### Checking a branch before the session ends

```sh
bin/mw check mw-gq6.8                # what mw next would refuse, printed; changes nothing
```

`mw next` tells a session its branch is refused only after the session has ended
and its fuel is spent. `mw check <story-id>` asks the same questions of the
story's branch while the session can still fix it: commits on the branch, none
signed by a machine, every formula step closed, the rig's tests passing in the
worktree. It prints each refusal in the words `mw next` would use, with the
detail `mw next` writes on the story (the steps still open, the commit, the
tests' last lines), and leaves with 1 when any check fails. It reports **every**
check that fails, not only the first: a session that fixes one and asks again
pays for the tests each time.

It is read-only. It writes no comment, run state or ledger line, commits nothing
in the vault, and asks git for nothing that writes — no fetch, no merge slot, no
landing worktree — so the commits are counted against `origin/<target>` as the
rig last saw it. It does not read the session's result, which is not written
until the session ends, and it does not try the merge, so a branch that passes
can still be stopped by a conflict or by the other host's work. The last formula
step is normally still open when a session runs it; the session's kickoff prompt
names the command and says so. See `features/check.feature`.

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
are still signed by its seat — the harness sets `BEADS_ACTOR=<seat>@<host>`. The
decision and the alternative turned down are in
[ADR 0005](docs/adr/0005-mw-acts-under-its-own-name.md).

The vault commit `mw next` makes ("Close out <id>: <title>") is signed the same
way: git's author and committer are `mw@<host>`, given to that one `git commit`
with `-c user.name` and `-c user.email`. The clone's own `user.name` and
`user.email` are never touched, so a commit by hand there is still yours.

A story claimed by hand is closed by hand under the name that claimed it:

```sh
bd --actor root close mw-gq6.30 --reason "landed by hand"   # claimed as root
bd reclaim mw-gq6.30                                        # or take it over first
```

### What a host is told

`~/.config/mw/config.toml`, with `MW_VAULT`, `MW_HOST`, `MW_CAP` and
`MW_HOST_SILENT_HOURS`, `MW_STALE_HOURS`, `MW_HANDOFF_AT`, `MW_RIG_MEMORY_BYTES`,
`MW_DISPATCH_SYNC_TRIES`, `MW_DISPATCH_SYNC_WAIT`, `MW_PUSH_TRIES`,
`MW_PUSH_WAIT_SECONDS`, `MW_MAX_ATTEMPTS`,
`MW_MILLHAND_ROUTINE_MODEL`, `MW_MILLHAND_REVIEW_MODEL`,
`MW_NUDGE_AFTER_MINUTES` and `MW_NUDGE_SYNC_STALE_MINUTES` ahead of it:

```toml
vault = "/root/millwright-vault"   # the one beads database and the seats
host  = "vps"                      # which of the factory's hosts this is
cap   = 1                          # sessions running here at once (default 1)
host_silent_hours = 2              # how long another host may go unsynced (default 2)
stale_hours = 2                    # how long a session may print nothing new before mw sweep calls it stuck (default 2)
handoff_at = 180000                # the context size, in tokens, at which mw seat context says handoff (default 180000)
rig_memory_bytes = 8000            # how large the Builder's memory of one rig may grow before mw status says prune (default 8000)
dispatch_sync_tries = 3            # how many times mw dispatch tries its sync when a name cannot be resolved (default 3)
dispatch_sync_wait = "15s"         # how long it waits between those tries (default 15s, at most 90s in all)
push_tries = 3                     # how many times mw next tries a push again after a fault at the remote itself (default 3)
push_wait_seconds = 20             # how long it waits between those tries (default 20)
max_attempts = 3                   # how many times a story is started in all before mw dispatch stops and mails the Mayor (default 3)
millhand_routine_model = "sonnet"  # the model of a routine wake, and of a wake by hand, of the Millhand (default sonnet)
millhand_review_model = "opus"     # the model of a review wake of the Millhand (default opus)
nudge_after_minutes = 60           # how long a claimed story may run with nothing mailed about it before mw nudge names it (default 60)
nudge_sync_stale_minutes = 20      # how stale another host's last sync may be before mw nudge names it (default 20)

[rigs]
millwright = "/root/millwright"    # where each rig is checked out here

[tests]
millwright = "make test"           # how a close-out asks this rig if it is green

[after_landing]
millwright = "make build"          # run in this rig's checkout once a landing has moved it (default: nothing)

[watch]                            # what mw watch looks at; leave it out to watch nothing
ssh = "vps"                        # the name ssh knows the watched host by
host = "vps"                       # its name in beads
outside = ["https://one.example", "https://two.example"]
blog = "https://blog.example.com"  # optional: the blog answering is a sign of life

[doctor]                           # what mw doctor checks; leave it out for the shipped defaults
units = ["mw-dispatch.service", "mw-millhand-tick.service", "mw-millhand-review.service", "mw-doctor.service"]
state_dir = "/root/.local/state/mw-doctor"  # default: ~/.local/state/mw-doctor
reach = ["api.anthropic.com:443", "github.com:443"]  # default; what the wifi check tries to reach
powershell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"  # default; wifi check's Windows-backed sign
```

A rig a story names but this host has no checkout of is said so plainly, and the
story is left for the host that has it. A rig `[tests]` does not name is checked
with `make test`. The `[tests]` line is also what a session working that rig may
run without being asked — the whole line, and each side of a top-level `&&` on
its own. A rig `[after_landing]` does not name has nothing run after a
landing; the table is for a host whose timers run a binary built in a rig's
checkout, which a landing leaves one commit stale until the command rebuilds it.
The command is one line, read by `/bin/sh` in the rig's checkout like a `[tests]`
line, and is stopped after five minutes.

## Recovering a story after a refused landing

```sh
bin/mw retry mw-gq6.8
```

`mw retry` is the hand recovery a refused landing used to take a person several
steps to run, as one command. It is run on the story's own host once its
session has ended — typically right after `mw next` has refused its landing.
It refuses, changing nothing at all, at the first of these that does not hold:

1. the session named on the story must not still be **running**;
2. a worktree a session left dirty is **committed** onto its branch, under a
   plain message naming the story and the attempt;
3. the branch's commits ahead of the target are **bundled** into
   `runs/<story>/attempt-<n>.bundle` in the vault, and the bundle must verify;
4. the vault must **accept and push** that bundle.

Only once all of that has held are the worktree and branch taken away — the
worktree never forced, and the branch deleted safely (`git branch -d`) and
forced (`-D`) only when that refuses it — and the claim given back with the
story set open again (not merely unassigned: a story left `in_progress` is
never offered to a dispatcher). One comment is left on the story naming the
branch commit the bundle captured, where the bundle is, and the commit the
vault pushed it as.

It never resets a story's attempts count, and a story already started
`max_attempts` times is refused with "attempts exhausted" rather than retried
— resetting the counter by hand is what allows another attempt. See
`features/retry.feature`.

## Keeping two hosts level

`mw sync` is the one command that brings this host level with the other, by
hand, from the dispatcher or on a timer. In order: the vault's files, with a
plain `git pull --rebase` and a push of what this host has and the other does
not; then one `bd sync` for the factory's single beads database, carrying a note
of when this host was last level, under `host.<name>.last_sync` in beads'
key-value store, so that either host can say how stale the other is. The note
is written just before that `bd sync`, which is what pushes it, so the other
host reads it on its next sync rather than one later; a `bd sync` that halts
pushed nothing, and the note is put back the way it was.

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

## Making a fresh vault

```sh
mw init --vault <dir> --prefix <prefix> [--host <name>] [--rig <name>=<dir>]...
```

`mw init` is the one command that runs before there is a vault or a config file,
so it reads neither. It lays the template built into the `mw` binary (an installed
`mw` needs no checkout beside it) into `<dir>`, makes `<dir>` a git repository with
one first commit that carries no attribution, and makes a beads database there whose
story ids begin with `<prefix>`: the one place mw runs `bd init`. It refuses, and
writes nothing, if `<dir>` exists and has anything in it, and a prefix beads would
not take is refused the same way. The commit is made by whoever git knows; on a
machine where git knows nobody it is made as `mw@<host>`.

It then writes `~/.config/mw/config.toml`, with the vault, `--host` (default: the
machine's hostname; the name a story's Path uses, `vps` or `laptop`, is what you want
here), `cap = 1` and a `[rigs]` table of the `--rig` flags, only if that file does not
exist. A file that is there is never read, merged or changed: `mw init` prints the
lines it would have written instead. It ends by printing what is owed next: a private
remote for the vault and `git push`, then `scripts/install-units.sh`. See
`features/init.feature`.

## Joining an existing vault

```sh
mw init --join <git-url> --vault <dir> [--host <name>] [--rig <name>=<dir>]...
```

`mw init --join` brings a second host onto a vault that already exists, instead of
making one: it clones `<git-url>` into `<dir>` (the same rule as a fresh vault's —
`<dir>` must not exist or must be empty), and picks up the vault's beads database
with `bd bootstrap` rather than making one — never `bd init`, never `bd migrate`,
never a forced push. Running it never makes this host the vault's designated
migrator; that stays whichever host it already was. `--join` and `--prefix` are
refused together, since a joined vault already has its own database.

It then writes `~/.config/mw/config.toml` under the same never-overwrite rule `mw
init` writes it under, and ends with one `mw sync`, printed or its failure.
Moving a seat's home to a new host with this command needs more besides — see
*Moving a host*, above. See `features/init.feature`.

## Running a host on a timer

A host that only works stories needs no session of its own to keep it going: a
`systemd --user` timer runs `mw dispatch` every five minutes, and each run
starts, spends and ends with that one command. No daemon, and no tokens spent
between ticks. The units are in `contrib/systemd/`: `mw-dispatch.service` (a
oneshot that runs `mw dispatch` as you) and `mw-dispatch.timer` (every five
minutes, on the clock).

**Install**, once per host. Write `~/.config/mw/dispatch.env` (the one line
below), then:

```sh
sh scripts/install-units.sh --enable mw-dispatch
```

`scripts/install-units.sh` *links* the pair into `~/.config/systemd/user` (so a
landing that changes a unit needs only `systemctl --user daemon-reload`), reloads
systemd, and enables the timer only with `--enable`. It takes any of the five
timer pairs in `contrib/systemd/` by name, and with none named lists them and
which are installed and active, changing nothing; `--dry-run` says what it would
do. It prints the `loginctl enable-linger` line as a hand step and never runs it.

A user unit gets a bare `PATH`, and the rig names no host's directories. The
host owns `~/.config/mw/dispatch.env`, one line saying where `mw`, `bd`, `git`,
`tmux`, `claude` and `go` are (`go` because a Builder runs the rig's tests):

```
PATH=/home/you/.local/bin:/home/you/go/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
```

The file is optional in the unit, so a missing one is not an error, but then
`mw` is not found. `mw dispatch` reads the rest — vault, host, cap, rigs — from
`~/.config/mw/config.toml`, as it does by hand.

**Stop it at once**: `systemctl --user disable --now mw-dispatch.timer`. That is
the damper. It stops any further run and leaves the sessions already started
alone, since they are tmux sessions and not the unit's to kill; end those with
`tmux kill-session` if that is what you want. `systemctl --user list-timers`
says whether it is armed.

**Its output** goes to the journal: `journalctl --user -u mw-dispatch`, with
`-f` to follow. Each run is the report `mw dispatch` prints. `systemctl --user
status mw-dispatch` shows the last run and how it ended.

**Exit 5 is not a failure.** `mw dispatch` syncs first (see above), and a vault
holding uncommitted work stops the sync's vault half; `mw dispatch` then stops
too, before it claims anything, and leaves with 5 (`mw sync` alone does the same
after syncing beads). The unit lists 5 in `SuccessExitStatus`, so that wait does not
show as a failed unit; the journal has the one line naming the files, and
nothing is dispatched until someone commits them. Any other non-zero status
still fails the unit and shows in `systemctl --user status`: 1 is a plain
failure, and 2 and 4 are beads' merge conflict and stuck working set, which
wait for a person.

**Exit 7 is not a failure either.** A host that has just woken from standby can
have no name resolution for a minute or two (`Could not resolve hostname
github.com`). `mw dispatch`'s sync fails on that, and only on that, and waits and
tries it again: `dispatch_sync_tries` times in all (3), `dispatch_sync_wait` apart
(`"15s"`; `MW_DISPATCH_SYNC_TRIES` and `MW_DISPATCH_SYNC_WAIT` answer ahead of the
file). A sync that gets through on a later try dispatches as usual and says `retried
the sync 2 times`. One that never does claims nothing, prints one line, `local
network fault: <what git said>; nothing dispatched`, and leaves with 7, which the
unit lists in `SuccessExitStatus=5 7`, so a sleeping network does not show as a
failed unit and the next tick simply tries again. The tries and the waits together
may not run past 90 seconds (`mw dispatch` refuses a config that would), because
the unit is stopped at two minutes. Every other sync failure is not retried and
fails as before, and `mw sync` by hand never waits. After changing the unit file,
copy it again and `systemctl --user daemon-reload`.

**A second tick while a dispatch is still running does no harm.** A oneshot unit
is never started while it is already running, so ticks do not overlap; the tick
is dropped. The real damper on spending is not the timer but mw's cap: however
often `mw dispatch` runs, it never has more sessions in flight than `cap`
allows. The unit stops a run that takes longer than two minutes
(`TimeoutStartSec`), because a oneshot otherwise waits for ever on a hung `git`
or `bd`.

`Persistent=false`: a host that was asleep or off does not catch up on the ticks
it missed, and is not woken for them. The next tick on the clock is the next
run.

Two more lines in the service are there for a reason. `KillMode=process`:
Builder sessions run in a tmux server that `mw dispatch` starts inside the
unit's control group, and systemd's default would kill the group, sessions
included, the moment `mw dispatch` exits. And `ExecStart=/usr/bin/env mw
dispatch` rather than `mw dispatch`: `env` looks `mw` up on the `PATH` from
`dispatch.env`, which systemd's own lookup would not use.

**Lingering.** A user manager normally runs only while you are logged in, so a
timer needs `loginctl enable-linger <you>` to fire on a host nobody is logged
into. That command needs root (`sudo`), and this rig never runs it for you. Check
first: `loginctl show-user $USER -p Linger` says `Linger=yes` or `Linger=no`. On
the Laptop, under WSL2, systemd was running and the answer was `Linger=yes`
already, so no `sudo` was needed there. Whether a WSL distribution keeps its
user manager up while no terminal is open was not tested: the timer fires only
while WSL itself is running. `scripts/check-timer-units.sh` (in `make lint`)
verifies the two unit files with `systemd-analyze` and starts nothing; it fails
only on what `systemd-analyze` says about the rig's own unit files, and prints
what it says about the host's own user units as ignored.

## The seat's tmux server on the VPS

A host whose seats run as root (the VPS) needs its tmux server itself kept up
by something that does not die with a user manager: `contrib/systemd/system/`
holds `mw-seat-tmux.service`, a *system* unit (no `--user`, no login needed)
that makes sure a tmux server with a session named `0` — the default socket
`infrastructure/tmux/tmux.go` talks to — exists, and does nothing when one
already does. It never kills the server it starts or found: `KillMode=process`
and `ExecStop=/bin/true` mean stopping or restarting the unit leaves every
session inside alone, and `OOMScoreAdjust=-900` makes the kernel spare it
before almost anything else on the box.

Why a system unit and not `systemctl --user`: on the VPS the tmux server used
to live under root's `--user` manager (`user-0.slice`). When the OOM killer
took the manager, systemd killed everything under it, the Mayor's session
included — and with nobody logging in to restart the manager, nothing would
have brought it back. A system unit answers to no user manager.

The same reasoning gives `mw-doctor` a system copy: `contrib/systemd/system/mw-doctor.service`
and `.timer`, the same pair as the `--user` one above (same cadence, same
cures) but run as root (`User=root`) and started at boot, not at login.
Install both with `sh scripts/install-units.sh --system --enable`, run as
root: it links every file under `contrib/systemd/system/` into
`/etc/systemd/system`, `systemctl daemon-reload`s once, and with `--enable`
arms `mw-seat-tmux.service` and `mw-doctor.timer`. Without root it refuses in
one line and changes nothing; it never touches the `--user` directory, and
`--dry-run` says what it would do.

A system unit gets a bare `PATH` too: `contrib/seat.env.example` is a template
for `/root/.config/mw/seat.env`'s one `PATH=` line (where `tmux`, and anything
the seat's own commands need, live), read by `mw-seat-tmux.service` via
`EnvironmentFile=-%h/.config/mw/seat.env` (`%h` is `/root` for the system
manager). A server a login already started is left exactly as it is: the unit
never restarts or replaces one that is already up, so it is not this unit's
server — not OOM-spared by it — until that server dies and the unit starts the
next one. Check a server's actual score with
`cat /proc/<its pid>/oom_score_adj`. The way back: `systemctl disable --now
mw-seat-tmux.service mw-doctor.timer`, then remove the symlinks the install
printed. `scripts/check-timer-units.sh` verifies the three system unit files
with `systemd-analyze` (no `--user`) the same way, and proves
`mw-seat-tmux.service`'s `ExecStart` against a stand-in `tmux`: a no-op when
session `0` is already up, `tmux new-session -d -s 0` when it is not.

## Mail

```sh
MW_SEAT=builder@laptop bin/mw mail send mayor -s "Ready for review" -m "The branch is up."
MW_SEAT=mayor bin/mw mail inbox
MW_SEAT=mayor bin/mw mail read mw-gq6.70
MW_SEAT=mayor bin/mw mail reply mw-gq6.70 -m "Merged, thank you."
bin/mw mail inbox --as governor
```

Mail is beads: a message is a bead of type `mail` (not beads' own `message`
type, which is ephemeral and never syncs), assigned to its recipient, its
subject the title, its body the description, `from` and `to` in its metadata.
It travels between hosts as the rest of the vault does, on `mw sync`: mail sent
in one clone is in the recipient's inbox in the other once both have synced.
This is the shape the stand-in `bin/mw-mail` in the vault writes, so mail
already sent stays readable. The vault must declare the type (`bd config set
types.custom mail`).

Mail is never a story. Every message is assigned to its recipient, carries no
Path, and is not a child of any epic, so none of the queries that offer stories
to a host (`mw dispatch`, `mw next`, `mw status`) returns one;
`features/mail.feature` runs those queries against a real database holding
mail, to keep it so.

- **`MW_SEAT`** says who a session is, as `seat` or `seat@host` (a session
  started by `mw dispatch` carries it). `send` and `reply` are signed by it. With
  it unset they refuse, naming it, and write nothing: mail is never sent as the
  Mayor or as anyone else by default. `inbox` and `read` take their mailbox from
  it too, or from `--as <seat>`, which stands in for it.
- **`send <to> -s <subject> [-m <body>]`** files a message to a seat (`mayor`,
  `builder`, `governor`, or any seat) and prints its id and who it went to. A
  message with no body says so.
- **`inbox`** lists the unread mail of `$MW_SEAT`, or of the seat `--as` names,
  oldest first: id, who it is from, when it was sent, its subject.
- **`read <id>`** prints a message's from, to, date, subject and body and closes
  it, which takes it out of the inbox. Reading it again prints it again. It
  refuses a bead that is not mail.
- **`reply <id> [-m <body>]`** sends to whoever sent message `<id>`, with the
  subject `Re: <subject>`, linked to the original by a `related` dependency (the
  kind `bin/mw-mail` used) and leaving the original as unread as it was. An id
  that is not mail is a plain error, naming it, and nothing is written.

`bd mail ...` is bd's own command for this and does nothing until it is told
where mail is kept. Point it at `mw mail`, and `bd mail inbox`, `bd mail send`,
`bd mail read` and `bd mail reply` keep working, now as `mw mail`:

```sh
bd config set mail.delegate "mw mail"
```

That is a setting of each host's own beads database, and a hand step: no Builder
changes it. `mw` has to be on the `PATH` of whoever runs `bd mail` (the binary
`make build` leaves is `bin/mw`; link or copy it somewhere on the `PATH`). The
setting can also come from `$BEADS_MAIL_DELEGATE`, which bd checks first. Until
it is changed, the delegate is the stand-in script, which writes the same beads.
See `features/mail.feature`.

`mw next` is a sender too: when it closes a story out it mails `mayor`, from
`mw@<host>`, one message titled `Landed: <story title>`, `Refused: <story title>`
(the checks turned the branch away) or `Blocked: <story title>` (anything else
stopped it). The body is the report `mw next` prints, and for a story that landed
nothing the whole reason too. A mail that cannot be sent is said on stderr and
changes nothing about the landing; a run that finds nothing to close sends none.
See `features/next.feature`.

## Telling the Mayor's window when mail arrives

A Mayor's own mail watcher dies with its session, so a report from the other
host can sit unread — and when nothing arrives at all, nothing wakes the
Mayor, which at night is when it matters most. `contrib/mail-notify` is a small
script that a second `systemd --user` timer runs every minute
(`mw-mail-notify.timer`, running `mw-mail-notify.service`, both in
`contrib/systemd/`). It reads no mail and starts nothing: at most it types two
fixed-shape lines into the live Mayor's tmux window, and Enter after each.
Each tick it skips everything if the 1-minute load is above 2.0; runs `mw sync`
at `nice 19` and idle I/O priority if the last was five minutes ago or more;
lists `bd mail inbox` and compares its ids with the ones it has announced; and
runs `mw nudge` (below), reusing the same synced beads. For new mail, it types
`New mail for mayor: <n> message(s). Run bd mail inbox.`; for whatever `mw
nudge` still has to say once its own per-condition hourly damper is applied,
`Quiet alarm for mayor: <clause>[; <clause> ...]. Run mw status.` — either or
both, only when the pane is not working (no `esc to interrupt`) and its input
line is empty. Otherwise it does nothing and tries again next tick, and it
records the ids as announced, and each clause's condition as fired, only after
each is typed. It never clears an input line, and a second tick while one runs
does nothing (`flock`).

`mw nudge` is the zero-token, read-only use case behind the second line: it
reads this host's claimed stories for one running longer than
`nudge_after_minutes` (default 60) with nothing landed, refused or blocked
mailed to the Mayor about it since it was claimed, and every other host a
story is pathed to for one whose last recorded sync, as this host last heard
it, is older than `nudge_sync_stale_minutes` (default 20) — both in
`~/.config/mw/config.toml` (*What a host is told*). It writes nothing: no
story is claimed, no note is left, no mail is sent. See `application/nudge.go`.

The window is found from the vault's `.mayor-acting`, free text the Mayor
writes: a window id (`@12`), the window's name (`mayor-2026-09-19-10`), or
`window 3`. If it names no one window, nothing is typed.

**Install**, once per host: `sh scripts/install-units.sh --enable mw-mail-notify`
(see *Running a host on a timer*). It prints one hand step for this pair, the
`ln -s` that puts `contrib/mail-notify` on `~/.local/bin` as `mw-mail-notify`.

The service reads the same `~/.config/mw/dispatch.env` as the dispatch timer for
its `PATH`, which must reach `mw`, `bd`, `tmux`, `flock` and `~/.local/bin`; and
the vault from `~/.config/mw/config.toml`. Its settings (`MW_MAIL_MAILBOX`,
`MW_MAIL_LOAD_LIMIT`, `MW_MAIL_SYNC_EVERY`, `MW_MAIL_STATE_DIR`,
`MW_TMUX_SOCKET`) go in an optional `~/.config/mw/mail-notify.env`, as `NAME=value`
lines; the script's header lists them. A tmux server other than the default is
named with `MW_TMUX_SOCKET`.

**Undo it**:

```sh
systemctl --user disable --now mw-mail-notify.timer
rm ~/.config/systemd/user/mw-mail-notify.service ~/.config/systemd/user/mw-mail-notify.timer ~/.local/bin/mw-mail-notify
systemctl --user daemon-reload
```

`disable --now` alone stops it at once. Its output is in the journal:
`journalctl --user -u mw-mail-notify`. The ids it has announced are in
`~/.local/state/mw-mail-notify/announced`; deleting that file makes it announce
whatever is unread again. `contrib/mailnotify_test.go` runs it against a private
tmux server with stand-ins for `bd` and `mw`, and `scripts/check-timer-units.sh`
verifies the units with `systemd-analyze` and starts nothing.

## A host's health line

Every 15 minutes a third `systemd --user` timer writes **one line** to
`~/.mw-health` saying whether the host, and the Mayor on it, are alive and well.
`contrib/health/mw-health.sh` is a POSIX shell script; `mw-health.timer` runs
`mw-health.service` (both in `contrib/systemd/`). It spends no tokens and it only
**looks**: it never restarts, stops or ends anything, never types into tmux,
never syncs, and writes nothing but that one file (made whole under a temp name
and moved into place, so a reader never sees half a line). Something else, on
this host or the other, reads the file.

```
2026-09-19T09:15:02Z load1=0.50 mem_avail_mb=1000 swap_used_mb=10 disk_pct=42 services=blog:up,api:up mayor=alive acting=match context=1000/180000 last_sync_age_s=300 syncs_running=0 verdict=ok
```

| Field | Means |
| --- | --- |
| `load1` | the 1-minute load average |
| `mem_avail_mb`, `swap_used_mb` | memory available, and swap in use, in MB |
| `disk_pct` | how full the filesystem holding `~` is |
| `services` | `systemctl is-active` for each unit in `MW_HEALTH_SERVICES` (the Governor's blog services on the VPS): `name:up` or `name:down`; `none` when the list is empty |
| `mayor` | `alive` when the tmux window named in the vault's `.mayor-acting` exists and holds more than a bare shell; `gone` when it does not; `none` with no `.mayor-acting` |
| `acting` | `match` when that window exists, `mismatch` when the file names no window that does, `none` with no file. A missing window is both `mayor=gone` and `acting=mismatch` |
| `context` | the Mayor's context as `<n>/<limit>`, from `mw seat context` when `mw` has it; otherwise `unknown` |
| `last_sync_age_s` | seconds since this host's `host.<host>.last_sync` note in beads, which `mw sync` writes; `unknown` if there is none |
| `syncs_running` | how many `mw sync` processes are running right now |
| `verdict` | `ok`, or `unwell:` and the reasons, comma-separated |

A reading the host cannot give is `unknown` and never makes it unwell. The
reasons, with the limits they use (all set at the top of the script, and
overridable in the unit's environment):

| Reason | When | Limit |
| --- | --- | --- |
| `load1` | above | `MW_HEALTH_LOAD1_MAX`, 4 |
| `mem_avail_mb` | below | `MW_HEALTH_MEM_AVAIL_MIN_MB`, 60 |
| `disk_pct` | above | `MW_HEALTH_DISK_PCT_MAX`, 90 |
| `service_down:<name>` | any service is not active | |
| `mayor_gone` | `mayor=gone` | |
| `acting_mismatch` | `acting=mismatch` | |
| `context_over_limit` | context above the limit `mw` gives | |
| `last_sync_stale` | `last_sync_age_s` above | `MW_HEALTH_SYNC_AGE_MAX_S`, 3600 |
| `syncs_running` | above | `MW_HEALTH_SYNCS_RUNNING_MAX`, 1 |

**Install**, once per host: `sh scripts/install-units.sh --enable mw-health`
(see *Running a host on a timer*); nothing here needs `sudo`. It prints one hand
step for this pair, the `ln -s` that puts `contrib/health/mw-health.sh` on
`~/.local/bin` as `mw-health`. `~/.config/mw/health.env` is optional,
`NAME=value` lines.

The service reads the same `~/.config/mw/dispatch.env` as the dispatch timer for
its `PATH`, which must reach `mw`, `bd`, `tmux`, `systemctl` and
`~/.local/bin`; the vault and host come from `~/.config/mw/config.toml`. Its
settings go in the optional `~/.config/mw/health.env`; the script's header lists
them. On the VPS, `MW_HEALTH_SERVICES="blog api"` names the blog's units, which
are looked up as system units. To try it first, run `mw-health` by hand and read
`~/.mw-health`.

**Undo it**:

```sh
systemctl --user disable --now mw-health.timer
rm ~/.config/systemd/user/mw-health.service ~/.config/systemd/user/mw-health.timer ~/.local/bin/mw-health ~/.mw-health
systemctl --user daemon-reload
```

`disable --now` alone stops it at once and leaves the last line where it was.
Its own output is in the journal, `journalctl --user -u mw-health`, and there is
normally none. `scripts/check-health.sh` (in `make lint`) runs the script against
stand-in commands, so it never reads the real host, and asserts the line for a
well host and for each of the nine reasons; `scripts/check-timer-units.sh`
verifies the units with `systemd-analyze` and starts nothing.

## Seeing what a host is doing

```sh
bin/mw status
```

`mw status` takes no arguments and reads the vault, the tracker, the runner's
session names and the Builder's ledger. It writes to none of them. It is the report for a phone: what this host is doing right now, in
five parts, and two more when there is something to say.

- **RUNNING** — the stories this host has claimed, each with the name of the
  tmux session to attach to. A story recorded `run=stopped` or `run=stuck` says
  `NOT RUNNING` rather than pretending, and a claimed story whose poured formula
  still has a step open says its close-out is blocked.
- **READY** — what this host could take now.
- **WAITING FOR THE GOVERNOR** — the ready or claimed stories labelled `hitl`,
  worked with the Governor present. They are listed here and not under RUNNING
  or READY, and the heading is left out when there are none.
- **BLOCKED** — what is waiting, each with only the work it is still waiting
  for: a wait that has finished is not listed.
- **OTHER HOSTS** — every other host a story is pathed to, and what it holds.
- **TICKS** — how this host's timers are doing, counted from the logs they
  keep: for `mw dispatch` and for the Millhand's tick, when the last good run
  was and how many runs since have failed (`failed 3 in a row`). A local network
  fault is counted apart (`7 local network faults, 0 failed`), and a good run
  resets both. A host that keeps neither log prints no section, and one that
  keeps only one prints that one. See below.
- **RIG MEMORY** — one line per rig whose Builder memory
  (`seats/builder/rigs/<rig>.md`) is larger than `rig_memory_bytes`, 8000 by
  default: `millwright 8412/8000 bytes: prune (Mayor)`. Every Builder reads that
  file at boot, so its size is fuel paid on every story, and this is what tells
  the Mayor to prune it. The section is left out when no rig is over, a rig with
  no memory file is not an error, and the archive a memory is pruned into
  (`<rig>-archive.md`) is never counted.
- **FUEL today** — the tokens the Builder's ledger charged on lines dated today,
  and 0 when the seat has no ledger yet. A session's fuel is charged by one line
  only: a story `mw next` closes out again (refused, then refused or landed) gets
  a second line for its outcome that says the fuel was already charged and holds
  no token figure.

A story filed with only its overrides is shown with the epic's defaults filled
in. The command only reads: no claim, no write to a bead, no note, no ledger
line and no session is started, so it costs no tokens. Every line fits 60
columns; a longer title is cut short with an ellipsis rather than wrapped.

The OTHER HOSTS part is the whole of the factory's safety net for a host that
has gone quiet. There is no failover: a host that stops syncing does not hand
its work back. Each host is shown with when it last recorded itself level (the
`host.<name>.last_sync` note `mw sync` leaves, as this host last read it), and
the stories pathed to it that are ready or already claimed. A host is **ASLEEP** when that note is older than `host_silent_hours` — two by default,
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

**TICKS.** A timer whose runs fail looks, from outside, like one that works, so
each timer keeps a log. `mw dispatch` appends one dated line to
`~/.local/state/mw-dispatch/log` for every run it makes, the failed ones too
(none for `--dry-run`): `ok: <n> started`, `ok: nothing ready`,
`local network fault`, or `failed: <one-line reason>`. `mw millhand tick` keeps
`~/.local/state/mw-millhand-tick/log` the same way. Each log is this host's own
and is cut to its last 500 lines. `mw status` counts them, newest first, back to
the last good run. A tick counts as failed when its line says it could not look
for the Millhand's window or tell whether it is needed, or that its wake or its
sync failed; as a local network fault when its watch found this host's own
network down.

The same counts go to the other host: every sync that gets level leaves them in
`host.<name>.ticks`, beside `last_sync` and pushed by the same `bd sync`, and
OTHER HOSTS shows them under that host — `dispatch · last good … / failed 12 in
a row` — without a tunnel to it. A host that keeps no log leaves no note. Nothing
wakes anyone on a count: it is only shown.

`mw status` only ever says this. Re-pathing a story is a person's act, never a
report's. The config keys it reads are `vault`, `host`, `host_silent_hours` and
`rig_memory_bytes` (*What a host is told*). See `features/status.feature`.

## Briefing a seat from a bead

```sh
bin/mw brief mw-6ww mw-gq6 mw-0om
bin/mw brief mw-6ww --comments 2
```

`mw brief` takes one or more epic ids and prints, for each and in the order
given, what a booting seat still needs of it, and none of what `bd show` would
also print. The `bd show` of a map is the whole description, every closed child
and every comment: about 12K of a Mayor's boot. `mw brief` prints:

- one line, the epic's title, id, status and priority (`The map · mw-6ww · open · P1`);
- its children that are not closed, under **In progress**, **Open** and **Held**
  (status `deferred`), a heading left out when nothing is under it. Each line
  carries the child's title, id and priority, the host and model its path names
  when it names them, and `waits on <title>` for each blocker that is not
  finished, whether the blocker is in this epic or another. A blocker that is
  closed is not named. A title is cut to 60 columns, and a blocker's to 24, with
  an ellipsis: the id finds the rest, and three maps stay under about 5000
  bytes;
- one line, `N closed`, counting the children left out.

With `--comments N` the newest N comments of each epic follow, oldest of them
first, each in full and never truncated: they hold the Governor's words. There
is no description. Output is plain text on standard output, written for a phone.

An id the tracker does not have is an error naming it, and nothing is printed,
not even the briefs of the ids before it. The command only reads: it writes
nothing to beads. It reads the vault and host from the config file, as `mw
status` does. See `features/brief.feature`.

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
  before it is called that. The clock starts at the claim when bd says when that
  was, and at sweep's first look when it does not; a session whose output has
  changed since the last sweep has its clock reset.
- A stuck story is commented on once, saying what was found, and recorded
  `run=stuck`, which `mw status` then shows as `NOT RUNNING`. A story that
  `mw next` or an earlier sweep already recorded gone is left alone, so sweeping
  twice comments once.

The threshold is `stale_hours` in the config file or `MW_STALE_HOURS`: whole
hours, at least 1, two by default. Along with `vault` and `host`, that is all
`mw sweep` reads from the config.

The only things sweep writes are those comments, `run=stuck` on the story's own
bead, and one note per claimed story in bd's key-value store (`sweep.<id>`: a
fingerprint of the session's output and when it was first seen), which is how a
sweep with no daemon remembers anything between runs. The memory is a note, not
state on the story, because every `bd set-state` files a closed event bead that
syncs to the other host, and a sweep every few minutes would file two per story
per pass; `run=stuck` is a state because that one is an event worth keeping, and
the note is cleared when it is recorded. Sweep never kills or restarts a session,
never gives a claim back, and never touches a worktree, git or the ledger:
settling a stuck claim is a separate command. One story's trouble — a session
that cannot be asked about, a write that fails — is reported on a `!` line and
the rest are still examined. See `features/sweep.feature`.

## Asking a seat how full its session is

```sh
bin/mw seat context
```

`mw seat context` prints one line, and takes no arguments beyond `--dir`:

```
context=142307 handoff_at=180000 ok session=0a1b2c3d
```

`context` is the size of the context the session's last assistant turn was
given: its input, cache-read and cache-creation tokens added together. It is
read from the newest transcript (the most recently written `*.jsonl`) Claude
Code keeps for a working directory under `~/.claude/projects`: the vault from
the config file unless `--dir` names another directory. A subagent's turns do
not count, only the session's own last one. `handoff_at` is the limit, and the
word after it is `ok` below the limit and `handoff` at it or above: a session
that reads `handoff` should finish its step, write what the next session needs
and hand off. `session` is the first eight characters of the session's id.

Both `ok` and `handoff` exit 0: being full is a finding, not a failure. A
directory with no transcript, or a transcript with no assistant turn yet, is an
error that names the directory it looked in. It costs no tokens, starts no
session and writes nothing.

The limit is `handoff_at` in the config file or `MW_HANDOFF_AT`: whole tokens,
at least 1, 180000 by default. See `features/seat_context.feature`. `mw seat` is
the parent for the commands about a seat's own session; later ones sit beside
`context`.

## Starting a seat's next session

```sh
bin/mw seat up mayor --effort high --reason "the handoff is written"
```

`mw seat up <seat>` starts one interactive Claude Code session for the seat, in
a new tmux window of the session `mw` is running in — or of a detached session
named `mw-seats`, when `mw` is running outside tmux. The window is named
`<seat>-<UTC date>-<nn>`, with `nn` one past the highest number the seat has
used for a handoff or for a window already open, and the line it prints names
it:

```
started the mayor seat in the window mayor-2026-09-19-13, booting from seats/mayor/handoffs/2026-09-19-12.md
```

The session is primed with the seat's charter, by path, and is told the seat's
own kickoff text if it keeps one, or a default that sends it to its charter and
its newest handoff. Either way `mw` appends the newest handoff to boot from and
the `--reason` it was started for. It runs in the vault, with `MW_SEAT` set to
the seat so that its mail is signed and never defaulted, at `--model` and
`--effort` when they are given, in `auto` permission mode. Nothing else the
vault holds is read or passed: no ledger, no memory of a rig, no prime from the
tracker — a session that wants any of it can read it itself, and a session
primed with it pays for every line at boot.

Handoffs are read from `seats/<seat>/hosts/<host>/handoffs/` for a seat that
keeps a directory for this host, and from `seats/<seat>/handoffs/` for one that
does not; the newest by name is the one it boots from.

Nobody is assumed to be watching, so the session never hangs on a permission
prompt. It stays in `auto` mode, where the classifier and the allow rules decide
what they can; but its `--settings` also carry a `PermissionRequest` hook that
answers *deny* to whatever would still have been asked of a person, with a
message saying nobody is watching. The denial is in the session's transcript,
and the session goes on with what it may do; it is not asked, and nothing waits
for an answer. (`--permission-prompts none`, which a Builder gets, only works
with `--print`, and a seat's session is interactive.) The Millhand's wake, which
is `mw seat up millhand` run by a timer, is always unattended.

`--attended` is for the seat you bring up by hand to talk to: it leaves the hook
out, and the session asks about a permission as Claude Code does. Only a person
passes it; no timer or wake ever does. The hook JSON follows Claude Code's hooks
reference, <https://code.claude.com/docs/en/hooks> (`PermissionRequest`: the
`hookSpecificOutput` with a `decision` of `behavior` `deny`), and the permission
modes reference, <https://code.claude.com/docs/en/permission-modes>.

It refuses, starts nothing and exits non-zero in three cases: the seat has no
charter, so there is nothing to boot into; it has written no handoff, so there
is nothing to boot from; or it is already acting — its acting file names a
window that is still open, and no handoff has been written since that window
was opened. The acting file (`.<seat>-acting` in the vault, host-local and
untracked) is never written by `mw`: the new session writes it at boot, and
that is the hand-over signal.

Run from a tmux window the acting file names, it also arms the reaper below on
that window, so the outgoing session's window closes itself once the
successor has the seat. `--reap-when-idle` arms one in idle mode on the window
it opens, for a session that will hand over to nobody — the Millhand's wake.
Neither is armed when `mw` runs outside tmux or from a window the acting file
does not name. `$MW_TMUX_SOCKET` names another tmux server than the default
one, as tmux's `-L`; it is for tests.

See `features/seat_up.feature`, and `infrastructure/tmux/window.go` for the
windows themselves.

## Closing a finished session's window

```sh
bin/mw seat reap mayor --window @3
bin/mw seat reap millhand --window @7 --when-idle
```

`mw seat reap <seat> --window <tmux window id>` is a detached, zero-token
watcher on one window: it looks every 30 seconds (`--interval`) and closes the
window when its session is finished. A session ends its own window; no session
ever closes another's. `mw seat up` starts it, and it can be run by hand.

- **Successor mode**, the default: it closes the window once the seat's acting
  file names someone else than it did when the watch was armed, the window that
  someone is named after is open, and the pane is idle.
- **Idle mode**, `--when-idle`, for a session with nobody to hand over to: it
  closes the window once a handoff newer than the window exists and the pane is
  idle. A window nothing can date counts as opened when the watch was armed.

Idle means an empty input line with nothing running, on two looks in a row. A
window whose input line holds text is never closed, in either mode: someone
may be typing. Only the tmux adapter reads a pane.

It gives up after `--limit` (3 hours), closing nothing and saying so, and
exits non-zero. Arming, closing, giving up, and finding the window already gone
each append one dated line to `.<seat>-reaper.log` in the vault:

```
2026-09-19T12:22:59Z reap @3: armed; waiting for .mayor-acting to name a successor whose window is open, and an idle pane
2026-09-19T12:24:29Z reap @3: closed: the seat is held by someone else, and the pane was idle
```

It replaces the vault's `bin/mayor-reap-window`, which did the same in bash.
See `features/seat_reap.feature`, and `infrastructure/tmux/reap.go` for how a
pane is read and a window closed.

## Waking the Millhand

```sh
bin/mw millhand --wake routine --reason "the mail timer saw a message"
bin/mw millhand --wake review
bin/mw millhand                     # by hand: the Governor is here
```

`mw millhand` brings up this host's Millhand: `mw seat up millhand` with
everything a wake settles already settled, so that a timer has one command to
run. There are two kinds of wake, each on its own model, and a third for when
the Governor is here (`--wake hand|routine|review`, `hand` by default):

| Wake | Model (config key) | Effort |
| --- | --- | --- |
| `hand`, `routine` | `millhand_routine_model`, `sonnet` | high |
| `review` | `millhand_review_model`, `opus` | high |

The models are fuel knobs: set them in the config file (see *What a host is
told*) or with `MW_MILLHAND_ROUTINE_MODEL` and `MW_MILLHAND_REVIEW_MODEL`. Every
wake arms the idle reaper (`--reap-when-idle` of `mw seat up`), so the window
closes itself once the Millhand has handed off. The kickoff is told the kind of
wake and the `--reason` for it, after the newest handoff to boot from. For a
review wake it is also told what `seats/millhand/hosts/<host>/review-since`
holds, when that file exists, and nothing about a mark when it does not. `mw`
only reads the mark: the Millhand moves it in its handoff.

If a window named `millhand-*` is already open, it starts nothing, says so in
one line and leaves with status **5**, so a timer can tell "already up" from a
failure (1):

```
Error: the Millhand is already up in the window millhand-2026-09-19-03: nothing was started
```

See `features/millhand.feature`.

### The routine timer's check: `mw millhand tick`

```sh
bin/mw millhand tick             # what a timer runs every 15 minutes
bin/mw millhand tick --dry-run   # say what it would do, start nothing
```

```
2026-09-19T15:45:00Z quiet
2026-09-19T16:00:00Z woke the Millhand: 1 unread message: "Please look at the queue"; 1 stuck story: "Teach the cat to sit" (mw-x.1)
```

A tick costs no tokens unless something needs the Millhand, so it is safe to
run every 15 minutes for ever. It looks in this order:

1. A window named `millhand-*` already open: it says `already up` and stops,
   unless that Millhand is finished. Finished is the rule `mw seat reap
   --when-idle` closes by: it wrote a handoff after its window opened, and its
   pane is idle at an empty input line on two looks in a row, 30 seconds apart
   (`tick_recheck_seconds` in the config file, or `MW_TICK_RECHECK_SECONDS`; 0 is
   no wait). Then the tick closes the window, adds one line to
   `.millhand-reaper.log` in the vault (`closed by the tick: finished at <handoff
   time>, window left open`), says so in its line (`closed the finished
   Millhand's window …`), and goes on as if no Millhand were up, so the same tick
   may wake a fresh one. This heals a window left open when its reaper died, gave
   up or slept through it. A Millhand whose wake never got going — its pane idle
   on the same two looks and no handoff written since its window opened, as when
   its first turn died on an API error — is restarted: the tick closes the window
   (`closed by the tick: up but idle since <opened>, no handoff` in the reaper
   log) and ends with a wake whatever else there is to wake for, telling the
   fresh Millhand why; its line says `restarted: up but idle since <opened>, no
   handoff`. A window whose input line holds text, or whose pane is working, is
   never closed: `already up`. A look the terminal cannot answer counts as not
   finished. `--dry-run` says `would close` or `would be restarted` and closes
   nothing.
2. With a `[watch]` table in the config file (see *Watching a host*), it applies
   `mw watch`'s rule to the host it watches. This comes before the sync, because
   a fault of this host's own network is one the sync would only time out on:
   `local-fault` is said in the line, wakes nobody, and the sync is skipped. The
   rest of the tick looks on this host all the same.
3. It runs one `mw sync`. A sync that fails is said in the line, and the tick
   looks on this host all the same.
4. Need is unread mail for `millhand@<host>` or plain `millhand`, a story
   `mw sweep` newly finds stuck on this host (sweep reports each story once), a
   watched host that is `unwell`, `stale` or `down`, or a `doctor.<check>` note
   `mw doctor` has newly written or changed. `ok` and `unreachable-once` are no
   need. The mail is only listed: it stays unread until the Millhand reads it.
5. No need is `quiet`. Need is ONE routine wake, as `mw millhand --wake
   routine` does it, whose reason names the mail subjects and the stuck story
   titles, five of each and then a count, the watch line verbatim: `mw watch
   says: unwell load1,mayor_gone`, and every doctor note, naming the check and
   its text verbatim, with the standing instruction to run `mw doctor <check>`
   by hand, read the log, and report. When the host is `down`, or `unwell` with
   `mayor_gone` among its reasons, the reason ends with the charter's one
   exception: *If the Mayor's process is gone and no handoff is under way you
   may run the one respawn command on the VPS.*

A doctor note is woken for once: the tick remembers, per check, the text of
the note it last woke for, and a note whose text has not changed since is not
woken for again. A fresh one — a new fault, or the same one again after the
check went ok and its note cleared — is.

It prints one dated line and appends it to
`~/.local/state/mw-millhand-tick/log` on this host, which is cut to its last
500 lines; nothing else it keeps grows. It leaves with 0 for everything but a
fault of its own: a wake that could not be started, or mail, stuck stories,
doctor notes or a watch that could not be looked at when nothing else called
for a wake (that is not `quiet`). `--dry-run` starts nothing and writes no log
line, and it runs neither the sweep, the watch nor the doctor note look-up,
because each records what it finds (a sweep the stories it calls stuck, a
watch its first failed check, the tick its own memory of a doctor note woken
for) and would leave nobody to wake for it. With no `[watch]` table the tick
does not consult `mw watch`.
See `features/millhand_tick.feature`.

### Waking the Millhand by timer

Two `systemd --user` timers in `contrib/systemd/` wake this host's Millhand, and
each needs the same `~/.config/mw/dispatch.env` as the dispatch timer (see
*Running a host on a timer*) so that `mw` is found:

| Timer | Runs | When |
| --- | --- | --- |
| `mw-millhand-tick.timer` | `mw millhand tick` (`mw-millhand-tick.service`) | at 7, 22, 37 and 52 minutes past every hour; no catch-up |
| `mw-millhand-review.timer` | `mw millhand --wake review --reason timer` (`mw-millhand-review.service`) | 07:30 and 19:30, local time; catches up |

**The tick** is the routine timer above: it costs no tokens unless the mail, the
sweep or the watch calls for a wake. Its minutes are off the dispatch timer's
and the health timer's, so the three do not start together. `Persistent=false`,
as for those: a host that was asleep does not run the ticks it missed.

**The review** always wakes the Millhand, on the review model, and spends that
fuel twice a day. `Persistent=true`: a review missed while the Laptop slept runs
once when it wakes, not once for every one missed. If a Millhand is already up,
`mw millhand` leaves with status 5; the unit has `SuccessExitStatus=5`, since
"already up" is a wait and not a failure. Both services have `KillMode=process`,
so the wake they start outlives the command that started it.

**The cadence is a fuel knob.** Change it with a drop-in, never by editing the
shipped unit; a change to the copy in `contrib/systemd/` is a change for every
host, and `scripts/check-timer-units.sh` fails on it. To wake the review once a
day:

```sh
systemctl --user edit mw-millhand-review.timer
```

```
[Timer]
OnCalendar=
OnCalendar=*-*-* 07:30
```

The empty `OnCalendar=` clears the shipped times first. The same works for the
tick's timer.

**Install**, once per host: `sh scripts/install-units.sh --enable
mw-millhand-tick mw-millhand-review` (see *Running a host on a timer*); nothing
here needs `sudo`. Name one and not the other if that is what the host wants.
`systemctl --user list-timers` says which are armed and when each next runs.

**Undo it**:

```sh
systemctl --user disable --now mw-millhand-tick.timer mw-millhand-review.timer
```

That stops any further wake at once and leaves a Millhand already up alone: it
is a tmux window, not the unit's to kill. Remove the four files from
`~/.config/systemd/user/` and run `systemctl --user daemon-reload` to take the
units away altogether.

**Lingering.** A user timer runs only while your user manager does, which is
while you are logged in, unless `loginctl enable-linger <you>` has been run
(`loginctl show-user $USER -p Linger` says which). Without it these timers do
not fire on a host nobody is logged into. That command needs root, and this rig
never runs it for you.

Each unit's own output is in the journal: `journalctl --user -u
mw-millhand-tick` and `-u mw-millhand-review`. `scripts/check-timer-units.sh` (in
`make lint`) verifies the four units with `systemd-analyze` and starts nothing.

## Watching a host from one that can lose its network

```sh
bin/mw watch
```

`mw watch` is for a host that sometimes loses its own network (the Laptop) and
so has to tell a fault of its own from a fault of the host it watches (the VPS).
It prints **one line**, appends the same line, dated, to
`~/.local/state/mw-watch/log`, and reads and writes nothing else but the state it
keeps in that directory. It takes no arguments and is run on a timer.

The watched host is named by a `[watch]` table in the config file:

```toml
[watch]
ssh     = "vps"                       # the name ssh knows it by
host    = "vps"                       # its name in beads
outside = ["https://one.example", "https://two.example"]
blog    = "https://blog.example.com"  # optional
```

The rule has three cases:

1. **Neither outside place answers**: `local-fault`. Nobody is woken and what
   `mw watch` remembers is left as it was.
2. **An outside place answers and ssh answers**: it reads the host's health line
   (see *A host's health line*) with exactly `cat ~/.mw-health`, `BatchMode` and a
   10 second timeout. The text is opaque but for its leading UTC timestamp and its
   trailing verdict: `ok`, `unwell <reasons>`, or `stale` when the timestamp is
   older than 40 minutes, is not a time, or there is no line. Whatever ssh
   answers, the count of failed checks (case 3) is forgotten.
3. **An outside place answers but ssh fails**: it looks for signs of life, the blog
   answering over HTTPS and the host's `last_sync` note in the local beads (the
   one `mw status` reads for OTHER HOSTS) fresher than 30 minutes. The first such
   check says `unreachable-once signs=<blog,sync,none>` and is remembered; the
   host is `down signs=<...>` only when a second failed check comes 3 minutes or
   more after the first.

| Line | Leaves with |
| --- | --- |
| `local-fault`, `ok`, `unreachable-once signs=...`, `nothing to watch` | 0 |
| `unwell <reasons>`, `stale`, `down signs=...` | 6 |

**6** means a wake is called for; 1 is still a failure, such as an unwritable
state directory. With no `[watch]` table the line is `nothing to watch`. A table
that does not say `ssh`, `host` and `outside` is refused. The memory (the time of
the first failed check) is `memory` beside the log. See `features/watch.feature`.

## mw doctor

```sh
bin/mw doctor              # every check
bin/mw doctor daemon-reload
bin/mw doctor --dry-run
```

`mw doctor` cures this host's known faults offline and without AI: a table of
checks, each the same shape — a **probe** that only reads and reports ok,
faulty (with a reason) or cannot-tell; a **cure** run only when the probe says
faulty and the **damper** allows it (a minimum wait between two cures and a cap
on how many one fault episode may spend before it gives up and waits for a
person); and a **way back**, printed by `--dry-run` and written to the log
beside every cure. An episode ends, and the count resets, the next time the
probe says ok. With no check named it works the whole table; named
(`mw doctor daemon-reload`), only that one.

Every run appends one dated line per check to `~/.local/state/mw-doctor/log`:
`<time> <check> <ok|cannot-tell|cured|damped|cure-failed> <reason>`, the way
back beside every cure. `--dry-run` prints what a faulty check would do and
changes nothing: no cure runs, no state is written, no log line is appended.
It leaves with 0 when every check is ok or cured, and 6 when any check is left
faulty and uncured (damped, or its cure failed), so a timer's journal shows it.

`mw doctor` never calls AI, sends mail or raises a push notice itself: a
check that cannot tell, or is cure-failed a second time in its episode, or is
damped because its episode hit the cap, gets a beads note of its own,
`doctor.<check>`, holding the time, the verdict, the reason and its last 3
log lines; a check back to ok has its note cleared. `mw millhand tick` is
what wakes the Millhand for one, once per note, telling it to run `mw doctor
<check>` by hand and read the log. The doctor may be offline when it tries to
write or clear a note: that failure is logged as `note-failed` and changes
nothing else; the next run retries.

The `[doctor]` table of the config file says which units **daemon-reload**
asks about, which hosts **wifi** and **tunnel** try to reach, where its
powershell.exe sign is, what unit and command **tunnel** restarts and runs,
and where state is kept:

```toml
[doctor]
units        = ["mw-dispatch.service", "mw-millhand-tick.service", "mw-millhand-review.service", "mw-doctor.service"]
state_dir    = "/root/.local/state/mw-doctor"   # default: ~/.local/state/mw-doctor
reach        = ["api.anthropic.com:443", "github.com:443"]
powershell   = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
tunnel_unit  = "reverse-tunnel.service"         # default; the unit tunnel restarts
tunnel_host  = "vps"                            # default: the [watch] table's host
tunnel_probe = "ss -ltn sport = :2222"          # default; run over ssh on tunnel_host
```

**daemon-reload** asks `systemctl --user show <unit> -p NeedDaemonReload` for
every unit and is faulty when any says yes; its cure is `systemctl --user
daemon-reload`, its damper 10 minutes with a cap of 3, and its way back "none
needed: daemon-reload is idempotent" (running it again, by hand or later, costs
nothing). A unit systemd has never loaded reads cannot-tell, not faulty.

**wifi** cures the fault behind mw-6ww.9: Wi-Fi stayed associated while DNS
and every TCP connection from WSL failed for six hours, cured in 30 seconds by
a manual Wi-Fi reconnect. Its probe resolves and TCP-connects (5 s each) to
`reach`'s hosts, ok if any answers; unreachable is only faulty once this
check's own record of when it first looked down is 5 minutes old or more —
before that it logs "faulty (waiting 5m)" and cures nothing, so one blip does
not bounce the network. Its cure reads the associated network's name (its
SSID) with `netsh wlan show interfaces`, refusing (joining nothing) if the
interface is not associated with one; then `netsh wlan disconnect`, a 3 s
settle, `netsh wlan connect name=<ssid>` — the same network, never another —
and up to 20 s giving the rejoin a chance before it returns. Its damper is 30
minutes with a cap of 3 per outage, and its way back is that same `netsh wlan
connect name=<ssid>` command, read fresh so it names whatever network is
actually associated. `powershell` is not run; its presence is only this
check's sign that the host is Windows-backed at all — absent, the check reads
cannot-tell, inert, on every run.

**tunnel** cures the fault behind the Laptop's reverse ssh tunnel to the VPS
staying dead after standby while its timer said SUCCESS: it runs one `ssh -o
BatchMode=yes -o ConnectTimeout=10 <tunnel_host> <tunnel_probe>`, faulty when
the listener it prints is absent, ok when present, and cannot-tell when ssh
itself fails to get through — the VPS being unreachable is `mw watch`'s
concern, not this check's. When the internet itself looks down (checked the
same way **wifi** does, over `reach`) that is cannot-tell too, so a dead
internet is never blamed on the tunnel. Its cure is `systemctl --user
restart <tunnel_unit>`, a 15 s settle, then a re-probe; its damper is 15
minutes with a cap of 3, and its way back is `systemctl --user stop
<tunnel_unit>`. It never touches the VPS beyond that one read-only ssh call,
never edits the unit, and never uses sudo.

**timers** cures a factory timer left stopped by the rig's own machinery
(a hand `systemctl --user stop`, a reinstall that missed its timer, a
daemon-reload that dropped one) and so gone silent, watching nobody: for each
`units` entry whose matching `<name>.timer` is enabled here at all (a timer
not installed on this host is skipped, not faulted) but not active, its cure
is `systemctl --user start <timer>`, its damper 30 minutes with a cap of 3,
and its way back `systemctl --user stop <timer>` for exactly the timers the
cure started.

**Install**, once per host: `sh scripts/install-units.sh --enable mw-doctor`
(see *Running a host on a timer*), which runs `mw-doctor.timer` at 2, 7, 12,
... past the hour — off the dispatch timer's own minutes, so the two never
start together. Unlike the rig's other units the service does not rely on
`PATH`: `ExecStart` names `~/.local/bin/mw` directly, since doctoring a host
whose own environment may be at fault is the point. `scripts/check-timer-units.sh`
(in `make lint`) verifies the pair with `systemd-analyze` and starts nothing.
See `features/doctor.feature`.

On a host whose seats run as root (the VPS), install the *system* copy of
this pair instead — `sh scripts/install-units.sh --system --enable` — so it
answers to no user manager; see *The seat's tmux server on the VPS*, below.

## Heavy work on a small host

The two heaviest things the VPS runs — a Clerk's verification (the rig's own
`go test`) and the notifier's `mw sync` (bd's dolt) — once ran at once and the
kernel killed the user manager. `contrib/mw-heavy` runs one command under a
memory cap, with the mail-notify lock held so no sync can land beside it.

**Install**, once per host, the same way as `contrib/mail-notify`: put it on
`PATH` with `ln -s "$(pwd)/contrib/mw-heavy" ~/.local/bin/mw-heavy` (run from
the rig's checkout). Then:

```sh
mw-heavy make test
```

When `systemd-run` is on `PATH` it runs the command as a transient, capped
scope (`--scope -p MemoryMax=... -p MemorySwapMax=...`, `--user` added unless
it is root); without `systemd-run` it runs the command plainly, under the
same lock, and says so in one line on stderr. Its exit status is always the
command's. `MW_HEAVY_MEMORY_MAX` (default `512M`) and `MW_HEAVY_SWAP_MAX`
(default `1G`) are the two caps; `MW_HEAVY_LOCK` overrides the lock file
(else the same one `contrib/mail-notify` locks, above); `MW_HEAVY_DRY=1`
prints the `systemd-run` line it would run and runs nothing. A Mayor's
verification of a landed branch runs the rig's tests through it.

`OOMScoreAdjust=500` and `MemoryMax=384M` are also set directly on
`mw-mail-notify.service`, `mw-health.service` and `mw-doctor.service`, so a
memory squeeze on the host picks one of these ticks over the seat or the
blog, and none of the three can itself run the host out of memory.
`mw-dispatch.service`, `mw-millhand-tick.service` and
`mw-millhand-review.service` carry neither: each can start the tmux server
that holds Builder or Millhand sessions, and those sessions would inherit
whatever cap and score the unit carried. `scripts/check-heavy.sh` (in `make
lint`) proves the script's locking, capping, fallback and dry run against a
stand-in `systemd-run` and a real `flock`, and that exactly these three units
carry `OOMScoreAdjust`.

## The Path

A story is worked by a **Path**: the rig it is worked in, the branch it
targets, and the harness, model, effort, formula and host of the session that
works it. An epic carries default path values and each story may override any
of them; `Story.PathFrom` overlays the two and rejects what is not a path — no
rig, no target branch, or a harness, model or effort the factory does not
know. See `features/path_validation.feature`.
