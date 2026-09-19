# Code map

Where everything in this rig is, for a session that has read nothing else.
`CONTEXT.md` holds the vocabulary; the ADRs hold the reasons. This is layout
only.

## Layers

| Layer | Directory | Rule for what goes there |
| --- | --- | --- |
| domain | `domain/` | Value types and their validation. Standard library only: no I/O, no ports. |
| application | `application/` | One file per use case, plus the port interfaces it reaches the world through. Imports domain and its own ports, never an adapter. |
| fakes | `application/apptest/` | In-memory stand-ins for the ports. Ships in the module; any test may import it. |
| infrastructure | `infrastructure/` | One subpackage per adapter. Implements a port, and is the only place bd, git, tmux, claude or the disk is touched. |
| command line | `cmd/mw/` | Cobra wiring only: read config, build the use case, run it. No decisions. |
| features | `features/` | Gherkin .feature files; their step code in `features/steps/`. |

New behaviour is a use case in `application/`. Anything that reaches outside
the process goes behind a port and into `infrastructure/`.

## Ports

Every adapter carries `var _ application.<Port> = ...`; `grep -rn "var _ "`
is the index.

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | `application/worktracker.go` | `infrastructure/beads/beads.go` (`Gateway`) | `application/apptest/faketracker.go` |
| `TrackerSync` | `application/sync.go` | `infrastructure/beads/sync.go` (same `Gateway`) | `application/apptest/faketracker.go` |
| `TrackerNotes` | `application/status.go` | same, read half | same |
| `VaultFiles` | `application/sync.go` | `infrastructure/vault/git.go` | `application/apptest/fakevaultfiles.go` |
| `Vault` | `application/seatboot.go` | `infrastructure/vault/vault.go` | `application/seatboot_test.go` |
| `Runner` | `application/runner.go` | `infrastructure/tmux/tmux.go` | `application/apptest/fakerunner.go` |
| `Harness` | `application/harness.go` | `infrastructure/claude/claude.go` | `application/seatboot_test.go` |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `Landing` | `application/landing.go` | `infrastructure/rig/landing.go` | none — real git in a temp repo |
| `Checks` | `application/landing.go` | `infrastructure/rig/checks.go` | none — a real command |
| `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/slot.go` | none — a real `flock` |
| `Dispatcher` | `application/landing.go` | `application.Dispatch` | — |
| `HostSync` | `application/dispatch.go` | `application.Sync` | — |

`infrastructure/config/config.go` is not a port: it is what this host knows
about itself (vault, host, cap, stale and host-silence hours, rigs, test
commands), read from `cmd/mw/` only.

## Use cases

| Use case | File | Command | Feature |
| --- | --- | --- | --- |
| `File` | `application/file.go` | `mw file` — `cmd/mw/file.go` | `features/file_plan.feature` |
| `Release` | `application/release.go` | `mw release` — `cmd/mw/release.go` | `features/release.feature` |
| `Dispatch` | `application/dispatch.go` | `mw dispatch` — `cmd/mw/dispatch.go` | `features/dispatch.feature` |
| `Next` | `application/next.go` | `mw next` — `cmd/mw/next.go` | `features/next.feature` |
| `Status` | `application/status.go` | `mw status` — `cmd/mw/status.go` | `features/status.feature` |
| `Sweep` | `application/sweep.go` | `mw sweep` — `cmd/mw/sweep.go` | `features/sweep.feature` |
| `Sync` | `application/sync.go` | `mw sync` — `cmd/mw/sync.go` | `features/sync.feature` |
| `SeatBoot` | `application/seatboot.go` | none — `Dispatch` and `Next` call it | `features/seat_boot.feature` |

`cmd/mw/main.go` runs the tree; `cmd/mw/root.go` holds it (one
`root.AddCommand` per command); `cmd/mw/version.go` is `mw version`, the one
command with no use case. `features/path_validation.feature` covers
`domain/path.go` and `features/ready_stories.feature` the `WorkTracker`
contract; neither has a command.

Neither port nor use case: `application/fuel.go` (what a session burned, from
its result JSON), `application/ledger.go` (a ledger row), `domain/plan.go` (a
plan file as JSON).

## Adding a command, end to end

`mw sweep` is the newest command and the one to copy at every step.

1. **Feature** — write `features/sweep.feature` first. Step text must be
   unique across all features (one suite), and Gherkin does not unescape `\"`.
2. **Steps** — `features/steps/sweep_steps.go`: one context struct, a
   `Before` hook that resets it, then ctx.Given/When/Then registrations.
   Register the initializer in `features/features_test.go`.
3. **Use case** — `application/sweep.go`: a struct of ports and settings, a
   `Run(ctx)` returning a report struct, and `String()` on the report.
4. **Port method** — if a port is short of something, add it to the interface
   with a doc comment, as `StoryState` in `application/worktracker.go`.
5. **Fake** — implement it in `application/apptest/faketracker.go`; the
   `var _` block at the foot of that file keeps it honest.
6. **Adapter** — implement it in `infrastructure/beads/beads.go`, covered by
   `infrastructure/beads/beads_integration_test.go`.
7. **Cobra** — `cmd/mw/sweep.go`: `newSweepCmd()` with `Use`, `Short`, `Long`,
   `Args`, and a `RunE` that only reads config and calls the use case. Add
   `root.AddCommand(newSweepCmd())` in `cmd/mw/root.go`.
8. **README** — a section in `README.md` beside "Keeping two hosts level",
   ending with a pointer to the feature file. (`mw status` and `mw sweep`
   lack theirs.)

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault` | `infrastructure/beads/beads_integration_test.go` | A real bd database in `t.TempDir()`, never the factory's. `bd` costs ~1s a call: one database per test. |
| `installFormula` | same file | Copies a real formula from `formulas/` into that database. |
| `standIn` | `infrastructure/beads/sync_test.go` | A script standing in for `bd` (via `beads.WithProgram`), for exit codes. |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | A tmux server on a socket of its own (`tmux.WithSocket`), killed on the way out. Never use the default server. |
| `aVault` | `infrastructure/vault/vault_test.go` | A temp vault with a seat in it. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two clones of one vault, for pull/push. |
| `aRig` | `infrastructure/rig/worktree_test.go` | A temp rig with a real origin. |
| `aRigDir` | `infrastructure/rig/slot_test.go` | A directory to take the merge slot in. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | A `config.toml` in a temp `HOME`. |
| `aFactory` | `application/dispatch_test.go` | A `Dispatch` on a temp vault, with tracker, worktrees and runner faked. |

Feature steps build the real thing in a temp directory — a rig, a bare origin,
a second clone for the other host, a vault with a ledger — and fake only the
tracker, the runner and the vault's git: `features/steps/next_steps.go`.

## Build and test

```sh
export PATH=$PATH:/usr/local/go/bin      # Go is not on PATH in a non-login shell
make build      # -> bin/mw
make test       # go test ./... , features included
make lint       # go vet ./... , then scripts/check-codemap.sh
```

One `go` command at a time on the VPS (1 vCPU, ~1 GB): the Makefile pins
`GOFLAGS=-p=1` and `GOMAXPROCS=1`. `make test` takes about 2.5 minutes.

- One package: `go test ./application/...` (add `-run TestName` for one test).
- One feature or one scenario: `MW_FEATURE=sweep.feature go test ./features`
  (append `:17` for the scenario at that line). Unset, `make test` still
  runs every feature, strictly; an unknown name fails the suite.
- `scripts/check-codemap.sh` (run by `make lint`) fails when this page names a
  path that does not exist, passes 8192 bytes, or leaves out a port, a use
  case or a command file. Its header states the exact rules.
- `make check-formulas` runs `scripts/check-formulas.sh` against a throwaway
  beads database. Not part of `make test`: it needs `bd`, `jq` and real time.
