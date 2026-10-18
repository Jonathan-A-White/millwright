# Code map detail

The application/ files of each use case in [docs/codemap.md](codemap.md), which holds the ports, the commands and the features. Read this page by need; check-codemap.sh counts a file named here as named in the map.

| Use case | In application/ |
|-|-|
| File, Release, Ice, Thaw, Retry, Cause, Note, Path* | application/file.go, application/release.go, application/ice.go, application/retry.go, application/cause.go, application/note.go, application/pathset.go |
| Show, Peek, Prove, StampHead, Tester[File], FactoryReport[Send], Invest[Book], Loop | application/show.go, application/stamphead.go, application/testerreport.go, application/testerfile.go, application/factoryreport.go, application/reportsend.go, application/invest.go, application/loop.go |
| Dispatch, Next, BatchLand, AfterLandingRun, BdRetry | application/dispatch.go, application/next.go, application/afterlandingrun.go |
| Check*, Status, CloudCheck, CloudMake, Brief | application/check.go, application/checkparts.go, application/status.go, application/cloud.go, application/cloudmake.go, application/brief.go |
| Sweep, Tidy, Sync, Nudge, Mail, Home[Move] | application/sweep.go, application/tidy.go, application/nudge.go, application/mail.go, application/home.go, application/homemove.go |
| Suite, SuiteFiler, NightlySuite, ContributeBack | application/suite{file,status}.go, application/contributeback.go |
| Config* | application/configset.go |
| IdeasOnly* | application/ideasonly.go, application/ideasonlyrelease.go |
| Seat* | application/seat{context,up,reap,handover}.go |
| Talk*, TalkShow | application/talkcall.go, application/talkmodel.go, application/talksay.go, application/talkwait.go |
| Millhand, Deputy, MillhandTick | application/millhand.go, application/deputy.go, application/millhandtick.go |
| Secrets*, Google*, Contacts (mw contacts find), Hands*, Ask, Watch, Doctor | application/secrets.go, application/hands.go, application/ask.go, application/watch.go, application/doctor.go, application/starved.go |
| SeatBoot{,Set}, SeatRead, Memory[Log], Errors, Init | application/seatboot.go, application/memory.go, application/memorymigrate.go, application/errors.go, application/init.go |
| Postern*, Digest + Factory loop section (LoopSource) | application/postern.go, application/posternmovehome.go, application/posternsnapshot.go, application/posternview.go, application/posternbead.go, application/posternhand.go, application/posternmirror.go, application/digest.go |
| Event* | application/eventfollow.go, application/eventlog.go, application/eventship.go, application/eventwait.go, application/eventnudge.go, application/eventsubscribe.go, application/eventspring.go, application/eventcontrol.go, application/eventtrim.go |
| Prompt*, Cards, Todo* | application/prompt.go, application/tododue.go, application/todosize.go, application/todorank.go, application/todosearch.go |
| Grist* | application/grist.go, application/gristtiming.go, application/gristlevel.go, application/gristgrind.go, application/gristsend.go, application/gristrequest.go, application/griststatus.go, application/gristeval.go, application/gristscore.go, application/gristrunstats.go, application/gristroom.go, application/gristsmoke.go |
| Triage*, Queue, Sleep | application/triage.go, application/queue.go, application/sleep.go, application/sleepgather.go, application/fuelkinds.go |
