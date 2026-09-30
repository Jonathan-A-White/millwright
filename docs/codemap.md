# Code map

## Layers

| Layer | Directory | Rule |
| --- | --- | --- |
| domain | `domain/` | Value types, validation; stdlib only. |
| application | `application/` | One file per use case, plus ports. |
| fakes | `application/apptest/` | Port fakes. |
| infrastructure | `infrastructure/` | One subpackage per adapter. |
| command line | `cmd/mw/` | Cobra wiring. |
| features | `features/` | Gherkin; steps in `features/steps/`. |
| template | `template/` | Born from `embed.go`. |

## Ports

Each adapter: `var _ application.<Port> = ...`.

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | `application/worktracker.go` | `infrastructure/beads/beads.go` | `apptest.FakeTracker` |
| `TrackerSync` | `application/sync.go` | `infrastructure/beads/sync.go` | `apptest.FakeTracker` |
| `TrackerNotes` | `application/status.go` | same, read half | same |
| `SweepNotes` | `application/sweep.go` | same | same |
| `VaultFiles` | `application/sync.go` | `infrastructure/vault/git.go` | `apptest.FakeVaultFiles` |
| `Mailbox`, `TidyMailbox` | `application/{mail,tidy}.go` | `infrastructure/beads/mail.go` | `apptest.FakeMailbox` |
| `Vault` | `application/seatboot.go` | `infrastructure/vault/vault.go` | `application/seatboot_test.go` |
| `Runner` | `application/runner.go` | `infrastructure/tmux/tmux.go` | `apptest.FakeRunner` |
| `Harness` | `application/harness.go` | `infrastructure/claude/claude.go` | `application/seatboot_test.go` |
| `SeatFiles`, `Windows`, `SeatHarness` | `application/seatup.go` | `infrastructure/{vault/seat,tmux/window,claude/claude}.go` | `apptest.FakeWindows` |
| `ReapTerminal`, `ReapLog`, `ReapArmer` | `application/seatreap.go` | `infrastructure/{tmux/reap,vault/reaplog,reaper/arm}.go` | `apptest.Fake{Windows,ReapArmer}` |
| `Transcripts` | `application/seatcontext.go` | `infrastructure/claude/transcripts.go` | none |
| `WatchProbes` | `application/watch.go` | `infrastructure/watch/watch.go` | `apptest.FakeWatch` |
| `Doctor{Check,State,Log,Notes}` | `application/doctor.go` | `infrastructure/doctor` | none |
| `TickLog` | `application/millhandtick.go` | `infrastructure/ticklog/ticklog.go` | `apptest.FakeTickLog` |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `SyncHaltMarker` | `application/sync.go` | `infrastructure/synchalt/synchalt.go` | `apptest.FakeSyncHaltMarker` |
| `Notifier` | `application/millhandtick.go` | `infrastructure/notify/notify.go` | none |
| `HomeMoveHost`, `OldHome` | `application/homemove.go` | `infrastructure/homemove/homemove.go` | none |
| `VaultBirth`, `TrackerBirth` | `application/init.go` | `infrastructure/{vault/birth,beads/init}.go` | none |
| `Landing`, `Checks`, `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/{landing,checks,slot}.go` | none |
| `AfterLanding` | `application/afterlanding.go` | `infrastructure/rig/afterlanding.go` | none |
| `Dispatcher` | `application/landing.go` | `application.Dispatch` | — |
| `HostSync` | `application/dispatch.go` | `application.Sync` | — |
| Postern, hands | `application/{postern*,hands}.go` | `infrastructure/{postern,hands*,homemove}` | `apptest.Fake{Postern,Cipher,SnapshotFile,NginxRunner,Transcriber,Hands*,HomeMover}` |
| `Grinder`, `GrindSource`, `GristState`, `GristLock` | `application/grist.go` | `infrastructure/{claude/grind,rig/grinds,hostlock/try}.go`, `infrastructure/grist` | `apptest.Fake{Grinder,Grinds,GristState,GristLock}` |

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
| `Tidy` | `application/tidy.go` | `mw tidy` — `cmd/mw/tidy.go` | `features/tidy.feature` |
| `Sync` | `application/sync.go` | `mw sync` — `cmd/mw/sync.go` | `features/sync.feature` |
| `Nudge` | `application/nudge.go` | `mw nudge` — `cmd/mw/nudge.go` | none |
| `Mail` | `application/mail.go` | `mw mail` — `cmd/mw/mail.go` | `features/mail.feature` |
| `Home` | `application/home.go` | `mw home` — `cmd/mw/home.go` | `features/home.feature` |
| `HomeMove` | `application/homemove.go` | `mw home move` — `cmd/mw/homemove.go` | none: `docs/home-move.md` |
| `Seat{Context,Up,Reap}` | `application/seatcontext.go`, `application/seatup.go`, `application/seatreap.go` | `mw seat context`/`up`/`reap` — `cmd/mw/seat.go` | `features/seat_{context,up,reap}.feature` |
| `Millhand` | `application/millhand.go` | `mw millhand` — `cmd/mw/millhand.go` | `features/millhand.feature` |
| `MillhandTick` | `application/millhandtick.go` | `mw millhand tick` — `cmd/mw/millhandtick.go` | `features/millhand_tick.feature` |
| `Watch` | `application/watch.go` | `mw watch` — `cmd/mw/watch.go` | `features/watch.feature` |
| `Doctor` | `application/doctor.go` | `mw doctor` — `cmd/mw/doctor.go` | `features/doctor.feature` |
| `SeatBoot` | `application/seatboot.go` | none: called by `Dispatch`, `Next` | `features/seat_boot.feature` |
| `Init` | `application/init.go` | `mw init` — `cmd/mw/init.go` | `features/init.feature` |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern key/inbox/send/snapshot/view/bead — cmd/mw/posternview.go, cmd/mw/posternbead.go | `features/postern_*.feature` |
| Hands{Add,List} | application/hands.go | mw hands add /list — cmd/mw/hands.go; `cmd/mw-hands-root` | `features/hands.feature` |
| `PosternServe`, `PosternNginx`, `PosternMirror` | `application/posternhand.go`, `application/posternmirror.go` | `mw postern serve`/`nginx`/`mirror` — `cmd/mw/postern.go`, `cmd/mw/posternmirror.go` | `features/postern_serve.feature` |
| `GristKey`, `GristGrind`, `GristSend`, `GristEval` | `application/grist.go`, `application/gristgrind.go`, `application/gristsend.go`, `application/gristeval.go` | `mw grist key`/`grind`/`send`/`eval` — `cmd/mw/grist.go` | `features/grist{,_send,_eval}.feature` |

`cmd/mw/root.go` holds the tree, `cmd/mw/main.go` runs it, `cmd/mw/version.go` is `mw version`.
`features/path_validation.feature` covers `domain/path.go`;
`features/{ready_stories,claim_lease}.feature`: `WorkTracker`.

## Test helpers

| Helper | Where | What it gives |
| --- | --- | --- |
| `throwawayVault`, `installFormula` | `infrastructure/beads/beads_integration_test.go` | Real bd, temp dir. |
| `standIn` | `infrastructure/beads/sync_test.go` | Stand-in `bd`. |
| `privateRunner` | `infrastructure/tmux/tmux_integration_test.go` | Private tmux. |
| `privateWindows` | `infrastructure/tmux/window_integration_test.go` | Tmux, seat. |
| `aVault` | `infrastructure/vault/vault_test.go` | Seat vault. |
| `twoHosts` | `infrastructure/vault/git_test.go` | Two clones. |
| `aRig` | `infrastructure/rig/worktree_test.go` | Rig + origin. |
| `mwConfig` | `cmd/mw/dispatch_test.go` | Temp-HOME config. |

## Build and test

See `CLAUDE.md`; then:

- One package: `go test ./application/...` (`-run`, `-tags beads_integration`).
- One feature: `MW_FEATURE=sweep.feature go test ./features` (`:17`: a scenario).
- `make lint` runs `scripts/check-*.sh`; `make check-formulas` needs `bd`, `jq`.
