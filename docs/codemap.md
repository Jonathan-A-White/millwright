# Code map

Where everything is; `CONTEXT.md` has the vocabulary, ADRs why.

## Layers

| Layer | Directory | Rule |
| --- | --- | --- |
| domain | `domain/` | Value types, validation; stdlib only. |
| application | `application/` | One file per use case, plus ports; imports no adapter. |
| fakes | `application/apptest/` | In-memory port stand-ins. |
| infrastructure | `infrastructure/` | One subpackage per adapter. |
| command line | `cmd/mw/` | Cobra wiring: read config, run the use case. |
| features | `features/` | Gherkin; step code in `features/steps/`. |
| template | `template/` | Born from `embed.go`; no host detail. |

## Ports

Every adapter: `var _ application.<Port> = ...`.

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | `application/worktracker.go` | `infrastructure/beads/beads.go` | `apptest.FakeTracker` |
| `TrackerSync` | `application/sync.go` | `infrastructure/beads/sync.go` | `apptest.FakeTracker` |
| `TrackerNotes` | `application/status.go` | same, read half | same |
| `SweepNotes` | `application/sweep.go` | same, `Note`s | same |
| `VaultFiles` | `application/sync.go` | `infrastructure/vault/git.go` | `apptest.FakeVaultFiles` |
| `Mailbox` | `application/mail.go` | `infrastructure/beads/mail.go` | `apptest.FakeMailbox` |
| `Vault` | `application/seatboot.go` | `infrastructure/vault/vault.go` | `application/seatboot_test.go` |
| `Runner` | `application/runner.go` | `infrastructure/tmux/tmux.go` | `apptest.FakeRunner` |
| `Harness` | `application/harness.go` | `infrastructure/claude/claude.go` | `application/seatboot_test.go` |
| `SeatFiles` | `application/seatup.go` | `infrastructure/vault/seat.go` | none |
| `Windows` | `application/seatup.go` | `infrastructure/tmux/window.go` | `apptest.FakeWindows` |
| `SeatHarness` | `application/seatup.go` | `infrastructure/claude/claude.go` | none |
| `ReapTerminal` | `application/seatreap.go` | `infrastructure/tmux/reap.go` | `apptest.FakeWindows` |
| `ReapLog` | `application/seatreap.go` | `infrastructure/vault/reaplog.go` | none |
| `ReapArmer` | `application/seatreap.go` | `infrastructure/reaper/arm.go` | `apptest.FakeReapArmer` |
| `Transcripts` | `application/seatcontext.go` | `infrastructure/claude/transcripts.go` | none |
| `WatchProbes` | `application/watch.go` | `infrastructure/watch/watch.go` | `apptest.FakeWatch` |
| `DoctorCheck`, `DoctorState`, `DoctorLog`, `DoctorNotes` | `application/doctor.go` | `infrastructure/doctor` | none |
| `TickLog` | `application/millhandtick.go` | `infrastructure/ticklog/ticklog.go` | `apptest.FakeTickLog` |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `SyncHaltMarker` | `application/sync.go` | `infrastructure/synchalt/synchalt.go` | `apptest.FakeSyncHaltMarker` |
| `Notifier` | `application/millhandtick.go` | `infrastructure/notify/notify.go` | none |
| `VaultBirth`, `TrackerBirth` | `application/init.go` | `infrastructure/vault/birth.go`, `infrastructure/beads/init.go` | none |
| `Landing` | `application/landing.go` | `infrastructure/rig/landing.go` | none |
| `Checks` | `application/landing.go` | `infrastructure/rig/checks.go` | none |
| `AfterLanding` | `application/afterlanding.go` | `infrastructure/rig/afterlanding.go` | none |
| `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/slot.go` | none |
| `Dispatcher` | `application/landing.go` | `application.Dispatch` | — |
| `HostSync` | `application/dispatch.go` | `application.Sync` | — |
| Postern ports (key, msg, snapshot, hand) | `application/postern*.go` | `infrastructure/postern` | `apptest.Fake{Postern,Cipher,SnapshotFile,NginxRunner}` |

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
| `SeatContext`, `SeatUp`, `SeatReap` | `application/seatcontext.go`, `application/seatup.go`, `application/seatreap.go` | `mw seat context`/`up`/`reap` — `cmd/mw/seat.go` | `features/seat_context.feature`, `features/seat_up.feature`, `features/seat_reap.feature` |
| `Millhand` | `application/millhand.go` | `mw millhand` — `cmd/mw/millhand.go` | `features/millhand.feature` |
| `MillhandTick` | `application/millhandtick.go` | `mw millhand tick` — `cmd/mw/millhandtick.go` | `features/millhand_tick.feature` |
| `Watch` | `application/watch.go` | `mw watch` — `cmd/mw/watch.go` | `features/watch.feature` |
| `Doctor` | `application/doctor.go` | `mw doctor` — `cmd/mw/doctor.go` | `features/doctor.feature` |
| `SeatBoot` | `application/seatboot.go` | none: `Dispatch`, `Next` call it | `features/seat_boot.feature` |
| `Init` | `application/init.go` | `mw init` — `cmd/mw/init.go` | `features/init.feature` |
| `PosternKeyInit`, `PosternKeyShow`, `PosternInbox`, `PosternSend` | `application/postern.go` | `mw postern key`/`inbox`/`send` (`--bead --recommend --option`; mails mayor) | `features/postern_key.feature` |
| `PosternSnapshot` | `application/posternsnapshot.go` | `mw postern snapshot` (`--json`; landed w/o `VERIFIED`) | none |
| `PosternServe`, `PosternNginx` | `application/posternhand.go` | `mw postern serve`/`nginx` (`--dry-run`) — `cmd/mw/postern.go` | `features/postern_serve.feature` |

`cmd/mw/root.go` holds the tree; `cmd/mw/main.go` runs it; `cmd/mw/version.go` is
`mw version`. `features/path_validation.feature` covers `domain/path.go`;
`features/ready_stories.feature` the `WorkTracker` contract.

Add a command: `docs/adding-a-command.md`.

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault` | `infrastructure/beads/beads_integration_test.go` | Real bd DB, temp dir. |
| `installFormula` | same file | Copies a formula in. |
| `standIn` | `infrastructure/beads/sync_test.go` | Stands in for `bd`. |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | Private tmux server. |
| `privateWindows` | `infrastructure/tmux/window_integration_test.go` | Tmux server + seat. |
| `aVault` | `infrastructure/vault/vault_test.go` | Vault with a seat. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two vault clones. |
| `aRig` | `infrastructure/rig/worktree_test.go` | Rig, real origin. |
| `aRigDir` | `infrastructure/rig/slot_test.go` | Merge-slot dir. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | `config.toml`, temp HOME. |
| `aFactory` | `application/dispatch_test.go` | Faked `Dispatch`, temp vault. |

`features/steps/next_steps.go`: real rig; fakes tracker, runner, vault git.

## Build and test

`make build`, `test`, `lint`: see `CLAUDE.md`.

- One package: `go test ./application/...` (`-run`, `-tags beads_integration`).
- One feature: `MW_FEATURE=sweep.feature go test ./features` (`:17` for a scenario).
- `make lint` runs `scripts/check-*.sh`; `make check-formulas` needs `bd`, `jq` (not in `make test`).
