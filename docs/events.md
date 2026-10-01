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

Every field is always present in the JSON, an empty string where unset.

## The kinds

| Kind | Machine | Bead | Detail |
| --- | --- | --- | --- |
| `bead_changed` | bead | the bead | what changed: `status`, `comment`, a field's name |
| `card_asked` | card (to `asked`) | the bead the card is on | the question's txid |
| `card_answered` | card (to `answered`) | same | the txid its ANSWER comment names: the answer's |
| `card_applied` | card (to `applied`) | same | the question's txid |
| `message` | none | the bead whose channel it is in, else empty | the message's txid |
| `talk_turn` | talk | empty | the turn's txid |
| `hands_ran` | none | the hitl bead | the step's id |
| `mail` | none | the mail bead | its box: the seat it is sent to |
| `job` | job | empty, or the bead the job worked on | the outcome, on `done` or `failed` |

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
is never read from the beads yet.

## Subscriptions

A seat hears its events without polling (mw-jrx0s.6). `seats/<seat>/subscribe.toml` in
the vault names the kinds it hears (`kinds = ["mail", "landing", "card_answered",
"message"]`; any kind above, and `landing`, a `bead_changed` that ends in `landed`) and,
for the deputy and the millhand, `spring = true`. `mw events wait --for <seat>` blocks on
the log until one comes; the follower types a nudge into the seat's idle pane, or runs
its up command when its window is down and it is marked spring. A `mail` event is the
seat's only when its detail, the box, is the seat.

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
| closed | open |

Claimed is a dispatch's claim, running a session at work, landed and refused
what mw next did, verified the Mayor's check. A bad landing is reopened or
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
- `emergency`: one event sent alone and at once; `from` equals `to`.
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
