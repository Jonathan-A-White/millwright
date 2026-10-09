# millwright

A personal software factory: one human directs a small set of AI-occupied
seats that turn conversations into tracked work and tracked work into commits,
across several rigs and two hosts, on a tight fuel budget.

Standing up your own? Start with [Setting up a vault](docs/setting-up-a-vault.md), a step-by-step guide, and [Best practices](docs/best-practices.md), the design lessons behind it. To use another chain, or none, see [Swapping the chain](docs/swapping-the-chain.md).

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

Which host is home is recorded in the vault: a tracked file, `home`, of one line
(the home host's name, `desktop` or `laptop`, then the UTC time and actor of the
last change). `mw home` prints the home, this host and whether this host is home;
`mw home --check` says nothing and leaves with 0 when it is, 1 when it is not
(then it prints the home's name alone on stdout, one line, for a caller that
must know where to go; the explanation goes to stderr) and 2 when it cannot
tell (no file, or one that is not understood: the caller decides what that
means). `mw home move <host>` writes it (below). See
`features/home.feature`.

### mw home move

```sh
mw home move laptop --dry-run       # on the Laptop: six steps and their ways back, none run
mw home move laptop --old-home-dead # on the Laptop: make it home, the desktop being dead
mw home move laptop --planned       # on the Laptop: make it home, both hosts up, the desktop flushes first
```

Run **on the host that becomes home**, by the Governor, never by the factory on its
own. It asks whether the old home answers ssh (its `[hands_hosts]` line, 10 s): if it
does, it stops (`old home is up: use --planned`); if it does not, it goes on only with
`--old-home-dead`. With `--planned` (both hosts up) a step follows: over ssh the old home's
Mayor is mailed `Hand off now` and waited for, up to 15 minutes (a Mayor is never killed;
if it does not go, the move stops with this host untouched), a final `mw sync` runs there,
its `postern-backend` stops and a final `mw postern mirror` copies its data here, and its
`dolt-beads` stops: nothing is lost, and the Mayor mail says `planned move`. Then, each step printed with its way back:
beads cloned from GitHub's `refs/dolt/data` (the embedded database is set aside in a
dated directory, never deleted; the `dolt-beads` unit is started if the host has one);
the vault's `home` file written, committed and pushed (the fence: an old home that
comes back stays quiet); the `postern-backend` unit started and its `/healthz` awaited
(never `mw postern serve` here: it writes `POSTERN_ISSUER_KEY` back); mail to the Mayor
and the vault's `bin/mayor-up`; and, last, the age of GitHub's backup and of the
Postern data, which is what the dead home took with it. A step that fails stops the
move and prints the ways back of what was done, last first. A move takes a minute or
so: run it in tmux, since a dropped ssh session kills it silently. `postern_data` (the
backend's data directory) is needed; `postern_local_url` (or `$MW_POSTERN_LOCAL_URL`)
says where this host's backend answers when that is not `http://<host>.mw:8787`. The
design, every step's way back and what was and was not checked is `docs/home-move.md`;
the tests are `application/homemove_test.go` and `cmd/mw/homemove_test.go`.

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

### The factory on one host (the desktop)

The factory has one home, the desktop: the Mayor, the one beads database, the
mail notifier and the postern backend all live there, always on. The database
runs as a Dolt server (`dolt sql-server` on the desktop), so every host's `bd`
writes the same rows and a claim is atomic across hosts; GitHub stops being how
the hosts see each other's beads and becomes a backup of them, pushed on a
timer. Since 2026-09-30 the home host serves the vault's beads through a Dolt SQL server (`dolt-beads.service`, server mode), and `bd` on every host reads its connection from `~/.config/mw/beads.env`. The Boost host (the one that is not home) reads and writes the same beads over the network: its `~/.config/mw/config.toml` names the home in `beads_server_host` and its `beads.env` points `BEADS_DOLT_SERVER_HOST` at it. Each host's `beads_sync` says which side of that it is on:

| `beads_sync` | The host | What `mw sync` does with beads |
| --- | --- | --- |
| `remote` (default) | keeps a copy of its own | one `bd sync` every sync, carrying the notes: *Keeping two hosts level* |
| `backup` | holds the one database: the desktop | writes the notes straight into it every sync; one `bd sync` as a backup only once the last got through `beads_backup_minutes` ago (30), recorded as `host.<name>.last_backup`; GC on its usual daily cadence |
| `shared` | reaches the desktop's database: the Laptop on a boost | writes the notes straight into it every sync; never a `bd sync` and never a GC, which are the desktop's |

In every mode the vault half runs exactly as before, and every sync that gets
the vault level writes the note of when the host was level, with its timers'
counts — so "laptop last synced 12 min ago" still means the Laptop's timers
last got it level 12 minutes ago. A backup that fails is said on the sync's one
line (and a merge conflict or a stuck working set is marked the way any such
halt is, for `mw status` and `mw nudge` to show), but it stops nothing and takes
no note back: no host's view of the work waits on it, and the next sync tries
again. `mw status` says the mode on its `BEADS SYNC` line
— with, on the desktop, how long ago the last backup got through — and no
longer calls another host's last sync "a cycle behind" once it is read live
out of the one database; `mw nudge` keeps the other hosts' ages beside this
host's own halt rather than dropping them. Any other `beads_sync` is refused,
naming the three.

**The desktop**, once the Mayor has moved there (above):

1. The beads database in Dolt's server mode, listening on the desktop's
   WireGuard address (10.88.0.3), with the desktop's own `bd` pointed at it
   too: once a server holds the database, no `bd` opens it on its own.
2. `beads_sync = "backup"` in `~/.config/mw/config.toml`, and
   `beads_backup_minutes` to change the half hour.
3. `MW_MAIL_SYNC_EVERY=60` in `~/.config/mw/mail-notify.env`: a sync there is a
   few local writes, with no remote round trip unless a backup is due.
4. The postern backend's environment, `mw postern serve --env-file <file>
   --addr 10.88.0.3:8787` (*The postern key*, below), then a restart of the
   backend.

**A boost on the Laptop**, when the Governor wants more Builders at once:

1. `beads_sync = "shared"` in the Laptop's config, and bd's own
   `BEADS_DOLT_SERVER_HOST=10.88.0.3`, `BEADS_DOLT_SERVER_PORT` and
   `BEADS_DOLT_PASSWORD` in its `~/.config/mw/dispatch.env` — which every
   timer reads — and in the shell anything runs `bd` from. It keeps no beads of
   its own and never syncs them.
2. Raise its `cap`, arm `mw-dispatch`, and path stories to `host=laptop`.
3. `mw doctor beads-server` says whether it reaches the database (*mw doctor*).

To end the boost, stop the Laptop dispatching. `mw` refuses a `cap` below 1
today, so rather than a cap of 0 that is `systemctl --user disable --now
mw-dispatch.timer` there; the sessions already running finish, and whatever is
still pathed to the Laptop is re-pathed (`bd update <id> --set-metadata
host=desktop`).

**What stays on the VPS**: nginx and its TLS, the WireGuard hub, the Governor's
blog, and a watchdog. nginx proxies the postern's `/api` — and `/api/events`,
the backend's event stream, unbuffered — to the backend on the desktop,
`http://desktop.mw:8787`: `mw postern nginx --conf <site>` there, whose
`--backend` defaults to `postern_backend`. Every factory timer the VPS ran for
the Mayor is turned off by the move itself, step 2 above.

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
of that Path, optionally labels, and the keys of the stories it needs done first. `mw file` reads
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

A Path's `host` names the one host that works the story. `host = "auto"` means
whichever host is under its cap and its load takes it: a host takes an `auto`
story only while its 1-minute load average is below its core count, and the claim
writes that host's name over `auto`, so every later reader sees a concrete host.
`mw status` lists a ready `auto` story under READY on every host, as `host auto`.

Every story is filed **held** — beads' `deferred` status — so that nothing can
be dispatched from a plan nobody has approved. `mw file` prints the tree it
filed, with each story's Path and what it waits on, and then asks. `--approve`
answers yes without asking; at a terminal, only a plain `y` or `yes` is a yes;
with nobody there, the plan stays held. Releasing sets every story back to open,
and beads then keeps back the ones still waiting on another, so the stories that
wait on nothing are exactly what a dispatcher can take. See
`features/file_plan.feature`.

### What a rig requires of its epics

A rig can ask more of its epics than the plan format does. Put a file in the
vault, `rigs/<rig>.toml`, and both hosts read it alike (it travels with the
vault's own sync, and nothing is rebuilt to change it):

```toml
epic_sections          = ["Demo"]   # headings the epic's description must contain
epic_last_story_labels = ["demo"]   # labels a story must carry that waits on every other story
```

Both keys are optional lists, and a rig with neither (or no file) is not
checked; millwright sets none. A section is matched as a markdown heading or as
a line starting `<Name>:`, ignoring case. A last story is a story carrying the
label that waits, directly or through others, on every other story of the epic;
in a plan, a story takes `"labels": ["demo"]`. `mw file` refuses a plan that
lacks any of them, naming each, and writes nothing. `mw status` lists every
open epic of such a rig that lacks one under EPICS MISSING REQUIREMENTS, which
reports an existing epic and never blocks it. Nothing here is loaded into any
seat's boot.

There is no off switch. Only the Governor's word waives one, once for each name,
with the words he said (names are case-sensitive, so `Demo` is the section and
`demo` the label):

```sh
bin/mw file plan.json --waive Demo --waive demo --because "no demo, it is a rename"
```

`--waive` without `--because` is refused, and so is naming something the rig does
not require. The epic gets an `epic-waiver:section:<name>` or `epic-waiver:story:<name>` label for each name and a
comment quoting his words, and `mw status` lists it under EPICS WAIVED, never as
missing. See `features/epic_requirements.feature` and
`features/status_epic_requirements.feature`.

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

A story whose formula bd refuses to pour (a step title, which carries the story's
title after the step's own words, over bd's 500-byte limit) is blocked the same
way, under the reason code `pour-refused`: `mw dispatch` keeps the claim, marks it
`run=blocked`, comments, writes a failed `dispatch-pour@<host>` job event, and
goes on to the next story rather than exiting 1. `mw file` refuses such a plan up
front, counting the longest step title of the story's formula with its `{{title}}`.

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
under a five-minute limit (longer for a rig the `[after_landing_limit]` table names, `postern = "20m"`), after the checkout was moved and never otherwise (see
*What a host is told*). It changes nothing about the landing: the story is recorded
landed, ledgered and closed exactly as without it. A command that exits non-zero,
is not found or outlives its limit is one plain line — `after landing: make
build: exit status 2: <the tail of its output>` — in the report, the ledger line,
a comment on the story and the `Landed:` mail to the Mayor, so that they know
this host's binary is old; success is `after landing: make build: ok` in the
report and the mail. A command stopped at its limit is also said first in the mail
to the Mayor, `after landing STOPPED at the limit: the site may be half-deployed`,
because a site copied by a command cut off midway is half-deployed; a rig that ships
a site uses `contrib/site-deploy`, which cannot leave it so (`docs/site-deploy.md`).
A rig the table does not name runs nothing.

A command that fails on a network fault that may pass — ssh's exit status 255, or
a name that did not resolve, a connection refused or timed out in the tail of its
output — is run once more after thirty seconds, and the report and the mail say
`after landing: <command>: retried once after 30s, because the first run failed on
a network fault that may pass (exit status 255)` before the second run's line; a
second failure is reported as any failure is. A command stopped at its limit is
not run again. The command runs under the rig's after-landing lock (a file beside
the rig's worktrees, `<rig>.after-landing-slot`; not the merge slot, which a
landing has given back by then), so two runs of it never go side by side.

`mw after-landing <rig>` runs that same command now, for a landing whose deploy
did not happen or failed: in the rig's checkout as it is, under the same lock
(it waits, saying who holds it, for a deploy that is running), the same limit
and the same one retry, restarting the follower after a successful build of
`millwright` as a landing does. It prints `after landing: make build: ok`, or the
failure line and exits non-zero. It fetches and moves nothing. A rig this host
has no checkout of, or names no command for, is refused. See
`features/after_landing.feature`.

When the rig is `millwright` and its command succeeded, the landing then runs
`systemctl --user try-restart mw-view-follow.service`, so that the follower
publishes the Governor's view with the binary just built and not the one it
started with (mw-gq6.232): a unit that is running is restarted and the report and
the mail say `restarted mw-view-follow.service on the new build`; one that is
stopped or not installed on this host is left alone and not mentioned. A
restart that fails is said in the report, in a comment on the story and first in
the mail to the Mayor, and the landing still counts. A failed build restarts
nothing, and no other rig restarts anything. The self-update tick below does the
same after its own successful build.

A rig whose backend is a program the home runs (postern's `server/`) names it in a
`[backend.<rig>]` table, and a landing that changed it is not left half deployed
(mw-gq6.185). `mw next` asks, before it merges, whether the story's own commits
changed anything under the table's `dir`; if they did, then once the story is
landed — on the home, at once; on any other host, by leaving a `backend.pending.*`
note that the home's next `mw dispatch` or `mw millhand tick` picks up — mw builds
the backend at the landed commit in a throwaway worktree (`build`, run in `dir`,
`{out}` where the binary goes), leaves it at `stage/<name of live>-<short commit>`,
files a new open `hitl` bead under the landed story's epic, writes the swap on it as
a hands step (nothing if the staged binary is already live; one backup of the old
one; install; restart; the health URL and `check` up to four times; the old binary
put back if they never answer) and sends the Governor one message on that bead's
channel. A build or filing that fails is tried again by each tick, three tries in all,
and then said on the landed story. A landing that left `dir` alone does nothing new.

The swap itself is automatic (mw-gq6.270), unless the table says `swap = "hands"`, which
keeps today's tap: the Governor's Approve on the card is then what runs it. With `swap =
"auto"`, the default, the home's next `mw dispatch` or `mw millhand tick` runs the staged
swap step itself, as the host's own user and with no tap, once no Talk is open: the Talk
is read from the Governor's last talk record `mw talk wait` heard, a turn that is under
30 minutes old, and a swap that finds one waits for the first tick after it ends, since a
restart in the middle of a Talk would drop his line. It never runs while another swap or
`mw postern inbox --apply` holds the inbox lock, it begins each staged commit at most
once, and a swap superseded by a newer staged one never runs. The result is said once on the
swap bead's channel: `backend <short> is live and answering`, or that the swap failed and
that the old backend was put back (the step does that itself when the health URL never
answers). A good swap closes the bead; a failed one leaves it open and `hitl`, the step's
output commented on it, and nothing retries it by itself. A host with no postern inbox
directory configured cannot take the lock, and keeps the tap.

The VPS standby that the front door falls back to is kept level too (mw-gq6.189). A table
that names `vps_host` (its name in `[hands_hosts]`) with `vps_stage`, `vps_live`,
`vps_service` and `vps_health` has the same landing copy the binary it just built to
`vps_stage/<name of vps_live>-<short commit>` on that host over the `[hands_hosts]` ssh
prefix, and file a second `hitl` bead under the epic whose one hands step swaps it in,
as a *root* step (the standby's unit is a system unit): the same already-live check, one
backup, install, restart, four tries at `vps_health` and the way back. A root step needs
the Governor's approval, so the standby's swap is always a tap, never the home's tick.
A copy that fails is a failed try like a failed build. `mw status` then says `standby
behind: <standby commit|none> vs <home commit>` when the two `/healthz` lines differ,
`standby level at <commit>` when they match, or `standby not checked (<why>)`.

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
in the vault, and asks git for nothing that writes — no fetch, no landing
worktree — so the commits are counted against `origin/<target>` as the
rig last saw it. It does not read the session's result, which is not written
until the session ends, and it does not try the merge, so a branch that passes
can still be stopped by a conflict or by the other host's work. The last formula
step is normally still open when a session runs it; the session's kickoff prompt
names the command and says so. See `features/check.feature`.

The one thing it takes is the rig's merge slot, around the rig's tests only: a
close-out's gate holds that slot, so a check on the same rig waits for it
(`waiting for the merge slot: held by ...` on stderr) rather than running its
whole gate on top of the close-out's, where both fail from load. `--no-slot`
skips the wait.

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
`MW_HOST_SILENT_HOURS`, `MW_HANDOFF_AT`, `MW_RIG_MEMORY_BYTES`,
`MW_DISPATCH_SYNC_TRIES`, `MW_DISPATCH_SYNC_WAIT`, `MW_PUSH_TRIES`,
`MW_PUSH_WAIT_SECONDS`, `MW_MAX_ATTEMPTS`,
`MW_MILLHAND_ROUTINE_MODEL`, `MW_MILLHAND_REVIEW_MODEL`,
`MW_NUDGE_AFTER_MINUTES`, `MW_NUDGE_SYNC_STALE_MINUTES`, `MW_POSTERN_BACKEND`,
`MW_POSTERN_FLOAT_SATS`, `MW_POSTERN_GOVERNOR_KEY`, `MW_POSTERN_KEY_FILE`,
`MW_POSTERN_SNAPSHOT_PATH`, `MW_POSTERN_VIEW_PATH`, `MW_POSTERN_CHANNEL`,
`MW_POSTERN_TRANSCRIBE_CMD`, `MW_BEADS_SYNC`, `MW_BEADS_BACKUP_MINUTES` and
`MW_HANDS_ROOT_HELPER` ahead of it:

```toml
vault = "/root/millwright-vault"   # the one beads database and the seats
host  = "vps"                      # which of the factory's hosts this is
cap   = 1                          # sessions running here at once (default 1)
host_silent_hours = 2              # how long another host may go unsynced (default 2)
handoff_at = 180000                # the context size, in tokens, at which mw seat context says handoff (default 180000)
rig_memory_bytes = 8000            # how large the Builder's memory of one rig may grow before mw status says prune (default 8000)
beads_budget_bytes = 1500000000    # how large this host's .beads may grow before mw status warns and mw doctor's beads-size goes faulty (default 1.5 GB; raise it on a host that serves every rig's beads)
dispatch_sync_tries = 3            # how many times mw dispatch tries its sync when a name cannot be resolved (default 3)
dispatch_sync_wait = "15s"         # how long it waits between those tries (default 15s, at most 90s in all)
push_tries = 3                     # how many times mw next tries a push again after a fault at the remote itself (default 3)
push_wait_seconds = 20             # how long it waits between those tries (default 20)
max_attempts = 3                   # how many times a story is started in all before mw dispatch stops and mails the Mayor (default 3)
millhand_routine_model = "sonnet"  # the model of a routine wake, and of a wake by hand, of the Millhand (default sonnet)
millhand_review_model = "opus"     # the model of a review wake of the Millhand (default opus)
deputy_model = "sonnet"            # the model mw deputy brings the Deputy up on (default sonnet)
nudge_after_minutes = 60           # how long a claimed story may run with nothing mailed about it before mw nudge names it (default 60)
nudge_sync_stale_minutes = 20      # how stale another host's last sync may be before mw nudge names it (default 20)
postern_backend = "http://desktop.mw:8787"  # where the postern's backend is reached (default shown)
postern_float_sats = 100000        # the postern's float cap, in testnet satoshis, enforced on every send (default 100000, PROVISIONAL)
postern_governor_key = ""          # the Governor's compressed public key, as hex, the postern backend answers to (default empty)
postern_key_file = "~/.config/mw/postern.key"  # where the Mayor's postern key is kept, outside the vault (default shown)
postern_snapshot_path = "~/.local/state/mw/snapshot.bin"  # where mw postern snapshot writes the encrypted snapshot (default shown)
postern_view_path = "~/.local/state/postern/view.b64"  # the live view mw postern view writes and the postern backend serves (default shown)
postern_channel = "direct"         # how mw postern send delivers: direct to the backend, or chain, a funded transaction (default chain)
home_move_bead = "mw-43v9x"        # the bead the Governor's Move home tap, its start and result, is written on (default shown)
postern_transcribe_cmd = ""        # what hears the Governor's voice notes, the audio's path appended, e.g. contrib/postern-transcribe (default empty: none are heard)
beads_sync = "remote"              # remote (a copy of its own), backup (holds the one database) or shared (reaches another host's) (default remote)
beads_backup_minutes = 30          # on a backup host, how long between two backups of the one database (default 30)
builder_nice = 10                #  a Builder session and mw next's gate run under nice -n this, so a grist's scorer and model keep the CPU; 0 is off (default 10)
hands_root_helper = "/usr/local/sbin/mw-hands-root"  # what a root hands step is handed to, through sudo -n, on every host (default shown)

[rigs]
millwright = "/root/millwright"    # where each rig is checked out here

[tests]
millwright = "make test"           # how a close-out asks this rig if it is green

[after_landing]
millwright = "make build"          # run in this rig's checkout once a landing has moved it (default: nothing)

[after_landing_limit]
postern = "20m"                    # how long that command may run before it is stopped, per rig (default 5m); see docs/site-deploy.md

[backend.postern]                  # a rig whose backend the home runs; leave it out for none (see mw next)
dir = "server"                     # a landing that changed anything here stages the backend (default server)
build = "go build -o {out} ./cmd/postern"  # run in dir of a throwaway worktree; {out} is where the binary goes
stage = "/home/jwhite/.local/share/postern"   # built binaries wait here as <name of live>-<short commit>
live = "/home/jwhite/.local/bin/postern"      # the binary the service runs; only the approved hands step touches it
service = "postern-backend"        # its systemd user unit
health = "https://postern.example.org/api/healthz"
vps_host = "vps"                   # optional: the VPS standby is kept level too (see mw next); with vps_stage, vps_live (full paths), vps_service (a system unit) and vps_health (its /healthz URL)
check = "/home/jwhite/.local/bin/backend-smoke"  # optional; run once the backend answers, 60 s at most; if it fails the step fails but the new backend stays. A command that reads `mw postern inbox` is left out of the step: the step runs inside that pass

[hands_hosts]                      # how this host reaches another host a hands step is for; leave it out to run steps for this host only
laptop = "ssh laptop"              # an ssh prefix, split on whitespace
vps = "ssh mw@allmymind.org"       # a NON-root login: over a root login a user step would run as root unchecked, so mw refuses it

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
line, and is stopped after five minutes, or after the time the rig has in
`[after_landing_limit]`.

That same command keeps the host's mw level without a landing of its own
(mw-gq6.183): on a host that names one for `millwright`, every `mw millhand
tick` first fetches that rig, fast-forwards a clean checkout that is behind
origin's `main` and runs the command in it, and says so once in the tick's line
(`self-update: millwright 027f977 → bfd385c, built`). A dirty checkout, one on
another branch or one with commits of its own is left exactly as it is, and the
line says why. A build that fails leaves the old `bin/mw`, is said in the line
and is tried again by the next tick (the commit last built is kept in
`~/.local/state/mw/built-millwright`). After a build that succeeds it restarts the
running `mw-view-follow.service` and says so in the line. A dry run does none of it. The command
is stopped after five minutes, so the tick's unit allows ten.

`mw dispatch` does the same look first (mw-gq6.184), once it holds the dispatch
lock and before it syncs, so a host that dispatches and runs no Millhand tick
keeps its `mw` level too; it prints the same `self-update:` line. A host that runs
both ticks builds a commit once, because both keep the one built marker. The
dispatch unit's `TimeoutStartSec` is ten minutes for the same reason.

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
— resetting the counter by hand is what allows another attempt.

A story labelled `hitl` is still retried and its claim still given back, but
dispatch passes a `hitl` story over, so the report adds a line naming the label
and the cure: `bd update <id> --remove-label hitl`. See `features/retry.feature`.

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
someone does, only this host is covered.

All of that is `beads_sync = "remote"`, the default: every host keeping a copy
of its own. On the host that holds the one database (`backup`) the `bd sync` is
only a backup, run once one is due, and on a host that reaches it (`shared`)
there is none; in both the note is written straight into the database every
sync. See *The factory on one host (the desktop)*, above, and
`features/sync.feature`.

## Going easy on a metered network

On a WSL host mw asks Windows whether the connection it is using is metered
(`NetworkCostType` Fixed or Variable is; Unrestricted, Unknown, no profile, a failed
call or one that takes over 10 s is not), by `powershell.exe` at its full `/mnt/c`
path, and keeps the answer for about a minute in `~/.local/state/mw/network.json`. Config
`metered = "auto"` (the default), `"yes"` or `"no"` (or `MW_METERED`) overrides it, for a
host that is not WSL. While the network is metered:

- `mw sync` skips a beads backup that is due, as `--no-backup` would, and says `backup
  skipped: metered network`; the vault's git pull and push stay, and the backup is still
  due on the next sync that finds the network unmetered.
- `mw dispatch` starts no story whose rig is named in the config's `[heavy_net]` table
  (`postern = true`), the rigs whose gate or close-out installs dependencies. The story
  stays open and the first tick after the network is unmetered takes it. Other stories go
  as before.
- `mw status` shows `NETWORK metered (Windows: <profile>, cost <type>)`, or `NETWORK unmetered`.

A change of state is written once, not per tick: an emergency-lane `job` event
`network@<host>` (running to failed) when it turns metered, so the Governor gets one push,
and one in the normal lane (running to done, `clears` that seq) when it turns back. See
`application/network_test.go` and `application/dispatch_network_test.go`.

## Stamping a rig's head by hand

A chain stamp (`docs/chain-stamps.md`) is queued on its own by each landing of
`mw next` and each vault push of `mw sync`. A head that neither saw has none, and
`mw stamp <rig> [<commit>]` queues one: the commit named, or by default the head
of origin's default branch of the rig's checkout (from `[rigs]`), read after a
fetch. The stamp has no story and the title `Head of <rig>: <subject>`; the
`chain-stamp` job broadcasts it within the minute, as it does any queued stamp.
`mw stamp` prints what it queued.

A commit that is already stamped is refused with `already stamped: <txid>`, or
`already stamped: queued, not yet sent` while it waits, and nothing is queued.
`mw stamp --all` stamps the head of every rig in `[rigs]` that has no stamp,
naming the rigs it skips. See `features/stamp.feature`.

## The postern key

The postern is the Mayor's testnet payment key: a plain file on the VPS,
outside the vault and its backups, host-local like `.mayor-acting`. `mw
postern key init` generates it once — a secp256k1 testnet key, written 0600 to
`postern_key_file` (default `~/.config/mw/postern.key`) — and refuses to
overwrite one that is already there. `mw postern key show` prints its
compressed public key and its testnet address; it never prints the private
key. See `features/postern_key.feature`.

`mw postern send` can ask a decision-needed question about a bead
(`--bead --recommend --option`, repeatable): once broadcast, the bead is
commented QUESTION and marked open with a note that records the txid and the
options offered. `mw postern inbox` reads a reply to it and appends ANSWER to
the bead, clears the note, and mails the Mayor.

An `--option` may end `|<bead>:<state>[,<bead>:<state>...]` (state: `open`,
`held`, `landed`, `verified` or `closed`), as `--option 'A: Release both|mw-x.1:open,mw-x.2:open'`:
the card shows the text alone, and once `mw postern inbox --apply` applies one of the
Governor's acts and one option's expectations all hold, the card is answered with that
option, `ANSWER (by his acts)`, its note cleared, the Mayor mailed and a reply posted
under the card; nothing is released or held on it.

If the answer is a Release tap — its text, trimmed and case-folded, is
"release", with or without a card's "A: " letter prefix — and the bead is an epic, `mw postern inbox` releases its held
stories itself, exactly as `mw release <epic>` would, with no Mayor turn: it
appends a RELEASED comment naming the stories now ready and mails the Mayor
what was released. This only happens when every guard holds: the reply's
verified sender is this host's configured `postern_governor_key`, and the
question it answers offered Release among its options. Any other answer is
still recorded as above, but releases nothing; a signer that is not the
Governor's key (including an unconfigured `postern_governor_key`), a question
that never offered Release, a bead that is not an epic, or an epic with
nothing held, also releases nothing — the Mayor is always mailed why.

A Hold answer ("hold", "B: Hold") to a question that offered Hold is held under
the same guards, as the postern hold action does: a story is held (a claimed
one is cancelled), and an epic has its open, unclaimed stories held.

A message carrying an attachment (postern's docs/protocol.md §8) is
downloaded — `GET /api/blobs/{hash}` with the same signed challenge every
other call carries — its sha256 checked against the hash it was announced
under (a mismatch is refused and writes nothing), decrypted with the postern
key, and written 0600 under `~/.local/state/mw/postern/inbox`, named by the
message's txid and an extension by its mime (`.png`, `.jpg` or `.webp`). `mw
postern inbox` prints the caption and the file's path; a bead comment naming
it ends with " [image: <path>]". A download or decrypt failure prints the
error and still records the text.

`mw postern send` delivers by `postern_channel`: `direct` hands the record
straight to the postern backend (postern's docs/protocol.md §9), its txid
`direct:<sha256>`, with no coins, no float cap and no broadcast; `chain`, the
default, is the funded transaction above. A host says `direct` only once the
backend it reaches takes direct records (`POST /api/messages`): an older
backend answers 404 to it, and there is no falling back to the chain. A
direct record also carries a short clear `summary` for the push's body, naming
the bead's title (`Answer: …`, `Check: …`, or the title alone for a message in a
bead's channel) or the channel (`In <name>`, `In Factory`),
never a word of the text; a chain record carries none. `--chain` also
broadcasts the message on chain, beside the direct delivery (no summary, the
same `postern_float_sats` cap), for a phone that cannot reach the backend; it
is added without the flag when the post `--re` names came by chain (a bare
txid) or the Governor's newest record did (the `postern.talk.channel` note
below), and then a chain that fails is only said. A message sent in a bead's channel
(`--bead-channel`; `--channel <name>` names another; `--re <txid>` answers inside a post's thread) is commented on that bead too, `MAYOR via postern, txid <id>:
<text>`, so the whole exchange lives on the bead. `--attach <file>`
(repeatable) encrypts a file to the Governor, uploads it to the backend's blob
store and announces it in the message (§8, §14): any file, at most 8 MiB, sent under
its base name, its type read from its extension (images, `.webm .ogg .m4a .mp3`
voice, `.pdf`, `.txt`), else `application/octet-stream`; `mw postern inbox`
writes a received file under the extension of the name it carries;
several files are several messages, the caption on the last. See
`features/postern_send.feature`.

The Governor's one-tap actions (§13: `release` a held story or an epic's held
stories, `hold` an open unclaimed story, set a `priority` 0 to 4, mark a story
`verified`) go straight to beads at zero tokens: the backend's on-message hook
runs `mw postern inbox --apply`, which applies every verified message from
`postern_governor_key` since the cursor — actions, replies, comments in a
bead's thread, voice notes — at most once per txid (a
`postern.applied.<txid>` note), comments what it did on the bead (`RELEASED by
the Governor via postern, txid …`, `VERIFIED by the Governor via postern
(<txid>)`) and mails the Mayor. It moves no cursor and prints no message's
text. The Mayor's own `mw postern inbox` then shows an applied message as one
line, `already applied earlier: applied <kind> <bead> txid <id>`, once, and never applies it twice; an action
this host does not know is left as text, and one it cannot apply (a hold on a
claimed story, say) is refused and the Mayor told why. BRC-78 has no replay
protection, so an action is applied only when the backend vouched for the
record's signer (the key that delivered it, or signed its transaction);
otherwise it too is left as text. A message in an open demo bead's channel
(label `demo`, or a title starting `Demo`) whose whole text is `Looks good` —
any case, one final `.` or `!` — closes the demo as the `close` action does,
the comment quoting him and the Mayor mailed `Closed: <bead> on his Looks good`;
anything else, or any other bead, is an ordinary comment. See
`features/postern_inbox_apply.feature`.

The Governor's *Move home* tap (§18, class `move-home`) is run by the same
pass on the host it names, `mw home move <host>` with `--planned` or
`--old-home-dead`, only when its signer is `postern_governor_key` and it is
under 30 minutes old, and written on `home_move_bead`. On a host that is not
home, `--apply` applies nothing else. See `docs/home-move.md`, *The tap in
Postern*.

A voice note — a Governor's message whose attachment is audio — is heard on
this host, never by a third party (§14): with `postern_transcribe_cmd` set,
the decrypted audio's path is appended to it and it is given five minutes;
what it prints is the transcript, written on the bead as `GOVERNOR (voice) via
postern, txid …: <transcript>` (or printed, for a named channel), sent back to
the Governor in the same thread as a `role: "transcript"` message, and mailed
to the Mayor. `contrib/postern-transcribe` is the command to set it to: it
converts the note with ffmpeg to 16 kHz mono WAV in a temporary directory and
runs whisper.cpp's `whisper-cli` on it, printing plain text only; the model is
`$POSTERN_WHISPER_MODEL`, by default `~/.local/share/whisper/ggml-base.en.bin`.

`mw postern view` writes the live view the Governor's app shows (§11): every
live epic, every bead under one at any depth (a closed one only within the
week; the rest counted in its epic's `done_earlier`), every live bead's parent
chain, and the needs waiting on the Governor — an open question, a live epic
with held stories, a story landed in the last day without `VERIFIED`, an open
`demo` or `hitl` bead, a story out of attempts, a host with work pathed to it
unsynced for 20 minutes — most blocking first, then oldest. It gzips the JSON,
seals it with BRC-78 to `postern_governor_key` and writes it base64,
atomically, to `postern_view_path`, for the backend's `GET /api/view`. It
reads every bead in one bd list and every note and needed comment in one bd sql
(two bd calls, about 0.7 s on the live vault), so it can run every few seconds; `--json` prints the plaintext. `mw postern bead <id>` prints one
bead in full (§12), sealed the same way, for the backend's
`GET /api/beads/{id}` (`POSTERN_BEAD_CMD`); an unknown bead leaves with status
3. See `features/postern_view.feature` and `features/postern_bead.feature`.

#### mw events: follow, emit, tail, wait

The home keeps one sequenced, append-only log of the factory's events
(`docs/events.md`): one JSON event per line in `events_log_path` (default
`~/.local/state/mw/events/log.jsonl`), fsynced on every append, its head in
`log.seq` beside it. Its seq is the cursor a handover or the app reads from.

`mw events follow [--every 1s]` does not exit. Every `--every` (default one
second) it reads the beads' head — Dolt's hash of the whole database, one
`bd sql "select dolt_hashof_db()"` call of about a tenth of a second, no tokens.
When the hash has moved (it moves on every bd write at once, the working set
included), it reads bd's own audit of the beads (its `events` table) and the
comments since its cursor, two more `bd sql` calls, and appends one event per
change by kind: a status move is `bead_changed` with from and to (one event per
step of the bead machine, so a claim and a start seen together are two), a
comment beginning `QUESTION` is `card_asked`, `ANSWER` `card_answered`, `RAN`
`hands_ran`, `The Governor by postern` `message`, any other comment or change
`bead_changed` with the state left alone, and a new mail bead is `mail` with
its box as the detail. It saves its cursor (`follow.json` beside the log) and
writes the sealed live view again, so a tap of the Governor's shows in the app
within two seconds. Its first run only reads where every bead stands and
appends nothing. A failure is logged and the loop goes on, tried again next
pass; one that repeats word for word is logged once. SIGTERM or SIGINT stops it
cleanly. `mw postern view --follow` is the same loop by its old name.

Sending. Each pass of the follower also ends by sending the events written since
its last batch to the Governor: it seals them as one `events` record to his key
(`docs/events.md`, "The batch") and puts it on chain (UTXOs, sign,
`/api/broadcast`) and delivers it direct (`POST /api/messages`) in the same pass.
The first event after a quiet spell goes at once; a burst goes as one batch per
2 seconds, or 50 events (and no more than fits a record) at once. When the chain
road fails (no UTXOs, the block explorer or `/api/broadcast` down) the batch goes
direct only, in the `fallback` lane, and is kept pending in `ship.json` beside
the log; every later pass puts the pending batches on chain first, oldest first,
in the `normal` lane with the same seq range, until they clear. The app dedupes
by seq. Two knobs in the `[events]` table of `~/.config/mw/config.toml` guard the
satoshis:

```toml
[events]
chain = true            # false: never touch the chain; every batch goes direct as fallback
chain_daily_cap = 500   # records put on chain in a UTC day; past it, batches go direct as fallback
emergency_daily_cap = 20  # emergency records put on chain in a UTC day, counted apart; past it, direct only
```

Past the cap the follower appends one `job` event (actor `events-follow@<host>`,
running to failed) saying so, once a day, and the pending batches wait for the
next UTC day. `mw status` has an `EVENTS` line: the log's head seq, the last seq
sent, the batches pending on chain and today's chain records of the cap. With no
postern key the follower writes events but sends none, and says so.

Springing the jobs (mw-jrx0s.14). The follower also keeps an idle factory at zero tokens: each pass
it reads the events written since the last and runs, with `systemctl --user start`, the unit a
timer would have started. A bead opened or landed (a step of a story's own molecule does not count)
runs `mw-dispatch.service`; an alarm (an event in the emergency lane), mail for the Millhand or a
failed `doctor` job runs `mw-millhand-tick.service`; and `mw-mail-notify.service`, whose sync and
backup the minutely timer drove, runs on the follower's own clock. A job is in flight at most
once: an event that springs it during a pass starts no second one beside it, and one more pass
follows the first, so a story opened just after the ready list was read is not left for the
heartbeat. Each pass is told in the log as `job` events, actor `dispatch@<host>`,
`millhand-tick@<host>` or `mail-notify@<host>`: `scheduled` (its detail says why: `bead mw-x opened`,
`heartbeat`, `clock`), `running`, `done` or `failed`. The timers of `contrib/systemd` are now
hourly heartbeats (a host with no follower gives them their old cadence with a drop-in), and
`install-units.sh --enable mw-view-follow` disables `mw-mail-notify.timer`. Three more knobs:

```toml
[events]
heartbeat = "1h"    # a job unrun this long is run anyway: the fallback for a missed event
clock = "5m"        # how often the follower runs mw-mail-notify.service
idle_after = "10m"  # no event but the jobs' this long: mw status says IDLE since HH:MM
```

`mw status` prints `IDLE since 14:05 · 0 harness processes` once the log has held nothing but
jobs' events since then for `idle_after` and no job is in flight; the count is the `claude`
processes alive (the seat reapers close an idle window), and is shown as `HARNESS n processes`
when the factory is not idle. Only the home's follower sees the beads' events, so another host
keeps its own timer at the pace it needs.

`mw events emit --kind job --actor dispatch@laptop --from scheduled --to running
[--bead <id>] [--detail ...]` appends one event of a job's own, stamped now, and
prints its seq; an event its machine forbids is refused and nothing is written.
`--actor` defaults to `mw@<host>`. `--emergency` puts the event in the emergency lane: the
follower sends it at once, alone, as a record of one event on chain and direct, ahead of the
pending batches and the 2 s window (past the day's cap too), and `mw status` adds `emergency N`
to its `EVENTS` line when N went today. `mw talk call` and mayor-stale's alarm (mw doctor)
each emit one. `mw events tail [--since N] [--follow]`
prints the events after seq N (default the whole log), one per line — `41
2026-10-01T13:02:07Z bead_changed mw-1 mw@laptop open->claimed status` — and
with `--follow` goes on printing what is appended.

`mw events emit --kind control --detail "pause-host laptop"` carries a word to the factory
(mw-jrx0s.16; docs/events.md, "Control"): `cancel` (with `--bead <id>`), `pause-host <host>`,
`resume-host <host>`, `cap <host> <n>` and `priority <n>` (with `--bead`). The Governor's hold tap
on a story a session has claimed writes a `cancel` (actor `governor@postern`); the follower, each
pass, closes that story's session, gives the claim back, holds the story, records `run=cancelled`
and comments `cancelled by <actor> at <time>`, leaving the worktree for `mw retry`. A
`pause-host` makes that host's `mw dispatch` do nothing, saying it is paused, until a
`resume-host`. `mw status` shows a `PAUSED` line and a `CANCELLED` section for the last day.
Both act on the home's log, so they reach the host whose follower and dispatch read it.

No seat polls (mw-jrx0s.6, Q7 rule 2 of mw-6ww.55). A seat says which events it hears in
`seats/<seat>/subscribe.toml` in the vault:

```toml
kinds = ["mail", "landing", "card_answered", "message"]   # one line; any event kind, or landing
spring = true   # deputy and millhand only: bring the seat up when its window is down
```

`landing` is a `bead_changed` that ends in `landed`; a `mail` event is the seat's only when its
box is the seat's. Each pass the follower reads these files and, for a seat with new matching
events: when the seat's window (`<seat>-*`, of several the one its acting file names) is up
and its pane idle at an empty input line, types `New events for <seat>: N. Run mw events tail
--since <seq>.` and, when mail is among them, `New mail for <seat>: N message(s). Run bd mail
inbox.`; leaves a busy pane alone and tells it on a later pass; and when the window is down
and the seat is marked `spring = true`, runs its up command (`mw deputy`, `mw millhand`) with
the same line as its reason. A seat whose window is down and not marked is told nothing: it
reads the log from its handoff. A seat starts at the log's head the first time it is seen
(history is not told); the cursors are `nudge.json` beside the log. A bad file is logged and
that seat left out. `mw events wait --for <seat> [--kinds k1,k2] [--since N] [--limit 50m]` is the
same subscription as a background call: it blocks, looking at the log's head once a second
and calling no bd, until a matching event, prints `New events for <seat>: N. Run mw events
tail --since <seq>.` and the matching lines, and exits so the harness wakes the seat; with
no event by `--limit` it says so and exits 0 (arm it again). `--kinds` defaults to the seat's
file, or `mail`; hyphens may stand for underscores; a bad kind is refused, with the kinds
listed. `contrib/mail-wait` is retired: it prints `retired: mw events wait` and runs it with
`--for $MW_MAIL_MAILBOX` and `--limit $MW_MAIL_WAIT_LIMIT`s. While the follower is active
the mail-notify tick no longer types the mail line for a seat whose file names mail.

A Mayor handover is a cursor on this log (mw-jrx0s.11): the successor boots, with
`bin/respawn-mayor`, while the old Mayor still answers; the old Mayor then runs `mw seat
handover [--at N] [--to <window>]` last. It emits a `handover` event, marks event N (default:
the log's head) as the last the old Mayor answers, and writes the successor's window name and N
into `.mayor-acting`, which the reaper on the old window waits for. From then on `mw events wait
--for mayor` and `mw talk wait` in the old window end at once, saying `handed over at N`, and
a Talk turn that arrives after it is left unprinted and the talk cursor unmoved, for the
successor to read. The successor's own `mw events wait` ends on the handover, saying it holds
the seat from N, and a wait begun `--since N` ignores it. A wait tells which window it is in
by `--as <window name>`, by default the tmux window it runs in; with none it is the old one.

The Mayor's commands on the log:

| Command | What it does |
| --- | --- |
| `mw events follow [--every 1s]` | The follower: writes the log, publishes the view, ships batches, nudges and springs seats and jobs. Never exits. |
| `mw events tail [--since N] [--follow]` | Prints the events after seq N, one per line; `--follow` keeps going. |
| `mw events emit --kind <k> [--bead <id>] [--emergency] ...` | Appends one event of its own, stamped now, and prints its seq; `--emergency` puts it in the emergency lane. |
| `mw events wait --for <seat> [--kinds ...] [--since N] [--limit 50m]` | Blocks on the log, no polling and no bd, until an event the seat subscribed to; ends on a handover. |
| `mw seat handover [--at N] [--to <window>]` | The old Mayor's last act: marks event N as the last it answers and names the successor. |

The follower is the user service `contrib/systemd/mw-view-follow.service`
(the name kept from when it only republished the view; a daemon, so no timer;
`Restart=on-failure`, `WantedBy=default.target`), which reads
`~/.config/mw/beads.env` for the home's `BEADS_DOLT_*` and, optionally,
`dispatch.env` for `PATH`. **Install**, once, on the home: `sh
scripts/install-units.sh --enable mw-view-follow`. While it is active the
mail-notify tick's view step is retired (below) and runs nothing. A home move
moves the log by hand: see `docs/home-move.md`.

`mw postern snapshot` writes the brief of every live epic (open or in
progress) as postern's docs/protocol.md §7 JSON: each epic's children still
waiting on a decision-needed question (`needs_you`), closed in the last 24
hours and not yet marked `VERIFIED` on a comment (`landed`), in progress then
open and unblocked by priority (`working`), and how many are closed in all
(`closed_count`, kept as a running total even of what `landed` also lists). A
`needs_you`, `working` or `landed` entry also carries the bead's description
and newest three comments, each cut to 4000 runes with a trailing marker. It
encrypts that JSON to `postern_governor_key` and writes it
atomically — a temp file, then renamed into place — to `postern_snapshot_path`
(default `~/.local/state/mw/snapshot.bin`), the file nginx serves to the
Governor's app. `--json` prints the plaintext instead of writing anything, for
inspection. The notifier tick (`contrib/mail-notify`) calls it once after `mw
nudge`, only on a host with a postern key file, and a failing snapshot is
logged but never stops the tick.

The postern's hand steps — this host's config lines and the postern backend's
environment, on the desktop, and the nginx site, on the VPS — are `mw postern
serve` and `mw postern nginx`, both idempotent, both backing up what they are
about to change first, and both taking `--dry-run`, which prints everything
they would write:

```sh
mw postern serve [--backend <url>] [--snapshot-path <path>] [--governor-key <hex>] [--env-file <path> [--addr <host:port>] [--view-path <path>] [--mw <path>] [--mayor-key <hex>]] [--backup-dir <dir>] [--dry-run]
mw postern nginx --conf <path> [--backend <url>] [--backup-dir <dir>] [--dry-run]
```

`mw postern serve` writes or replaces `postern_backend`, `postern_snapshot_path`
and `postern_governor_key` in this host's config file — a re-run with the same
values changes nothing — and makes the snapshot's own directory. Each defaults
to what the config already says, so a flag is only needed to change one; the
Governor's key has no default, so it is said once. With `--env-file`, naming the
backend's environment file (a systemd `EnvironmentFile`), it also writes or
replaces the backend's own lines there, every other line — `POSTERN_ANCHOR`,
`POSTERN_DATA` and the rest — left as it was:

| Line | From |
| --- | --- |
| `POSTERN_ADDR` | `--addr`: `127.0.0.1:8787` by default, `10.88.0.3:8787` on the desktop |
| `POSTERN_VIEW_FILE` | `--view-path`: `postern_view_path`, which serve writes into the config too |
| `POSTERN_BEAD_CMD` | `<--mw> postern bead`; `--mw` is the mw running serve by default |
| `POSTERN_ON_MESSAGE` | `<--mw> postern inbox --apply` |
| `POSTERN_MAYOR_KEY` | `--mayor-key`: this host's postern key's public half by default |
| `POSTERN_ISSUER_KEY` | the Governor's key: he issues the licence |

It makes the view's directory, and says to restart the backend, which reads its
environment only when it starts.

`mw postern nginx` ensures a `location = /api/events` block ahead of the
general `/api/` location — the backend's event stream, held open for as long as
the app is:

```nginx
    # mw-api-events
    location = /api/events {
        proxy_pass http://desktop.mw:8787;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
```

and a `location = /snapshot` block (aliased to the config's own
`postern_snapshot_path`, no-store, nosniff, served as an opaque octet stream),
and points every `/api` upstream at `--backend` (`postern_backend` by
default). Given `--backend` twice, it writes an `upstream postern_api` block over
both instead (the first takes the traffic, the rest are written `backup`, so a
down standby costs no connect timeout), points every `/api` location at it with
`proxy_next_upstream error timeout http_503 non_idempotent` and a short `proxy_connect_timeout` (a standby
backend answers 503 before doing anything, so a retried POST is safe), so a move
needs no nginx edit; one `--backend` writes what it always did. All
marker-based, so a re-run is a no-op; it then runs `nginx -t` and,
only once that passes, `systemctl reload nginx`, restoring the backup
automatically if the test fails so a bad edit is never left live. Both print
the backup path and the way back. `--backup-dir` defaults to `tidy` under
the caller's home (`/root/tidy` on the VPS, run as root). See
`features/postern_serve.feature`.

### mw postern mirror

```sh
mw postern mirror
```

The home copies the postern backend's data to the boost, so that a dead home
loses at most the last ten minutes of it. On the home, `mw postern mirror` runs
one `rsync -rpR` of `postern_data` (the backend's `POSTERN_DATA`, a full path,
the same on both hosts: `postern-index.jsonl`, `postern-vapid.json`,
`postern-push-subscriptions.json` and `blobs/`) to the same path on the other
host, reached as that host's line of `[hands_hosts]` says (`desktop = "ssh
desktop"`; the last word is the host rsync copies to). When `postern_watchdog_target`
is set, an rsync destination such as `root@vps:/var/lib/postern-watchdog/`, it
also copies `postern-vapid.json` and `postern-push-subscriptions.json` there:
the VPS watchdog's copy, which `postern-watchdog-sync.timer` makes by hand on the
desktop today.

It also copies the mill's state (`grist_state_dir`, default
`~/.local/state/mw/grist`: the cursor, `grinds.jsonl` and `undelivered/`, a full
path, the same on both hosts) to the boost by a second rsync, so that a home move
keeps the daily counts and the answers not yet delivered. When that directory is not
there it copies nothing and says nothing; there is no new timer and no new setting.

```toml
postern_data = "/home/me/postern-data"
postern_watchdog_target = "root@allmymind.org:/var/lib/postern-watchdog/"
[hands_hosts]
desktop = "ssh desktop"
```

A host that is not home does nothing, says so and leaves with 0: every host may
run the timer. A boost whose index's last line (its `firstSeen`) is newer than
this host's is left alone, so an older index never overwrites a newer one: the
run says so and leaves with 1, after it has made the watchdog's copy. The mill's
state has the same rule, keyed on the `time` of the last line of `grinds.jsonl`:
a boost whose last grind is newer is left alone, and the run says so. Nothing
here writes to this host's own data. `$MW_POSTERN_DATA` and
`$MW_POSTERN_WATCHDOG_TARGET` say the two settings in place of the file.

**Install**, once per host: `sh scripts/install-units.sh --enable
mw-postern-mirror` (see *Running a host on a timer*), which runs
`mw-postern-mirror.timer` at 3, 13, 23, ... minutes past the hour. It is covered
by `scripts/check-timer-units.sh` and `cmd/mw/postern_mirror_test.go`, which run
it against a stand-in rsync and ssh.

## Grist: apps' AI work

An app (Cairn first) sends the factory grist: AI work to be answered, not
built, sealed to the **mill key** and carried by the postern backend as a
`grist` record (postern's `docs/protocol.md` §18). The mill runs on the
factory's home host.

`mw grist key` makes the mill key once, 0600 at `grist_key_file` (default
`~/.config/mw/mill.key`; never the Mayor's key), and prints its public key and
fingerprint. It never overwrites a key or prints the private half.

`mw grist grind` is one pass, then it exits. It reads what the backend holds
for the mill key and answers each grist once, sealed back to its sender with
`re` set to the grist's txid:

- **refused**, with no session, when the backend's `signer_apps` stamp does not
  name the grist's app (the Governor's `postern_governor_key` may use any app),
  the app has no grind for the grist's kind and version, its model is not
  allowed, or its photos or the sender's grist today are over a limit;
- **answered** or **failed** otherwise, after one short Claude Code session
  with no seat. The session runs in a private temp directory holding the
  opened photos, may only Read them, and must answer in the grind's schema.

The grind is `grinds/<kind>.json` at the app rig's `main`, read with its
instructions and schema at that commit, and the commit is stamped on the
answer. Before it reads, the mill fetches the rig's remote `main` (at most
once a minute per rig) and reads the remote's commit when it is ahead of the
rig's local `main`, so a grind landed on another host goes live without a
landing here; if the remote cannot be reached it reads the local commit and
says so in its report. A grind takes one of the mill's own grind slots, first come first served; the host's `cap` on Builders neither holds a grind back nor counts it.
The mill grinds up to `[grist]` `concurrency` grists at once (default 2), oldest
first, each in its own temp directory and run record, with one lock file
`grind-<n>.lock` for each; a grist past that waits for a grind to end, and the
cursor moves only past grists whose grind has ended. A grind that finds no slot
free to start waits for the next pass (the mill says "the mill is at its limit
(2 of 2 grinds running)"): the next grist's hook,
or the next `mw dispatch` tick, which on the host that is home (`mw home`) and
where the config file has a `[grist]` table runs one pass after its claims. The
mill never waits behind Builders, which run for minutes to hours while a grind
answers someone's live use in seconds; `mw dispatch` does not count a running
grind as one of its sessions, so Builders at the cap do not stop a grind and a
grind never takes a Builder's place. After answering, the grist's photos are
deleted from the backend. Each grist handled adds one line to
`grinds.jsonl` in `grist_state_dir` (default `~/.local/state/mw/grist`): time,
txid, app, kind, sender fingerprint, model, effort, where each came from, status,
reason, Fuel, seconds and commit, never the grist's content. A grist's `grist`
header may carry `model` and `effort`: they replace the grind file's for that
grist, and must be within `[grist]` `models` and `efforts`, else the grist is
refused naming the value.

```toml
grist_key_file  = "/home/jwhite/.config/mw/mill.key"   # MW_GRIST_KEY_FILE
grist_state_dir = "/home/jwhite/.local/state/mw/grist" # MW_GRIST_STATE_DIR

[grist]                  # the ceilings above every grind; these are the defaults
models = "haiku,sonnet,opus"
efforts = "low,medium,high"   # what a grist may ask for; the grind file's own effort is not held to it
max_attachments = 4
max_attachment_bytes = 8388608
daily_limit = 50         # grist a day from one key
concurrency = 2          # grists ground at once, the mill's own limit; the rest queue. The host's cap on Builders does not apply
timeout = "10m"

[grist-apps]             # where each app's rig is checked out here
cairn = "/home/jwhite/rigs/Cairn"
```

The postern backend pairs with it through three environment lines:
`POSTERN_MILL_KEY` (the public key `mw grist key` prints), `POSTERN_ON_GRIST`
(`mw grist grind`, by full path) and `POSTERN_APPS` (`cairn=cairn`, each app's
licence collection to its name). `mw postern serve` does not write these yet,
so add them to the backend's environment file by hand. See
`features/grist.feature`.

A grind may say `"forward": "mayor"` instead of naming a model, for a grist that is
for a person to read, such as an app's feedback. The mill then runs no session
and burns no Fuel: it opens the grist's text and pictures as for any grind,
keeps the pictures under `forwarded/<txid>/` of `grist_state_dir`
(`picture-1.jpg`, `picture-2.png`, ...), sends one mail to the mayor titled
`Feedback from <app>: ` and the first 80 characters of the text, whose body is the
full text, the sender's key and fingerprint, the grist's id and the pictures'
full paths, and answers the app `{"status":"sent"}` so it can say Sent. The text is
the request's `text` field, or the whole request when it has none. `mayor` is the only
value; anything else refuses the grist ("The app's grind forwards to someone the mill
does not forward to."), as does a grist over the grind's or the factory's attachment
limits (a grind taking six pictures needs `[grist]` `max_attachments = 6`). A grist
the mill cannot mail is failed, so the app may send it again. `model`, `effort`,
`instructions` and `answerSchema` are not read. `mw grist runs` and `stats` count it
as the kind `forward`. An app's `grinds/feedback.json`:

```json
{
  "grind": 1, "app": "lampas", "kind": "feedback", "versions": ["1"],
  "forward": "mayor",
  "attachments": { "min": 0, "max": 6, "mime": ["image/jpeg", "image/png", "image/webp"], "maxBytes": 4194304 }
}
```

See `features/grist_forward.feature`.

`mw grist send` is the terminal's sender: it sends a grist as a key it is
given, as an app's phone would, and can wait for the answer.

```sh
mw grist send --key <file> --app cairn --kind sweep --request request.json \
  [--photo drawer.jpg]... [--backend <url>] [--wait 5m] [--schema-version 1.1]
```

It proves `--key` to the backend, learns the mill key from `GET /api/me`, seals
each photo (`.jpg`, `.png` or `.webp`, at most four) and the request to it,
uploads the photos and posts the grist. The version of the app's schema is the
request's `schemaVersion` unless `--schema-version` says otherwise. Without
`--wait` it prints the grist's id. With `--wait` it pages the backend for the
answer whose `re` is that id and prints it as JSON, leaving with 0 when it is
answered, 1 when it is refused or failed (the reason on standard error), and 2
when none came in time. `--key` has no default, so it never reads the Mayor's
key unless told to, and nothing prints a private key; `--backend` defaults to
`postern_backend`. See `features/grist_send.feature`.

`mw grist eval --grind <rig>/grinds/<kind>.json --photos <dir> [--models haiku,sonnet] [--out <dir>]` grinds each photo
that has a `<name>.txt` of expected names once per model, serially, as the mill would, and scores hits, misses, extras and unsure items with seconds, cost and tokens per photo and model.
It writes `eval-<UTC>.jsonl` and `.md` under `--out` and never touches the backend, the mill's counts or the cap; `--help` says the rest. See `features/grist_eval.feature`.

`mw grist score --engine local --target "the cat sat" --audio clip.webm [--lang en]`
hands a recording of someone reading a text aloud to a scoring engine and prints
its result as JSON: for each word the phonemes expected and produced, whether it
was read right, left out, added, mispronounced or hesitated over, and its
accuracy. Engines are the `[scorers]` table's `engines` (default `["local"]`),
with `local_url` (default `http://127.0.0.1:8765`), `azure_key_file` and
`azure_region` beside it; the local engine needs `ffmpeg` and the scorer of
`contrib/scorer` (install and way back in its `README.md`), and the `azure` engine
(Azure Speech pronunciation assessment, a third party hears the clip) needs `ffmpeg`,
a 0600 `azure_key_file` and `azure_region`. The contract, with
the request and response JSON, is `docs/scorers.md`; see `features/scorer.feature`.

A grist may carry a recording (`audio/webm`, `audio/ogg`, `audio/mp4`, `audio/mpeg`,
`audio/wav`) for a grind whose file says `"scoring": {"audio": true, "target_field":
"target_text"}` and lists the type under `attachments.mime`; a grind that does not
score refuses it. `scoring.langs` (`["el", "en"]`, first the default, absent `["en"]`)
names the languages it scores in and the request's `lang` picks one; an engine without
the language (Azure for Greek) is skipped with a note. The mill scores each recording with every `[scorers]` engine
against the text in the request's `target_field`, before the harness session, and
adds `reading_result` to the request as text: `{<engine>: <ReadingResult> | {"error":
"..."}}`. An engine that fails never fails the grind, and the audio is never in the
session's directory or its prompt. The answer carries the same `reading_result` (and
`reading_results` for several recordings) beside `answer`; a grind that scored nothing
answers without them. A grind file may set `maxTurns` (default: the
harness's own). Every grind that ran is kept under `grist_state_dir`'s
`runs/<txid>/`: `input.json` (the request as the session was given it),
`attachment-N.<ext>`, `scorers.json`, `answer.json` and `timing.json`, 0600, never
deleted by `mw`; `mw grist runs [--since 24h|2026-10-07]` lists them (txid, kind, model,
seconds, answered|refused|failed). `timing.json` also keeps `sent` (the grist
record's own time, so `received` minus `sent` is the queue wait) and `scorers` (each
engine's seconds), each left out when not known; `mw grist stats --kind tutor-turn
[--last 20]` prints, for the last runs of that kind, the median and worst seconds of
each phase (queue, scoring and each engine, harness, total) and how many runs had it. See `docs/scorers.md` and `features/grist_audio.feature`.

## Steps for his hands

A step only the Governor's hands could take — a `sudo` line, a unit to
enable, a file to move between hosts — is written down by the Mayor, not
typed as a `!` line for him to copy (postern's docs/protocol.md §17):

```sh
mw hands add <bead> --id <id> --host <host> --as user|root [--way-back '<commands>'] [--replace] -- '<commands>'
mw hands list <bead>
```

`mw hands add` keeps the step in the bead's note `hands.<bead>` (a JSON array
of `{id, host, as, run, way_back, added_at}`), comments it on the bead exactly
as it will run (`HANDS STEP <id> on <host> as <as>:`, then the commands and the
way back, fenced), and labels the bead `hitl`. The commands are one quoted
argument after `--`. An id is used once per bead; `--replace` changes a step,
which changes its hash and so voids any approval of the old one. `mw hands
list` prints each step with its sha256 and whether it has run.

Postern shows a `hitl` bead's steps under his hands (the view's `hands` need
carries each with its §17 sha256 and, once run, `ran`). He approves a step with
his key — a fresh fingerprint, then a signature over its hash and the time —
and the approval arrives as a `run` action. `mw postern inbox --apply` runs it
only when every check holds: it came from `postern_governor_key` and the
backend vouched for its signer; the step on the bead still hashes to what he
approved ("the step changed since you approved it" otherwise); the signature
verifies; the bead waits on no open bead ("it waits on <title> (<id>).
Approve it again once that is done." otherwise); the approval is under 5
minutes old (`HandsApprovalMaxAge` in `domain/hands.go`) and not over 2
minutes ahead; it was not signed before the step was added (its `added_at`);
and that approval has not run before (a `hands.approval.<hash>.<time>` note,
written before the step starts). A step for this host (`host`) runs here; a
step for another runs over that host's ssh prefix in `[hands_hosts]`, and one
with no entry is refused naming the key to add. `as: user` runs `sh -c` as
this host's user for at most 10 minutes; `as: root` hands the whole request, on
standard input, to `sudo -n` `hands_root_helper`. The run is recorded in
`hands.ran.<bead>.<id>` (`{at, exit, host}`), commented on the bead (`RAN step
<id> on <host> as <as>, exit <n> (approved by the Governor via postern, txid
…)` and the last 4000 characters of output), sent back to him in the bead's
thread, and mailed to the Mayor. The record is first written as started (exit -1,
`mw hands list` says "started … has not reported back") before the step runs, and
the finished one replaces it, so a pass cut off mid-step never leaves "not run". A refusal goes out the same three ways, says
why, and is never tried again.

The postern backend starts `mw postern inbox --apply` as its on-message hook, so
a step that restarts the backend (`systemctl --user restart postern-backend`)
would kill the pass that runs it, its ran record and its Ran mail with it. When
systemd started `mw` (`INVOCATION_ID` is set) and `systemd-run` is on `PATH`,
the `--apply` pass therefore runs itself again in a transient unit of its own
(`systemd-run --user --collect --wait --pipe`, without `--user` as root),
outside the service's cgroup: the copy left in the service relays its output
and exit status while it lives, and the pass finishes, records and mails
whatever happens to the service. This covers a root step too, which `sudo`
starts in a new session but not out of the cgroup. Where no unit can be
started, the pass runs where it is, as before.

A root step runs through `mw-hands-root` (`make build` builds it into `bin/`),
a small program that takes nothing from its arguments or environment. It
refuses unless it runs as root; reads the request (at most 64 KiB) from
standard input; trusts only `/etc/mw-hands/governor.pub` and
`/etc/mw-hands/host`, and only when they and their directory are root's and
writable by no one else; refuses a step for another host, a step that is not
`as: root`, a hash that does not match, a signature that does not verify, an
approval over 5 minutes old or 2 ahead, and an approval already in
`/var/lib/mw-hands/used` — where it records the approval before it runs
anything. Then it runs `/bin/sh -c` as root, with a fixed environment and a
10-minute limit, streams the output, and leaves with the step's own status; a
refusal is exit 126 and one line on standard error. So the host's own account
can run nothing as root without his signature, and an approval runs once, on
the host it was given for.

He installs it once, by his own hands, on each host that should run root
steps:

```sh
sudo ~/millwright/contrib/install-hands-root <user> [<governor public key hex>] [--yes]
```

It refuses unless run as root and until `bin/mw-hands-root` is built (`make -C
~/millwright build`); takes the key, when not given, from `postern_governor_key`
in `<user>`'s own mw config (its home from the password database, never
`$HOME`), and this host's name from `host` there; checks the key is 66 hex
starting 02 or 03, shows it with a fingerprint, and asks before writing
anything (`--yes` skips the question). It installs `/usr/local/sbin/mw-hands-root`
(0755), `/etc/mw-hands/governor.pub` and `/etc/mw-hands/host` (0644) and
`/etc/sudoers.d/mw-hands` (0440) holding exactly `<user> ALL=(root) NOPASSWD:
/usr/local/sbin/mw-hands-root`, checked with `visudo -cf` before it is moved into
place, all root's; makes `/var/lib/mw-hands` (0700); and prints the way back.
On a host reached as root (the VPS), `<user>` is `root`: the key and host come
from root's own mw config, and no sudoers file is written, since root runs
`sudo -n` with none; the rest is the same. It still refuses a malformed user
name. Re-running it replaces the same files and keeps the record of approvals run.
See `features/hands.feature` and `features/postern_inbox_apply.feature`.

## Saved prompts

The Mayor keeps prompts on the postern backend, so the Governor can run one by
name from the app. The draft is a file in the vault (`seats/mayor/prompts/`);
`mw prompt` keeps it, reads it back and runs it. Each call is authenticated as
the home's postern key is for `/api/messages`, against `/api/prompts`.

| Command | Does |
| --- | --- |
| `mw prompt save <name> --summary <text> --body-file <path> [--option '<flag>:<type>=<default>']...` | PUTs `{name, summary, signature, body}`, in place of a prompt of that name. The type is `string`, `int` or `bool`; `<flag>:<type>:required` has no default and must be given |
| `mw prompt list` | one line a prompt: name, summary, signature, tab-separated |
| `mw prompt show <name>` | the whole prompt: summary, signature and body |
| `mw prompt run <name> [--<flag> <value>]...` | checks the call against the signature (an unknown flag, a value not of its type or a required option left out is refused, naming the signature), then prints `PROMPT /<name>` and the options as given, the body with each `<flag>` replaced by its value, and `FACTS` |
| `mw prompt run <name> --card --title <title> --item <item>... [--bead-channel <id>]` | sends the answer the Mayor composed from those facts as a live card, as `mw card send` does, with the prompt's name on the card; prints the card's txid, not the facts |
| `mw card send --title <title> --item '<text>\|<links csv>\|<bead>:<state>'... [--bead-channel <id>]` | posts a `card` record sealed to the Governor as a message is, by `postern_channel`, with no summary; prints its txid, then each item numbered |
| `mw card update <txid> [--item '<n>. <text>\|...']... [--link <n> <bead>]... [--tick <n>]...` | posts a `card-update` record naming the card: items added or replaced by number, links added (`--link <n>:<bead>` too), items ticked off |

`FACTS` is read from the tracker and its notes and spends no tokens, one section
each, `none` when empty, a bead always as its id: `WAITING FOR THE GOVERNOR`
(ready beads labelled `hitl`), `LANDED NOT VERIFIED` (each landed story with its
closing comment's `HOW TO CHECK IT`, as the view's verify need has it), `OPEN
CARDS` (the `postern.question.*` notes), `OPEN DEMOS` (open beads labelled
`demo`) and `HANDS STEPS THAT WAIT` (the `hands.<bead>` steps with no run).
See `features/prompt.feature`.

A live card is the answer kept current: each item has its text, the beads it
links to and what it expects of a bead (`open`, `landed`, `verified`, `closed`
or `answered`), and the card subscribes to those beads and the event kinds the
expectations need (`bead_changed`, `card_answered`), so the app ticks an item off
when its event arrives. An item that gives no expectation has one derived from
its ask: `VERIFIED on X` expects X verified, `Looks good on X` X closed, `Approve
X` and `Answer X` X answered, `Release X` X open. X is the first bead id after
the ask that the item links to, else its first link, else the first bead id. A
state outside those five is refused and nothing is sent. The card's plaintext is
`domain.Card`, an update's `domain.CardUpdate`. See `features/card.feature`.

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
refused together, since a joined vault already has its own database. If `bd
bootstrap` leaves `.beads/config.yaml` modified only by its dropped trailing
newline (bd 1.3.0), that is restored so the first sync is not blocked by it;
anything else it changes is left alone and reported the way any uncommitted
change is.

It then writes `~/.config/mw/config.toml` under the same never-overwrite rule `mw
init` writes it under, and ends with one `mw sync`, printed or its failure.
Moving a seat's home to a new host with this command needs more besides — see
*Moving a host*, above. See `features/init.feature`.

## Running a host on a timer

A host that only works stories needs no session of its own to keep it going: a
`systemd --user` timer runs `mw dispatch` (every five minutes on a host with no event
follower; on the home, where the follower runs the unit the moment a bead is opened or
lands, the timer is an hourly heartbeat, see "mw events: follow, emit, tail, wait"), and each run
starts, spends and ends with that one command. No daemon, and no tokens spent
between ticks. The units are in `contrib/systemd/`: `mw-dispatch.service` (a
oneshot that runs `mw dispatch` as you) and `mw-dispatch.timer` (hourly, on the
clock; a drop-in with `OnCalendar=*:0/5` restores five minutes).

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
the unit is stopped at ten minutes. Every other sync failure is not retried and
fails as before, and `mw sync` by hand never waits. After changing the unit file,
copy it again and `systemctl --user daemon-reload`.

**A second tick while a dispatch is still running does no harm.** A oneshot unit
is never started while it is already running, so ticks do not overlap; the tick
is dropped. The real damper on spending is not the timer but mw's cap: however
often `mw dispatch` runs, it never has more sessions in flight than `cap`
allows. The unit stops a run that takes longer than ten minutes
(`TimeoutStartSec`), because a oneshot otherwise waits for ever on a hung `git`
or `bd`. Ten, not two: on the Boost every `bd` call crosses the tunnel, so a
tick that launches a story can run past two minutes.

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

If the server dies anyway — every pane exiting, `tmux kill-server`, anything
— `Restart=always` and `RestartSec=2` bring the unit's `ExecStart` back
within a couple of seconds, which starts an empty server the next thing that
needs the seat's session (the doctor's cure, a login) finds waiting instead
of a login scope outside the unit. `StartLimitIntervalSec=0` means a server
that keeps dying never exhausts systemd's default restart budget and leaves
the unit `failed`: it always tries again. This is why there is no
`RemainAfterExit=yes` here: that directive keeps a unit reporting `active`
once its tracked process is gone instead of restarting it, which is exactly
how the tmux server going missing once went unnoticed until the next login.
`scripts/check-seat-tmux-respawn.sh` proves the restart live, on a throwaway
`--user` unit and tmux socket so the real session `0` is never touched. Its
units run `tmux` by absolute path with `-L` their own socket, and carry
`RuntimeMaxSec` with a start limit (a bare `RuntimeMaxSec` only restarts a
`Restart=always` unit), so a run killed with SIGKILL leaves nothing that can
outlive it or reach the default socket; the next run's start sweeps what is
stale, and `scripts/check-seat-tmux-leak.sh` proves all three.

The unit is `Type=simple`, not `forking`: `ExecStart` runs `has-session ||
new-session`, then settles into a loop that polls `has-session` and sleeps
for as long as session `0` stays up, so that loop's own process is what
`Type=simple` tracks as the unit's main process — whether this unit started
the session or only found one already there. `Type=forking` used to sit here
instead, expecting `ExecStart` to fork a server and exit; that held only in
the branch where no session existed yet. When one was already up (a login,
or an earlier run of this unit), `has-session` succeeded and the whole
command returned immediately with nothing forked into the unit's cgroup, so
systemd saw its tracked process gone a moment after starting it and, with
`Restart=always`/`RestartSec=2`, restarted the unit every 2 seconds forever
— the found session untouched throughout, but the unit never settling. The
watch loop is what closes that: `scripts/check-seat-tmux-respawn.sh` proves,
on a throwaway unit and socket with a session started before the unit ever
runs, that it now reaches `active`/`running` with zero restarts and leaves
that session exactly as it found it.

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
`mw-seat-tmux.service`'s `ExecStart` against a stand-in `tmux`: a no-op that
settles into watching `has-session` when session `0` is already up,
`tmux new-session -d -s 0` followed by the same watch when it is not.

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
Each tick it skips everything (bar a stale view, below) if the 1-minute load is above `MW_MAIL_LOAD_LIMIT` (the core count); runs `mw sync`
at `nice 19` and idle I/O priority if the last was `MW_MAIL_SYNC_EVERY` seconds
ago or more (300, five minutes; 60 on the desktop, where a sync is a few local
writes);
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

**A draft on the prompt line blocks the typed lines** (mw-gq6.131): the
notifier never types over an unsent line, so while one sits in the Mayor's
prompt, nothing it would type is ever announced. The route that does not care
what is on the prompt is a wait the Mayor's own harness runs: `mw events wait`
(above) now, and `contrib/mail-wait` before it, retired and running it. What
follows is how `contrib/mail-wait` worked, for a host that still has the old
script: a zero-token script the Mayor arms at boot, in the background, and arms again
each time it ends (on new mail, or after `MW_MAIL_WAIT_LIMIT`, 3000 seconds):
every `MW_MAIL_WAIT_EVERY` seconds (30) it lists `bd mail inbox` and, on a host
with a postern key file, reads `mw postern inbox --unread-count`, and ends as
soon as an id the notifier has not announced turns up or the count rises above
the one last noted. Its output — the inbox, or the postern line — is what wakes
the Mayor, so mail reaches it within about a minute whatever the prompt holds,
and nothing is typed into the window. It records the ids as announced and the
count as noted when it ends, so the notifier never repeats them. It runs `mw
sync` only on a host whose beads are a copy of its own (`beads_sync` remote), no
oftener than `MW_MAIL_SYNC_EVERY` and under the notifier's lock, sharing its
`last-sync`; on a host whose `bd` is the Dolt server or reaches it (`backup` or
`shared`, the desktop) mail arrives with no sync and it runs none. While armed
it stamps `wait-armed` in the notifier's state directory each poll; the
notifier reads a stamp under `MW_MAIL_WAIT_STALE` seconds old (180) as "a wait
is armed" and types neither the mail nor the postern line, leaving them still
new for the wait. With no wait armed (none started, or the stamp gone stale)
the notifier types as before, as the fallback. It still types the quiet alarm
either way, and it still does the sync, the view and the snapshot. **Arm it**:
`ln -s ~/millwright/contrib/mail-wait ~/.local/bin/mw-mail-wait` once per host,
then run `mw-mail-wait` in the background from the Mayor's session and re-arm it
whenever it ends (`seats/mayor/procedures.md`, Boot step 7). A second one, with
one armed, says so and ends.

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
`MW_MAIL_VIEW_EVERY`, `MW_MAIL_VIEW_TIMEOUT`, `MW_MAIL_VIEW_MAX_AGE`,
`MW_MAIL_SNAPSHOT_EVERY`,
`MW_MAIL_SNAPSHOT_TIMEOUT`, `MW_MAIL_WAIT_STALE`, `MW_TMUX_SOCKET`) go in an optional
`~/.config/mw/mail-notify.env`, as `NAME=value` lines; the script's header
lists them. A tmux server other than the default is named with
`MW_TMUX_SOCKET`.

On a host with a postern key file, the tick also refreshes the postern
snapshot (`mw postern snapshot`, above) once the vault's beads have changed
since the last attempt and at most once every `MW_MAIL_SNAPSHOT_EVERY` seconds
(default 600, ten minutes) — never unconditionally. Each attempt is bounded to
`MW_MAIL_SNAPSHOT_TIMEOUT` seconds (default 60) and killed past that, logged
once with how long it ran; a slow host whose beads take longer than the default
to snapshot should raise `MW_MAIL_SNAPSHOT_TIMEOUT` — a snapshot that never
finishes inside its timeout is never worth retrying more often (mw-tfne4.12).

Just before it, on the host that serves the live view, the tick refreshes the
view the postern backend serves — `mw postern view` — once the beads have
changed and at most once every `MW_MAIL_VIEW_EVERY` seconds. That setting is
the opt-in: unset (the default) there is no view step at all, so a host whose
notifier predates the live view never starts building one; the host running
the backend that serves the view installs its notifier with
`MW_MAIL_VIEW_EVERY=30` (the vault's hosts/desktop-move.md step 4.3). Bounded to
`MW_MAIL_VIEW_TIMEOUT` seconds (default 60) and logged the same way. It first
asks `mw postern view --help` whether this mw has the view at all (looking for
the view's own usage line: cobra exits 0 for a subcommand it does not know),
and passes over an mw without it silently. The step is retired — it says
"retired, mw postern view --follow does it" once and runs nothing, in a tick
skipped for load as well — whenever `mw-view-follow.service` is active. The view and the snapshot read the
beads' level once a tick between them.

A tick skipped for load (the 1-minute load average above `MW_MAIL_LOAD_LIMIT`,
which defaults to the host's core count, `nproc`; 2.0 where the cores cannot be
counted) still refreshes the view when the beads have changed and the last view
is at least `MW_MAIL_VIEW_MAX_AGE` seconds old (default 300, five minutes), so a
host kept busy by its Builders never leaves the app more than about that far
behind. It logs `skipping this tick but publishing the view, Ns stale`. The
mail line, the sync, the quiet alarm and the snapshot stay skipped, and the view
runs under the same lock as ever (mw-gq6.150).

**Undo it**:

```sh
systemctl --user disable --now mw-mail-notify.timer
rm ~/.config/systemd/user/mw-mail-notify.service ~/.config/systemd/user/mw-mail-notify.timer ~/.local/bin/mw-mail-notify
systemctl --user daemon-reload
```

`disable --now` alone stops it at once. Its output is in the journal:
`journalctl --user -u mw-mail-notify`, with a line as each of steps 2, 3 and 5
starts, so a tick systemd kills shows where. Its sync is `mw sync --no-backup`: the
beads backup (a dolt push) is left to the follower's own sync and dispatch, so a tick
never outlasts its `TimeoutStartSec=4min`, which is above the sync lock's 120 s wait.
The ids it has announced are in
`~/.local/state/mw-mail-notify/announced`; deleting that file makes it announce
whatever is unread again. `contrib/mailnotify_test.go` and
`contrib/mailwait_test.go` run them against a private
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
  `NOT RUNNING` rather than pretending, one recorded `run=blocked` (a refused
  landing) says `refused, waiting on the Mayor (mw retry or a hold)`, and a
  claimed story whose poured formula still has a step open says its close-out
  is blocked.
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
the stories pathed to it that are ready or already claimed. On a host whose
`beads_sync` is `backup` or `shared` that note is read live out of the one
database rather than a cycle behind, and the report's `BEADS SYNC` line says
which mode this host is in and, on the desktop, how long ago its last backup
got through. A host is **ASLEEP** when that note is older than `host_silent_hours` — two by default,
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
report's. The config keys it reads are `vault`, `host`, `host_silent_hours`,
`rig_memory_bytes`, `beads_budget_bytes` and `beads_sync` (*What a host is told*). See `features/status.feature`.

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
a timer, to find the stories it has claimed whose session has gone away. It
reads the stories this host has claimed and asks the tracker which of them
StaleClaims lists: a claim whose lease — five minutes, bd's own fixed TTL —
ran out with no heartbeat since. It costs no tokens and starts no session.

A found story is commented on once, saying when its lease expired, and
recorded `run=stuck`, which `mw status` then shows as `NOT RUNNING`. A story
that `mw next` or an earlier sweep already recorded gone is left alone, so
sweeping twice comments once.

`mw next` itself heartbeats a claimed story's lease every two minutes for as
long as its session is running, so a session actually at work never goes
stale; StaleClaims is left to report only a session that really has gone
away. Along with `vault` and `host`, that is all `mw sweep` reads from the
config.

The only things sweep writes are those comments and `run=stuck` on the
story's own bead. Sweep never kills or restarts a session, never gives a
claim back, and never touches a worktree, git or the ledger: settling a stuck
claim is a separate command. One story's trouble — a write that fails — is
reported on a `!` line and the rest are still examined. See
`features/sweep.feature`.

## Peeking at a running Builder

```sh
bin/mw peek mw-3evcnk.1
```

`mw peek <story>` is how to see where a Builder has got to without guessing
from a tmux target name (which dots in a story id break: the session of
`mw-3evcnk.1` is `mw-3evcnk_1`). It prints the host, the session and whether it
is running, exited or gone, when the story was launched and how long ago, the
formula's current step, and the last 20 lines of what the harness has recorded
of the session, or of its pane when it has recorded none. A closed story says
so, and how long it ran.

The step is the first open step of the story's molecule with how many are still
open; a story with no molecule recorded is a `print-mode run, steps not
tracked`. A story pathed to another host is read there: mw runs `mw peek
--here` on it over the ssh line `[hands_hosts]` gives, and prints what comes
back. `--here` reads this host's session whoever the story is pathed to. Peek
writes nothing and costs no fuel. See `features/peek.feature`.

## Closing what is plainly finished

```sh
bin/mw tidy [--dry-run]
```

`mw tidy` closes an `Answer: ...` mail bead over a day old, closes a mail bead
the mailbox holds read and over seven days old, and clears a postern question
note whose bead is closed. It does nothing else: it never closes a story, an
epic, a map or a hitl bead, and never deletes. Each act writes
`Tidied by mw tidy: <why>` on what it touched, and one line to the Millhand's
tick log. The tick runs it after its sweep; `--dry-run` lists what it would do
and changes nothing. The bounds are the table in `docs/tidy.md`; see
`features/tidy.feature`.

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

## Switching the Mayor's model in a talk

```sh
bin/mw talk model sonnet --talk talk-7 --turn 3
```

`mw talk model opus|sonnet|fable|haiku` answers the Governor's model chip in a
talk. A session cannot change its own model, and typing `/model` into a live
Claude Code window failed and froze the seat on a dialog, so the switch is a
fresh Mayor on the chosen model. The command types into no window and starts
nothing. It maps the chip name to the full model id (`sonnet` is
`claude-sonnet-5-5`, `opus` `claude-opus-5-5`, `fable` `claude-fable-5-1`,
`haiku` `claude-haiku-4-5-20251001`), speaks on the talk "Switching to Sonnet:
a fresh Mayor takes the line in about a minute", appends a dated line to
`.mayor-talk.log` in the vault, and prints the one line the Mayor runs next:

```
hand off, then: bin/respawn-mayor high claude-sonnet-5-5
```

`--talk` and `--turn` name the Governor's turn it answers, as `mw talk wait`
printed them. An unknown chip name is refused, non-zero, before anything is
said. If the answer cannot be sent, that is logged and no respawn line is
printed. `mw talk wait` names the same `hand off, then:` line under a turn that
changes the model. See `features/talk_model.feature`.

## Waiting for the Governor's next turn in a talk

```sh
bin/mw talk wait
```

`mw talk wait` is a zero-token wait, for the Mayor's harness to run in the
background: it holds the postern backend's `/api/events` stream open, opening
it again after a pause that doubles from `--min-backoff` to `--max-backoff`,
and on a `message` event reads the records since its own cursor (the bd kv note
`postern.talk.cursor`, never the postern inbox's). It ends at the first talk
record that decrypts, is verifiably the Governor's and is a turn or the end of
the talk, and prints the turn (talk id, turn, role, model, cut, text), the
milliseconds from the event to the print, and any new Deputy mail to the Mayor
(each message once, noted under `postern.talk.mail`; nothing is marked read).
A talk record to another key or from anyone else is passed over.

```
talk talk-7 turn 3 (role turn)
model sonnet
cut yes
text: What landed today?
index-to-print 12 ms
```

A talk the Governor opened from a card, a thread or a prompt carries an `about`
field, and the wait prints it after the text line as `about: bead mw-xxx <title>`
(no such line when the record has none), so the first answer is about the right thing.

It also ends when a new postern message for the Mayor's key arrives, so the
Governor's words in a channel are not left unread while a talk runs. A message
is new when it is past the postern inbox's cursor (read, in the wait's own
terms, with `mw postern inbox`, which this wait only reads and never moves);
each is reported once, under its channel, txid and first line, and left unread
for `mw postern inbox` to read:

```
new postern message: 1 unread, read them with mw postern inbox
  channel general, txid direct:e1065e4e...: I think the deputy has stopped answering me here
```

A Governor turn that arrives with one wins: the turn is printed first and the
messages after it.

It also ends the instant the Governor's **Call me** arrives: a `call` record
(postern's `docs/protocol.md` section 21) whose plaintext is a `request` from
him is printed as `call <txid> at <time>: <text>`, and one whose plaintext is a
`later` on a ring prints `later <ring txid>`. The Mayor answers a request with
`mw talk call` (below) or on the Talk line. A call record to another key, from
anyone else, or a ring, is passed over.

```
call direct:3f2a… at 2026-10-01T14:03:11Z: Call me
```

`mw postern inbox` and its `--unread-count` skip talk and call records, so
`mail-wait` never wakes the Mayor a second time for one turn. A first run starts at the
index's head, so a turn from before it ever ran is not waited for. It ends,
saying so, after `--limit` (`MW_TALK_WAIT_LIMIT` seconds, 3000 by default,
like `mail-wait`): arm it again. The Mayor's key is let onto the event stream
without a licence (postern's `docs/protocol.md` section 20), but reading the
records behind an event needs a cockpit licence on it. See
`features/talk_wait.feature`.

## Answering the Governor in a talk

```sh
bin/mw talk say "Two stories landed." --talk talk-7 --turn 3
bin/mw talk say "One moment." --talk talk-7 --turn 3 --holding
bin/mw talk say "Goodbye." --talk talk-7 --turn 4 --end
bin/mw talk say "Two stories landed." --talk talk-7 --turn 3 --link mw-x.1 --link mw-x.2
```

`mw talk say` encrypts the words to the Governor as postern's `docs/protocol.md`
section 20 turn plaintext (role `answer`, `holding` or `end`, with the `--talk`
and `--turn` of the Governor's turn it answers, as `mw talk wait` printed them)
and delivers the record straight to the backend, whatever `postern_channel`
says. The record's class is `talk` and it carries no summary, so no word of it
is pushed or logged. It touches no bead and no note, and prints the txid and the
milliseconds it took, which the Governor's eight seconds are spent against.
`--link <bead>` (repeatable) adds the ids to a `links` array in the plaintext,
beside `text` and never in it, so the Governor can open them from the answer
without hearing them read; with no `--link` the plaintext has no `links` key.
Postern's section 20 does not yet define `links`: that is the postern side's story.

```
talk talk-7 turn 3 (role answer) sent
txid direct:3f2a…
elapsed 41 ms
```

See `features/talk_say.feature`.

## Calling the Governor back

```sh
bin/mw talk call "Back now: two landings."
bin/mw talk call "Back now: two landings." --link mw-x.1 --link mw-x.2
bin/mw talk call "Back now: two landings." --chain
```

`mw talk call` is the Mayor's call-back, the answer to a call request that
`mw talk wait` printed. It encrypts the text to the Governor as postern's
`docs/protocol.md` section 21 `ring` plaintext (`role`, `text`, and `at`, the
Unix seconds now) and delivers the record straight to the backend, whatever
`postern_channel` says, exactly as `mw talk say` does. The record's class is
`call` and it carries, in the clear beside `class` and `ct`, `"role": "ring"`,
which makes the backend push "The Mayor is calling", and the text cut to 80
runes as the clear `summary`, the push's body. It touches no bead and no note, prints the txid and the milliseconds it
took, and refuses when `postern_governor_key` is not set. `--link <bead>`
(repeatable) adds the ids to a `links` array in the plaintext, beside `text`
and never in it; section 21 does not yet define `links`, as it did not for a
talk turn.

`--chain` is for a phone that cannot reach the backend: the ring is also
broadcast on chain, with the role and no summary (a chain record is public for
good, so the reason is never on it), through the backend's broadcast (the backend is local
to the Mayor, so this works whatever the phone can reach), under the same
`postern_float_sats` cap as `mw postern send`, and both txids are printed. You
do not have to remember it: `mw talk wait` notes, under the bd note
`postern.talk.channel`, how the Governor's newest talk or call record came, and
when it came by chain (a bare txid) `mw talk call` broadcasts without the flag;
after a direct one (`direct:<id>`), or before any, it does not. The direct
delivery goes first. A `--chain` that fails is an error naming the txid that
did go direct; a chain added by itself that fails is only said, on a
`chain: not sent: ...` line, and the call still succeeds.

The ring also rides the emergency lane (mw-jrx0s.12): `mw talk call` writes one `message`
event, in that lane, to the home's event log, its detail the ring's txid, so the follower
sends the Governor's app a record of it at once. A failure to write it is said and the call
still succeeds.

```
call ring sent
txid direct:3f2a…
chain txid 9c1e…
elapsed 41 ms
```

(The `chain txid` line is there only when the ring was broadcast.)

What the phone does: on a direct ring it reads the record from the backend; on a
ring that was also broadcast it can read the chain record when the backend is out
of reach. Either way it is the same ring, and it is class `call`, never pushed
with words.

See `features/talk_call.feature` and `features/talk_wait.feature`.

## The Talk line and the Deputy

A Talk is a spoken conversation between the Governor and the Mayor on
Postern's Talk line, made of turns. Five commands serve it, each described in
its own section above or below:

| Command | What it does |
| --- | --- |
| `mw talk wait` | Holds the event stream open and ends at the first turn (or end), or call request, the Governor sends the Mayor's key; prints it. `--limit` (default 50m, `$MW_TALK_WAIT_LIMIT`), `--min-backoff`, `--max-backoff`. |
| `mw talk call <text> [--chain] [--link <bead>]...` | Sends the Mayor's call-back, a `ring` in a class `call` record, direct, with the clear role `ring` and the text as its summary; `--chain` (or a chain-borne last Governor record) also broadcasts it. |
| `mw talk say <text> --talk <id> --turn <n>` | Sends the Mayor's spoken answer, class `talk`, no summary. `--holding` for a short answer while the real one comes, `--end` to end the talk. |
| `mw talk model opus\|sonnet\|fable\|haiku --talk <id> --turn <n>` | Answers a model chip: speaks the switch, logs it, prints `hand off, then: bin/respawn-mayor high <full id>` for a fresh Mayor. Types into no window. |
| `mw deputy [--reason text]` | Brings up the Deputy, who does the clerical and orchestration work while the Mayor talks. |

```sh
bin/mw deputy --reason "the Governor is in a talk; land mw-abc.1"
```

`mw deputy` starts the Deputy's next session on this host, as `mw seat up
deputy` does, in a window named `deputy-*` that closes itself once the session
has handed off, at high effort on config `deputy_model` (`sonnet`, or
`$MW_DEPUTY_MODEL`). Its kickoff tells it to arm `mw events wait --for deputy
--kinds mail`, work the mail, report by mail and hand off when idle,
then gives the `--reason`. It refuses and starts nothing when
`seats/deputy/charter.md` is missing. If a `deputy-*` window is already open it
starts nothing, says so in one line and exits **8**, so the Mayor can mail the
Deputy and fire, and tell "already up" from a failure (1).

An open window is not always a Deputy that is reading its mail: one that
finished a brief and sits at an empty prompt will not see mail until something
wakes it. So when the window is up and the Deputy's box holds unread mail, `mw
deputy` looks at the pane. If it is idle at an empty input line, `mw deputy`
types the same line the Mayor's notifier types, `New mail for deputy: N
message(s). Run bd mail inbox.`, with the half-second pause before Enter that
the notifier uses, prints `nudged the Deputy in window <name>` and exits 0. If
the pane is busy (`esc to interrupt` on screen, or text already on the input
line) it types nothing, says `the Deputy is busy in window <name>; the mail
waits` and exits **8**. With no unread mail, or a pane or box it cannot read,
it says "already up" as above. See
`features/deputy.feature`, `features/talk_wait.feature`, `features/talk_say.feature`
and `features/talk_model.feature`.

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
what hub and unit **wg** dials and restarts, and where state is kept:

```toml
[doctor]
units          = ["mw-dispatch.service", "mw-millhand-tick.service", "mw-millhand-review.service", "mw-doctor.service"]
state_dir      = "/root/.local/state/mw-doctor"   # default: ~/.local/state/mw-doctor
reach          = ["api.anthropic.com:443", "github.com:443"]
powershell     = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
tunnel_unit    = "reverse-tunnel.service"         # default; the unit tunnel restarts
tunnel_host    = "vps"                            # default: the [watch] table's host
tunnel_probe   = "ss -ltn sport = :2222"          # default; run over ssh on tunnel_host
doctor_wg_hub  = "10.88.0.1:22"                   # default; the hub's ssh host:port, dialed over wg0
doctor_wg_unit = "wg-quick@wg0"                   # default; the unit wg restarts
tmp_leftovers_budget_bytes = 200000000            # default (200 MB); tmp-leftovers' and go-build's own budget
root_disk_budget_bytes     = 0                    # default 0 = root-disk-budget is inert; else the Windows drive's free bytes when the WSL vhdx was put there
root_disk_margin_bytes     = 10000000000          # default (10 GB); headroom kept under that budget
battery_low_percent        = 25                   # default; battery alarms on a fall to this, discharging
battery_critical_percent   = 10                   # default; and again on a fall to this
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

**wg** cures this host's wg-quick@wg0 unit going down while the hub stays
reachable no other way: it TCP-dials `doctor_wg_hub`'s ssh port over wg0, no
root, ok when it answers, faulty once it has looked unreachable for 3 minutes
straight. It is cannot-tell, not faulty, when the internet itself looks down
(checked the same way **wifi** and **tunnel** do), when this host has no wg0
interface at all, or when wg0's own address is the hub's — this host being
the hub itself, with nothing to restart. Its cure is `sudo -n systemctl
restart <doctor_wg_unit>`, a 15 s settle, then a re-probe; a `sudo -n` that
needs a password fails naming the sudoers line this host lacks, rather than a
bare exec error. Its damper is 15 minutes with a cap of 3, and its way back
(never run by the check) is `sudo systemctl stop <doctor_wg_unit>`. It never
edits the unit or the wireguard config, and never runs anything elevated
beyond that one restart.

**timers** cures a factory timer left stopped by the rig's own machinery
(a hand `systemctl --user stop`, a reinstall that missed its timer, a
daemon-reload that dropped one) and so gone silent, watching nobody: for each
`units` entry whose matching `<name>.timer` is enabled here at all (a timer
not installed on this host is skipped, not faulted) but not active, its cure
is `systemctl --user start <timer>`, its damper 30 minutes with a cap of 3,
and its way back `systemctl --user stop <timer>` for exactly the timers the
cure started.

**beads-server** is for a host whose `bd` talks to a Dolt server: a `shared` host
(the Laptop on a boost) reaching the desktop's database, and the home
(`backup`) reaching its own `dolt-beads` server. It is one TCP dial, with a 3 s
timeout, of `BEADS_DOLT_SERVER_HOST` on `BEADS_DOLT_SERVER_PORT` (3307 when
unset) as `mw doctor`'s own environment has them, from `dispatch.env`. It is
faulty when the server does not answer — on the home the reason says it is this
host's own `dolt-beads` and to check `systemctl --user status dolt-beads` — and
there is no cure from here (starting the unit is for a person to ask for), so,
as with **beads-size**, the second faulty run writes the note that wakes the
Millhand; **wg** is the check that restarts a shared host's end of the link. A
`remote` host, and a `backup` host with no `BEADS_DOLT_SERVER_HOST` in that
environment (beads still embedded), read ok, saying why there is nothing to
reach; a `shared` host with none reads cannot-tell. `mw-doctor.service` reads
`dispatch.env` and not `beads.env`, so on the home `dispatch.env` must also carry
`BEADS_DOLT_SERVER_HOST` (and `BEADS_DOLT_SERVER_PORT` when it is not 3307), as
the Laptop's does: otherwise the check reads ok and never dials.

**beads-size** watches `.beads` against the same budget `mw status`'s
BEADS line warns on, 1.5 GB unless `beads_budget_bytes` (or `MW_BEADS_BUDGET_BYTES`)
says otherwise; past it there is no cure — repacking would delete packs
— so it only ever writes the check's own `doctor.beads-size` note for the
Millhand to look at.

**beads-stores** looks only at the vault's files: faulty when `.beads/dolt` and
`.beads/embeddeddolt` both exist, which is what a `bd` run without
`BEADS_DOLT_*` leaves beside a server-mode database. The reason names the
stray path; there is no cure — a person checks it is empty, removes it, and
sources `~/.config/mw/beads.env` — so the second faulty run writes the note.
mw's own `bd` runner now refuses a vault holding `.beads/dolt` while
`BEADS_DOLT_SERVER_HOST` is empty.

**postern-transcribe** watches, on the home host only, that a voice note the
Governor sends can be heard: it is faulty when `postern_transcribe_cmd` is unset,
when its program is not an executable, or — for `contrib/postern-transcribe` —
when `ffmpeg`, the whisper CLI or the model file is missing, and the reason names
which. There is no cure: installing them is host work. Without a transcriber
`mw postern inbox` says "voice note, not transcribed: postern_transcribe_cmd is
not set" on the line it prints and in the bead comment. `mw doctor postern-transcribe
--dry-run` shows it.

**tmp-leftovers** cures the fault behind the VPS reaching 94% disk on
2026-09-25 from the factory's own dead leftovers: a killed bd's dolt spool
files (`/tmp/nbs-spool-*`), `/tmp/bd`, and a stale Claude Code session
directory (`/tmp/claude-0/<session>`). "Dead" is read the way `fuser` or
`lsof +D` would answer it — no process on this host has the path open, read
by hand over `/proc` so no external program has to be on PATH — never a path
this check does not know about, and never one that is live. Faulty once
their total is past `tmp_leftovers_budget_bytes`, its cure is a plain
`os.RemoveAll` on exactly the dead ones found; its damper is 5 minutes with a
cap of 3, and its way back is "none: nothing to restore" — everything it
removes had no live owner. `~/.cache/go-build` is checked against the same
budget on its own and cleared with `go clean -cache`, a rebuildable cache,
rather than deleted by hand; it is never folded into the leftovers' own
total. A go-build cache past budget with a file open under it, or a `go`,
`gotestsum`, `compile`, `link` or `vet` process running, is a live build or
test rather than a leftover: Probe reports it ok instead of faulty, and Cure
leaves it for a later tick rather than clearing it out from underneath.

**root-disk-budget** watches the desktop's WSL root against the Windows
drive that holds its `ext4.vhdx`. The vhdx is sparse: it grows as ext4
allocates blocks and never shrinks by itself, and when that drive is full
WSL's root goes read-only (the desktop was down about two hours on
2026-10-03). WSL there is sealed from Windows, so the doctor cannot read the
drive's free space; the nearest read-only signal from inside is the root's
used bytes (`statfs` of `/`), a lower bound of the vhdx's size, since files
deleted inside WSL do not shrink the vhdx and compaction is a hands step.
`root_disk_budget_bytes` is the drive's free space at the time the vhdx was
placed there; the check is faulty once used bytes pass that budget less
`root_disk_margin_bytes`. With the budget at 0, the default, it is inert: it
says ok, "n/a: no root_disk_budget_bytes set", and writes nothing. It changes
nothing on the host: its cure is one note to the Mayor naming the figures and
the hands step (free space on that drive, compact the vhdx with
`Optimize-VHD`, or move it); its damper is 6 hours with no cap, so the Mayor
hears again no more often than that, and its way back is "set
`root_disk_budget_bytes = 0` in `[doctor]`". The check stays faulty until the
budget is raised or the drive has room.

**mayor-gone** respawns the Mayor when the window its own vault-local
`.mayor-acting` names is gone, or is open but its pane holds nothing but a
bare shell — the same reading of `.mayor-acting`
`contrib/health/mw-health.sh` keeps for its own `mayor=gone` line. Its probe
is cannot-tell, naming which, when `.mayor-acting` is absent (a host the
Mayor does not sit on) or the vault's `bin/mayor-up` is missing or not
executable — it never invents its own way to bring a Mayor up. Its cure is
always that script, `MW_DOCTOR=1 <vault>/bin/mayor-up`, given 120 s: it
brings up the tmux server if that is what is missing, then respawns from the
newest handoff with a note that its predecessor is gone. Its damper is 30
minutes with a cap of 2 per episode, and its way back, once a cure has run,
is `tmux kill-window -t '<window id mayor-up started>'` — before that, dry
run or damped, it is the mayor-up line itself, there being no window id yet
to know a kill from. `mw doctor mayor-gone` is the recovery by hand;
`--dry-run` prints the line it would run and changes nothing.

**battery** is the warning the home never got before it slept on a flat
battery (2026-10-01, 17:54-18:09Z; mw-gq6.206). It reads the first
`/sys/class/power_supply/BAT*`: faulty only while the status is `Discharging`
and the capacity is at or under `battery_low_percent`, and then only once per
fall below each line, the low one and then `battery_critical_percent`. Its cure
is the alarm itself, in the emergency lane of the event log (`mw events emit
--emergency`) — "Laptop battery 24%, discharging: plug it in or it sleeps" — so
the Governor's phone buzzes within one 5-minute doctor run. What it has alarmed
at is forgotten when the battery next charges or rises above the low line; an
alarm that could not be written is tried again, 3 times at most. Charging, Full,
and a host with no battery are ok. It changes nothing on the host.

**mayor-stale** is the Mayor's heartbeat. A held Mayor's pane is redrawn
while a turn runs (an elapsed clock beside "esc to interrupt"), so the check
keeps a hash of `tmux capture-pane` in its own state and faults once the pane
of the window `.mayor-acting` names has stood unchanged for
`mayor_stale_minutes` (the `[doctor]` table; default 15, checked every 5
minutes by the timer) while it shows "esc to interrupt" (a frozen turn) or
text on the input line (a nudge nobody took). A Mayor waiting at an empty
`❯` prompt redraws nothing and is alive however long it stands. A
seat not held — no `.mayor-acting`, a host that is not home, a window gone or
holding only a bare shell — says ok; the last two are `mayor-gone`'s to
judge. A fresh beat (the pane changes) clears it and starts the count again.
Its cure closes the stale window, runs the vault's `bin/mayor-up` (up to
three tries, as the old Mayor's process may take a moment to go) and sends
one `alarm`-class Postern push naming the window, the minutes and the new
window, or that no Mayor could be started. A pane showing the harness's
"Do you want to proceed?" is a Mayor waiting on a person, not a dead one:
the window is left alone and the alarm carries the prompt and its options.
Damper: 30 minutes, cap 1 per episode, so one respawn and one alarm to a
stall. Its way back is `tmux kill-window -t '<window id mayor-up started>'`.
`mw doctor mayor-stale` runs it by hand; `--dry-run` reports a stale seat
and changes nothing but its own record of the pane.

**mayor-stuck** finds a Mayor that holds the seat but cannot get an answer:
the network under it changed (on 2026-10-06 the Laptop left the phone tether
and the proxy the Mayor was started behind, an ssh tunnel, closed), so every
reply is an "API Error" while the process lives and the pane redraws, which
`mayor-gone` and `mayor-stale` both call fine. It reads the Mayor's transcript
(the Transcripts port, as `mw seat context` does) and faults when the last 10
minutes hold at least one harness reply and every one was an API Error; a
single reply that worked, or no reply, is ok. A seat not held says ok, as for
`mayor-stale`. Its cure first drops each of `HTTPS_PROXY`, `HTTP_PROXY`,
`ALL_PROXY` and their lowercase forms whose host and port refuse a TCP connect
from the environment `bin/mayor-up` runs in and from the tmux server's global
environment (`tmux set-environment -g -u`), keeping a proxy that answers;
then closes the Mayor's window and runs `bin/mayor-up`, which starts a
successor from the newest handoff with no handover; then sends one alarm, on
the emergency lane, naming the window it started and any proxy it dropped. The
Governor is told once to an episode: damper 30 minutes, cap 1, and the alarm is
cleared by a normal-lane event once the Mayor answers again. Its way back is
`tmux kill-window -t '<window id mayor-up started>'`. `mw doctor mayor-stuck`
runs it by hand; `--dry-run` names the finding and changes nothing.

**postern-channel** faults the home host, and only it, when its config does not
send the Mayor's postern messages directly: `postern_channel` reads chain
(the default, when `~/.config/mw/config.toml` lacks the key) rather than
direct, or `MW_POSTERN_CHANNEL` says so. Every send is then a chain
transaction, and past WhatsOnChain's newest-100 history cap the Mayor is locked
out. It names the key and the file and has no cure — the doctor never edits the
config; a person adds `postern_channel = "direct"`. A host the vault's home file
says is not home reads ok.

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
command's. `MW_HEAVY_MEMORY_MAX` (default `512M`, the VPS's; half of `MemTotal` on a host with more than 8 GB) and `MW_HEAVY_SWAP_MAX`
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

## Licence

millwright is released under the MIT licence; see [LICENSE](LICENSE).

A rig's file may also say `guest = "<owner>"`: a guest repo, whose owner is not the
Governor and whose stories the factory files only on the owner's ask. `mw file`
refuses a plan with a story in one, naming the rig and the owner, unless it is
given `--guest-ask "<their words>"`, which are written on the epic. `mw status` and
`mw brief` show `guest: <owner>` beside a guest rig's stories.
