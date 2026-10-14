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
| `hitl` | the story is labelled hitl: the Governor must be present, the Mayor works it |
| `no-path` | the story's path cannot be read |
| `no-host` | the path names no host |
| `other-host` | the path names another host |
| `no-room` | the host has no room: load, or a grist tutor in use |
| `landed` | its last run landed it and only the close was lost |
| `blocked` | it waits on a story that is not closed |
| `rig-not-checked-out` | the rig is not under `[rigs]` in the config file |
| `smoke-hold` | the rig's last grist smoke failed and the story does not name a failing example |
| `metered-network` | the rig installs dependencies and the network is metered |
| `formula-not-installed` | the path's formula is not installed in the vault |
| `no-sweep` | a bug filed after the sweep rule has no line beginning `Sweep:` in its acceptance (below) |
| `attempts-exhausted` | it was started `max_attempts` times already |
| `handed-back` | its Builder handed it back (a comment opening `HANDED BACK`) and no later comment opening `RE-PATHED:` says what changed (below) |
| `shared-files` | it names a file a running story of the rig names or has changed |
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
BUGS WITHOUT A SWEEP, and nothing when there are none.

## The handed-back gate

A story the Builder handed back (its newest hand-back comment opens
`HANDED BACK`) is not dispatched, and `mw retry` refuses it, until a comment
opening `RE-PATHED:` follows: the same Builder given the same story hands it back
the same way. `mw retry <id> --repathed "<what changed>"` writes that comment and
retries. `mw status` lists such stories under HANDED BACK with the date of the
hand-back.
