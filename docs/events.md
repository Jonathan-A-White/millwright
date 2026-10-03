# Events

The factory owns its state machines, defined once in Go in `domain/events`,
and every event is a transition of one of them (mw-6ww.55, Q6 A). The app is
a projector: events in, screens out. This page is the same tables in prose,
and it is where the field names of the `events` record are agreed: postern's
docs/protocol.md section 22 uses exactly these.

## An event

| Field | JSON | What it holds |
| --- | --- | --- |
| Seq | `seq` | The home's log number: unique, increasing from 1. Order and dedupe by it, never by txid. |
| Ts | `ts` | When it happened, RFC 3339, UTC. |
| Kind | `kind` | One of the kinds below. |
| Bead | `bead` | The bead it is about; empty where the kind has none. |
| Actor | `actor` | Who made the change: a bd actor (`mw@laptop`, `builder@desktop`), or a job as `<job>@<host>`. |
| From | `from` | The state before; empty for a machine's first state, and for a kind with no machine. |
| To | `to` | The state after; empty for a kind with no machine. |
| Detail | `detail` | What changed, per kind below. |
| Lane | `lane` | `normal`, `emergency` or `fallback`: the lane of the batch it came in. |
| Clears | `clears` | The seq of the emergency this event ends; present only when it ends one. An app takes that emergency as resolved. |

Every field but `clears` is always present in the JSON, an empty string where unset. `clears` is
left out of an event that ends no emergency, so an event without it reads as it always did.

## The kinds

| Kind | Machine | Bead | Detail |
| --- | --- | --- | --- |
| `bead_changed` | bead | the bead | what changed: `status`, `comment`, `verified` (a comment marked it verified), a field's name |
| `card_asked` | card (to `asked`) | the bead the card is on | the question's txid |
| `card_answered` | card (to `answered`) | same | the txid its ANSWER comment names: the answer's |
| `card_applied` | card (to `applied`) | same | the question's txid |
| `message` | none | the bead whose channel it is in, else empty | the message's txid |
| `talk_turn` | talk | empty | the turn's txid |
| `hands_ran` | none | the hitl bead | the step's id |
| `mail` | none | the mail bead | its box: the seat it is sent to |
| `job` | job | empty, or the bead the job worked on | the outcome, on `done` or `failed` |
| `handover` | none | empty | `<seat> to <successor window> at <N>`: the old session answers nothing past event N |
| `control` | none | the bead for `cancel` and `priority`, else empty | the word and what it takes: `cancel`, `pause-host <host>`, `resume-host <host>`, `cap <host> <n>`, `priority <n>` |

A `bead_changed` event whose from and to are the same state is a change that
left the status alone (a comment, an edited field). Every other event of a
kind with a machine is one of that machine's transitions, and an event of a
kind with none has empty from and to. A seat or a screen subscribes by kind
and bead; `mail` is how a seat hears a bd mail bead sent to it. The scheduled
jobs are the dispatch pass, the millhand tick, mail notify, backup,
self-update and prune (`dispatch@laptop`, `millhand-tick@desktop`, ...).

## The log

The home numbers every event in its log, `mw events` (README, "mw events: follow, emit,
tail"): `mw events follow` writes the beads' events, reading bd's own audit of the beads and
their comments every second, and `mw events emit` a job's. A bead's state is read from its
status and its run label: deferred is held, an in-progress bead is claimed until its run
label says running, landed, or refused (blocked, stopped, stuck). A status move the follower
saw only the two ends of is one event per step of the bead machine between them. Verified
is read from one comment: the first on a bead that begins `VERIFIED` (the Governor's tap, the
Mayor's check; the rule the postern view uses) is a `bead_changed` from the bead's state,
usually `closed`, to `verified`, detail `verified`. A later one on the same bead, a comment
that only mentions VERIFIED, and `NOT VERIFIED` are plain `comment` events.

## Subscriptions

A seat hears its events without polling (mw-jrx0s.6). `seats/<seat>/subscribe.toml` in
the vault names the kinds it hears (`kinds = ["mail", "landing", "card_answered",
"message"]`; any kind above, and `landing`, a `bead_changed` that ends in `landed`) and,
for the deputy and the millhand, `spring = true`. `mw events wait --for <seat>` blocks on
the log until one comes; the follower types a nudge into the seat's idle pane, or runs
its up command when its window is down and it is marked spring. A `mail` event is the
seat's only when its detail, the box, is the seat.

The subscribe file is TOML, two keys, both optional but `kinds`:

```toml
kinds = ["mail", "landing", "card_answered", "message"]
spring = true
```

`kinds` is one line of event kinds (hyphens may stand for underscores) or `landing`; a bad
name leaves the seat out and is logged. `spring = true` is for the deputy and the millhand
only: the follower brings the seat up when its window is down. Without a file a seat hears
`mail`. The follower keeps each seat's cursor in `nudge.json` beside the log; a seat first
seen starts at the head.

A seat is handed over on the log (mw-jrx0s.11): the successor boots while the old
session still answers, and `mw seat handover --at <N>` (default: the log's head) emits a
`handover` event and writes the successor's name and N into the seat's acting file. A
`mw events wait --for <seat>` or `mw talk wait` that began before the event, in any
window but the successor's, ends at once on it with "handed over at N"; the successor's
`mw events wait` ends on it with "you hold the seat from N", and one begun `--since N` or
later ignores it. The old session reads and answers up to N, the successor from N on.

## Springing the jobs

The follower runs the factory's jobs (mw-jrx0s.14): `dispatch` (a bead_changed to open, or to
landed, of a bead that is no step of a molecule), `millhand-tick` (an emergency-lane event, a
mail whose box is the millhand, a job event `doctor@<host>` ending failed) and `mail-notify` (its
own clock only), each by starting its systemd unit. Every pass is a run of the job machine under
the actor `<job>@<host>`: scheduled, whose detail says why (`bead mw-x opened`, `bead mw-x landed`,
`alarm`, `heartbeat`, `clock`), running, then done, or failed with the failure as its detail. A
job unrun for `[events] heartbeat` is run for `heartbeat`; the pass is in flight at most once,
and an event that springs it meanwhile earns one more pass after it. A pass cut short because the
follower is stopping (a landing of millwright restarts it onto the new build, ending the systemctl
the pass waited on) is not failed: it is `done` with the detail `cut short by the follower
stopping; run again when it is back`, and the first look of the follower that comes back runs it
again, scheduled for `run again after the follower restarted`.

## Control

A `control` event is a word to the factory (mw-jrx0s.16), not a thing that happened:
`mw events emit --kind control --detail "pause-host laptop"`, with `--bead <id>` for the words
about a bead. An unknown word, or a bead where the word is about a host, is refused before
anything is written. A seat subscribes to `control` like any kind, and hears every word.

| Word | Bead | Who acts on it |
| --- | --- | --- |
| `cancel` | the story | the follower of the host that holds the claim |
| `pause-host <host>` | none | that host's dispatch passes, until `resume-host <host>` |
| `resume-host <host>` | none | undoes the latest pause |
| `cap <host> <n>` | none | no one yet: carried to the seats that subscribe |
| `priority <n>` | the story | no one yet: carried to the seats that subscribe |

A `cancel` is what the Governor's hold tap on a claimed story writes (actor
`governor@postern`; the story must be in progress and assigned). Each pass the follower
reads the cancels since its cursor, kept as `control` in `nudge.json` beside the log; a
follower with no cursor starts at the head, so a cancel from before it began is history. For
a story claimed on its own host it closes the story's session as the reaper closes a window,
gives the claim back, holds the story, sets its run state to `cancelled`, and comments
`cancelled by <actor> at <time>`. The worktree and branch stay for a Clerk or `mw retry`. A
cancel of a story that is not claimed, or is claimed on another host, is left and said on the
follower's stderr. Only the home's log is read, so a pause or a cancel reaches the host whose
follower and dispatch read that log. `mw status` shows a `PAUSED` line and the runs cancelled
in the last day.

## The machines

"(start)" is before the first state: the empty `from`. Anything not listed
is refused, naming the machine and both states: `the bead machine has no
transition from "landed" to "claimed"`.

### bead

States: held, open, claimed, running, landed, refused, verified, closed.

| From | To |
| --- | --- |
| (start) | held, open |
| held | open, closed |
| open | held, claimed, closed |
| claimed | running, open, held |
| running | landed, refused, open, held |
| refused | open, held, closed |
| landed | verified, closed, open, held |
| verified | closed |
| closed | open, verified |

Claimed is a dispatch's claim, running a session at work, landed and refused
what mw next did, verified the Mayor's check (a VERIFIED comment, even on a closed bead). A bad landing is reopened or
held; a closed bead may be reopened.

### card

States: asked, answered, applied.

| From | To |
| --- | --- |
| (start) | asked |
| asked | answered |
| answered | applied |

A card is answered once: a second answer is refused.

### talk

States: open, turn, answer, ended.

| From | To |
| --- | --- |
| (start) | open |
| open | turn, ended |
| turn | turn, answer, ended |
| answer | turn, ended |

A turn may follow a turn: he may speak again before the Mayor answers.

### job

States: scheduled, running, done, failed.

| From | To |
| --- | --- |
| (start) | scheduled |
| scheduled | running |
| running | done, failed |
| done | scheduled |
| failed | scheduled |

## The batch

One `events` record's plaintext is one batch: the events numbered `from` to
`to`, every one, in seq order, all in the batch's `lane`.

- `normal`: the ~2 s batch, on chain and direct.
- `emergency`: one event sent alone and at once; `from` equals `to`. A writer asks for it
  by putting the event in the log in this lane: `mw events emit --emergency`, `mw talk call`
  (a `message` whose detail is the ring's txid), and mayor-stale's alarm in `mw doctor` (a
  `job` event, running to failed, actor `doctor@<host>`, the alarm's text as detail; the
  boost-reach and battery alarms too). When the alarm's condition ends (the Boost answers
  again, the Mayor is fresh again, the battery recovers or is plugged in) `mw doctor` writes
  ONE event in the `normal` lane, not an emergency: a `job` event, running to done, actor
  `doctor@<host>`, the way back as detail, whose `clears` is the seq of the emergency it ends. The
  doctor keeps that seq between runs; an alarm whose emergency could not be written has none, and
  its end names no `clears` (or, for the Mayor and the battery, says nothing). A battery that fell to
  Low and then to Critical wrote two emergencies and so clears both, one event each. Each
  pass of the follower's sender sends these first: on chain and direct together, before the
  pending fallback batches are retried and without waiting for the 2 s window, and on chain
  even past `chain_daily_cap`. They have an allowance of their own, `[events] emergency_daily_cap`
  (default 20 a UTC day), which `chain_daily_cap` does not count: past it an emergency goes
  direct only, the follower says so, and it is not put on chain later. An emergency's detail is
  at most 2000 bytes (`mw doctor` cuts its alarm's text to fit). With the chain unreachable it goes direct in this lane and is
  put on chain later in the `normal` lane, as a fallback batch is. The batches that follow
  leave it out, so the app has it once. `mw status` counts the day's emergencies.
- `fallback`: a batch sent direct only while the chain could not be reached
  or the day's `chain_daily_cap` was spent, re-sent on chain later in the
  `normal` lane with the same seq range and events, so the app dedupes it by
  seq. With `[events] chain = false` every batch is `fallback` and none is
  re-sent. A batch holds at most 50 events and
  what fits a record (about 7 KB of text), the rest following in the next.

```json
{
  "from": 41,
  "to": 43,
  "lane": "normal",
  "events": [
    {
      "seq": 41,
      "ts": "2026-10-01T13:02:07Z",
      "kind": "bead_changed",
      "bead": "mw-jrx0s.4",
      "actor": "mw@laptop",
      "from": "open",
      "to": "claimed",
      "detail": "status",
      "lane": "normal"
    },
    {
      "seq": 42,
      "ts": "2026-10-01T13:02:07Z",
      "kind": "card_answered",
      "bead": "mw-6ww.55",
      "actor": "mw@laptop",
      "from": "asked",
      "to": "answered",
      "detail": "direct:a79d45422283b7f73c07ca4b61c81220ea469c85f5d781bfec668cf3a152e933",
      "lane": "normal"
    },
    {
      "seq": 43,
      "ts": "2026-10-01T13:02:08Z",
      "kind": "job",
      "bead": "",
      "actor": "dispatch@laptop",
      "from": "scheduled",
      "to": "running",
      "detail": "",
      "lane": "normal"
    }
  ]
}
```

`domain/events/testdata/batch.json` is this example, and the package's tests
marshal it byte for byte.
