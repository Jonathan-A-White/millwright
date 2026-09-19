# Code map

Where everything in this rig is. `CONTEXT.md` holds the vocabulary; the ADRs
hold the reasons. This is layout only.

## Layers

| Layer | Directory | Rule for what goes there |
| --- | --- | --- |
| domain | `domain/` | Value types and validation. Standard library only. |
| application | `application/` | One file per use case, plus the ports it reaches the world through. Imports domain and its own ports, never an adapter. |
| fakes | `application/apptest/` | In-memory stand-ins for the ports; any test may import them. |
| infrastructure | `infrastructure/` | One subpackage per adapter: the only place bd, git, tmux, claude or the disk is touched. |
| command line | `cmd/mw/` | Cobra wiring only: read config, build and run the use case. |
| features | `features/` | Gherkin .feature files; step code in `features/steps/`. |

New behaviour is a use case in `application/`; anything reaching outside the
process goes behind a port into `infrastructure/`.

## Ports

Every adapter carries `var _ application.<Port> = ...`.

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

`infrastructure/config/config.go` is not a port: what this host knows about
itself (vault, host, cap, stale and silence hours, rigs, test commands), read
from `cmd/mw/` only.

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

Neither port nor use case: `application/fuel.go` (what a session burned),
`application/ledger.go` (a ledger row), `domain/plan.go` (a plan file).

## Adding a command, end to end

Copy `mw sweep`.

1. **Feature** — `features/sweep.feature` first. Step text must be unique
   across all features, and Gherkin does not unescape `\"`.
2. **Steps** — `features/steps/sweep_steps.go`: a context struct, a `Before`
   reset hook, ctx.Given/When/Then; register it in `features/features_test.go`.
3. **Use case** — `application/sweep.go`: a struct of ports and settings, and
   `Run(ctx)` returning a report with `String()`.
4. **Port method** — if a port falls short, add it with a doc comment, as
   `StoryState` in `application/worktracker.go`.
5. **Fake** — `application/apptest/faketracker.go`.
6. **Adapter** — `infrastructure/beads/beads.go`, covered by
   `infrastructure/beads/beads_integration_test.go`.
7. **Cobra** — `cmd/mw/sweep.go`: `newSweepCmd()` with `Use`, `Short`, `Long`,
   `Args` and a `RunE` that only reads config and calls the use case; add it
   in `cmd/mw/root.go`.
8. **README** — a section beside "Keeping two hosts level", ending with a
   pointer to the feature file.

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault` | `infrastructure/beads/beads_integration_test.go` | A real bd database in `t.TempDir()`, never the factory's. ~1s a `bd` call: one per test. |
| `installFormula` | same file | Copies a `formulas/` formula into it. |
| `standIn` | `infrastructure/beads/sync_test.go` | A script standing in for `bd` (`beads.WithProgram`). |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | A tmux server on its own socket (`tmux.WithSocket`). Never the default server. |
| `aVault` | `infrastructure/vault/vault_test.go` | A vault with a seat in it. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two clones of one vault, for pull/push. |
| `aRig` | `infrastructure/rig/worktree_test.go` | A rig with a real origin. |
| `aRigDir` | `infrastructure/rig/slot_test.go` | A directory to take the merge slot in. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | A `config.toml` in a temp `HOME`. |
| `aFactory` | `application/dispatch_test.go` | A `Dispatch` on a temp vault, with fakes. |

Feature steps build a real rig, origin, second clone and vault in a temp
directory and fake only the tracker, runner and vault's git:
`features/steps/next_steps.go`.

## Build and test

```sh
export PATH=$PATH:/usr/local/go/bin      # Go is not on PATH in a non-login shell
make build      # -> bin/mw
make test       # go test -tags beads_integration ./... , features included
make lint       # go vet -tags beads_integration ./... , then scripts/check-*.sh
```

One `go` command at a time on the VPS (1 vCPU): the Makefile pins
`GOFLAGS=-p=1` and `GOMAXPROCS=1`.

- One package: `go test ./application/...` (`-run TestName` for one). A plain
  `go test` skips the real-`bd` cases of `infrastructure/beads` (most of the
  suite's clock): add `-tags beads_integration`, as `make test` does.
- One feature: `MW_FEATURE=sweep.feature go test ./features` (`:17` appended
  for the scenario at that line). An unknown name fails the suite.
- `scripts/check-codemap.sh` fails when this page names a missing path,
  passes 8192 bytes, or omits a port, use case or command file.
  `scripts/check-timer-units.sh` checks `contrib/systemd/` and
  `contrib/mail-notify` (test: `contrib/mailnotify_test.go`).
- `make check-formulas` runs `scripts/check-formulas.sh` on a throwaway
  database. Not in `make test`: needs `bd`, `jq` and real time.
