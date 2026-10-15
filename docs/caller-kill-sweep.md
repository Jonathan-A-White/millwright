# What a killed caller leaves behind

A sweep (mw-gq6.459) of every `mw` command that runs a long external process in
the foreground of its caller, and what is left if the caller is killed (a
`timeout`, a closed session, `kill -9`) part way. Read from the code, not by
killing each one; the cloud make was the one tried.

| Command | Long process | If the caller is killed | Half-state? |
|-|-|-|-|
| `mw cloud check` (make) | `vultr-boost up`: terraform apply, 6+ min from the snapshot | **Fixed here.** The check only writes the box into the book as *being made* and begins `mw cloud make` in a session and scope of its own; it returns in seconds. Before, the wrapper was a child of the check (`exec.CommandContext`): a caller kill took the wrapper's bash with SIGKILL, no trap ran, terraform went on and made the box, and its state sat in the wrapper's tmpfs directory, never sealed into the vault. Billed, in no book, and `down` found no state to destroy (cloud1/cloud2, 2026-10-15). | No longer |
| `mw cloud check` (destroy), idle or cap | `vultr-boost down`: terraform destroy, then the peer off the hub | Still in the foreground of the check, a minute or so. The book drops a box only after `Down` succeeds, so a kill leaves it in the book and the next check destroys it again. Same unsealed-state hazard as above if terraform is killed half way: it is seen as `down` failing, not as a silent orphan. | Possible, self-healing unless the state was lost (below) |
| `mw cloud make` itself | `vultr-boost up`, 20 min limit | The limit kills the wrapper with SIGKILL, and so the same hazard as the old check. The box is in the book as *being made*, and a check gives it up after 45 min and runs `down`, which finds no state if the wrapper never sealed it. | Possible at the limit, not on a caller kill |
| `vultr-boost snapshot` | Vultr snapshot request, polled | By hand only (docs/vultr-boost.md, "The ready-made image"). A kill leaves a snapshot Vultr goes on making, billed for its size; nothing in mw refers to it. | A stray snapshot, to delete at my.vultr.com |
| `vultr-boost` (the script) | all of the above | `trap 'exit 130' INT TERM` runs `cleanup`, which `rm -rf`s the tmpfs state **without `seal_state`**: even a polite SIGTERM mid-apply loses the state of a box terraform has made. Filed from this story, not changed in it. | Yes: the one real hole left |
| `mw check`, `mw next` gates | the rig's tests (`rig/checks.go` `runLine`) | A cancelled context sends SIGTERM to the test's process group, then SIGKILL after 10 s. `kill -9` of mw itself leaves the group (it has `Setpgid`) running to its end, writing only into the story's worktree or a throwaway one; the result is lost, nothing is merged or pushed. The host gate lock and the merge slot are flocks, gone with the process. | A stray test run; no landing state |
| `mw next` (close-out) | merge, rig tests, push, after-landing command, in that order | The cross-host merge slot is a lease that expires (`CrossSlotTTL`). Landing is recorded as `run=landed` right after the push, before the after-landing command, and a second `mw next` of a landed story only closes it, merging, testing and pushing nothing again. A kill between the push and `run=landed` is covered by the ledger line written before it. A kill during the after-landing command (the rig's deploy) can leave the rig half deployed, which is why the mail says so when the command is stopped at its limit. | After-landing only |
| `mw suite`, `mw check --background` | the rig's tests | Begun detached (`Setsid`, a systemd scope), as this story does for the cloud make: the caller's death does not touch them. | No |
| `mw postern inbox --apply` | hands steps | Handed off to a transient systemd unit when started by a service (`postern_detach.go`). | No |

Since mw-gq6.460 the cloud check also lists Vultr's instances and destroys each box
named for a cloud box that the book does not hold, so a box left by any of the
kills above is destroyed at the next check, whatever state was lost.

Not swept: `homemove`, `doctor`'s probes and `mw hands` steps, which are not part of
the story's list and were not read for this.
