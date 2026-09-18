# millwright

A personal software factory: one human directs a small set of AI-occupied
seats that turn conversations into tracked work and tracked work into commits,
across several rigs and two hosts, on a tight fuel budget.

`mw` is the factory's command line. Today it knows its own version, files the
Mayor's plans with `mw file`, reads and writes stories through beads, runs
sessions in tmux, keeps the two hosts level with `mw sync`, starts a fresh
Builder session for each ready story with `mw dispatch`, and closes each
finished story out with `mw next` — which lands it, ledgers what it burned,
closes it and dispatches whatever is ready next.

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
than by the session. Assembling launches nothing and spends no fuel. See
`features/seat_boot.feature`.

## Dispatching a story

```sh
bin/mw dispatch --dry-run   # what it would start, writing nothing at all
bin/mw dispatch             # claim, cut, pour, boot, start
```

`mw dispatch` is the one command that spends fuel, so everything it does before
spending any is reversible. In order, once: `mw sync`, so that this host sees
the other host's claims before it makes its own; what this host already has in
flight, which is what the cap counts; what is ready here. Then, per story, up to
the cap:

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

`--dry-run` prints what it would start and writes nothing: nothing is synced,
claimed, fetched, cut, poured or started. See `features/dispatch.feature`.

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
2. every **step** of the story's poured formula must be closed;
3. the **rig's own tests** must pass in the story's worktree.

Then, under the rig's **merge slot**, `mw/<story-id>` is merged into the target
branch as the remote has it — in a throwaway detached worktree, so neither the
rig's checkout nor the story's worktree is disturbed. If that was not a
fast-forward the tests are run **again** on the merged result, because nothing
has ever tested that combination: the story's tests passed on the story's branch
and the other host's passed on its own. The push is never forced; a push the
remote refuses because the other host got there first is fetched, merged and
pushed again, a bounded number of times.

Only then: the worktree and its branch go, one line is appended to the seat's
ledger, the story is closed with the reason, `mw sync` brings the hosts level so
that the other host sees a closed story rather than a claimed one, and whatever
is ready here is dispatched.

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
`duration_ms`. The ledger is opened for append and never read back: there is no
code path in mw that can rewrite a line a seat has already written. See
`features/next.feature`.

### What a host is told

`~/.config/mw/config.toml`, with `MW_VAULT`, `MW_HOST` and `MW_CAP` ahead of it:

```toml
vault = "/root/millwright-vault"   # the one beads database and the seats
host  = "vps"                      # which of the factory's hosts this is
cap   = 1                          # sessions running here at once (default 1)

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
work stops it — committing is a seat's job. A rebase that cannot finish is
undone, so the vault is left as it was found. `bd sync`'s exit code is surfaced
as it is and becomes mw's own: 2 (a merge conflict beads will not resolve) and 4
(a working set only a person can clear) stop mw with a plain message and are
never retried or auto-resolved.

Ledgers are appended to by both hosts and edited by neither, so the vault's
`.gitattributes` must carry `seats/*/ledger.md merge=union` — two hosts' appends
then merge by keeping every line instead of conflicting. `mw sync` writes that
line if it is missing and says so; committing it is a seat's job, and until
someone does, only this host is covered. See `features/sync.feature`.

## The Path

A story is worked by a **Path**: the rig it is worked in, the branch it
targets, and the harness, model, effort, formula and host of the session that
works it. An epic carries default path values and each story may override any
of them; `Story.PathFrom` overlays the two and rejects what is not a path — no
rig, no target branch, or a harness, model or effort the factory does not
know. See `features/path_validation.feature`.
