# Chain stamps

A chain stamp is a record that says a rig's branch held a commit, without saying
which rig or which commit. This page is a stub: the record kind is built and
verified in code (`domain/stamp.go`, `application/stamp.go`); sending one, and
the check that reads them back, come in the epic's later stories.

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
