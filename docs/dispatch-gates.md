# Dispatch gates

Every reason `mw dispatch` leaves a ready story unclaimed, in the order a story
meets them. A story passed over stays open and unclaimed (`mw dispatch --dry-run`
prints the reason beside it); none of these spends one of its attempts. The list
is `application.DispatchGates`; each gate is marked `// gate: <name>` in
`Dispatch.run`, and a test holds the three together. A new gate is added to all
three.

| Gate | Holds the story when |
|-|-|
| `paused` | the host is paused by a pause-host event: nothing at all is done |
| `hitl` | the story is labelled hitl, or demo with no files key (his own demo): the Governor must be present, the Mayor works it |
| `no-path` | the story's path cannot be read |
| `no-host` | the path names no host |
| `other-host` | the path names another host |
| `harness-hold` | the harness stopped a session here (usage limit, rate limit, overloaded or failing API) and the time it named has not come (below) |
| `no-room` | the host has no room: load, or a grist tutor in use |
| `landed` | its last run landed it and only the close was lost |
| `blocked` | it waits on a story that is not closed |
| `rig-not-checked-out` | the rig is not under `[rigs]` in the config file |
| `smoke-hold` | the rig's last grist smoke failed and the story does not name a failing example in its smoke_examples, title or description |
| `metered-network` | the rig installs dependencies and the network is metered |
| `no-grist-key` | the story's acceptance or description runs `mw grist eval` or `mw grist smoke`, or its path carries `smoke_examples`, and the host has no readable grist test key (`grist_test_key_file`); it waits for a host that has one (mw-gq6.741) |
| `formula-not-installed` | the path's formula is not installed in the vault |
| `no-sweep` | a bug filed after the sweep rule has no line beginning `Sweep:` in its acceptance (below) |
| `tester-no-landed-story` | a Tester story's description has no `Landed story: <id>` line: its session could not file a finding (mw-gq6.594) |
| `attempts-exhausted` | it was started `max_attempts` times already |
| `handed-back` | its Builder handed it back (a comment opening `HANDED BACK`) and no later comment opening `RE-PATHED:` says what changed (below) |
| `needs-files-key` | it names no files key and its rig's file says `files_expected = true`: `mw path set <id> files=<csv>` ends it, and it holds no other story back (mw-gq6.599) |
| `shared-files` | it names a file a running story of the rig names or has changed; since when is kept in the note `host.<host>.files_held`. A files key naming a directory or a glob over more than 20 files is refused by `mw path set` and `mw file` (globs are counted too, mw-gq6.633), so none is held behind a crowd |
| `testers-per-rig` | it is a Tester story and its rig runs `testers_per_rig` (the `[dampers]` key, default 1) Tester stories already, on any host |
| `cap` | the host runs as many sessions as it may |

## The sweep gate

When a bug is seen, the story looks for the whole class, not only the case seen.
A story of type bug, titled `[bug]` or labelled `bug`, filed on or after
2026-10-15 (`domain.SweepRuleFrom`) on any rig is not dispatched unless a line of
its acceptance criteria begins `Sweep:` (any case, after an optional number or
bullet) and says what is checked over the whole class. The escape for a true
one-off is `Sweep: none, because <reason>`; `Sweep: none` with no reason does not
pass. Older bugs are not asked.

The first time a dispatch passes a bug over for it, it comments on the bead and
mails the Mayor, once per bead. `mw status` lists such bugs under
BUGS WITHOUT A SWEEP, and nothing when there are none. `mw file` refuses a plan
with such a bug in it, and `mw tester file` such a bug finding, with the same
words, before anything is filed (mw-gq6.447).

## The handed-back gate

A story the Builder handed back (its newest hand-back comment opens
`HANDED BACK`) is not dispatched, and `mw retry` refuses it, until a comment
opening `RE-PATHED:` follows: the same Builder given the same story hands it back
the same way. `mw retry <id> --repathed "<what changed>"` writes that comment and
retries. `mw status` lists such stories under HANDED BACK with the date of the
hand-back.

## The harness-hold gate

A session the harness stopped for something that is not the work's is held by
its close-out under its own reason (`usage-limit`, `rate-limit`, `overloaded`,
`server-error`), the claim kept. Until the time the stop names (the reset a usage
limit names, or a quarter hour after the session ended for the rest) the host
claims nothing new; once it has come, a dispatch gives the stopped story back
itself, its stopped attempt not counted, and it is started again. `mw retry` of
such a story does not count the attempt either, and keeps the hold. The hold is
kept in the host's note `host.<host>.harness_hold`.

A usage limit can stop the harness before it writes its JSON: the close-out
then reads the limit from what the harness printed in its place, or from the
session's pane, and keeps the stop it read in the run (`harness-stop.txt`,
numbered by attempt as the result is) for dispatch, `mw retry` and `mw status`.
All of them read the limit's notice with the one reader, `HarnessStopOf` and
`HarnessStopOfWords` (mw-gq6.471).

## The smoke-hold gate, and the gates that cannot hold their own fix

A failed grist smoke holds the open stories of its rig, except the fix: a story
whose `smoke_examples` metadata, title or description names a failing example (as
`<app>/<kind>/<example>`, `<kind>/<example>` or the bare example name standing on its own;
`application.NamesSmokeExample`, the same reading the smoke's "already named" match uses,
mw-gq6.773) runs over the hold, and `mw dispatch` says so (`runs over
the grist smoke hold of <rig>: ...`). `mw grist smoke <app> --filed <bead>` with
no example names the examples the last smoke failed on, and `mw status` names the
beads that run over a hold (mw-gq6.579). Closing the bead ends it.

The other gates cannot hold a fix behind the very failure it is to mend, each
for its own reason: `shared-files` and `testers-per-rig` wait only for stories
already running, which are past every gate and end on their own; `harness-hold`,
`no-room` and `paused` wait for the host, not for a story; `no-sweep`,
`tester-no-landed-story`, `handed-back` and `attempts-exhausted` read the story's own bead; `blocked` waits
on a dependency a person filed. The false-hold guard is `mw release`'s, not
dispatch's, and no smoke touches it.

## The starved-rig alarm

When a pass leaves a rig with ready stories passed over (the Governor's own
excepted) and none running or started, the host's dispatch notes it in
`host.<host>.starved`: the rig, when it was first so held, the hold and why. A
rig held that way for over an hour (`application.StarvedAfter`) with a story
ready and none running is an alarm: `mw doctor` (check `starved-rig`) mails the
Mayor once per episode, and `mw status` prints the same line first under DAMPERS.
It clears when a story of the rig starts. The hold is named from the gate that
passed most of the rig's stories over, by `application.StarvedHolds`; a new gate
is mapped there too, and a test says so.

| Gate | Hold name |
|-|-|
| `hitl` | Governor's own story |
| `no-path` | unreadable path |
| `no-host` | no host |
| `other-host` | another host (asleep?) |
| `harness-hold` | harness stop |
| `no-room` | no room |
| `landed` | lost close |
| `blocked` | blocked |
| `rig-not-checked-out` | rig not checked out |
| `smoke-hold` | smoke hold |
| `metered-network` | metered network |
| `no-grist-key` | no grist key |
| `formula-not-installed` | formula not installed |
| `no-sweep` | no sweep |
| `tester-no-landed-story` | tester names no landed story |
| `attempts-exhausted` | attempts exhausted |
| `handed-back` | handed back |
| `needs-files-key` | needs files key |
| `shared-files` | files overlap |
| `testers-per-rig` | damper |
| `cap` | cap |
| `pour-refused` | pour refused |
| `claim-lost` | claim lost |
