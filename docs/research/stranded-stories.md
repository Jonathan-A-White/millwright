# Research: stories stranded on a host that cannot make progress

Answers mw-6ww.79, for the parked grilling mw-6ww.69. The Governor, 2026-10-03: "auto pull cancel
and pull stories over to where they can make progress after a reasonable timeout." Read-only
research: no product code changed.

## What the two outages left behind

The events log starts 2026-10-01 13:59Z, nine minutes before the first WSL stall, so it holds the
whole of them. Three signs, all found in `~/.local/state/mw/events/log.jsonl` (10,108 events, 1.7
MB) and in beads:

- **A burst of `bead_changed running -> refused` by `mw@desktop`.** At the disk's read-only
  failure (13:07Z) five Builders were refused with no result within 74 seconds (13:08:37 to
  13:09:51: mw-6ww.78, .79, .80, .81, .83). After a `resume-host` at 13:54Z, six more died the same
  way, 14:03 to 14:05, before the second `pause-host` (seq 9850). The WSL kills left no such
  burst, and no claim: the desktop claimed nothing between 2026-10-01 14:00Z and 10-02 21:51Z, and
  its last story before the 03:20Z kill closed before it. They idled the desktop; they stranded no
  claimed story.
- **Claimed stories whose host is dead.** `bd list --status in_progress` with `host=desktop`.
  Today it shows none, since the Mayor moved them by hand.
- **Ready stories pathed to the dead host.** Three sit open with `host=desktop`: mw-kmgi38.9,
  mw-kmgi38.10, mw-gq6.252. Nothing will start them while it is paused.

It cost real tries. mw-6ww.79 died at 13:08Z, was retried at 13:56Z, died again at 14:04Z, and was
re-pathed by the Mayor at 14:43Z on the Governor's tap, 95 minutes after the first death. Its
`attempts` is 3 of `DefaultMaxAttempts` 3 (`application/attempts.go:35`): two tries went to a host
that could not write.

## What exists today

- **Detection, status only.** `mw status` calls a host asleep when its `last_sync` note is more
  than `DefaultHostSilence`, 2 hours, old and marks its stories "stranded" with a re-path hint
  (`application/status.go:20-26`, `:607-640`, `:976-983`). The type's own comment says "no automatic
  failover... a person re-paths it" (`:244-248`). The hint is `bd update <id> --set-metadata host=`
  (`:87`).
- **`mw doctor boost-reach`** alarms the Governor after 30 minutes without an ssh answer, and the
  probe now writes a file, so a read-only disk counts (`infrastructure/doctor/boostreach.go:22`;
  mw-gq6.254). It alarms; it moves nothing.
- **`pause-host <host>`** is a log event that stops that host's own dispatch
  (`application/eventcontrol.go:38`, `application/dispatch.go:436`). It does not move a story.
- **Re-pathing is written in exactly one place**: a claim of a `host=auto` story writes the claimer's
  name (`application/dispatch.go:828-832`); a give-back writes `auto` again (`:841`). The Mayor does
  all else by hand.
- **Taking.** `ReadyForHost` offers a host its own and `auto` stories only
  (`application/worktracker.go:132-140`); `auto` also passes a load check (`dispatch.go:561-571`,
  `:758`). So `host=auto` is already the "pulled over to where it can progress" state.
- **Reclaiming.** `reclaimDeadPane` runs on the claiming host for its own claims
  (`dispatch.go:1154`); `mw sweep` reads only this host's claims (`application/sweep.go:64-83`) and
  marks them `run=stuck`; `ReclaimStory` never passes `--any-replica`, "who may reclaim the other
  host's claims is not settled" (`infrastructure/beads/beads.go:679`). A dead host reclaims nothing.
  A reclaim also refunds the attempt (mw-y0dkzp): the session ended and no close-out ever ran, so
  `attempts` goes down by one (never below 0) and the reclaim comment says so; a story tried
  `max_attempts` times with a dead pane is reclaimed and started, not exhausted. A refusal
  (`run=blocked`) is never reclaimed or refunded, and the 2026-10-01 refusals above would not have
  been refunded either: that needs a signal that the host, not the story, failed (option B).
- **Giving back another host's claim is refused.** `ReleaseClaim` carries `--if-assignee <own
  actor>` (`infrastructure/beads/dispatch.go:218-224`), so the home cannot release `mw@desktop`'s
  claim; the Mayor had to run `bd update` by hand and filed a bug.
- **A returning host copes with a leftover.** `mw retry` on another host hands the claim back without
  reading a worktree (`application/retry.go:292`, `:364`, mw-gq6.256), and the owning host's next
  dispatch bundles the leftover before a fresh cut. A heartbeat from a claim no longer held fails
  (`worktracker.go:433`), which stops a returned host's session from running on beside a new one.
- **A stranded claim's clock**: `claimed_at` metadata (`attempts.go:25`) and the bd lease
  (`LeaseExpires`).

## What the two signs miss

The 2-hour silence rule would have caught a WSL kill but, I believe, not the disk: the desktop's sync
runs over the network to bd and kept writing events until 14:05Z while its sessions died. I have not
read its `last_sync` note for that hour. The refused burst catches the disk, not the kill. So one
signal is not enough, and a story whose host is merely slow must not be moved.

## Options

**A. Collector, no rule.** A read-only `mw stranded` (or a status section) folds the log and beads
into one list: stories claimed or ready on a host that is paused, silent over 2 hours, or showing a
refused burst. Fuel: none, it is code; a Mayor reading it takes a few hundred tokens instead of a
400k-token raw log. Parts: one pure fold beside `PausedHost`, one command. The Governor's tap stays
in the loop, so a 95-minute gap stays a 95-minute gap.

**B. The home re-paths on a timeout.** Option A's fold, run by the home's Millhand tick, plus an
action on each story it finds past the timeout: release the claim (naming the actual assignee),
set `host=auto`, record `repathed_from=<host>`, comment, emit a `job` event, and give back the
attempts the stranded host burned. Fuel: none until a Builder re-runs the story, which is the cost
wanted. Parts: the fold, one release that may name another actor, an attempts refund, a timeout
setting, a tick step. Risk: a host that is slow, not dead, runs a session beside the new one. The
heartbeat fence above and the leftover bundling cover most of it.

**C. Every live host pulls.** No writer: a dispatch treats a story pathed to a host past the
timeout as `auto`. Parts: two `last_sync` note reads per dispatch and a reclaim of a foreign claim.
Nothing is recorded as a decision, which hides the move from `mw status` and the Mayor.

**D. Pull `auto` more widely.** Path most stories as `auto` and drop the rule. Cheapest, and it
helps only new stories; stories pinned to a host (a Windows-side job, a rig on one host only) stay
stranded.

## Recommendation

**A first, then B**, in three steps: (1) the fold and `mw stranded`, tested on the real log above
(the five-in-74-seconds burst must flag mw-6ww.79); (2) a release that takes the claim's own
assignee, which fixes the bug the Mayor hit and is useful alone; (3) the rule in the Millhand tick,
off by default until the Governor has watched step 1's list for a week. The rule re-paths a story only
when its host is paused for over 1 hour, silent for over 2 hours (the existing constant), or showing
two or more no-result refusals of different stories within 15 minutes with none since; never a
`hitl` story, never one that `run=landed`, never one whose rig is not checked out on a live host.
The move is to `auto`, not to one named host, so a host that returns first may take it. Reject C for
hiding the decision, D as not covering the pinned. Those signs are the rule's inputs; none alone is trusted.

## Questions only the Governor can answer

1. **How long is "a reasonable timeout"?** Recommended: 2 hours of silence, 1 hour paused, or the
   refused burst at once. The burst does not wait, since sessions already died.
2. **May a story pinned to a host move?** Recommended: yes, but only stories whose rig is on another
   host; the rest are listed, not moved. Stories needing a host's own hardware carry a label that
   keeps them put.
3. **Do the stranded tries count?** Recommended: no, the move refunds the attempts that left no
   result on the stranded host.
4. **Should the Mayor still be asked?** Recommended: not before the move, only a card after
   ("moved N stories off desktop; undo?"), since his tap took 95 minutes today.
5. **What happens when the stranded host returns?** Recommended: it bundles its leftovers and takes
   `auto` work like any host; the story keeps its new owner.
6. **May the rule cancel too?** His words say "auto pull cancel". Recommended: yes for a claim past
   the timeout whose host's pane cannot be read, as a `cancel` event written by the home.
