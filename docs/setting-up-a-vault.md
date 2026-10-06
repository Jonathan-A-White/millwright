# Setting up a vault

A step-by-step guide to standing up your own Millwright factory, from an empty
machine to a first Mayor session, then to more rigs, more hosts, a phone and (if you
want it) the BSV chain. It is written to be followed top to bottom by a person or
by an agent. It points at the README and the other pages for detail rather than
repeating them: where a step says *see*, the link is the reference.

Words with a fixed meaning here (Seat, Session, Story, Path, Formula, Rig, Host,
Fuel, Governor, Mayor, Builder) are defined in [`CONTEXT.md`](../CONTEXT.md).
You are the Governor of your own factory.

## What you get

A **vault** is one private git repository holding the factory's state: the seats'
charters and handoffs, host notes, plans, and the one beads database that tracks
every rig's work. You talk to the **Mayor**, a Seat whose Sessions turn what you
want into epics of stories, each with acceptance criteria and a Path. A
**dispatcher** on each host that builds (`mw dispatch`) claims the ready stories
within its cap, cuts a worktree in the story's **rig**, pours the story's
**formula** into steps and starts a **Builder** Session in a tmux window. The
Builder works the steps and commits; `mw next` checks the acceptance criteria and
lands the branch. Between sessions nothing runs but timers and the **follower**,
which costs no Fuel. The seat charters are plain files you own and may rewrite
(see [`template/seats/README.md`](../template/seats/README.md)).

Why the factory is built this way, rule by rule and with what to do on less, is
[Best practices](best-practices.md).

## Prerequisites

- A Linux host with systemd and a user manager. Debian or Ubuntu (WSL Ubuntu
  included) is what the installer supports; anywhere else, install the tools
  below by hand.
- Sudo, once, for the packages the installer will name, and to let user units run
  while you are logged out (`loginctl enable-linger`).
- Tools: git, tmux, jq, ripgrep, python3, curl, make and the GitHub CLI `gh`; Go
  and `bd` (the beads command line) at the versions pinned in
  [`scripts/pins.env`](../scripts/pins.env). Go is only needed to build `mw`.
  `dolt` is needed only if a host will serve the beads database to others (a
  second host, below).
- The Claude Code harness, installed and logged in on every host that runs
  Sessions. Fuel is whatever your Claude plan allows; the factory is built to
  spend it carefully.
- An account on a git host with a **private** repository for the vault, and
  credentials on each host that can push to it (`gh auth login && gh auth setup-git`,
  or a deploy key).
- Optional, and not needed to begin: a second always-on machine, a domain name,
  a phone.

## One host first

Do everything in this chapter on one machine. It becomes the **home**: the host
that holds the beads database and the Mayor.

1. **Install `mw` and its tools.** Run the one-line installer from the README's
   *Quick start* (read it first if you prefer: it never handles a secret and
   never runs `sudo`). It builds `mw` and puts it on your `PATH`, then prints the
   hand steps that are yours: log in to Claude, log in to GitHub, tell git who
   you are. Do them and run each one's check. Running it again is safe.
   See [*Installing*](../README.md#installing).
2. **Make the vault.** `mw init --vault <dir> --prefix <prefix> --host <name>`,
   where `<prefix>` begins every story id (`demo` gives `demo-ab1`) and `<name>`
   is this host's name in stories' Paths (`home`, `laptop`). It lays down the
   seat kit from the template (the Mayor, Deputy, Builder and Millhand charters,
   the Mayor's procedures sheet and handoff shape, the vault's `CLAUDE.md`), makes
   `<dir>` a git repository with a first commit, makes the beads database, and
   writes `~/.config/mw/config.toml` unless one is there. See
   [*Making a fresh vault*](../README.md#making-a-fresh-vault) and
   [`template/seats/README.md`](../template/seats/README.md).
3. **Give the vault a private remote** and push: create the empty private
   repository, then `git -C <dir> remote add origin <url>` and
   `git -C <dir> push -u origin HEAD`. The remote is the vault's backup and how
   other hosts will join it. Nothing in the vault is meant to be public.
4. **Beads: start embedded.** A fresh vault's beads database is embedded: no
   server, no extra process, one writer at a time, which is right for one host.
   `bd` is run one command at a time; `mw` follows that rule and so should you.
   The Dolt server mode is for sharing the database with a second host and is
   in [A second host](#a-second-host-the-boost). Check beads works with
   `bd -C <dir> list`: an empty list is right.
5. **Read the seat kit and make it yours.** In `<dir>/seats/`: write
   `mayor/vision.md` (what you want from the factory; it is yours alone to
   write), skim each charter, and set the beads migrator line in the vault's
   `CLAUDE.md` to this host's name. Charters change only with your word, so
   edit them now, commit, push, and treat later changes as deliberate.
6. **Install the formulas.** A formula is the procedure a story is worked by;
   the two this rig ships are `chore` and `tdd-feature`. Copy them into the
   vault's beads directory and check they are listed:
   `mkdir -p <dir>/.beads/formulas`, then
   `cp formulas/*.formula.json <dir>/.beads/formulas/` from the `mw` checkout,
   then `bd -C <dir> formula list`. See [`docs/formulas.md`](formulas.md).
7. **Check the host.** `mw status` prints what the factory sees (empty is fine)
   and `mw doctor` checks the host itself. A line you do not understand is in
   [*mw doctor*](../README.md#mw-doctor).

### tmux and the user units

Seats and Builders run in tmux windows: every Builder gets its own tmux session
named after its story, and `mw seat up` opens a window for a seat. tmux needs no
configuration. If you want the Mayor's tmux server kept alive across a user
manager dying (a small server under memory pressure), see
[*The seat's tmux server on the VPS*](../README.md#the-seats-tmux-server-on-the-vps);
a laptop or desktop does not need it.

The units live in [`contrib/systemd/`](../contrib/systemd/) and are *linked* into
`~/.config/systemd/user` by [`scripts/install-units.sh`](../scripts/install-units.sh);
run from the `mw` checkout. Nothing is armed unless you say `--enable`.

1. Write `~/.config/mw/dispatch.env`, one line, saying where `mw`, `bd`, `git`,
   `tmux`, `claude` and `go` are, because a user unit gets a bare `PATH`:
   `PATH=<your bin dirs>:/usr/local/bin:/usr/bin:/bin`. See
   [*Running a host on a timer*](../README.md#running-a-host-on-a-timer).
2. Run `sh scripts/install-units.sh` with no name to list the pairs and what is
   installed, then arm the ones this host wants: `sh scripts/install-units.sh
   --enable mw-view-follow mw-dispatch`. `mw-dispatch` runs a dispatch and
   `mw-view-follow` is the follower. `mw-health` and `mw-doctor` are the host's
   health line and self-check, worth arming; `mw-millhand-tick` and
   `mw-millhand-review` belong to a Millhand, which you can add later.
   `--dry-run` says what any command would do.
3. Let the units run while you are logged out: check `loginctl show-user $USER -p
   Linger`; if it says `Linger=no`, run `sudo loginctl enable-linger $USER`.
   The installer prints this and never runs it.
4. **The follower** (`mw events follow`, unit `mw-view-follow`) is the factory's
   zero-token loop: it watches the beads database for changes, writes the event
   log and springs the other jobs (dispatch when a bead is opened, mail checks)
   so the timers are only heartbeats. With no Postern key it writes events and
   sends none. Enabling it also retires `mw-mail-notify.timer`: the follower runs
   that job. See [*mw events: follow, emit, tail, wait*](../README.md#mw-events-follow-emit-tail-wait)
   and [`docs/events.md`](events.md).
5. **Stop it at once** with `systemctl --user disable --now mw-dispatch.timer`.
   Sessions already running are tmux sessions and are left alone. Output is in
   `journalctl --user -u mw-dispatch`.

A host's `cap` (in `~/.config/mw/config.toml`, 1 by default) is the most Builder
Sessions it runs at once. Leave it at 1 until you have watched a few stories land.

### The first Mayor, by hand

A fresh vault has no handoff, so `mw seat up mayor` has nothing to boot from yet.
The first conversation is by hand instead:

1. Open `claude` with the vault as its working directory.
2. Tell it to read `seats/mayor/charter.md`, `seats/mayor/vision.md` and
   `seats/mayor/procedures.md`.
3. Talk. Say what you want built. The Mayor records it, asks questions until
   each piece is sharp, and proposes epics of stories.
4. End by having it write the first handoff, in the shape of
   `seats/mayor/handoffs/TEMPLATE.md`, into `seats/mayor/handoffs/`, and commit
   and push it.

From then on every Session starts with `mw seat up mayor`, which opens a tmux
window, primes the Session from the charter and the newest handoff, and runs it
in the vault. See [*Starting a seat's next session*](../README.md#starting-a-seats-next-session).

## Adding a rig

A rig is a git repository the factory works on. On each host that will build in
it:

1. Check it out somewhere (any directory) and make sure `git push` to its origin
   works from that host.
2. Name it in `~/.config/mw/config.toml`, under `[rigs]`: `myrig = "<path>"`.
   (`mw init --rig myrig=<path>` writes the line when it makes the file.) The
   name is what a story's Path calls it. A story is only ever claimed on a host
   that has the rig checked out.
3. Give the Builder what it needs to work there: a file
   `seats/builder/rigs/<rig>.md` in the vault, the Builder's memory of the rig
   (the commands to build and test, and the traps), under the byte budget
   described in [`template/seats/builder/rigs/README.md`](../template/seats/builder/rigs/README.md).
   The rig's own `CLAUDE.md` should say how to build and test it.
4. Optionally ask more of its epics: a file `rigs/<rig>.toml` in the vault names
   description sections an epic must have and labels its last story must carry.
   `mw file` refuses a plan that lacks one. See
   [*What a rig requires of its epics*](../README.md#what-a-rig-requires-of-its-epics).
5. Say what happens after a landing, if anything (deploying a built site, say):
   see [`docs/site-deploy.md`](site-deploy.md).

The Mayor then files a plan: an epic with a default Path and its stories
(`mw file plan.json`). Everything is filed held and only your approval releases
it (`--approve`, or `mw release <epic>`). See
[*Filing a plan*](../README.md#filing-a-plan) and, for what a dispatch does,
[*Dispatching a story*](../README.md#dispatching-a-story). Try
`mw dispatch --dry-run` first: it says what it would start and writes nothing.

## A second host (the Boost)

A second machine gives the factory more Builders at once, and somewhere to run
when the first is off. The home keeps the one beads database; the other host is
the **Boost** and reaches it over the network.

1. **Put the hosts on a private network**: any that gives each a stable
   address and keeps the database off the public internet. WireGuard is what
   the factory itself uses (see [`contrib/wg-enrol`](../contrib/wg-enrol)).
2. **Serve beads from the home.** Install `dolt`, run a `dolt sql-server` over
   the vault's `.beads/dolt` bound to the home's private address, create the
   user `bd` logs in as, and keep its connection in `~/.config/mw/beads.env`
   (`BEADS_DOLT_SERVER_HOST`, `BEADS_DOLT_SERVER_PORT`, the user and password,
   a file only you can read), as a user unit started at login. Put the host
   and port in `dispatch.env` too, so the timers see them. Set `beads_sync =
   "backup"` in the home's config: it keeps GitHub as a backup of beads on a
   half-hourly cadence. Step 2 of [`docs/home-move.md`](home-move.md) shows
   exactly how a host with this server is brought up on a fresh data directory,
   and [*The factory on one host*](../README.md#the-factory-on-one-host-the-desktop)
   says what each setting means. Check with `mw doctor beads-server`.
3. **Join on the Boost**: `mw init --join <vault-url> --vault <dir> --host <name>
   --rig myrig=<path>`. It clones the vault and attaches to the beads
   database; it never makes a database of its own and never overwrites a config
   file that is there. See [*Joining an existing vault*](../README.md#joining-an-existing-vault).
4. Set `beads_sync = "shared"` on the Boost, point its `dispatch.env` at the
   home's beads server, raise its `cap`, run `sh scripts/install-units.sh
   --enable mw-dispatch` there, and path stories to the Boost with
   `host=<name>`. It keeps no beads of its own and never syncs them.
5. To move the Mayor's home later, or if the home dies, see
   [`docs/home-move.md`](home-move.md) and [*Moving a host*](../README.md#moving-a-host).
   Two hosts must never run the same seat's timers at once.

### A small always-on server as the front door (optional)

A small VPS is not a home (nothing needs to stay up on it) but is useful as the
one public address: it terminates TLS for the Postern backend (below), is the
hub of the private network, and can run a watchdog that checks the home from
outside (see [*Watching a host from one that can lose its network*](../README.md#watching-a-host-from-one-that-can-lose-its-network)).
Skip it if you do not need to reach the factory from outside your own network.
A host whose seats run as root needs its tmux server kept up by a system unit:
[*The seat's tmux server on the VPS*](../README.md#the-seats-tmux-server-on-the-vps).

## Talking to the Mayor from a phone: Postern (optional)

Postern is a separate project: an encrypted messaging backend and phone app. Its
backend is a Go service you build and run yourself from its own repository; its
protocol and server notes live there. Millwright is the Mayor's side of it:
`mw postern ...` signs and sends the Mayor's messages and reads yours, and
the factory's events and live view are sealed to your key and shown in the app.
It is how you get a question on your phone, tap *Release* on an epic, or send a
voice note, at zero Fuel until the Mayor needs to answer.

**What it takes.** The backend running on the home (a user unit, `postern-backend`,
with its own env file), reachable from your phone, usually behind the front door's
TLS; a key for the Mayor (`mw postern key init`, then `mw postern key show` for its
public key) and your key's public half as `postern_governor_key` in
`~/.config/mw/config.toml`; and the backend's address (`postern_backend`). Read
[*The postern key*](../README.md#the-postern-key) first, then
[*mw postern mirror*](../README.md#mw-postern-mirror) for keeping the backend's data
safe, and [*What a host is told*](../README.md#what-a-host-is-told) for every setting.
Set `postern_channel = "direct"` to hand records to the backend without any
chain: see the next chapter.

**Running without it.** Leave out the backend, the key and `postern_governor_key`.
Nothing else needs them. You talk to the Mayor in the terminal (`mw seat up
mayor`); the Mayor asks you its questions there, in the Session; you release epics with `mw release <epic>` and approve
plans with `mw file --approve`. The follower writes its event log and sends
nothing. Seats still mail each other through beads (`mw mail`).
Add Postern later: nothing you did without it has to be undone.

## Stamps and the BSV chain (optional)

Two features put records on the BSV chain (testnet only today, no real money):
**chain stamps**, which prove a rig's branch held a commit at a time, and
**Postern's chain delivery**, which broadcasts the events batches and Postern
messages for a phone that cannot reach the backend directly. Neither is needed to
run the factory. See [`docs/chain-stamps.md`](chain-stamps.md) for the stamp
record, queue and `mw prove`. To supply another chain, or none, see
[`docs/swapping-the-chain.md`](swapping-the-chain.md).

**Running without the chain** (the default for a first vault):

- Do not run `mw postern key init` and leave `postern_governor_key` unset.
  Without a key nothing can be signed or broadcast, so nothing reaches the chain.
- If you do use Postern, set `postern_channel = "direct"` (the backend takes
  records directly, with no coins and no broadcast) and, in `~/.config/mw/config.toml`,
  the follower's switch:

  ```toml
  [events]
  chain = false
  ```

  Every events batch then goes direct only, as the fallback lane does.
- Stamps are queued after every landing and every vault push, whether or not
  you want them: `mw next` and `mw sync` write each to `pending.jsonl` in
  `~/.local/state/mw/stamps/` and never wait on it. Without a key they stay
  there, and the follower's `chain-stamp` job fails its pass once a minute with
  a line saying stamps wait. That is noise, not damage: nothing lands any
  differently. `mw prove` has nothing to show. Clear the queue
  file when you like; it holds only stamps you chose not to send.
- Everything else in this guide (beads, seats, dispatch, landings, the follower,
  mail) works the same.

**Running with it.** Make the key and set `postern_governor_key`; the backend must
serve the key's coins and take broadcasts. The key is a testnet key and the fee
is 1 satoshi per kilobyte, so no money is spent. Then the `chain-stamp` job sends
the queue, comments `STAMP <txid> ...` on the story, and `mw prove <rig> <commit>`
shows the transaction, its block time and an explorer link. `mw stamp <rig>` stamps
a head by hand. The record keeps the rig and commit out of the clear. Mainnet is
not built: [*Limits and what mainnet needs*](chain-stamps.md#limits-and-what-mainnet-needs).

## Checklist

On the first host:

- [ ] `mw version` runs; Claude and GitHub are logged in; `git config` knows you.
- [ ] `mw init` made the vault; its private remote exists and `git push` works.
- [ ] `bd -C <vault> list` works; `bd -C <vault> formula list` shows `chore` and
      `tdd-feature`.
- [ ] `seats/mayor/vision.md` is written; charters read and committed.
- [ ] `~/.config/mw/config.toml` names the vault, this host and its rigs;
      `dispatch.env` has the `PATH` line.
- [ ] `loginctl show-user $USER -p Linger` says `Linger=yes`.
- [ ] `systemctl --user list-timers` shows `mw-dispatch`; `mw-view-follow` is
      active; `mw status` and `mw doctor` print without a fault.
- [ ] The first Mayor Session ran by hand and wrote a handoff, committed and pushed.
- [ ] `mw seat up mayor` opens a window that boots from that handoff.
- [ ] `mw dispatch --dry-run` says what it would start (or that nothing is ready).

For each rig: checked out and listed under `[rigs]`; `seats/builder/rigs/<rig>.md`
written; the rig's own `CLAUDE.md` says how to build and test; first plan filed
held, then released by you.

Optional:

- [ ] Second host: `mw doctor beads-server` reaches the home; `mw init --join` done;
      `beads_sync` set on both; only one host runs the Mayor's timers.
- [ ] Postern: backend healthy, keys made, `postern_governor_key` set, a test
      message from the phone shows in `mw postern inbox`.
- [ ] Chain: you decided on purpose; if not, `postern_channel = "direct"` and
      `[events] chain = false`.
