# millwright

A personal software factory: one human directs a small set of AI-occupied
seats that turn conversations into tracked work and tracked work into commits,
across several rigs and two hosts, on a tight fuel budget.

`mw` is the factory's command line. Today it knows its own version, reads and
writes stories through beads, runs sessions in tmux, and keeps the two hosts
level with `mw sync`; dispatching sessions follows.

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
