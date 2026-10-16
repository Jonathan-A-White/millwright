# Code map

## Ports

| Port | Declared in (application/) | Real adapter (infra/) |
|-|-|-|
| `WorkTracker` | application/worktracker.go | beads |
| TrackerNotes, SweepNotes, BeadGraph, IceNotes, IcedBeads | application/{status,sweep,ice}.go | same |
| VaultFiles, TrackerSync, TrackerHistory, TrackerFlush, SyncHaltMarker | application/sync.go | {vault/git,beads/sync}.go, synchalt, flushlog |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | beads/mail.go |
| EpicRules (+LandingRules) | application/epicrules.go | vault/epicrules.go |
| Vault, Harness, SeatBootFiles, RigDocs, BootCommands | seatboot.go, application/seatboot_manifest.go, application/harness.go | {vault,claude,shellcmd}, rig/rigfiles.go |
| Runner, BdProgram | application/runner.go, application/bdretry.go | {tmux,bdprogram} |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | {vault/{seat,reaplog},tmux/window,claude/claude}.go |
| Transcripts{,Tail,Replies,Titled,Turns}, TranscriptWrites, HostSigns, PeekRemote | application/seatcontext.go, application/peek.go, nudge.go | {claude,peekremote,vault/git.go} |
| Reap{Terminal,Log,Armer} | application/seatreap.go | {tmux/reap,vault/reaplog,reaper/arm}.go |
| WatchProbes | watch.go | watch |
| Doctor*, VPSProbe | doctor.go, application/vpsnginx.go | {doctor,vpsnginx} |
| Network{Probe,Store,Reader} | application/network.go | network |
| CardLog, ReportSentLog | application/card.go, reportsend.go | {cardlog,reportlog} |
| CloseOutMarks, LiveHeartbeats, UserProcesses (Tester) | application/closeout.go, application/liveheartbeats.go, application/testerleftovers.go | {closeout,procs} |
| Worktrees, Rebases, BranchChanges, RigFiles | application/worktrees.go, application/wrongrig.go | rig/{worktree,rigfiles}.go |
| HostLoad, GateLock, Cloud*, BenchmarkBook | application/hostload.go, application/gate.go, application/benchmark.go | {hostload,hostlock/gate.go,cloud,vault/{cloudbook,benchmarks}.go} |
| TickLog, Notifier, HomeMoveHost, OldHome, {Vault,Tracker}Birth | application/{millhandtick,homemove,init}.go | {ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go |
| Landing, Checks, SpecChecks, MergeSlot, Batch*, LandProcesses | application/landing.go, application/batchland.go | rig/{landing,checks,slot}.go, vault/batch.go, apartproc |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter, FormulaInstaller | application/afterlanding.go, application/selfupdate.go, application/backendstage.go, application/formulainstall.go | {rig/{afterlanding,built,backend},userunits,vault/formulas}.go |
| EventLog, BeadFeed, {Follow,Nudge}Cursors, ShipStates, SubscribeFiles, EventSpringer, EventController, HarnessCount | application/event*.go, status.go | {eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | {postern,hands*,homemove} |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | {bsv,stampqueue,chainlookup}, rig/worktree.go |
| Grinder, WarmGrinder, GristPoolBoard, GrindSource, Grist{State,Lock,RunStore,ForwardStore,RequestStore,Landings}, GristSmoke*, VisionSource | grist.go, application/gristpool.go, application/vision.go, application/gristruns.go, griststatus.go, gristsmoke.go | {claude/grind,rig/{grinds,landingof},hostlock/try}.go, grist |
| IdeasOnlyReader, IdeasOnlyFiles | ideasonly.go | {postern/http.go,ideasonly} |
| Suite{Trees,Runner,Records,History,Processes} | application/suite.go, application/suitenightly.go | {rig/{worktree,checks}.go,vault/vault.go,apartproc} |
| Scorer, SecretStore | application/scorer.go, secrets.go | {scorer,sops} |

Fakes: apptest/fake*.go, Fake<Port>; seatboot ports: seatboot*_test.go; gate lock, self-update, backend swap: same-named features.

## Use cases

| Use case | File | Command | Feature (features/*.feature) |
|-|-|-|-|
| File, Release, Ice, Thaw, Retry, Cause, Note, Path* | application/file.go, application/release.go, application/ice.go, application/retry.go, application/cause.go, application/note.go, application/pathset.go | cmd/mw/file.go, cmd/mw/release.go, cmd/mw/ice.go, cmd/mw/retry.go, cmd/mw/cause.go, cmd/mw/note.go, cmd/mw/path.go | {file*,release,retry*,cause,note,path_set} |
| Show, Peek, Prove, StampHead, Tester[File], FactoryReport[Send], Invest[Book] | application/show.go, application/stamphead.go, application/testerreport.go, application/testerfile.go, application/factoryreport.go, application/reportsend.go, application/invest.go | cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go, cmd/mw/tester.go, cmd/mw/report.go, cmd/mw/reportsend.go, cmd/mw/invest.go | {show,peek,prove,stamp,tester,report*,invest} |
| Dispatch, Next, BatchLand, AfterLandingRun, BdRetry | application/dispatch.go, application/next.go, application/afterlandingrun.go | cmd/mw/dispatch.go, cmd/mw/next.go, cmd/mw/land.go, cmd/mw/afterlanding.go, cmd/mw/bd.go | {dispatch,next,batch_land,after_*,bd_*} |
| Check*, Status, CloudCheck, CloudMake, Brief | application/check.go, application/status.go, application/cloud.go, application/cloudmake.go, application/brief.go | cmd/mw/check.go, cmd/mw/status.go, cmd/mw/cloud.go, cmd/mw/brief.go | {check,status,cloud,brief} |
| Sweep, Tidy, Sync, Nudge, Mail, Home[Move] | application/sweep.go, application/tidy.go, application/nudge.go, application/mail.go, application/home.go, application/homemove.go | cmd/mw/sweep.go, cmd/mw/tidy.go, cmd/mw/sync.go, cmd/mw/nudge.go, cmd/mw/mail.go, cmd/mw/home.go, cmd/mw/homemove.go | {sweep,tidy,sync,mail,home} |
| Suite, SuiteFiler, NightlySuite, ContributeBack | application/suite{file,status}.go, application/contributeback.go | cmd/mw/suite.go, cmd/mw/suitenightly.go | {suite*,contribute_back} |
| Config* | application/configset.go | cmd/mw/config.go | config_* |
| IdeasOnly* | application/ideasonly.go, application/ideasonlyrelease.go | cmd/mw/ideasonly.go | ideas_only, postern_inbox_apply, talk_wait |
| Seat* | application/seat{context,up,reap,handover}.go | cmd/mw/seat.go | seat_* |
| Talk* | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go | cmd/mw/talk.go | talk_* |
| Millhand, Deputy, MillhandTick | application/millhand.go, application/deputy.go, application/millhandtick.go | cmd/mw/millhand.go, cmd/mw/deputy.go, cmd/mw/millhandtick.go | {millhand*,deputy} |
| Secrets*, Hands*, Ask, Watch, Doctor | application/secrets.go, application/hands.go, application/ask.go, application/watch.go, application/doctor.go | cmd/mw/secrets.go, cmd/mw/hands.go, cmd/mw/ask.go, cmd/mw/watch.go, cmd/mw/doctor.go | {secrets,hands,ask,watch,doctor} |
| SeatBoot{,Set}, SeatRead, Memory[Log], Errors, Init | application/seatboot.go, application/memory.go, application/memorymigrate.go, application/errors.go, application/init.go | cmd/mw/init.go, cmd/mw/memory.go, cmd/mw/errors.go, infra/memoryqueries | {seat_boot,init,memory,errors} |
| Postern{Inbox,View,Bead,Serve,Mirror}, Digest | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go, application/posternhand.go, application/posternmirror.go, application/digest.go | cmd/mw/postern.go, cmd/mw/posternview.go, cmd/mw/posternbead.go, cmd/mw/posternmirror.go, cmd/mw/digest.go | postern_* |
| Event* | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go | cmd/mw/events.go | event_follow |
| Prompt*, Cards, Todo* | application/prompt.go, application/tododue.go, application/todosize.go, application/todorank.go | cmd/mw/prompt.go, cmd/mw/card.go, cmd/mw/todo.go | {prompt,card,todo*} |
| Grist* | application/grist.go, application/gristtiming.go, application/gristlevel.go, application/gristgrind.go, application/gristsend.go, application/gristrequest.go, application/griststatus.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go, application/gristroom.go, application/gristsmoke.go | cmd/mw/grist.go, cmd/mw/gristsmoke.go | {grist*,scorer} |
| Triage*, Queue, Sleep | application/triage.go, application/queue.go, application/sleep.go, application/sleepgather.go, application/fuelkinds.go | infra/beads/triage.go, cmd/mw/triage.go, cmd/mw/queue.go, cmd/mw/sleep.go | - |

cmd/mw/root.go, cmd/mw/version.go

changelog_files: infra/rig/version.go > domain/changelog.go; union_merge_files: rig/landing.go

scripts/check-codemap.sh
