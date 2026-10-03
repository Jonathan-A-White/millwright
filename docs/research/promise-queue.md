# Research: a promise queue in mw

Answers mw-6ww.78, for the parked grilling mw-6ww.76. The Governor, 2026-10-03: a priority queue
"to make sure that nothing falls off the plate", where you can put work down, delegate it, "add
new promises late in a lightweight way", change a priority, and have it learn priority from his
feedback. Read-only research: no product code changed.

## What exists today

- **Mail is the model for a new bead type.** `TypeMail = "mail"` is a custom type the vault
  declares (`infrastructure/beads/mail.go:22`; the vault's `types.custom` is `mail` today, so
  adding one is `bd config set types.custom mail,promise`). A message is a `bd create --type
  mail` with an assignee, a label, metadata and `--storage-class versioned` so it syncs
  (`mail.go:39-60`); a reply is a `related` link. A new bead of type mail is turned into a `mail`
  event (`application/eventfollow.go:463`).
- **bd already holds a queue.** `bd create` takes `--priority 0-4`, `--due`, `--defer` and
  `--assignee`; `bd ready` sorts by priority and hides future-deferred beads; `bd list --overdue`,
  `--due-before` and `--sort priority` exist (bd 1.3.0). Add, find, change priority and update are
  `bd create`, `list`, `update`, `show`. Nothing needs inventing for the data structure.
- **mw already changes a priority.** `SetStoryPriority` (`application/worktracker.go:489-492`) is
  what the Governor's priority tap calls (`application/posternapply.go:473`) and it comments
  `PRIORITY n set by the Governor via postern, txid …` on the bead. A `control priority <n>`
  word is parsed (`domain/events/control.go:21`) but nothing acts on it yet
  (`application/eventcontrol.go:102`).
- **Every bead change is already an event.** The follower reads bd's audit and emits
  `bead_changed` for any bead (`eventfollow.go:450-470`), so a promise made, re-ordered or kept
  reaches the log, the springer and the Governor's app with no new code.
- **A card can show a list and tick it.** A live card item expects a bead to reach `open`,
  `landed`, `verified`, `closed`, `answered` or `held` (`domain/card.go`, `ExpectStates`), and
  ticks itself from events. A promise kept is `closed`.
- **Dispatch would not take a promise.** `ReadyForHost` asks for ready unassigned beads, then
  drops any whose Path names no host (`infrastructure/beads/dispatch.go:28,120-125`). A promise
  with an owner and no Path is outside it; the exclusion is still worth making explicit.
- **Time has one clock.** The follower springs jobs by event, and each job has its own heartbeat
  clock (`docs/events.md`, "Springing the jobs"; `mail-notify` runs "its own clock only").

## What is missing

Today a promise ("id to follow", "I'll file a ticket") lives in the Mayor's head between turns and
handoffs. There is no bead for it, no command to say it in one line, no list a successor reads
at boot, and nothing that fires when one is overdue. Priority taps are applied but never kept as
anything a later Mayor could learn from.

## Options

**A. Convention only.** A promise is an ordinary bead (`task`, label `promise`, owner a seat,
`--due` optional); the Mayor's charter says "write it down in one `bd create`". Fuel: zero code,
about 150 tokens per promise made, a list read costs 300 to 800. Parts: one label, one charter
line. Fails the "any delegate" and "nothing falls" tests: nothing reads the list at boot, a plain task
looks like a story to `bd ready`, and nothing fires on overdue.

**B. A `promise` bead type and `mw promise`.** The vault declares type `promise`. Commands:
`mw promise add "<text>" [--due +2h] [--owner deputy] [--priority 1]`, `list`, `done <id>`,
`drop <id> --why`, `move <id> --priority n`, `hand <id> --to <seat>`. Made: one `bd create --type
promise --storage-class versioned`, owner a seat, metadata `{made_in: <bead or channel>,
kind: ticket|reply|check}`. Kept: `bd close`, the reason its proof. Broken: closed with label
`promise:broken` and the reason, never silently deleted. Shown: `mw promise list` (priority
then due, one line each, about 25 tokens a promise), the boot kickoff prints open promises for
the seat, and `mw status` counts overdue ones. Fuel: zero model tokens in `mw`; a boot adds
the list. Parts: one type, one use case with a port narrowed from `WorkTracker`, one cobra
command, a fake, one feature, one `--exclude-type promise` where `ready` is read. It follows
mail file for file, about 300 lines plus tests.

**C. B plus time.** A job `promise-due` (own heartbeat, springing on a promise's `--due`) that
sends mail to the owner, and an emergency-lane event when a promise made to the Governor is
past due. Fuel: zero tokens until a seat is woken; a woken Deputy costs a session. Parts: B, a
job, one new event detail, a clock. "Real time" is this, and only this: a deadline and a
wake-up, not a scheduler.

**D. B plus learning.** Record every priority change with its reason (the postern comment above,
plus `mw promise move --why`), and a periodic digest the Mayor reads: what he raised, lowered
or dropped, by kind. A model call would produce a rule from it: roughly 5k tokens a week at
Sonnet. Parts: B, a comment convention, a digest command, a prompt. The first useful learning
needs a few dozen examples, which at today's rate is weeks away.

**E. A mw-native queue (file or bd kv).** A sorted file in the vault. Fuel zero; parts: a format,
a merge story across two hosts, no events, no cards, no sync beyond the vault's git. Rejected:
it rebuilds what bd gives, and beads are what the Governor's app already projects.

## Recommendation

**B now, C as its second story, D deferred.** B reuses beads for storage, bd's sort for the
queue, the event log and cards for showing it, and mail's shape for the code, so it costs one
small use case and no new store. C adds the one thing "nothing falls off the plate" needs:
something that notices when a promise passes its time. D waits for data: begin by only
recording reasons, which B gives us, then decide.

Split into stories: (1) the type, `add`, `list`, `done`, `drop`, `move`, `hand`, with the
exclusion from `ready`; (2) the boot and status read, so a handoff carries the list; (3) the
card row for promises made to the Governor; (4) `promise-due`; (5) the digest.

## Questions only the Governor can answer

1. **Is a promise a bead?** Recommended: yes, type `promise`, so it syncs, shows in his app and
   survives a handoff or a home move. The lighter file (E) is rejected.
2. **Who may add and pop?** Recommended: the Mayor, the Deputy and any Builder may add; only the
   owner closes; anyone may hand it on; only he may drop one made to him.
3. **What does "real time" mean?** Recommended: a deadline and a wake-up (C), checked once a
   minute at most, no sub-second guarantee. "Answer him within a minute" becomes `--due +1m`
   plus an emergency event; say if that is not what he meant.
4. **How does he teach priority?** Recommended: the priority tap (it exists, `posternapply.go:473`),
   each recorded with a reason; a thumbs on a card item is new app protocol and not assumed; no learner until he has given
   about fifty. Does a reorder on the phone count as a lesson, or only a tap marked as one?
5. **What is the default priority and due for a late promise?** Recommended: priority 2, no
   due, until the Mayor sets one at the next break; one made to him gets priority 1.
6. **Does he want to see them?** Recommended: one standing card "Promises" with each open
   promise as an item that ticks on `closed`, refreshed by `mw card update` when one is made
   or kept, not a new screen.
