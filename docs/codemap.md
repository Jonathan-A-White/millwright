# Code map

## Layers

domain/(events) stdlib · application/ use cases, ports · infra/ adapters · cmd/mw/ cobra · features/ Gherkin · template/ embed

## Ports

| Port | Declared in | Real adapter | Fake (apptest.) |
|-|-|-|-|
| `WorkTracker` | application/worktracker.go | infra/beads | FakeTracker |
| TrackerNotes, SweepNotes, BeadGraph | application/{status,sweep}.go | same | same |
| VaultFiles, TrackerSync, SyncHaltMarker | application/sync.go | infra/{vault/git,beads/sync}.go, infra/synchalt | Fake{VaultFiles,Tracker,SyncHaltMarker} |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | infra/beads/mail.go | FakeMailbox |
| EpicRules (+LandingRules: lenient read) | application/epicrules.go | infra/vault/epicrules.go | FakeEpicRules |
| Vault | application/seatboot.go | infra/vault | seatboot_test.go |
| Runner, BdProgram | application/runner.go, application/bdretry.go | infra/{tmux,bdprogram} | FakeRunner, FakeBd |
| Harness | application/harness.go | infra/claude | seatboot_test.go |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | infra/{vault/{seat,reaplog},tmux/window,claude/claude}.go | Fake{Windows,Acting*} |
| Transcripts{,Tail,Replies}, PeekRemote | application/seatcontext.go, application/peek.go | infra/{claude,peekremote} | FakePeek |
| Reap{Terminal,Log,Armer} | application/seatreap.go | infra/{tmux/reap,vault/reaplog,reaper/arm}.go | Fake{Windows,ReapArmer} |
| WatchProbes | application/watch.go | infra/watch | FakeWatch |
| Doctor{Check,State,Log,Notes}, VPSProbe | application/doctor.go, application/vpsnginx.go | infra/{doctor,vpsnginx} | - |
| Network{Probe,Store,Reader} | application/network.go | infra/network | FakeNetwork* |
| CardLog, ReportSentLog | application/card.go, application/reportsend.go | infra/{cardlog,reportlog} | Fake{CardLog,ReportSentLog} |
| CloseOutMarks, LiveHeartbeats | application/closeout.go, application/liveheartbeats.go | infra/{closeout,procs} | Fake*Mark*, FakeHeartbeats |
| Worktrees, BranchChanges | application/worktrees.go | infra/rig/worktree.go | dispatch_test.go |
| HostLoad, GateLock, Cloud*, BenchmarkBook | application/hostload.go, application/gate.go, application/benchmark.go | infra/{hostload,hostlock/gate.go,cloud,vault/{cloudbook,benchmarks}.go} | Fake{HostLoad,Cloud*,Benchmarks}; features/next_gate_lock.feature |
| TickLog, Notifier, HomeMoveHost, OldHome, VaultBirth, TrackerBirth | application/{millhandtick,homemove,init}.go | infra/{ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go | FakeTickLog |
| Landing, Checks, SpecChecks, MergeSlot, Holding | application/landing.go | infra/rig/{landing,checks,slot}.go | - |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter, FormulaInstaller | application/afterlanding.go, application/selfupdate.go, application/backendstage.go, application/formulainstall.go | infra/{rig/{afterlanding,built,backend},userunits,vault/formulas}.go | features/{self_update,backend_swap}.feature |
| EventLog, BeadFeed, FollowCursors, ShipStates, SubscribeFiles, NudgeCursors, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | infra/{eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go | Fake{EventLog,Follow*,Ship*,Tracker,Subscribe*} |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | infra/{postern,hands*,homemove} | Fake{Postern*,Cipher,Hands*,HomeMover,Prompts} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | infra/{bsv,stampqueue,chainlookup}, rig/worktree.go | Fake{Chain,Stamp*,Commit*,RigHeads} |
| Grinder, WarmGrinder, GristPoolBoard, GrindSource, GristState, GristLock, GristRunStore, GristForwardStore, GristRequestStore, GristLandings, GristSmoke*, VisionSource | application/grist.go, application/gristpool.go, application/vision.go, application/gristruns.go, application/griststatus.go, application/gristsmoke.go | infra/{claude/grind,rig/{grinds,landingof},hostlock/try}.go, infra/grist | Fake{Grinder,WarmGrinder,Grinds,Grist*,Landings,Visions} |
| IdeasOnlyReader, IdeasOnlyFiles | application/ideasonly.go | infra/{postern/http.go,ideasonly} | FakePostern; features/ideas_only.feature (real files) |
| ConfigFile | application/configset.go | infra/configfile | features/config_set.feature (real file) |
| SuiteTrees, SuiteRunner, SuiteRecords, SuiteHistory, SuiteProcesses | application/suite.go, application/suitenightly.go | infra/{rig/{worktree,checks}.go,vault/vault.go,suiteproc} | FakeSuite{Trees,Runner,Records,Processes} |
| Scorer | application/scorer.go | infra/scorer | FakeScorer |
| SecretStore | application/secrets.go | infra/sops | FakeSecretStore |

## Use cases

| Use case | File | Command | Feature |
|-|-|-|-|
| File, Release, Retry, Cause, Path{Set,Show} | application/file.go, application/release.go, application/retry.go, application/cause.go, application/pathset.go | cmd/mw/file.go, cmd/mw/release.go, cmd/mw/retry.go, cmd/mw/cause.go, cmd/mw/path.go | features/{file*,release,retry,cause,path_set}.feature |
| Show, Peek, Prove, StampHead, Tester[File], FactoryReport[Send] | application/show.go, application/stamphead.go, application/testerreport.go, application/testerfile.go, application/factoryreport.go, application/reportsend.go | mw show/peek/prove/stamp/tester[ file]/report[ send] - cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go, cmd/mw/tester.go, cmd/mw/report.go, cmd/mw/reportsend.go | features/{show,peek,prove,stamp,tester,report*}.feature |
| Dispatch, Next, AfterLandingRun, BdRetry (mw bd) | application/dispatch.go, application/next.go, application/afterlandingrun.go, application/bdretry.go | mw dispatch/next/after-landing/bd - cmd/mw/dispatch.go, cmd/mw/next.go, cmd/mw/afterlanding.go, cmd/mw/bd.go | features/{dispatch,next,after_*,bd_*}.feature |
| Check{,Background,Logs,Starter}, Status, CloudCheck, Brief | application/check.go, application/status.go, application/cloud.go, application/brief.go | mw check/status/cloud/brief - cmd/mw/check.go, cmd/mw/status.go, cmd/mw/cloud.go, cmd/mw/brief.go | features/{check,status,cloud,brief}.feature |
| Sweep, Tidy, Sync, Nudge, Mail, Home[Move] | application/sweep.go, application/tidy.go, application/sync.go, application/nudge.go, application/mail.go, application/home.go, application/homemove.go | mw sweep/tidy/sync/nudge/mail/home - cmd/mw/sweep.go, cmd/mw/tidy.go, cmd/mw/sync.go, cmd/mw/nudge.go, cmd/mw/mail.go, cmd/mw/home.go, cmd/mw/homemove.go | features/{sweep,tidy,sync,mail,home}.feature |
| Suite, SuiteFiler, NightlySuite, ContributeBack | application/suite.go, application/suitefile.go, application/suitenightly.go, application/suitestatus.go, application/contributeback.go | mw suite - cmd/mw/suite.go, cmd/mw/suitenightly.go | features/{suite*,contribute_back}.feature |
| ConfigSet, ConfigGet | application/configset.go | mw config set/get - cmd/mw/config.go | features/config_set.feature |
| IdeasOnly{Show,Mode,Release}, the keeping and giving back of his records | application/ideasonly.go, application/ideasonlyrelease.go, application/posternideas.go, application/talkwaitideas.go | mw ideas-only - cmd/mw/ideasonly.go | features/ideas_only.feature, postern_inbox_apply, talk_wait |
| Seat{Context,Up,Reap,Handover} | application/seat{context,up,reap,handover}.go | mw seat <sub> - cmd/mw/seat.go | features/seat_*.feature |
| Talk{Call,Model,Say,Wait} | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | mw talk <sub> - cmd/mw/talk.go | features/talk_*.feature |
| Millhand, Deputy, MillhandTick | application/millhand.go, application/deputy.go, application/millhandtick.go | mw millhand [tick]/deputy - cmd/mw/millhand.go, cmd/mw/deputy.go, cmd/mw/millhandtick.go | features/{millhand*,deputy}.feature |
| Secrets{Put,Get,List}, Hands{Add,List}, Ask, Watch, Doctor | application/secrets.go, application/hands.go, application/ask.go, application/watch.go, application/doctor.go | mw secrets/hands/ask/watch/doctor - cmd/mw/secrets.go, cmd/mw/hands.go, cmd/mw/ask.go, cmd/mw/watch.go, cmd/mw/doctor.go | features/{secrets,hands,ask,watch,doctor}.feature |
| SeatBoot, Memory, Init | application/seatboot.go, application/rigfacts.go, application/memory.go, application/memorymigrate.go, application/init.go | mw init/memory query/eval/demote/promote boot: no - cmd/mw/init.go, cmd/mw/memory.go | features/{seat_boot,init,memory}.feature |
| Postern{Key*,Inbox,Send,Snapshot,View,Bead} | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go | mw postern <sub> - cmd/mw/posternview.go, cmd/mw/posternbead.go | features/postern_*.feature |
| Event{Follow,Emit,Tail,Ship,Wait,Nudge,Spring,Control,Trim} | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go | mw events <sub> - cmd/mw/events.go | features/event_follow.feature |
| Postern{Serve,Nginx,Mirror} | application/posternhand.go, application/posternmirror.go | mw postern <sub> - cmd/mw/postern.go, cmd/mw/posternmirror.go | features/postern_serve.feature |
| Prompt{Save,List,Show,Run}, Cards, TodoDue/TodoReminder | application/prompt.go, application/tododue.go | mw prompt/card/todo - cmd/mw/prompt.go, cmd/mw/card.go, cmd/mw/todo.go | features/{prompt,card,todo_*}.feature |
| Grist{Key,Grind,Send,Request,Status,Eval,Score,Runs,Stats,Timing,Smoke,Level} | application/grist.go, application/gristtiming.go, application/gristlevel.go, application/gristgrind.go, application/gristsend.go, application/gristrequest.go, application/griststatus.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go, application/gristroom.go, application/gristsmoke.go | mw grist <sub> - cmd/mw/grist.go, cmd/mw/gristsmoke.go | features/{grist*,scorer}.feature |

cmd/mw/root.go, cmd/mw/version.go

changelog_files: infra/rig/version.go > domain/changelog.go

gate check: scripts/check-codemap.sh
