# Moving the home: `mw home move <host>`

The factory's home is the one host, the desktop or the Laptop, that holds the beads
database, the Mayor and the live Postern backend (`CONTEXT.md`: Home, Boost). The
vault's tracked `home` file says which. `mw home move <host>` makes another host the
home, in one line, run by the Governor and never by the factory on its own.

This page is the design of two paths. The **dead old home** path: the old home does not
answer, and the new home takes over from what GitHub, the Postern mirror and the vault
hold. The **planned** path (`--planned`, *The planned path* below): both hosts are up,
and the old one hands off, flushes and stands down first, so that nothing is lost. A move
onto a home that answers ssh, without `--planned`, is refused with `old home is up: use
--planned`.

## The line

```sh
mw home move laptop --dry-run                 # on the Laptop: every step and its way back, none run
mw home move laptop --old-home-dead           # on the Laptop: the move, the desktop being dead
mw home move laptop --planned                 # on the Laptop: the move, the desktop being up
```

- It runs **on the host that becomes home.** `mw home move desktop` on the Laptop is
  refused, and so is a move to `vps`: the home is the desktop or the Laptop, never the VPS.
- `--old-home-dead` is the Governor's word that the old home is dead. Without it a move
  onto an old home that does not answer stops at step 1. The tap in Postern passes it
  when the old home does not answer ssh, and `--planned` when it does (below).
- `--planned` is for an old home that answers ssh, and `--old-home-dead` for one that does
  not: giving both is refused, and `--planned` onto a home that does not answer stops at
  step 1 and names `--old-home-dead`.
- `--dry-run` prints the six steps (seven with `--planned`), what each does and each way
  back, and runs **none** of
  them: no ssh, bd, git, systemctl, mayor-up, no mail, no write. It reads the vault's
  `home` file and the config, nothing else.
- It takes a minute or so, most of it the bootstrap (736 MB on 2026-09-28: 32 s). Run it
  in tmux: `mw` stops cleanly on SIGINT or SIGTERM and prints the ways back, but a
  dropped ssh session (SIGHUP) kills it without a word.
- A move to the host that is already home is refused, and a move that stopped part-way
  is finished by hand (see the end): a second run would set the fresh database aside.

## What each host holds beforehand

A move needs no hands, because both hosts hold what it needs all the time (Q3 of the
grilling, `mw-nrcbe`). `mw home move` does not create any of this and stops, saying so,
when a piece is missing:

| Piece | Where | Used by |
| --- | --- | --- |
| a `[hands_hosts]` line for the old home, `desktop = "ssh desktop"` | `~/.config/mw/config.toml` | step 1 |
| `postern_data`, the backend's `POSTERN_DATA` (a full path) | config | step 6 |
| `beads_sync = "auto"` | config | after the move: the sync follows the home file |
| the `dolt-beads` user unit and its env file, if the host serves beads over the network | `~/.config/systemd/user/`, `~/.config/mw/beads.env` | step 2, optional |
| the `postern-backend` user unit and its env file, **issuer key already off** | `~/.config/systemd/user/`, `~/.config/postern/env` | step 4 |
| the Mayor's Postern key | `~/.config/mw/postern.key` | the Mayor, step 5 |
| `postern_local_url`, only if the backend is not at `http://<host>.mw:8787` | config or `$MW_POSTERN_LOCAL_URL` | step 4 |

## The six steps

Every step prints what it did and **its way back**. A step that fails stops the move; the
ways back of everything done so far, the failing step's own included, are printed last
step first, and `mw home move` leaves with status 1.

### 1. The old home

Asks whether the old home answers ssh: `ssh -o BatchMode=yes -o ConnectTimeout=10
<[hands_hosts] alias> true`, 10 s. Any live sshd is an answer, the remote command's own
failure and a refused key included; only a connection that never comes up (timed out, no
route, no such name, nothing listening) is "does not answer".

- Answers: stop. `old home is up: use --planned`. `--old-home-dead` does not override
  this: it is the word for a host that does not answer. With `--planned` an answer is what
  it wants: see *The planned path*.
- Does not answer, no `--old-home-dead`: stop, naming the flag.
- Does not answer, `--old-home-dead`: go on.

**Way back:** nothing has changed.

### 2. Beads, from GitHub

Everything here is what was done by hand on 2026-09-28 (`mw-nrcbe`), in this order:

1. Read when GitHub's `refs/dolt/data` was written (the tip commit's date; a one-commit
   `git fetch --depth=1 --filter=blob:none` into a scratch repository, so the backup's
   hundreds of megabytes are not copied and the vault is not touched). **If GitHub cannot
   be read the move stops here, before anything on this host is touched.**
2. Take this host's sync lock (the one `mw sync`, dispatch and the Millhand's tick take),
   so that none of them runs in the middle of the swap.
3. If the `dolt-beads` unit is running here, stop it: it holds `<vault>/.beads/dolt` open.
   Then set **both** directories aside by a rename: `<vault>/.beads/embeddeddolt` to
   `~/beads-embeddeddolt-aside-<UTC time>` and `<vault>/.beads/dolt`, the directory the
   unit serves, to `~/beads-dolt-aside-<UTC time>`: never copied, never deleted. A vault
   with neither has nothing to set aside. Without the second, a stale `.beads/dolt` (the
   desktop's, or the Laptop's after a WSL restart with the unit enabled) would be started
   and reported as "answers with N beads".
4. `bd bootstrap --yes`, which clones `refs/dolt/data`; then `git checkout --
   .beads/config.yaml`, because bootstrap drops that file's trailing newline.
5. Ask bd how many beads it has: none is a stop (a database that is not one to make the
   home of). **A planned move also compares it with the old home's own count**, taken over
   ssh in the stand-down (below), after the final flush and before its `dolt-beads` stops:
   a count that differs stops the move, saying both numbers and where the asides are. A
   dead old home has no count to compare: the output says so, and the number is not
   checked against anything.
6. If this host has the `dolt-beads` user unit, start it (a unit already running is left
   alone) and count again. If not, stay embedded: `beads_sync = auto` then reads `home`
   as **backup** mode, pushing to GitHub every five minutes.

**Way back:** `systemctl --user stop dolt-beads` if the move started it, then `mv
<vault>/.beads/embeddeddolt <vault>/.beads/embeddeddolt.from-github` (only if bootstrap got
as far as making one) and `mv ~/beads-embeddeddolt-aside-<time> <vault>/.beads/embeddeddolt`;
the same for `.beads/dolt` (`.beads/dolt.from-github`, `~/beads-dolt-aside-<time>`); last,
`systemctl --user start dolt-beads` if it was running before the move and the move stopped
it. The printed lines carry the real paths and the order.

**What bootstrap does about `.beads/dolt` (run for real with bd 1.3.0 on scratch repositories
and a scratch Dolt server, 2026-09-30).** `bd bootstrap --yes` never makes `<vault>/.beads/dolt`.
With `BEADS_DOLT_SERVER_*` set and a server answering, it clones into *that server's own data
directory*; with the server stopped it fails, `connection refused`, and leaves no database.
So a move on a host that runs `dolt-beads` and has `beads.env` in its shell stops at
bootstrap once the unit is stopped, with the asides kept and the ways back printed: no stale
directory is served, and a fresh one is not made either. The output says so. What the unit
serves after such a move is whatever is in its `WorkingDirectory`: nothing, if `.beads/dolt`
is gone, and the unit then fails to start loudly (runbook-CHECK Q7, still not seen).

### 3. The vault

`git pull --rebase` brings the dead home's last pushes in, then the `home` file is
written (`<host> <UTC time> mw@<host>`), committed alone (`home: <host> (moved from <old>:
the old home is dead)`) and pushed. **This is the fence.** An old home that comes back
pulls its vault, reads that it is not home, and stays quiet (see *When the old home comes
back*). If the push fails the commit stays here, and the move stops: the old home has no
fence yet.

**Way back:** `git -C <vault> revert --no-edit <the commit> && git -C <vault> push`. That
puts the previous line back (or removes the file, when there was none).

### 4. The Postern backend

Starts the `postern-backend` user unit and waits up to 45 s for its `/healthz` to answer
as home: HTTP 200 and not `"standby": true`.

- Both hosts run the backend all the time. On a host that is not home it is in **standby**
  (every `/api` route answers 503 but the send routes, `/healthz` says `standby: true`, no
  push goes out) and it re-runs `mw home --check` every 30 s. So the unit is usually
  already running, and leaves standby by itself within 30 s of step 3: this step waits for
  that. A unit already running is not restarted.
- **Never `mw postern serve` here.** It writes `POSTERN_ISSUER_KEY` back into the env file
  and the Mayor loses Postern (decision B, `mw-f758y.22.7`). The move only starts the unit;
  the env file was placed beforehand with the issuer key off.
- Without the `postern-backend` unit the step stops and says so.

**Way back:** `systemctl --user stop postern-backend`, if the move started it. A backend
that was already running goes back to standby by itself once step 3 is undone.

### 5. The Mayor

Sends mail to `mayor`, signed `mw@<host>`: `Home moved to <host> at <UTC time>: the old
home is dead; beads from GitHub as of <the time of refs/dolt/data>`. It goes into the new
database, so the new Mayor finds it on its first read, and reaches the others on the
next sync. Then it runs the vault's `bin/mayor-up`: exit 0 is a Mayor started (its tmux
window is the last line printed), exit 3 is a Mayor already sitting here, and 4 (cannot)
and 5 (this host is not home) stop the move.

**Way back:** `tmux kill-window -t '<the window mayor-up printed>'`. The message stays
sent; if the move is undone, send the Mayor a note.

### 6. What was lost

Prints two ages, so that the Governor knows what the dead home took with it:

- **GitHub's beads backup:** the time of `refs/dolt/data` and how long ago. Whatever the old
  home wrote to beads after that is not here. On the home the backup runs every five
  minutes (`beads_sync = auto`), so this is normally minutes.
- **The Postern data here:** the last record of `postern-index.jsonl` in `postern_data`,
  and how long ago. The home copies its data to the boost every ten minutes
  (`mw postern mirror`), so whatever the old home received after that is not here.

It changes nothing and never fails the move: an index it cannot read says so.

**Way back:** none needed.

If `beads_sync` is not `auto` on this host the move ends with a note to set it: the
sync would not follow the home file otherwise.

## The planned path

`mw home move <host> --planned`, run on the new home with **both hosts up**: the Governor
moves the home on purpose, say the desktop's Mayor to the Laptop. It is the six steps
above with a step put in after step 1, so seven, and with beads that come from what the
old home has just pushed rather than from whenever its last backup ran. Nothing is lost.

Step 1 asks the same question, and a planned move wants the answer yes: `--planned` onto
a home that does not answer stops with `--planned needs the old home up`, naming
`--old-home-dead`. Nothing has changed.

### 2. The old home stands down

Everything here runs **on the old home, over its `[hands_hosts]` ssh alias**, as its own
`mw`, so it uses that host's own config, vault and beads (the environment of
`~/.config/mw/beads.env` included). Nothing on the new home has changed yet: if anything
here fails the move stops with the new home as it was, and the ways back below are printed.

1. **Hand-off.** Mail to the old home's Mayor, filed in the old home's beads (where that
   Mayor reads), signed `mw@<new host>`: `Hand off now: the home moves to <host>`, with
   what to do: write the handoff, `mw sync`, clear `.mayor-acting`, stop.
2. **Wait, up to 15 minutes**, asking every 15 s for `.mayor-acting` in its vault to be
   empty, or for its Mayor process to be gone (`bin/respawn-mayor`'s own markers: the
   `.mayor-acting` file, and a `claude ... -n "Mayor (after ..."` process). The vault is
   `$MW_VAULT` there, or the `vault` line of its config. **If the Mayor does not go, the
   move stops** and says what it saw: a Mayor is never killed. Ask it to hand off (or stop
   it yourself), then run the move again; the mail is sent once more, which is harmless.
3. **`mw sync`** there: a final backup push of the beads (GitHub's `refs/dolt/data`) and the
   vault, with the Mayor's handoff in it.
4. **Stop its `postern-backend`** user unit (a unit it does not have, or that is not
   running, is left), then **`mw postern mirror`** there, a final copy of the backend's
   data to this host. The mirror is what a home does every ten minutes; run after the
   backend has stopped, it is complete.
5. **`bd count`** there (in its vault, with `beads.env` sourced, so it asks the server the old
   home itself uses): the number the new home's database has to answer with, compared in
   the beads step. The move keeps it in memory only.
6. **Stop its `dolt-beads`** user unit, the Dolt server's writers, if it has one. Once the
   `home` file changes (step 4) it is a boost, and `beads_sync = auto` reads `shared` there.

**Way back:** the hand-off mail stays sent: mail the Mayor on the old home to carry on.
`ssh <old> systemctl --user start postern-backend` and `... start dolt-beads` for what was
stopped (the backend also goes back to standby by itself while the home is elsewhere). The
sync and the mirror change nothing that needs undoing.

### Steps 3 to 7: as in the dead path (its 2 to 6)

Beads from GitHub (`bd bootstrap`), the vault's `home` file (`home: <host> (planned move
from <old>)`), the backend, the Mayor, what was lost: the same, numbered one higher, with
these differences:

- Beads are the old home's final push from step 2, so the **Mayor mail says `planned move`**
  (`Home moved to <host> at <time>: planned move; <old> handed off and flushed first, beads
  from its final push ...`), not that the old home is dead.
- The Mayor on the new home boots from **the handoff the old one wrote**: step 2 made the old
  Mayor write it and push it, and step 4's `git pull --rebase` brought it in, so
  `bin/mayor-up` starts from the newest handoff.
- Step 7 says nothing is lost, and prints the same two ages as a record.
- The old home is a **boost** afterwards, not a dead machine: its Dolt server is stopped, its
  backend is in standby, its Mayor is gone, and it can be a home again by `mw home move` the
  other way.

## When the old home comes back

The fence is the `home` file in the vault, read by everything that acts as a home:

- `bin/mayor-up` pulls the vault, asks `mw home --check`, and exits 5 without starting a Mayor;
  the doctor's `mayor-gone` is ok on a host that is not home; the nudge is quiet.
- The Postern backend there sees `mw home --check` fail within 30 s and goes to standby.
- `beads_sync = auto` makes it a boost: its bd is pointed at the new home's beads server.

What is **not** merged: its own embedded database and Postern data stay on its disk,
holding whatever it wrote after the last backup and mirror (the ages printed by step 6).
Nothing reads them back. If they matter, they are looked at by hand before its database is
set aside; on 2026-09-29 the Mayor did that for the desktop's (`mw-nrcbe`: its rows were
compared with the Laptop's, and there was nothing to bring across).

## Finishing by hand

A second `mw home move` is refused once the home file names this host, because step 2 would
set the fresh database aside again. So a move that stopped is finished from here. The
failing step named in the error says where to start (with `--planned` the numbers are one
higher, from the beads step on); steps 4 to 6 do not depend on each other and are safe to
repeat:

| Stopped at | Finish with |
| --- | --- |
| 1, or 2 before the swap | nothing changed: fix the cause and run the move again |
| 2 after the swap | the printed way back, then run the move again |
| 3, the pull or the write | fix the cause (a dirty vault, no network), then step 3 by hand: write the `home` line, commit it, push |
| 3, the push | `git -C <vault> push` once GitHub answers (the commit is already made), then steps 4 and 5 |
| 4 | `systemctl --user start postern-backend`; `curl <backend>/healthz` says `standby` until `mw home --check` passes |
| 5 | `mw mail send mayor` for the note, then `<vault>/bin/mayor-up` (`MW_MAYOR_UP_DRY=1` first shows what it would do) |
| 6 | nothing to finish: it only reads |

## The tap in Postern

The Governor's *Move home to <host>* on the Me screen sends a message of class `move-home`,
`{"host": "<host>"}` (postern's `docs/protocol.md` §18). The backend that indexes it runs
its on-message hook, `mw postern inbox --apply`, and that pass (`application/posternmovehome.go`):

- **refuses** it, and runs nothing, unless the record's signer, the key the backend vouches
  for (it signed the transaction, or delivered the record), is `postern_governor_key`; and
  unless it is under **30 minutes** old (older is a replay). A refusal is marked on the
  txid (`postern.applied.<txid>`, `refused move-home …`), written on `home_move_bead` and
  mailed to the Mayor.
- naming **the other host**, does nothing: it is that host's to run.
- naming **this host**, asks the old home over ssh (its `[hands_hosts]` line, 10 s), marks the
  txid started on this host's own disk (`move-home.<txid>` beside the inbox's attachments, so
  no txid is ever started twice, whatever the tracker says) and runs
  `mw home move <host> --planned` when it answers, `--old-home-dead` when it does not;
  its start and its result (exit status, the last of its output) are written on
  `home_move_bead` (config or `$MW_HOME_MOVE_BEAD`, default `mw-43v9x`) and mailed.

On a host that is not home the pass applies **nothing but a move-home**, so a boost's hook
never records what the home's records too. On a boost the beads server is the old home's:
each bd call before the move is given 20 s, and one that fails is printed, never a reason to
stop; a boost that cannot read the tracker at all writes nothing until the move has run.
See `features/postern_move_home.feature`.

## What was checked, and what was not

Checked with stand-ins for ssh, bd, systemctl, git, the backend and `bin/mayor-up`
(`application/homemove_test.go`, `infrastructure/homemove/homemove_test.go`,
`cmd/mw/homemove_test.go`; the planned path in `application/homemoveplanned_test.go` and
`infrastructure/homemove/oldhome_test.go`): the order of the six steps, `--dry-run` running none of them, a
dead old home refused without `--old-home-dead`, a live one refused with the `--planned`
line, a failing step stopping the move and printing the ways back of what was done, and
the ways back themselves (the vault revert and the database restore were run for real
against scratch repositories). The date of `refs/dolt/data` was read from the real vault's
GitHub remote, read-only: 0.6 s, 148 KB.

## Which server host a session's bd reaches

`beads.env` holds `BEADS_DOLT_SERVER_HOST` as it was when the file was written, and a move
of the home makes that wrong (the desktop's file says `10.88.0.2`, the Laptop). Nothing
rewrites the file. Instead every session `mw` starts (a Builder's, the Millhand's and its
tick's, a seat's window) sources the file first, then exports the host `mw` resolved from
the home file, which wins:

- on a boost under `beads_sync = "auto"`: `beads_server_host`, or `<home>.mw`;
- on the home under `auto`: `127.0.0.1` (`dolt-beads.service` binds `0.0.0.0:3307`), but only
  when the vault holds `.beads/dolt`; a home that stayed embedded is left with the file's value;
- any other `beads_sync`, or a home that cannot be told: nothing is exported, and the
  file's value stands.

`mw` sets the same host in its own environment, so the `bd` it runs itself agrees. The
password and the file's other values are never put on a command line; the host is a value, not
a secret. **A long-lived seat window (the Mayor's) keeps the host it was started with until it
is respawned**: after a move, `mw seat up mayor` (or the handoff that does it) is what
makes it reach the new home.

**Not checked: a real move.** Two host facts are assumed from the story, not seen:

- **The `dolt-beads` server's data directory.** Step 2 sets the *embedded* database and
  `.beads/dolt` aside and bootstraps a new one, then starts the unit. On the desktop, where
  `beads.env` puts every bd in server mode, what that unit serves after a move (see *What
  bootstrap does about `.beads/dolt`*: bootstrap makes no `.beads/dolt`) has not been run
  against the real unit. The step counts beads after bootstrap and again after the unit
  starts, and a planned move compares both with the old home's count, so an empty or stale
  server stops the move (with the ways back); the demo (`mw-43v9x.13`) is where the rest is
  learned.
- **`bd bootstrap` with the server-mode environment set.** As above.

The planned path was checked the same way: the old home's steps run before any of the new
home's, in the order above; a Mayor that does not go within the wait stops the move with
the new home untouched (nothing but the mail on the old home has run); each stand-down step
that fails stops the move there; the new Mayor's mail says `planned move`. The remote script
that asks whether the Mayor has gone (`.mayor-acting`, `pgrep`) was run for real by a stand-in
ssh against a temporary vault. **Not checked: a real planned move**, with a real Mayor on the
old home handing off in 15 minutes; and that `mw`, `mw postern mirror` and `systemctl --user`
work as run by a non-login ssh command on the desktop and the Laptop (the command sets `PATH`,
`beads.env` and `XDG_RUNTIME_DIR` itself, but the WSL Laptop has not been asked). The demo
(`mw-43v9x.13`) is where that is learned.
