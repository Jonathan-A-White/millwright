# Code map

## Layers

domain/(events): stdlib · application/ (apptest/: fakes): use cases, ports · infra/: adapters · cmd/mw/: cobra · features/ (steps/): Gherkin · template/: embed

## Ports

| Port | Declared in | Real adapter | Fake |
| --- | --- | --- | --- |
| `WorkTracker` | application/worktracker.go | infra/beads/ | apptest.FakeTracker |
| TrackerNotes, SweepNotes, BeadGraph | application/{status,sweep}.go | same | same |
| VaultFiles, TrackerSync, SyncHaltMarker | application/sync.go | `infra/{vault/git,beads/sync}.go`, infra/synchalt | apptest.Fake{VaultFiles,Tracker,SyncHaltMarker} |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | infra/beads/mail.go | apptest.FakeMailbox |
| EpicRules | application/epicrules.go | infra/vault/epicrules.go | apptest.FakeEpicRules |
| Vault | application/seatboot.go | infra/vault | application/seatboot_test.go |
| Runner | application/runner.go | infra/tmux | apptest.FakeRunner |
| Harness | application/harness.go | infra/claude | seatboot_test.go |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | infra/{vault/{seat,reaplog},tmux/window,claude/claude}.go | apptest.Fake{Windows,ActingFile} |
| Transcripts{,Tail,Replies}, PeekRemote | application/seatcontext.go, application/peek.go | infra/{claude,peekremote} | apptest/fakepeek.go |
| Reap{Terminal,Log,Armer} | application/seatreap.go | infra/{tmux/reap,vault/reaplog,reaper/arm}.go | apptest.Fake{Windows,ReapArmer} |
| WatchProbes | application/watch.go | infra/watch | apptest.FakeWatch |
| Doctor{Check,State,Log,Notes}, VPSProbe | application/doctor.go, application/vpsnginx.go | infra/{doctor,vpsnginx} | none |
| Network{Probe,Store,Reader} | application/network.go | infra/network | apptest.FakeNetwork* |
| CardLog | application/card.go | infra/cardlog | apptest.FakeCardLog |
| CloseOutMarks | application/closeout.go | infra/closeout | apptest.Fake*Mark* |
| Worktrees | application/worktrees.go | infra/rig/worktree.go | application/dispatch_test.go |
| HostLoad | application/hostload.go | infra/hostload | apptest.FakeHostLoad |
| TickLog, Notifier, HomeMoveHost, OldHome, VaultBirth, TrackerBirth | application/{millhandtick,homemove,init}.go | infra/{ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go | apptest.FakeTickLog |
| Landing, Checks, MergeSlot, Holding | application/landing.go | infra/rig/{landing,checks,slot}.go | none |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter | application/afterlanding.go, application/selfupdate.go, application/backendstage.go | infra/{rig/{afterlanding,built,backend},userunits}.go | features/{self_update,backend_swap}.feature |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | infra/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | apptest.Fake{EventLog,FollowCursors,Ship*,Tracker,Subscribe*,Nudge*} |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | infra/{postern,hands*,homemove} | apptest.Fake{Postern*,Cipher,SnapshotFile,NginxRunner,Transcriber,Hands*,HomeMover,Prompts} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | infra/{bsv,stampqueue,chainlookup}, rig/worktree.go | apptest.Fake{Chain,StampQueue,CommitNotes,RigHeads} |
| Grinder, GrindSource, GristState, GristLock, GristRunStore, GristForwardStore | application/grist.go, application/gristruns.go | infra/{claude/grind,rig/grinds,hostlock/try}.go, infra/grist | apptest.Fake{Grinder,Grinds,GristState,GristLock} |
| Scorer | application/scorer.go | infra/scorer/ | apptest.FakeScorer |
| SecretStore | application/secrets.go | infra/sops | apptest.FakeSecretStore |

## Use cases

| Use case | File | Command | Feature |
| --- | --- | --- | --- |
| File, Release, Retry | application/file.go, application/release.go, application/retry.go | mw file/release/retry - cmd/mw/file.go, cmd/mw/release.go, cmd/mw/retry.go | features/{file_plan,release,retry}.feature |
| Show, Peek, Prove, StampHead | application/show.go, application/stamphead.go | mw show/peek/prove/stamp - cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go | features/{show,peek,prove,stamp}.feature |
| Dispatch | application/dispatch.go | mw dispatch - cmd/mw/dispatch.go | features/dispatch.feature |
| Next, AfterLandingRun | application/next.go, application/afterlandingrun.go | mw next, mw after-landing - cmd/mw/next.go, cmd/mw/afterlanding.go | features/{next,after_landing}.feature |
| Tester | application/testerreport.go | mw tester report - cmd/mw/tester.go; [tester]: rigs until model effort | features/tester.feature |
| Check | application/check.go | mw check - cmd/mw/check.go | features/check.feature |
| Status | application/status.go | mw status - cmd/mw/status.go | features/status.feature |
| Brief | application/brief.go | mw brief - cmd/mw/brief.go | features/brief.feature |
| Sweep, Tidy | application/sweep.go, application/tidy.go | mw sweep/tidy - cmd/mw/sweep.go, cmd/mw/tidy.go | features/{sweep,tidy}.feature |
| Sync | application/sync.go | mw sync - cmd/mw/sync.go | features/sync.feature |
| Nudge | application/nudge.go | mw nudge - cmd/mw/nudge.go | none |
| Mail | application/mail.go | mw mail - cmd/mw/mail.go | features/mail.feature |
| Home | application/home.go | mw home - cmd/mw/home.go | features/home.feature |
| HomeMove | application/homemove.go | mw home move - cmd/mw/homemove.go | docs/home-move.md |
| Seat{Context,Up,Reap,Handover} | application/seat{context,up,reap,handover}.go | mw seat <sub> - cmd/mw/seat.go | features/seat_{context,up,reap}.feature |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk <sub> - cmd/mw/talk.go | features/talk_*.feature |
| Millhand, Deputy | application/millhand.go, application/deputy.go | mw millhand/deputy - cmd/mw/millhand.go, cmd/mw/deputy.go | features/{millhand,deputy}.feature |
| MillhandTick | application/millhandtick.go | mw millhand tick - cmd/mw/millhandtick.go | features/millhand_tick.feature |
| Secrets{Put,Get,List} | application/secrets.go | mw secrets - cmd/mw/secrets.go | features/secrets.feature |
| Watch | application/watch.go | mw watch - cmd/mw/watch.go | features/watch.feature |
| Doctor | application/doctor.go | mw doctor - cmd/mw/doctor.go | features/doctor.feature |
| SeatBoot | application/seatboot.go | none: Dispatch, Next | features/seat_boot.feature |
| Init | application/init.go | mw init - cmd/mw/init.go | features/init.feature |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern <sub> - cmd/mw/posternview.go, cmd/mw/posternbead.go | `features/postern_*.feature` |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring,Control,Trim} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go | mw events <sub> - cmd/mw/events.go | features/event_follow.feature |
| Hands{Add,List} | application/hands.go | mw hands <sub> - cmd/mw/hands.go; cmd/mw-hands-root | features/hands.feature |
| Postern{Serve,Nginx,Mirror} | application/posternhand.go, application/posternmirror.go | mw postern <sub> - cmd/mw/postern.go, cmd/mw/posternmirror.go | features/postern_serve.feature |
| Prompt{Save,List,Show,Run}, Cards | application/prompt.go | mw prompt/card - cmd/mw/prompt.go, cmd/mw/card.go | features/{prompt,card}.feature |
| Grist{Key,Grind,Send,Eval,Score,Runs,Stats} | application/grist.go, application/gristgrind.go, application/gristsend.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go | mw grist <sub> - cmd/mw/grist.go | features/{grist{,_send,_eval,_audio,_concurrent,_forward},scorer}.feature |

cmd/mw/root.go: the tree; cmd/mw/version.go.
