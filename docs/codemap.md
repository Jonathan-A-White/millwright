# Code map

Where everything is; `CONTEXT.md` has the vocabulary, ADRs the reasons.

## Layers

| Layer | Directory | Rule |
| --- | --- | --- |
| domain | `domain/` | Value types, validation; stdlib only. |
| application | `application/` | One file per use case, plus ports; imports no adapter. |
| fakes | `application/apptest/` | In-memory port stand-ins. |
| infrastructure | `infrastructure/` | One subpackage per adapter (bd, git, tmux, claude, disk). |
| command line | `cmd/mw/` | Cobra wiring: read config, run the use case. |
| features | `features/` | Gherkin; step code in `features/steps/`. |
| template | `template/` | What a fresh vault is born from (`embed.go`); no host detail. |

## Ports

Every adapter: `var _ application.<Port> = ...`.

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
| `SeatFiles` | `application/seatup.go` | `infrastructure/vault/seat.go` | none |
| `Windows` | `application/seatup.go` | `infrastructure/tmux/window.go` | `application/apptest/fakewindows.go` |
| `SeatHarness` | `application/seatup.go` | `infrastructure/claude/claude.go` | none |
| `ReapTerminal` | `application/seatreap.go` | `infrastructure/tmux/reap.go` | `application/apptest/fakereap.go` |
| `ReapLog` | `application/seatreap.go` | `infrastructure/vault/reaplog.go` | none |
| `ReapArmer` | `application/seatreap.go` | `infrastructure/reaper/arm.go` | `application/apptest/fakereap.go` |
| `Transcripts` | `application/seatcontext.go` | `infrastructure/claude/transcripts.go` | none |
| `WatchProbes` | `application/watch.go` | `infrastructure/watch/watch.go` | `application/apptest/fakewatch.go` |
| `DoctorCheck`, `DoctorState`, `DoctorLog`, `DoctorNotes` | `application/doctor.go` | `infrastructure/doctor` | none |
| `TickLog` | `application/millhandtick.go` | `infrastructure/ticklog/ticklog.go` | `application/apptest/faketicklog.go` |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `SyncHaltMarker` | `application/sync.go` | `infrastructure/synchalt/synchalt.go` | `application/apptest/fakesynchalt.go` |
| `Notifier` | `application/millhandtick.go` | `infrastructure/notify/notify.go` | none |
| `VaultBirth`, `TrackerBirth` | `application/init.go` | `infrastructure/vault/birth.go`, `infrastructure/beads/init.go` | none |
| `Landing` | `application/landing.go` | `infrastructure/rig/landing.go` | none |
| `Checks` | `application/landing.go` | `infrastructure/rig/checks.go` | none |
| `AfterLanding` | `application/afterlanding.go` | `infrastructure/rig/afterlanding.go` | none |
| `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/slot.go` | none: real `flock` |
| `Dispatcher` | `application/landing.go` | `application.Dispatch` | — |
| `HostSync` | `application/dispatch.go` | `application.Sync` | — |

`infrastructure/config/config.go` is not a port: read by `cmd/mw/`.

## Use cases

| Use case | File | Command | Feature |
| --- | --- | --- | --- |
| `File` | `application/file.go` | `mw file` — `cmd/mw/file.go` | `features/file_plan.feature` |
| `Release` | `application/release.go` | `mw release` — `cmd/mw/release.go` | `features/release.feature` |
| `Retry` | `application/retry.go` | `mw retry` — `cmd/mw/retry.go` | `features/retry.feature` |
| `Show` | `application/show.go` | `mw show` — `cmd/mw/show.go` | `features/show.feature` |
| `Dispatch` | `application/dispatch.go` | `mw dispatch` — `cmd/mw/dispatch.go` | `features/dispatch.feature` |
| `Next` | `application/next.go` | `mw next` — `cmd/mw/next.go` | `features/next.feature` |
| `Check` | `application/check.go` | `mw check` — `cmd/mw/check.go` | `features/check.feature` |
| `Status` | `application/status.go` | `mw status` — `cmd/mw/status.go` | `features/status.feature` |
| `Brief` | `application/brief.go` | `mw brief` — `cmd/mw/brief.go` | `features/brief.feature` |
| `Sweep` | `application/sweep.go` | `mw sweep` — `cmd/mw/sweep.go` | `features/sweep.feature` |
| `Sync` | `application/sync.go` | `mw sync` — `cmd/mw/sync.go` | `features/sync.feature` |
| `Nudge` | `application/nudge.go` | `mw nudge` — `cmd/mw/nudge.go` | none |
| `Mail` | `application/mail.go` | `mw mail` — `cmd/mw/mail.go` | `features/mail.feature` |
| `SeatContext` | `application/seatcontext.go` | `mw seat context` — `cmd/mw/seat.go` | `features/seat_context.feature` |
| `SeatUp` | `application/seatup.go` | `mw seat up` — `cmd/mw/seat.go` | `features/seat_up.feature` |
| `SeatReap` | `application/seatreap.go` | `mw seat reap` — `cmd/mw/seat.go` | `features/seat_reap.feature` |
| `Millhand` | `application/millhand.go` | `mw millhand` — `cmd/mw/millhand.go` | `features/millhand.feature` |
| `MillhandTick` | `application/millhandtick.go` | `mw millhand tick` — `cmd/mw/millhandtick.go` | `features/millhand_tick.feature` |
| `Watch` | `application/watch.go` | `mw watch` — `cmd/mw/watch.go` | `features/watch.feature` |
| `Doctor` | `application/doctor.go` | `mw doctor` — `cmd/mw/doctor.go` | `features/doctor.feature` |
| `SeatBoot` | `application/seatboot.go` | none: `Dispatch`, `Next` call it | `features/seat_boot.feature` |
| `Init` | `application/init.go` | `mw init` — `cmd/mw/init.go` | `features/init.feature` |

`cmd/mw/root.go` holds the tree; `cmd/mw/main.go` runs it; `cmd/mw/version.go`
is `mw version` (no use case).
`features/path_validation.feature` covers `domain/path.go`;
`features/ready_stories.feature` the `WorkTracker` contract (no commands).

Adding a command: `docs/adding-a-command.md`.

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault` | `infrastructure/beads/beads_integration_test.go` | A real bd database, temp dir. |
| `installFormula` | same file | Copies a formula into it. |
| `standIn` | `infrastructure/beads/sync_test.go` | Stands in for `bd`. |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | A private tmux server. |
| `privateWindows` | `infrastructure/tmux/window_integration_test.go` | A tmux server, seats session. |
| `aVault` | `infrastructure/vault/vault_test.go` | A vault with a seat. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two clones of one vault. |
| `aRig` | `infrastructure/rig/worktree_test.go` | A rig, real origin. |
| `aRigDir` | `infrastructure/rig/slot_test.go` | A merge-slot directory. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | A `config.toml`, temp `HOME`. |
| `aFactory` | `application/dispatch_test.go` | A faked `Dispatch`, temp vault. |

Feature steps build a real rig, origin, clone, vault in a temp dir; fake only
tracker, runner, vault git: `features/steps/next_steps.go`.

## Build and test

`make build`, `make test`, `make lint`: see `CLAUDE.md`.

- One package: `go test ./application/...` (`-run TestName`); add
  `-tags beads_integration` for the real-`bd` cases, as `make test` does.
- One feature: `MW_FEATURE=sweep.feature go test ./features` (`:17` for one scenario).
- `make lint` runs `scripts/check-*.sh`: this page, `template/`, `contrib/systemd/`, install scripts.
- `contrib/mw-heavy` caps memory: `MW_HEAVY_MEMORY_MAX`, `MW_HEAVY_SWAP_MAX`.
- `make check-formulas` needs `bd`, `jq`, real time; not in `make test`.
