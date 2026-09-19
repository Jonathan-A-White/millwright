# What a Claude Code result.json actually counts

Researched for mw-gq6.41, 2026-09-19. Evidence: the two real run directories
named below, plus Claude Code's own documentation. Nothing here is inferred
from a single run.

## The question

The session for mw-gq6.33 ran on the VPS from 02:05Z to 02:20Z and made two
commits. The `result.json` mw kept for it says `num_turns: 1`,
`duration_ms: 3625`, 65,375 tokens — and `total_cost_usd: 0.7433436`. The
first dogfood story, mw-rk4.1, says 12 turns, 261 seconds, 314,082 tokens and
`$0.2201868`. Both cannot be totals of the same kind: the cheaper run has five
times the tokens.

## The answer, in one line

**A result file's fields do not all cover the same span.** `usage`,
`num_turns` and `duration_ms` cover the *one result message they belong to*.
`total_cost_usd`, `modelUsage` and `duration_api_ms` carry the running total
for the **whole run**. mw was reading the fuel out of the narrow group.

| Field | What it covers | Subagents |
| --- | --- | --- |
| `usage` | that result message's turn only | **excluded** |
| `num_turns` | turns since the previous result message | — |
| `duration_ms` | wall clock since the previous result message | — |
| `duration_api_ms` | whole run, time spent waiting on the model | included |
| `total_cost_usd` | whole run | included |
| `modelUsage` | whole run, split by model | included |
| `result_index` | which result message of the run this is, from 0 | — |

## Why a `claude -p` run emits more than one result message

`--output-format json` prints only the last result message, but it is not
always the only one. Claude Code's headless documentation:

> If Claude starts a background subagent or workflow, `claude -p` instead stays
> open until that work completes… If Claude starts a Monitor watch during a
> `claude -p` run, Claude Code waits for the watch until it times out… **While
> it waits, Claude keeps responding to what the watch reports.**

Each of those wake-ups is a turn, and each turn emits its own result message.
The cost-tracking documentation says what that does to the fields:

> In streaming input mode, one `query()` call carries multiple user turns and
> each turn emits its own result message. The result fields differ in scope:
> **`usage`**: covers only that turn, and within it only the main agent loop,
> not any subagents it ran. **`total_cost_usd` and `modelUsage`**: carry the
> running total for the whole call so far.
>
> In a call where your app never sends `/clear`, `/reset`, or `/new`, read the
> latest result for call totals rather than summing across results.
>
> Where you have the choice, account from `total_cost_usd` or `modelUsage`
> rather than `usage`.

It also says `usage` undercounts as soon as a subagent runs, whatever else is
going on:

> | `usage` | Excluded. Counts only the top-level agent loop, so tokens
> consumed inside subagents are not added |
> | `total_cost_usd` | Included. |
> | `modelUsage` | Included… broken down by model |

The mw side of this is lucky: mw redirects the session's stdout to
`result.json`, and `--output-format json` prints one object, the last result
message. So mw already has the right file — it was reading the wrong half of it.

## The evidence from the two runs

**mw-gq6.33** (`runs/mw-gq6.33/result.json`, VPS, 2026-09-19). Its final result
message carries `"origin": {"kind": "task-notification"}` and
`"result_index": 2` — the third result message of the run, produced when a
monitor woke the session. The session's own last words in `result` are *"Another
monitor timed out: the one waiting for `go test` to exit."*

| | `usage` | `modelUsage` (one model, `claude-sonnet-5`) |
| --- | ---: | ---: |
| input | 4 | 84 |
| output | 104 | 12,827 |
| cache read | 64,917 | 1,973,788 |
| cache write | 350 | 55,037 |
| **total** | **65,375** | **2,041,736** |

`num_turns: 1`, `duration_ms: 3625`, `duration_api_ms: 151065`. A file whose
wall clock (3.6 s) is a fortieth of its own API time (151 s) is telling you
plainly that the two are not measuring the same span.

**mw-rk4.1** (`runs/mw-rk4.1/result.json`, same day). No `origin`,
`result_index: 0` — one result message, no wake-ups, no subagents. Here `usage`
and `modelUsage` agree to the token: 18 / 5,342 / 281,094 / 27,628, total
314,082. `num_turns: 12`, `duration_ms: 261451`, `duration_api_ms: 54040`.

### The cost proves which figure is the total

Claude Code computes `total_cost_usd` locally from a bundled price table. Fit
the rates against mw-rk4.1's `modelUsage` and they come out round — $2.00/M
input, $10.00/M output, $0.20/M cache read, $4.00/M 1-hour cache write — and
they then reproduce **both** files' cost to the last printed digit:

| Run | figure used | computed | file says |
| --- | --- | ---: | ---: |
| mw-rk4.1 | `modelUsage` | 0.2201868 | 0.2201868 |
| mw-gq6.33 | `modelUsage` | 0.7433436 | 0.7433436 |
| mw-gq6.33 | `usage` | 0.0154314 | — |

So `total_cost_usd` is computed from `modelUsage`, and mw-gq6.33's $0.74 bought
2.04 M tokens, not 65 K. That is the whole puzzle: the cost was right all along
and the token count beside it was the last three seconds of a fifteen-minute
session.

(The rates are a fit, not a quote. Claude Code's documentation warns that both
`total_cost_usd` and `costUSD` are client-side estimates from a price table
bundled at build time, "not authoritative billing data". The fit is here as
evidence that the two fields are computed from each other, not as a price list.)

## What mw does about it

`application/fuel.go` now reads `modelUsage`, summed across every model the
session used, and falls back to `usage` only when a file has no `modelUsage` at
all. `SessionResult` carries three new facts:

- `WholeSessionFuel` — whether `Fuel` came from `modelUsage`.
- `Woken` — whether `result_index > 0`, i.e. this result message is not the
  run's first, so `Turns` and `Duration` start from the last wake-up.
- `APIDuration` — `duration_api_ms`, the only whole-run length in the file.

The ledger's fuel column says which figure each number is, so nobody has to
come back to this page to read a line:

```
2,041,736 tokens (in 84 · out 12,827 · cache read 1,973,788 · cache write 55,037), whole session, 1 turn after the last wake-up, $0.74, 2m of API time
314,082 tokens (in 18 · out 5,342 · cache read 281,094 · cache write 27,628), whole session, 12 turns, $0.22, 4m
```

A woken session shows API time rather than `duration_ms`, because `duration_ms`
there is the last turn's and reads as a fifteen-minute session that took three
seconds. Wall-clock length for the whole session is **not in the result file at
all**; the only honest whole-run length it holds is API time, which is less
than wall clock because a session sits idle while a command runs.

## What is still not known

- Whether the earlier result messages of a woken run are written anywhere mw
  can reach. They are not in `result.json` (only the last is printed), so the
  session transcript JSONL would be the only source, and mw does not read it.
  It would not add much: `modelUsage` in the last message already carries the
  run.
- `result_index` and `origin` are not in the published schema. They were
  present on every real `result.json` in the vault when this was written, and
  mw reads them leniently: an absent `result_index` is 0, which means "not
  woken", which is the safe reading.
- Whether `num_turns` counts turns since the run started or since the previous
  result message cannot be told apart from one sample of 1. Either way it is
  not the session's turn count for a woken run, which is why the ledger says
  "after the last wake-up" rather than trusting it.

## Sources

- https://code.claude.com/docs/en/agent-sdk/cost-tracking — field scopes,
  subagent table, "account from `total_cost_usd` or `modelUsage` rather than
  `usage`", the client-side-estimate warning.
- https://code.claude.com/docs/en/headless — background tasks and Monitor
  watches holding a `-p` run open, and Claude responding to what a watch reports.
- `runs/mw-gq6.33/result.json` and `runs/mw-rk4.1/result.json` in the vault.
  Transcriptions of both, faithful in every field mw reads, are in
  `application/testdata/`.
