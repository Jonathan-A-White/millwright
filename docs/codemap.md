# Code map

## Layers

domain/ stdlib · application/ use cases, ports · infra/ adapters · cmd/mw/ cobra · features/ Gherkin · template/ embed

## Ports

| Port | Declared in | Real adapter | Fake |
|-|-|-|-|
| `WorkTracker` | application/worktracker.go | infra/beads | FakeTracker |
| TrackerNotes, SweepNotes, BeadGraph, IceNotes, IcedBeads | application/{status,sweep,ice}.go | same | same |
| VaultFiles, TrackerSync, SyncHaltMarker | application/sync.go | infra/{vault/git,beads/sync}.go, infra/synchalt | Fake{VaultFiles,Tracker,SyncHaltMarker} |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | infra/beads/mail.go | FakeMailbox |
| EpicRules (+LandingRules) | application/epicrules.go | infra/vault/epicrules.go | FakeEpicRules |
| Vault, Harness, SeatBootFiles, RigDocs, BootCommands | application/seatboot.go, application/seatboot_manifest.go, application/harness.go | infra/{vault,claude,shellcmd}, rig/rigfiles.go | seatboot{,_manifest}_test.go |
| Runner, BdProgram | application/runner.go, application/bdretry.go | infra/{tmux,bdprogram} | FakeRunner, FakeBd |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | infra/{vault/{seat,reaplog},tmux/window,claude/claude}.go | Fake{Windows,Acting*} |
| Transcripts{,Tail,Replies,Titled,Turns}, PeekRemote | application/seatcontext.go, application/peek.go | infra/{claude,peekremote} | FakePeek |
| Reap{Terminal,Log,Armer} | application/seatreap.go | infra/{tmux/reap,vault/reaplog,reaper/arm}.go | Fake{Windows,Reap*} |
| WatchProbes | application/watch.go | infra/watch | FakeWatch |
| Doctor{Check,State,Log,Notes}, VPSProbe | application/doctor.go, application/vpsnginx.go | infra/{doctor,vpsnginx} | - |
| Network{Probe,Store,Reader} | application/network.go | infra/network | FakeNetwork* |
| CardLog, ReportSentLog | application/card.go, application/reportsend.go | infra/{cardlog,reportlog} | Fake{CardLog,ReportSentLog} |
| CloseOutMarks, LiveHeartbeats, UserProcesses (Tester) | application/closeout.go, application/liveheartbeats.go, application/testerleftovers.go | infra/{closeout,procs} | Fake*Mark*, FakeHeartbeats, FakeUserProcesses |
| Worktrees, Rebases, BranchChanges, RigFiles | application/worktrees.go, application/wrongrig.go | infra/rig/{worktree,rigfiles}.go | - |
| HostLoad, GateLock, Cloud*, BenchmarkBook | application/hostload.go, application/gate.go, application/benchmark.go | infra/{hostload,hostlock/gate.go,cloud,vault/{cloudbook,benchmarks}.go} | Fake{HostLoad,Cloud*,Benchmarks}; features/next_gate_lock.feature |
| TickLog, Notifier, HomeMoveHost, OldHome, VaultBirth, TrackerBirth | application/{millhandtick,homemove,init}.go | infra/{ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go | FakeTickLog |
| Landing, Checks, SpecChecks, MergeSlot, Batch{Records,Metrics}, LandProcesses | application/landing.go, application/batchland.go | infra/rig/{landing,checks,slot}.go, vault/batch.go, apartproc | FakeLand* |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter, FormulaInstaller | application/afterlanding.go, application/selfupdate.go, application/backendstage.go, application/formulainstall.go | infra/{rig/{afterlanding,built,backend},userunits,vault/formulas}.go | features/{self_update,backend_swap}.feature |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | infra/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | Fake{EventLog,Follow*,Ship*,Tracker,Subscribe*} |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | infra/{postern,hands*,homemove} | Fake{Postern*,Cipher,Hands*,HomeMover,Prompts} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | infra/{bsv,stampqueue,chainlookup}, rig/worktree.go | Fake{Chain,Stamp*,Commit*,RigHeads} |
| Grinder, WarmGrinder, GristPoolBoard, GrindSource, GristState, GristLock, GristRunStore, GristForwardStore, GristRequestStore, GristLandings, GristSmoke*, VisionSource | application/grist.go, application/gristpool.go, application/vision.go, application/gristruns.go, application/griststatus.go, application/gristsmoke.go | infra/{claude/grind,rig/{grinds,landingof},hostlock/try}.go, infra/grist | Fake{Grind*,WarmGrinder,Grist*,Landings,Visions} |
| IdeasOnlyReader, IdeasOnlyFiles | application/ideasonly.go | infra/{postern/http.go,ideasonly} | FakePostern |
| SuiteTrees, SuiteRunner, SuiteRecords, SuiteHistory, SuiteProcesses | application/suite.go, application/suitenightly.go | infra/{rig/{worktree,checks}.go,vault/vault.go,apartproc} | FakeSuite* |
| Scorer, SecretStore | application/scorer.go, application/secrets.go | infra/{scorer,sops} | Fake{Scorer,SecretStore} |

## Use cases

| Use case | File | Command | Feature |
|-|-|-|-|
| File, Release, Ice, Thaw, Retry, Cause, Note, Path{Set,Show} | application/file.go, application/release.go, application/ice.go, application/retry.go, application/cause.go, application/note.go, application/pathset.go | cmd/mw/file.go, cmd/mw/release.go, cmd/mw/ice.go, cmd/mw/retry.go, cmd/mw/cause.go, cmd/mw/note.go, cmd/mw/path.go | features/{file*,release,retry*,cause,note,path_set}.feature |
| Show, Peek, Prove, StampHead, Tester[File], FactoryReport[Send], Invest[Book] | application/show.go, application/stamphead.go, application/testerreport.go, application/testerfile.go, application/factoryreport.go, application/reportsend.go, application/invest.go | cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go, cmd/mw/tester.go, cmd/mw/report.go, cmd/mw/reportsend.go, cmd/mw/invest.go | features/{show,peek,prove,stamp,tester,report*,invest}.feature |
| Dispatch, Next, BatchLand, AfterLandingRun, BdRetry | application/dispatch.go, application/next.go, application/afterlandingrun.go | cmd/mw/dispatch.go, cmd/mw/next.go, cmd/mw/land.go, cmd/mw/afterlanding.go, cmd/mw/bd.go | features/{dispatch,next,batch_land,after_*,bd_*}.feature |
| Check{,Background,Logs,Starter}, Status, CloudCheck, CloudMake, Brief | application/check.go, application/status.go, application/cloud.go, application/cloudmake.go, application/brief.go | cmd/mw/check.go, cmd/mw/status.go, cmd/mw/cloud.go, cmd/mw/brief.go | features/{check,status,cloud,brief}.feature |
| Sweep, Tidy, Sync, Nudge, Mail, Home[Move] | application/sweep.go, application/tidy.go, application/nudge.go, application/mail.go, application/home.go, application/homemove.go | cmd/mw/sweep.go, cmd/mw/tidy.go, cmd/mw/sync.go, cmd/mw/nudge.go, cmd/mw/mail.go, cmd/mw/home.go, cmd/mw/homemove.go | features/{sweep,tidy,sync,mail,home}.feature |
| Suite, SuiteFiler, NightlySuite, ContributeBack | application/suitefile.go, application/suitestatus.go, application/contributeback.go | cmd/mw/suite.go, cmd/mw/suitenightly.go | features/{suite*,contribute_back}.feature |
| Config{Set,Get,File,ExportBoost} | application/configset.go | cmd/mw/config.go | features/config_*.feature |
| IdeasOnly{Show,Mode,Release}, his records | application/ideasonly.go, application/ideasonlyrelease.go, application/posternideas.go, application/talkwaitideas.go | mw ideas-only - cmd/mw/ideasonly.go | features/ideas_only.feature, postern_inbox_apply, talk_wait |
| Seat{Context,Up,Reap,Handover} | application/seat{context,up,reap,handover}.go | mw seat <sub> - cmd/mw/seat.go | features/seat_*.feature |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk <sub> - cmd/mw/talk.go | features/talk_*.feature |
| Millhand, Deputy, MillhandTick | application/millhand.go, application/deputy.go, application/millhandtick.go | cmd/mw/millhand.go, cmd/mw/deputy.go, cmd/mw/millhandtick.go | features/{millhand*,deputy}.feature |
| Secrets{Put,Get,List}, Hands{Add,List}, Ask, Watch, Doctor | application/secrets.go, application/hands.go, application/ask.go, application/watch.go, application/doctor.go | cmd/mw/secrets.go, cmd/mw/hands.go, cmd/mw/ask.go, cmd/mw/watch.go, cmd/mw/doctor.go | features/{secrets,hands,ask,watch,doctor}.feature |
| SeatBoot{,Set}, SeatRead, Memory[Log], Errors, Init | application/seatboot.go, application/memory.go, application/memorymigrate.go, application/errors.go, application/init.go | cmd/mw/init.go, cmd/mw/memory.go, cmd/mw/errors.go, infra/memoryqueries | features/{seat_boot,init,memory,errors}.feature |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern <sub> - cmd/mw/posternview.go, cmd/mw/posternbead.go | features/postern_*.feature |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring,Control,Trim} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go | mw events <sub> - cmd/mw/events.go | features/event_follow.feature |
| Postern{Serve,Nginx,Mirror} | application/posternhand.go, application/posternmirror.go | mw postern <sub> - cmd/mw/postern.go, cmd/mw/posternmirror.go | features/postern_serve.feature |
| Prompt*, Cards, Todo* | application/prompt.go, application/tododue.go, application/todosize.go, application/todorank.go | cmd/mw/prompt.go, cmd/mw/card.go, cmd/mw/todo.go | features/{prompt,card,todo*}.feature |
| Grist* | application/grist.go, application/gristtiming.go, application/gristlevel.go, application/gristgrind.go, application/gristsend.go, application/gristrequest.go, application/griststatus.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go, application/gristroom.go, application/gristsmoke.go | mw grist <sub> - cmd/mw/grist.go, cmd/mw/gristsmoke.go | features/{grist*,scorer}.feature |
| Triage{Dump,Diff,Orphans} | application/triage.go | infra/beads/triage.go, cmd/mw/triage.go | - |

cmd/mw/root.go, cmd/mw/version.go

changelog_files: infra/rig/version.go > domain/changelog.go; union_merge_files: rig/landing.go

scripts/check-codemap.sh
