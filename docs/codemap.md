# Code map

## Layers

domain/, domain/events: stdlib · application/ (apptest/: fakes): use cases, ports · infrastructure/: adapters · cmd/mw/: cobra · features/ (steps/): Gherkin · template/: embed

## Ports

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | application/worktracker.go | infrastructure/beads/ | apptest.FakeTracker |
| TrackerNotes, SweepNotes, BeadGraph | application/{status,sweep}.go | same | same |
| VaultFiles, TrackerSync | application/sync.go | `infrastructure/{vault/git,beads/sync}.go` | apptest.Fake{VaultFiles,Tracker} |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | `infrastructure/beads/mail.go` | apptest.FakeMailbox |
| EpicRules | application/epicrules.go | `infrastructure/vault/epicrules.go` | apptest.FakeEpicRules |
| Vault | application/seatboot.go | infrastructure/vault | application/seatboot_test.go |
| Runner | application/runner.go | infrastructure/tmux | apptest.FakeRunner |
| Harness | application/harness.go | infrastructure/claude | application/seatboot_test.go |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | infrastructure/{vault/{seat,reaplog},tmux/window,claude/claude}.go | apptest.Fake{Windows,ActingFile} |
| Transcripts{,Tail,Replies}, PeekRemote | application/seatcontext.go, application/peek.go | infrastructure/{claude,peekremote} | apptest/fakepeek.go |
| Reap{Terminal,Log,Armer} | application/seatreap.go | infrastructure/{tmux/reap,vault/reaplog,reaper/arm}.go | apptest.Fake{Windows,ReapArmer} |
| WatchProbes | application/watch.go | infrastructure/watch | apptest.FakeWatch |
| Doctor{Check,State,Log,Notes}, VPSProbe | application/doctor.go, application/vpsnginx.go | infra/{doctor,vpsnginx} | none |
| Network{Probe,Store,Reader} | application/network.go | infrastructure/network | apptest.FakeNetwork* |
| TickLog | application/millhandtick.go | infrastructure/ticklog | apptest.FakeTickLog |
| CardLog | application/card.go | infrastructure/cardlog | apptest.FakeCardLog |
| Worktrees | application/worktrees.go | `infrastructure/rig/worktree.go` | application/dispatch_test.go |
| HostLoad | application/hostload.go | infrastructure/hostload | apptest.FakeHostLoad |
| SyncHaltMarker | application/sync.go | infrastructure/synchalt | apptest.FakeSyncHaltMarker |
| Notifier, HomeMoveHost, OldHome, VaultBirth, TrackerBirth | application/{millhandtick,homemove,init}.go | infrastructure/{notify/notify,homemove/homemove,vault/birth,beads/init}.go | none |
| Landing, Checks, MergeSlot, Holding | application/landing.go | infrastructure/rig/{landing,checks,slot}.go | none |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter | application/afterlanding.go, application/selfupdate.go, application/backendstage.go | infrastructure/{rig/{afterlanding,built,backend},userunits}.go | features/{self_update,backend_swap}.feature |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | infrastructure/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | apptest.Fake{EventLog,FollowCursors,Ship*,Tracker,Subscribe*,Nudge*} |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | infrastructure/{postern,hands*,homemove} | apptest.Fake{Postern*,Cipher,SnapshotFile,NginxRunner,Transcriber,Hands*,HomeMover,Prompts} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | infra/{bsv,stampqueue,chainlookup}, rig/worktree.go | apptest.Fake{Chain,StampQueue,CommitNotes,RigHeads} |
| Grinder, GrindSource, GristState, GristLock, GristRunStore | application/grist.go, application/gristruns.go | infrastructure/{claude/grind,rig/grinds,hostlock/try}.go, infrastructure/grist | apptest.Fake{Grinder,Grinds,GristState,GristLock} |
| Scorer | application/scorer.go | infrastructure/scorer/ | apptest.FakeScorer |

## Use cases

| Use case | File | Command | Feature |
| --- | --- | --- | --- |
| File | application/file.go | mw file - cmd/mw/file.go | features/file_plan.feature |
| Release | application/release.go | mw release - cmd/mw/release.go | features/release.feature |
| Retry | application/retry.go | mw retry - cmd/mw/retry.go | features/retry.feature |
| Show, Peek, Prove, StampHead | application/show.go, application/peek.go, application/prove.go, application/stamphead.go | mw show/peek/prove/stamp - cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go | features/{show,peek,prove,stamp}.feature |
| Dispatch | application/dispatch.go | mw dispatch - cmd/mw/dispatch.go | features/dispatch.feature |
| Next | application/next.go | mw next - cmd/mw/next.go | features/next.feature |
| Check | application/check.go | mw check - cmd/mw/check.go | features/check.feature |
| Status | application/status.go | mw status - cmd/mw/status.go | features/status.feature |
| Brief | application/brief.go | mw brief - cmd/mw/brief.go | features/brief.feature |
| Sweep, Tidy | application/sweep.go, application/tidy.go | mw sweep/tidy - cmd/mw/sweep.go, cmd/mw/tidy.go | features/{sweep,tidy}.feature |
| Sync | application/sync.go | mw sync - cmd/mw/sync.go | features/sync.feature |
| Nudge | application/nudge.go | mw nudge - cmd/mw/nudge.go | none |
| Mail | application/mail.go | mw mail - cmd/mw/mail.go | features/mail.feature |
| Home | application/home.go | mw home - cmd/mw/home.go | features/home.feature |
| HomeMove | application/homemove.go | mw home move - cmd/mw/homemove.go | none: docs/home-move.md |
| Seat{Context,Up,Reap,Handover} | application/seat{context,up,reap,handover}.go | mw seat context/up/reap/handover - cmd/mw/seat.go | features/seat_{context,up,reap}.feature |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk call/say/wait/model cmd/mw/talk.go | features/talk_*.feature |
| Millhand, Deputy | application/millhand.go, application/deputy.go | mw millhand/deputy - cmd/mw/millhand.go, cmd/mw/deputy.go | features/{millhand,deputy}.feature |
| MillhandTick | application/millhandtick.go | mw millhand tick - cmd/mw/millhandtick.go | features/millhand_tick.feature |
| Watch | application/watch.go | mw watch - cmd/mw/watch.go | features/watch.feature |
| Doctor | application/doctor.go | mw doctor - cmd/mw/doctor.go | features/doctor.feature |
| SeatBoot | application/seatboot.go | none: called by Dispatch, Next | features/seat_boot.feature |
| Init | application/init.go | mw init - cmd/mw/init.go | features/init.feature |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern key/inbox/send/snapshot/view/bead - cmd/mw/posternview.go, cmd/mw/posternbead.go | `features/postern_*.feature` |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring,Control} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go | mw events follow/emit/tail/wait - cmd/mw/events.go | features/event_follow.feature |
| Hands{Add,List} | application/hands.go | mw hands add/list - cmd/mw/hands.go; cmd/mw-hands-root | features/hands.feature |
| Postern{Serve,Nginx,Mirror} | application/posternhand.go, application/posternmirror.go | mw postern serve/nginx/mirror - cmd/mw/postern.go, cmd/mw/posternmirror.go | features/postern_serve.feature |
| Prompt{Save,List,Show,Run}, Cards | application/prompt.go, application/card.go | mw prompt/card - cmd/mw/prompt.go, cmd/mw/card.go | features/{prompt,card}.feature |
| Grist{Key,Grind,Send,Eval,Score,Runs} | application/grist.go, application/gristgrind.go, application/gristsend.go, application/gristeval.go, application/gristscore.go | mw grist key/grind/send/eval/score/runs - cmd/mw/grist.go | features/{grist{,_send,_eval,_audio},scorer}.feature |

cmd/mw/root.go: the tree; cmd/mw/version.go.
