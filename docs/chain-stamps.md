# Chain stamps

A chain stamp is a record on the blockchain that says a rig's branch held a
commit, without saying which rig or which commit. Anyone who later holds the rig
name and the commit id can show the stamp was on chain by its block's time; no one
else can read what it stamps. Today it is testnet only. The record kind is built
and verified in code (`domain/stamp.go`, `application/stamp.go`); the queue, the
job and `mw prove` are below.

## What gets stamped

- Each commit `mw next` lands on a story's target branch: one stamp, queued after
  the push succeeds, never blocking the landing.
- Each vault push of `mw sync` that sent at least one commit (rig `vault`; below).

Nothing else: no git hooks, no commit that is not a landing or a vault push.

## The record

The payload is a postern record (docs/protocol.md section 1 of
github.com/Jonathan-A-White/postern) of kind `stamp`, framed as `nftgate`
version 1 like a message. Fields, in this order:

| Field | In the clear | What it holds |
| --- | --- | --- |
| `v` | yes | Always 1. |
| `kind` | yes | Always `stamp`. |
| `to` | yes | The Governor's compressed public key, hex. |
| `from` | yes | The sender's compressed public key, hex. |
| `ts` | yes | Unix seconds, when the sender built it. |
| `commitment` | yes | Lower-case hex SHA-256 of the rig, a newline, then the full commit id. |
| `ct` | no | The body, sealed to `to` by the same Cipher as a message's text. |

The body is JSON: `rig`, `branch`, `commit`, `story`, `title` (the story's
title), `host`, `at` (UTC, RFC 3339). Nothing of it but the commitment is in the
clear, so the chain shows neither the rig nor the commit.

## The queue and the sent record

Both live in the state directory's `stamps/`, `~/.local/state/mw/stamps/` (setting
`stamps_dir`, `$MW_STAMPS_DIR`), on the host that landed:

| File | Holds |
| --- | --- |
| `pending.jsonl` | One line per stamp not yet broadcast: the `stamp` (rig, branch, commit, story, title, host, `at`) and, once a try failed, `attempts` and `last_error`. |
| `sent.jsonl` | One line per broadcast stamp: the same `stamp`, its `txid`, `sent_at` and `attempts`. |
| `lock` | A flock held while either file is rewritten. |

`sent.jsonl` is the only place the rig name and commit sit beside the txid; it is
what `mw prove` reads, so a stamp is provable from the host that sent it.
The port is `StampQueue` (application/chainstamp.go), the adapter
infrastructure/stampqueue.

## The job

`chain-stamp` is a follower job (docs/events.md), every minute on its own clock. A
pass takes each pending stamp, seals it (`SealStamp`), signs and broadcasts it
through the Postern backend as a public chain record, moves it to `sent.jsonl`
beside its txid, and comments `STAMP <txid> for <commit> (testnet)` on the story
(a stamp with no story, a vault push, comments on none). A stamp the backend will
not take stays pending with its failed tries counted and is retried the next
minute; a pass never fails for it.

## Vault pushes

`mw sync` queues a stamp after every vault push that sent at least one commit:
rig `vault`, the vault's branch, the pushed head commit, its subject as the
title, and no story, so the job comments on none and the txid is only in
`sent.jsonl`. A queue that will not take it is a `chain stamp: not queued` note
on the sync's line; the sync stands.

## The git note

When the chain-stamp job has broadcast a stamp and this host has a checkout of
the stamp's rig (the `[rigs]` table of the config), it runs
`git notes --ref=chain add -f -m <txid> <commit>` there, so `git log
--notes=chain` shows each stamped commit's txid (notes ref `refs/notes/chain`,
local to the checkout; nothing pushes it). A note that cannot be written is said
on the job's stderr and never un-sends the stamp.

## Proving

`mw prove <rig> <commit>` (the commit in full or by its first characters) finds
the sent stamp in `sent.jsonl` and asks WhatsOnChain's testnet API
(`https://api.whatsonchain.com/v1/bsv/test/tx/hash/<txid>`) about it. It prints
the txid, the block's height and time (UTC), the preimage (rig and commit), the
commitment and `https://test.whatsonchain.com/tx/<txid>`; a transaction with no
block yet is "in the mempool, no block yet". A commit with no sent stamp is
`no stamp for <rig> <commit>` and exit status 1. A stamp still pending is not
sent, so it is not found.

## Verifying

Whoever holds a rig name and a commit id recomputes the commitment and compares
(`domain.Stamp.Verify`). `application.OpenStamp` also refuses a sealed body whose
rig and commit do not make the commitment shown beside it.

## Limits and what mainnet needs

Testnet only. The factory's testnet key pays the fee (1 sat/kB, no money). The
network is fixed in code: testnet addresses in infrastructure/postern/transaction.go
and record.go (`AnchorAddress`), the testnet WIF in infrastructure/postern/keyfile.go,
the testnet WhatsOnChain endpoints in application/prove.go (`ExplorerURL`) and
infrastructure/chainlookup. A testnet stamp is a trial: it proves nothing a
mainnet block would.

Mainnet is a later story and needs three things:

1. A funded mainnet key: a key file whose address holds real coins, and a backend
   that serves its UTXOs and broadcasts to mainnet.
2. The network switch: the testnet address and anchor in transaction.go and
   record.go, the key's WIF prefix, and the explorer and API URLs of `mw prove`
   made mainnet, with stamps already sent staying marked testnet.
3. The Governor's word. Real coins are spent on every stamp.
