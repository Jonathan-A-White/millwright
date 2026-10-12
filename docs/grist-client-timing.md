# client_timing: the phone's part of a grist's round trip

The mill times its own part of an answer (`mw grist timing`, sent on the
backend to delivered). An app can report the phone's part, so the whole round
trip, tap to shown, can be read. This is the millwright side of the grist
protocol; the apps read postern's `docs/protocol.md`, section 18, which should
carry the same field (a follow-up on postern's side).

## The request

A grist's plaintext (the object sealed to the mill key) may carry one optional
member, `client_timing`, about the app's **previous answered grist**:

```json
{
  "grist": {"app": "cairn", "kind": "sweep", "v": "1.1"},
  "input": {},
  "client_timing": {
    "txid": "direct:…",
    "tap_to_sent": 900,
    "network": 260,
    "poll_network": 100,
    "delivered_to_seen": 3000,
    "seen_to_shown": 300
  }
}
```

| Field | Unit | Meaning |
|-|-|-|
| `txid` | | the txid of the earlier grist whose answer this measures. Required. |
| `tap_to_sent` | ms | the user's tap to the request accepted by the backend, upload included |
| `delivered_to_seen` | ms | the answer delivered on the backend to the app's poll seeing it |
| `seen_to_shown` | ms | the answer seen to drawn on screen |
| `network` | ms | the round trip of the poll request that saw the answer plus that of the request that sent it, summed. It lies inside `tap_to_sent` and `delivered_to_seen`: it is shown as its own phase and is not added to them. |
| `poll_network` | ms | optional: the poll's round trip alone. `delivered_to_seen` less `poll_network` is the wait between checks. |

Every timing field is optional; send the ones the app measured, at least one.
Unknown members are ignored.

## What the mill does

It never refuses a grist for its `client_timing`. It records the report when
the `txid` is a grist the mill answered, and delivered, for the **same key**,
and every number given is a number of milliseconds from 0 to 600000 (ten
minutes). Anything else is ignored and logged in the pass's notes: an unknown
txid, another key's, a negative, absurd or non-numeric value, no timing at all.
The first report for a grist stands; a second is ignored and logged.

The report is kept in `client_timing.jsonl` beside `grinds.jsonl` in the mill's
state directory (the record's lines are written once, when the grist is
answered), one line for each earlier grist, with the fields above as
`<name>_ms`.

## What mw grist timing shows

For each app and kind, beside the mill's phases, where the apps reported them:
`tap_to_sent`, `delivered_to_seen`, `wait`, `seen_to_shown`, `network` and
`round_trip`, each with its count, median and p90 in seconds.
`round_trip` is tap to shown: `tap_to_sent` + the mill's `total` +
`delivered_to_seen` + `seen_to_shown`, known for a grist that reported all
three and whose answer was delivered.
