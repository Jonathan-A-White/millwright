# Code map

## Ports

| Port | In application/ | Adapter (infra/) |
|-|-|-|
| `WorkTracker` | application/worktracker.go | beads |
| TrackerNotes, SweepNotes, BeadGraph, IceNotes, IcedBeads (WithRunCache: one bead and kv list a read-only run) | application/{status,sweep,ice}.go | same, beads/runcache.go |
| VaultFiles, Tracker{Sync,History,Flush}, SyncHaltMarker | application/sync.go | {vault/git,beads/sync}.go, synchalt, flushlog |
| Mailbox, TidyMailbox | application/{mail,tidy}.go | beads/mail.go |
| EpicRules (+LandingRules) | application/epicrules.go | vault/epicrules.go |
| Vault, Harness, SeatBootFiles, RigDocs, BootCommands | seatboot.go, application/seatboot_manifest.go, application/harness.go | {vault,claude,shellcmd}, rig/rigfiles.go |
| Runner, BdProgram | application/runner.go, application/bdretry.go | {tmux,bdprogram} |
| SeatFiles, Windows, SeatHarness, ActingFile | application/seatup.go, application/seathandover.go | {vault/{seat,reaplog},tmux/window,claude/claude}.go |
| Transcript*, HostSigns, PeekRemote | application/seatcontext.go, application/peek.go, nudge.go | {claude,peekremote,vault/git.go} |
| Reap{Terminal,Log,Armer} | application/seatreap.go | {tmux/reap,vault/reaplog,reaper/arm}.go |
| WatchProbes | watch.go | watch |
| Doctor*, VPSProbe, RecheckSkipCheck | doctor.go, application/vpsnginx.go, application/recheckskips.go | {doctor,vpsnginx} |
| Network* | application/network.go | network |
| CardLog, ReportSentLog | application/card.go, reportsend.go | {cardlog,reportlog} |
| CloseOutMarks, LiveHeartbeats, UserProcesses (Tester) | application/closeout.go, application/liveheartbeats.go, application/testerleftovers.go | {closeout,procs,benchflag} |
| Worktrees, Rebases, BranchChanges, RigFiles | application/worktrees.go, application/wrongrig.go | rig/{worktree,rigfiles}.go |
| HostLoad, GateLock, Cloud*, BenchmarkBook | application/hostload.go, application/gate.go, application/benchmark.go | {hostload,hostlock/gate.go,ticketq,cloud,vault/{cloudbook,benchmarks}.go} |
| MemberHost | application/member.go | member |
| TickLog, Notifier, HomeMoveHost, OldHome, {Vault,Tracker}Birth | application/{millhandtick,homemove,init}.go | {ticklog,notify/notify,homemove/homemove,vault/birth,beads/init}.go |
| Landing, Checks, SpecChecks, RelatedChecks, NamedChecks (main's red), MergeSlot, Batch*, LandProcesses | application/landing.go, application/mainred.go, application/batchland.go | rig/{landing,checks,specs,slot}.go (specs, related leave out timing_tests), vault/batch.go, apartproc |
| AfterLanding, SelfUpdate, BuiltMarks, BackendBuilds, UnitRestarter, FormulaInstaller | application/afterlanding.go, application/selfupdate.go, application/backendstage.go, application/formulainstall.go | {rig/{afterlanding,built,backend},userunits,vault/formulas}.go |
| EventLog (EventNewestLog, NewestOf), BeadFeed, {Follow,Nudge}Cursors, ShipStates, SubscribeFiles, EventSpringer, EventController, HarnessCount | application/event*.go, application/eventnewest.go, status.go | {eventlog,procs,userunits}, vault/subscribe.go, beads/feed.go |
| Postern, hands, Prompts | application/{postern*,hands,prompt}.go | {postern,hands*,homemove} |
| PosternInboxShow (kept messages, --show) | application/posternkeep.go | postern_inbox |
| Chain, StampQueue, StampStore, CommitNotes, RigHeads | application/chain.go, application/chainstamp.go, application/prove.go | {bsv,stampqueue,chainlookup}, rig/worktree.go |
| Grinder, WarmGrinder, GristPoolBoard, GristTalkStore, GrindSource, Grist*, VisionSource | grist.go, application/gristpool.go, application/vision.go, application/gristruns.go, application/personcap.go, griststatus.go, gristsmoke.go | {claude/grind,rig/{grinds,landingof},hostlock/try}.go, grist |
| GristCredits, GristCredit{Key,State}, GristCreditsAdmin | application/gristcredits.go | {grist/credits,claude/grind}.go, cmd/mw/grist_credits.go |
| IdeasOnlyReader, IdeasOnlyFiles | ideasonly.go | {postern/http.go,ideasonly} |
| Suite*, Timing* | application/suite.go, application/suitenightly.go, application/timingnightly.go | {rig/{worktree,checks,timing}.go,vault/{vault,timing}.go,apartproc} |
| Scorer, ScoreTimeout, SecretStore, Google*, ContactBook | application/scorer.go, secrets.go, application/google.go, application/contacts.go | {scorer,sops,google} |

Fakes: application/apptest/fake*.go.

## Use cases

Each use case's application/ files: [docs/codemap-detail.md](codemap-detail.md), by need.

| Use case | Command | features/*.feature |
|-|-|-|
| File, Release, Ice, Thaw, Stuck (--inversions; inversions in DAMPERS), Retry, Cause, Note, Path* | cmd/mw/file.go, cmd/mw/release.go, cmd/mw/ice.go, cmd/mw/stuck.go, application/dampers.go, cmd/mw/retry.go, cmd/mw/cause.go, cmd/mw/note.go, cmd/mw/path.go | {file*,release,retry*,cause,note,path_set} |
| Show, Peek, Prove, StampHead, Tester[File], FactoryReport[Send], Invest[Book], Loop (rank: application/looprank.go) | cmd/mw/show.go, cmd/mw/peek.go, cmd/mw/prove.go, cmd/mw/stamp.go, cmd/mw/tester.go, cmd/mw/report.go, cmd/mw/reportsend.go, cmd/mw/invest.go, cmd/mw/loop.go | {show,peek,prove,stamp,tester,report*,invest} |
| Dispatch, Next, BatchLand, AfterLandingRun, BdRetry | cmd/mw/dispatch.go, cmd/mw/next.go, cmd/mw/land.go, cmd/mw/afterlanding.go, cmd/mw/bd.go | {dispatch,next,batch_land,after_*,bd_*}; a landing may pass the 10240-byte map cap by 10% (codemapBandPercent) and files one trim bug; an `AC n NOT met` closing comment marks the Landed mail and files a held bug (application/acunmet.go) |
| Check*, Status, CloudCheck, CloudMake, Brief | cmd/mw/check.go, cmd/mw/status.go, cmd/mw/cloud.go, cmd/mw/brief.go | {check,status,cloud,brief} |
| Sweep, Tidy, Sync, Nudge, Mail, Home[Move] | cmd/mw/sweep.go, cmd/mw/tidy.go, cmd/mw/sync.go, cmd/mw/nudge.go, cmd/mw/mail.go, cmd/mw/home.go, cmd/mw/homemove.go | {sweep,tidy,sync,mail,home} |
| Suite, SuiteFiler, NightlySuite, NightlyTiming, ContributeBack | cmd/mw/suite.go, cmd/mw/suitenightly.go, cmd/mw/timingnightly.go | {suite*,timing_nightly,contribute_back} |
| Config* | cmd/mw/config.go | config_* |
| IdeasOnly* | cmd/mw/ideasonly.go | ideas_only, postern_inbox_apply, talk_wait |
| Member[Remove] ([docs/family.md](family.md)) | cmd/mw/member.go | member |
| Seat* | cmd/mw/seat.go | seat_* |
| Talk*, TalkShow | cmd/mw/talk.go | talk_* |
| Millhand, Deputy, MillhandTick | cmd/mw/millhand.go, cmd/mw/deputy.go, cmd/mw/millhandtick.go | {millhand*,deputy} |
| Secrets*, Google*, Contacts (mw contacts find), Hands*, Ask, Watch, Doctor | cmd/mw/secrets.go, cmd/mw/google.go, cmd/mw/contacts.go, cmd/mw/hands.go, cmd/mw/ask.go, cmd/mw/watch.go, cmd/mw/doctor.go | {secrets,hands,ask,watch,doctor} |
| SeatBoot{,Set}, SeatRead, Memory[Log], Errors, Init | cmd/mw/init.go, cmd/mw/memory.go, cmd/mw/errors.go, infra/memoryqueries | {seat_boot,init,memory,errors} |
| Postern* (+clear), Digest + Factory loop (LoopSource) | cmd/mw/postern.go, cmd/mw/posternview.go, cmd/mw/posternbead.go, cmd/mw/posternmirror.go, cmd/mw/digest.go | postern_* |
| Event* | cmd/mw/events.go | event_follow |
| Prompt*, Cards, Todo* | cmd/mw/prompt.go, cmd/mw/card.go, cmd/mw/todo.go, cmd/mw/todo_search.go, infra/todoindex/index.go | {prompt,card,todo*} |
| Grist* (eval, smoke: turn_2) | cmd/mw/grist.go, cmd/mw/gristsmoke.go, cmd/mw/grist_talks.go | {grist*,scorer} |
| Triage*, Queue, Fold (+supersede), Sleep, Verify (mw verify match) | infra/beads/triage.go, cmd/mw/triage.go, cmd/mw/queue.go, cmd/mw/fold.go, cmd/mw/sleep.go, cmd/mw/verify.go | verify_match |

cmd/mw/root.go, cmd/mw/version.go

changelog_files: infra/rig/version.go > domain/changelog.go; union_merge_files: rig/landing.go

scripts/check-codemap.sh
