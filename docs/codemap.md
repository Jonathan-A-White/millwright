# Code map

Where everything is. `CONTEXT.md` has the vocabulary, the ADRs the reasons.

## Layers

| Layer | Directory | Rule |
| --- | --- | --- |
| domain | `domain/` | Value types and validation. Standard library only. |
| application | `application/` | One file per use case, plus its ports. Never imports an adapter. |
| fakes | `application/apptest/` | In-memory stand-ins for the ports. |
| infrastructure | `infrastructure/` | One subpackage per adapter: the only place bd, git, tmux, claude or the disk is touched. |
| command line | `cmd/mw/` | Cobra wiring only: read config, run the use case. |
| features | `features/` | Gherkin; step code in `features/steps/`. |

New behaviour is a use case; anything outside the process goes behind a port.

## Ports

Every adapter carries `var _ application.<Port> = ...`.

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | `application/worktracker.go` | `infrastructure/beads/beads.go` (`Gateway`) | `application/apptest/faketracker.go` |
| `TrackerSync` | `application/sync.go` | `infrastructure/beads/sync.go` (`Gateway`) | `application/apptest/faketracker.go` |
| `TrackerNotes` | `application/status.go` | same, read half | same |
| `SweepNotes` | `application/sweep.go` | same, `Note`s | same |
| `VaultFiles` | `application/sync.go` | `infrastructure/vault/git.go` | `application/apptest/fakevaultfiles.go` |
| `Mailbox` | `application/mail.go` | `infrastructure/beads/mail.go` (`Gateway`) | `application/apptest/fakemailbox.go` |
| `Vault` | `application/seatboot.go` | `infrastructure/vault/vault.go` | `application/seatboot_test.go` |
| `Runner` | `application/runner.go` | `infrastructure/tmux/tmux.go` | `application/apptest/fakerunner.go` |
| `Harness` | `application/harness.go` | `infrastructure/claude/claude.go` | `application/seatboot_test.go` |
| `SeatFiles` | `application/seatup.go` | `infrastructure/vault/seat.go` | none: temp vaults |
| `Windows` | `application/seatup.go` | `infrastructure/tmux/window.go` | `application/apptest/fakewindows.go` |
| `SeatHarness` | `application/seatup.go` | `infrastructure/claude/claude.go` | none |
| `ReapTerminal` | `application/seatreap.go` | `infrastructure/tmux/reap.go` | `application/apptest/fakereap.go` |
| `ReapLog` | `application/seatreap.go` | `infrastructure/vault/reaplog.go` | none: temp vaults |
| `ReapArmer` | `application/seatreap.go` | `infrastructure/reaper/arm.go` | `application/apptest/fakereap.go` |
| `Transcripts` | `application/seatcontext.go` | `infrastructure/claude/transcripts.go` | none: temp-dir fixtures |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `Landing` | `application/landing.go` | `infrastructure/rig/landing.go` | none: real git |
| `Checks` | `application/landing.go` | `infrastructure/rig/checks.go` | none |
| `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/slot.go` | none: real `flock` |
| `Dispatcher` | `application/landing.go` | `application.Dispatch` | — |
| `HostSync` | `application/dispatch.go` | `application.Sync` | — |

`infrastructure/config/config.go` is not a port: this host's settings, read
from `cmd/mw/` only.

## Use cases

| Use case | File | Command | Feature |
| --- | --- | --- | --- |
| `File` | `application/file.go` | `mw file` — `cmd/mw/file.go` | `features/file_plan.feature` |
| `Release` | `application/release.go` | `mw release` — `cmd/mw/release.go` | `features/release.feature` |
| `Show` | `application/show.go` | `mw show` — `cmd/mw/show.go` | `features/show.feature` |
| `Dispatch` | `application/dispatch.go` | `mw dispatch` — `cmd/mw/dispatch.go` | `features/dispatch.feature` |
| `Next` | `application/next.go` | `mw next` — `cmd/mw/next.go` | `features/next.feature` |
| `Check` | `application/check.go` | `mw check` — `cmd/mw/check.go` | `features/check.feature` |
| `Status` | `application/status.go` | `mw status` — `cmd/mw/status.go` | `features/status.feature` |
| `Brief` | `application/brief.go` | `mw brief` — `cmd/mw/brief.go` | `features/brief.feature` |
| `Sweep` | `application/sweep.go` | `mw sweep` — `cmd/mw/sweep.go` | `features/sweep.feature` |
| `Sync` | `application/sync.go` | `mw sync` — `cmd/mw/sync.go` | `features/sync.feature` |
| `Mail` | `application/mail.go` | `mw mail` — `cmd/mw/mail.go` | `features/mail.feature` |
| `SeatContext` | `application/seatcontext.go` | `mw seat context` — `cmd/mw/seat.go` | `features/seat_context.feature` |
| `SeatUp` | `application/seatup.go` | `mw seat up` — `cmd/mw/seat.go` | `features/seat_up.feature` |
| `SeatReap` | `application/seatreap.go` | `mw seat reap` — `cmd/mw/seat.go` | `features/seat_reap.feature` |
| `SeatBoot` | `application/seatboot.go` | none: `Dispatch`, `Next` call it | `features/seat_boot.feature` |

`cmd/mw/root.go` holds the tree (one `root.AddCommand` per command);
`cmd/mw/main.go` runs it; `cmd/mw/version.go` is `mw version`, no use case.
`features/path_validation.feature` covers `domain/path.go`,
`features/ready_stories.feature` the `WorkTracker` contract; no commands.
Neither port nor use case: `application/fuel.go`, `application/ledger.go`,
`domain/plan.go`.

## Adding a command

Copy `mw sweep`.

1. **Feature** — `features/sweep.feature` first. Step text is unique across
   all features; Gherkin does not unescape `\"`.
2. **Steps** — `features/steps/sweep_steps.go`: context struct, `Before` reset,
   ctx.Given/When/Then; register in `features/features_test.go`.
3. **Use case** — `application/sweep.go`: struct of ports and settings, `Run(ctx)`
   returning a report with `String()`.
4. **Port method** — if a port falls short, add it with a doc comment.
5. **Fake** — `application/apptest/faketracker.go`.
6. **Adapter** — `infrastructure/beads/beads.go`, tested in
   `infrastructure/beads/beads_integration_test.go`.
7. **Cobra** — `cmd/mw/sweep.go`: `newSweepCmd()`, a `RunE` that only reads
   config and calls the use case; add it in `cmd/mw/root.go`.
8. **README** — a section beside "Keeping two hosts level", ending with a
   pointer to the feature file.

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault` | `infrastructure/beads/beads_integration_test.go` | A real bd database in `t.TempDir()`. ~1s a `bd` call: one per test. |
| `installFormula` | same file | Copies a `formulas/` formula into it. |
| `standIn` | `infrastructure/beads/sync_test.go` | A script standing in for `bd`. |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | A tmux server on its own socket. |
| `privateWindows` | `infrastructure/tmux/window_integration_test.go` | A tmux server and seats session of its own. |
| `aVault` | `infrastructure/vault/vault_test.go` | A vault with a seat in it. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two clones of one vault. |
| `aRig` | `infrastructure/rig/worktree_test.go` | A rig with a real origin. |
| `aRigDir` | `infrastructure/rig/slot_test.go` | A directory to take the merge slot in. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | A `config.toml` in a temp `HOME`. |
| `aFactory` | `application/dispatch_test.go` | A `Dispatch` on a temp vault, with fakes. |

Feature steps build a real rig, origin, clone and vault in a temp directory,
faking only tracker, runner and vault git: `features/steps/next_steps.go`.

## Build and test

`make build`, `make test`, `make lint`: see `CLAUDE.md`.

- One package: `go test ./application/...` (`-run TestName`). Add
  `-tags beads_integration` for the real-`bd` cases, as `make test` does.
- One feature: `MW_FEATURE=sweep.feature go test ./features` (`:17` appended
  for the scenario at that line).
- `scripts/check-codemap.sh` checks this page; `scripts/check-timer-units.sh`
  checks `contrib/systemd/` and `contrib/mail-notify`.
- `make check-formulas` needs `bd`, `jq` and real time; not in `make test`.
