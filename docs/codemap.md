# Code map

## Layers

`domain/`, `domain/events`: stdlib · `application/`: use cases, ports · `application/apptest/`: fakes · `infrastructure/`: adapters · `cmd/mw/`: cobra · `features/`: Gherkin, `features/steps/` · `template/`: embed.
`domain/`, `domain/events`: stdlib · `application/`: use cases, ports · `application/apptest/`: fakes · `infrastructure/`: an adapter each · `cmd/mw/`: cobra · `features/`: Gherkin, `features/steps/`

## Ports

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | `application/worktracker.go` | `infrastructure/beads/` | `apptest.FakeTracker` |
| `TrackerSync` | `application/sync.go` | `infrastructure/beads/sync.go` | `apptest.FakeTracker` |
| `TrackerNotes`, `SweepNotes` | `application/{status,sweep}.go` | same | same |
| `VaultFiles` | `application/sync.go` | `infrastructure/vault/git.go` | `apptest.FakeVaultFiles` |
| `Mailbox`, `TidyMailbox` | `application/{mail,tidy}.go` | `infrastructure/beads/mail.go` | `apptest.FakeMailbox` |
| `EpicRules` | `application/epicrules.go` | `infrastructure/vault/epicrules.go` | `apptest.FakeEpicRules` |
| `Vault` | `application/seatboot.go` | `infrastructure/vault/vault.go` | `application/seatboot_test.go` |
| `Runner` | `application/runner.go` | `infrastructure/tmux/tmux.go` | `apptest.FakeRunner` |
| `Harness` | `application/harness.go` | `infrastructure/claude/claude.go` | `application/seatboot_test.go` |
| `SeatFiles`, `Windows`, `SeatHarness`, `ActingFile` | `application/seatup.go`, `application/seathandover.go` | `infrastructure/{vault/{seat,reaplog},tmux/window,claude/claude}.go` | `apptest.Fake{Windows,ActingFile}` |
| `Transcripts` | `application/seatcontext.go` | `infrastructure/claude/transcripts.go` | none |
| `Reap{Terminal,Log,Armer}` | `application/seatreap.go` | `infrastructure/{tmux/reap,vault/reaplog,reaper/arm}.go` | `apptest.Fake{Windows,ReapArmer}` |
| `WatchProbes` | `application/watch.go` | `infrastructure/watch/watch.go` | `apptest.FakeWatch` |
| `Doctor{Check,State,Log,Notes}` | `application/doctor.go` | `infrastructure/doctor` | none |
| `TickLog` | `application/millhandtick.go` | `infrastructure/ticklog/ticklog.go` | `apptest.FakeTickLog` |
| `Worktrees` | `application/worktrees.go` | `infrastructure/rig/worktree.go` | `application/dispatch_test.go` |
| `HostLoad` | `application/hostload.go` | `infrastructure/hostload/hostload.go` | `apptest.FakeHostLoad` |
| `SyncHaltMarker` | `application/sync.go` | `infrastructure/synchalt/synchalt.go` | `apptest.FakeSyncHaltMarker` |
| `Notifier`, `HomeMoveHost`, `OldHome`, `VaultBirth`, `TrackerBirth` | `application/{millhandtick,homemove,init}.go` | `infrastructure/{notify/notify,homemove/homemove,vault/birth,beads/init}.go` | none |
| `Landing`, `Checks`, `MergeSlot`, `Holding` | `application/landing.go` | `infrastructure/rig/{landing,checks,slot}.go` | none |
| `AfterLanding`, `SelfUpdate`, `BuiltMarks`, `BackendBuilds` | `application/afterlanding.go`, `application/selfupdate.go`, `application/backendstage.go` | `infrastructure/rig/{afterlanding,built,backend}.go` | `features/self_update.feature` |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, HarnessCount | application/event{log,follow,ship,subscribe,nudge,spring}.go, status.go | infrastructure/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | apptest.Fake{EventLog,FollowCursors,Ship*,Tracker,Subscribe*,Nudge*} |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventController | application/event*.go | infrastructure/eventlog, vault/subscribe.go, beads/feed.go | apptest.Fake{EventLog,Follow*,Ship*,Tracker,Subscribe*,Nudge*} |
| Postern, hands | `application/{postern*,hands}.go` | `infrastructure/{postern,hands*,homemove}` | `apptest.Fake{Postern*,Cipher,SnapshotFile,NginxRunner,Transcriber,Hands*,HomeMover}` |
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
| `Seat{Context,Up,Reap,Handover}` | `application/seat{context,up,reap,handover}.go` | `mw seat context`/`up`/`reap`/`handover` — `cmd/mw/seat.go` | `features/seat_{context,up,reap}.feature` |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk call/say/wait/model cmd/mw/talk.go | features/talk_*.feature |
| `Millhand` | `application/millhand.go` | `mw millhand` — `cmd/mw/millhand.go` | `features/millhand.feature` |
| `Deputy` | `application/deputy.go` | `mw deputy` — `cmd/mw/deputy.go` | `features/deputy.feature` |
| `MillhandTick` | `application/millhandtick.go` | `mw millhand tick` — `cmd/mw/millhandtick.go` | `features/millhand_tick.feature` |
| `Watch` | `application/watch.go` | `mw watch` — `cmd/mw/watch.go` | `features/watch.feature` |
| `Doctor` | `application/doctor.go` | `mw doctor` — `cmd/mw/doctor.go` | `features/doctor.feature` |
| `SeatBoot` | `application/seatboot.go` | none: called by `Dispatch`, `Next` | `features/seat_boot.feature` |
| `Init` | `application/init.go` | `mw init` — `cmd/mw/init.go` | `features/init.feature` |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern key/inbox/send/snapshot/view/bead — cmd/mw/posternview.go, cmd/mw/posternbead.go | `features/postern_*.feature` |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go | mw events follow/emit/tail/wait — cmd/mw/events.go | none |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Control} | application/eventfollow.go, application/eventcontrol.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go | mw events follow/emit/tail/wait — cmd/mw/events.go | none |
| Hands{Add,List} | application/hands.go | mw hands add/list — cmd/mw/hands.go; `cmd/mw-hands-root` | `features/hands.feature` |
| `Postern{Serve,Nginx,Mirror}` | `application/posternhand.go`, `application/posternmirror.go` | `mw postern serve`/`nginx`/`mirror` — `cmd/mw/postern.go`, `cmd/mw/posternmirror.go` | `features/postern_serve.feature` |
| `Grist{Key,Grind,Send,Eval}` | `application/grist.go`, `application/gristgrind.go`, `application/gristsend.go`, `application/gristeval.go` | `mw grist key`/`grind`/`send`/`eval` — `cmd/mw/grist.go` | `features/grist{,_send,_eval}.feature` |

## Test helpers

`throwawayVault`, `installFormula` (real bd), `standIn` (fake bd): `infrastructure/beads/`; `privateRunner`, `privateWindows` (tmux): `infrastructure/tmux/`; `aVault`, `twoHosts`: `infrastructure/vault/`; `aRig`: `infrastructure/rig/`; `mwConfig` (temp HOME): `cmd/mw/`.

## Build and test

See `CLAUDE.md`.

- One feature: `MW_FEATURE=sweep.feature go test ./features`.

`cmd/mw/root.go`: the tree; `cmd/mw/version.go`.
