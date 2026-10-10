# Code map

## Layers

domain/(events): stdlib · application/: use cases, ports · infra/: adapters · cmd/mw/: cobra · features/: Gherkin · template/: embed

## Ports

| Port | Declared in | Real adapter | Fake |
|-|-|-|-|
| `WorkTracker` | application/worktracker.go | infra/beads | apptest.FakeTracker |
| TrackerNotes, SweepNotes, BeadGraph | application/{status,sweep}.go | same | same |
| VaultFiles, TrackerSync, SyncHaltMarker | application/sync.go | infra/{vault/git,beads/sync}.go, infra/synchalt | apptest.Fake{VaultFiles,Tracker,SyncHaltMarker} |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | infra/beads/mail.go | apptest.FakeMailbox |
| EpicRules | application/epicrules.go | infra/vault/epicrules.go | apptest.FakeEpicRules |
| Vault | application/seatboot.go | infra/vault | seatboot_test.go |
| Runner | application/runner.go | infra/tmux | apptest.FakeRunner |
| Harness | application/harness.go | infra/claude | seatboot_test.go |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | infra/{vault/{seat,reaplog},tmux/window,claude/claude}.go | apptest.Fake{Windows,Acting*} |
| Transcripts{,Tail,Replies}, PeekRemote | application/seatcontext.go, application/peek.go | infra/{claude,peekremote} | apptest.FakePeek |
| Reap{Terminal,Log,Armer} | application/seatreap.go | infra/{tmux/reap,vault/reaplog,reaper/arm}.go | apptest.Fake{Windows,ReapArmer} |
| WatchProbes | application/watch.go | infra/watch | apptest.FakeWatch |
| Doctor{Check,State,Log,Notes}, VPSProbe | application/doctor.go, application/vpsnginx.go | infra/{doctor,vpsnginx} | - |
| Network{Probe,Store,Reader} | application/network.go | infra/network | apptest.FakeNetwork* |
| CardLog | application/card.go | infra/cardlog | apptest.FakeCardLog |
| CloseOutMarks | application/closeout.go | infra/closeout | apptest.Fake*Mark* |
| Worktrees | application/worktrees.go | infra/rig/worktree.go | dispatch_test.go |
| HostLoad, Cloud*, BenchmarkBook | application/hostload.go, application/benchmark.go | infra/{hostload,cloud,vault/{cloudbook,benchmarks}.go} | apptest.Fake{HostLoad,Cloud*,Benchmarks} |
| TickLog, Notifier, HomeMoveHost, OldHome, VaultBirth, TrackerBirth | application/{millhandtick,homemove,init}.go | infra/{ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go | apptest.FakeTickLog |
| Landing, Checks, MergeSlot, Holding | application/landing.go | infra/rig/{landing,checks,slot}.go | - |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter, FormulaInstaller | application/afterlanding.go, application/selfupdate.go, application/backendstage.go, application/formulainstall.go | infra/{rig/{afterlanding,built,backend},userunits,vault/formulas}.go | features/{self_update,backend_swap}.feature |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | infra/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | apptest.Fake{EventLog,FollowCursors,Ship*,Tracker,Subscribe*} |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | infra/{postern,hands*,homemove} | apptest.Fake{Postern*,Cipher,Hands*,HomeMover,Prompts} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | infra/{bsv,stampqueue,chainlookup}, rig/worktree.go | apptest.Fake{Chain,StampQueue,Commit*,RigHeads} |
| Grinder, GrindSource, GristState, GristLock, GristRunStore, GristForwardStore, GristSmoke* | application/grist.go, application/gristruns.go, application/gristsmoke.go | infra/{claude/grind,rig/grinds,hostlock/try}.go, infra/grist | apptest.Fake{Grinder,Grinds,Grist*} |
| Scorer | application/scorer.go | infra/scorer | apptest.FakeScorer |
| SecretStore | application/secrets.go | infra/sops | apptest.FakeSecretStore |

## Use cases

| Use case | File | Command | Feature |
|-|-|-|-|
| File, Release, Retry | application/file.go, application/release.go, application/retry.go | mw file/release/retry - cmd/mw/file.go, cmd/mw/release.go, cmd/mw/retry.go | features/{file_plan,release,retry}.feature |
| Show, Peek, Prove, StampHead | application/show.go, application/stamphead.go | mw show/peek/prove/stamp - cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go | features/{show,peek,prove,stamp}.feature |
| Dispatch, Next, AfterLandingRun | application/dispatch.go, application/next.go, application/afterlandingrun.go | mw dispatch/next/after-landing - cmd/mw/dispatch.go, cmd/mw/next.go, cmd/mw/afterlanding.go | features/{dispatch,next,after_landing}.feature |
| Tester | application/testerreport.go | mw tester report - cmd/mw/tester.go | features/tester.feature |
| Check, Status, CloudCheck, Brief | application/check.go, application/status.go, application/cloud.go, application/brief.go | mw check/status/cloud/brief - cmd/mw/check.go, cmd/mw/status.go, cmd/mw/cloud.go, cmd/mw/brief.go | features/{check,status,cloud,brief}.feature |
| Sweep, Tidy, Sync, Nudge, Mail | application/sweep.go, application/tidy.go, application/sync.go, application/nudge.go, application/mail.go | mw sweep/tidy/sync/nudge/mail - cmd/mw/sweep.go, cmd/mw/tidy.go, cmd/mw/sync.go, cmd/mw/nudge.go, cmd/mw/mail.go | features/{sweep,tidy,sync,mail}.feature |
| Home, HomeMove | application/home.go, application/homemove.go | mw home [move] - cmd/mw/home.go, cmd/mw/homemove.go | features/home.feature |
| Seat{Context,Up,Reap,Handover} | application/seat{context,up,reap,handover}.go | mw seat <sub> - cmd/mw/seat.go | features/seat_*.feature |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk <sub> - cmd/mw/talk.go | features/talk_*.feature |
| Millhand, Deputy, MillhandTick | application/millhand.go, application/deputy.go, application/millhandtick.go | mw millhand [tick]/deputy - cmd/mw/millhand.go, cmd/mw/deputy.go, cmd/mw/millhandtick.go | features/{millhand,deputy,millhand_tick}.feature |
| Secrets{Put,Get,List}, Hands{Add,List}, Ask, Watch, Doctor | application/secrets.go, application/hands.go, application/ask.go, application/watch.go, application/doctor.go | mw secrets/hands/ask/watch/doctor - cmd/mw/secrets.go, cmd/mw/hands.go, cmd/mw/ask.go, cmd/mw/watch.go, cmd/mw/doctor.go | features/{secrets,hands,ask,watch,doctor}.feature |
| SeatBoot, Memory, Init | application/seatboot.go, application/rigfacts.go, application/memory.go, application/memorymigrate.go, application/init.go | mw init/memory query - cmd/mw/init.go, cmd/mw/memory.go | features/{seat_boot,init,memory}.feature |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern <sub> - cmd/mw/posternview.go, cmd/mw/posternbead.go | features/postern_*.feature |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring,Control,Trim} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go | mw events <sub> - cmd/mw/events.go | features/event_follow.feature |
| Postern{Serve,Nginx,Mirror} | application/posternhand.go, application/posternmirror.go | mw postern <sub> - cmd/mw/postern.go, cmd/mw/posternmirror.go | features/postern_serve.feature |
| Prompt{Save,List,Show,Run}, Cards | application/prompt.go | mw prompt/card - cmd/mw/prompt.go, cmd/mw/card.go | features/{prompt,card}.feature |
| Grist{Key,Grind,Send,Eval,Score,Runs,Stats,Smoke,Level} | application/grist.go, application/gristlevel.go, application/gristgrind.go, application/gristsend.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go, application/gristroom.go, application/gristsmoke.go | mw grist <sub> - cmd/mw/grist.go, cmd/mw/gristsmoke.go | features/{grist*,scorer}.feature |

cmd/mw/root.go, cmd/mw/version.go

changelog_files: infra/rig/version.go writes domain/changelog.go + rig files.
