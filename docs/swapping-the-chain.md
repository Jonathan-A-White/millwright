# Swapping the chain

Millwright puts a few records on a public chain today: BSV testnet. This page says
where that is cut off from the rest of the code, what a replacement must do, and
what to switch off if you want no chain at all. It describes what is in the code
now.

## What mw uses the chain for

- **A public copy of a post.** `mw postern send --chain` (and a reply to a post that
  itself came by chain) also puts the message on chain, sealed, with no summary.
- **A call that reaches a phone when the backend cannot.** `mw talk call --chain`.
- **The events batches' chain lane.** The follower puts each batch on chain beside
  the direct line, within a daily cap.
- **Stamps and `mw prove`.** [Chain stamps](chain-stamps.md) are queued by every
  landing and vault push, sent by the follower's `chain-stamp` job; `mw prove` looks
  the transaction up.

**Not the chain, and they stay:** the postern key and its signatures, the sealing of
every body to the Governor's key, direct delivery to the backend, and the event log.
A factory with no chain keeps all of them.

## The seam

One interface, `Chain` in [`application/chain.go`](../application/chain.go). Every
use case that touches a chain holds one and nothing else.

| Method | What a caller relies on |
| --- | --- |
| `Send(ctx, payload)` | Puts `payload`, a postern record as JSON, on the chain as one record, paid for by the postern key, and returns its transaction id. An error means nothing went. |
| `Balance(ctx)` | The postern key's balance in satoshis, confirmed and unconfirmed together. A send is refused before `Send` when it is over `postern_float_sats`. |
| `Tx(ctx, txid)` | Returns a `ChainTx`: `Known` false if the chain has not heard of it, `Height` 0 while it waits in the mempool, `Time` the block's UTC time. |

The implementation is chosen in one place, `newChain` in
[`cmd/mw/chain.go`](../cmd/mw/chain.go); every command calls it. Today it returns
`bsv.New(...)`. The worked example of a second implementation is the in-memory
[`application/apptest/fakechain.go`](../application/apptest/fakechain.go), which the
tests use in place of BSV.

Two things sit outside the port: the explorer link `mw prove` prints is built from
`ExplorerURL` in [`application/prove.go`](../application/prove.go), and the stamp
and message record formats are postern's, not the chain's.

## The chain files

- [`application/chain.go`](../application/chain.go): the port and `ChainTx`.
- [`cmd/mw/chain.go`](../cmd/mw/chain.go): the one constructor.
- [`infrastructure/bsv/bsv.go`](../infrastructure/bsv/bsv.go): the BSV adapter; it
  signs from the key's coins and has the backend broadcast.
- [`infrastructure/chainlookup/chainlookup.go`](../infrastructure/chainlookup/chainlookup.go):
  WhatsOnChain, behind `Tx`.
- [`infrastructure/postern/keyfile.go`](../infrastructure/postern/keyfile.go) and
  [`infrastructure/postern/http.go`](../infrastructure/postern/http.go): the key's
  signing and the backend's coin calls, which `bsv` uses.
- [`application/chainstamp.go`](../application/chainstamp.go),
  [`application/stamp.go`](../application/stamp.go),
  [`domain/stamp.go`](../domain/stamp.go),
  [`infrastructure/stampqueue/stampqueue.go`](../infrastructure/stampqueue/stampqueue.go)
  and [`application/prove.go`](../application/prove.go): stamps.
- [`application/eventship.go`](../application/eventship.go): the events chain lane.
- [`application/apptest/fakechain.go`](../application/apptest/fakechain.go): the fake.

## Another chain

1. Write a type that implements `Chain`, in its own package under `infrastructure/`.
   Copy the shape of `infrastructure/bsv` and its test.
2. `Send` must return an error and spend nothing when it cannot put the record on
   chain: callers keep a stamp or a batch and try again later.
3. `Balance` must answer in satoshis, or in whatever unit you set `postern_float_sats`
   in. If your chain has no float to cap, return 0.
4. `Tx` must answer `Known: false` for an unknown id, not an error; an error means
   the lookup itself failed.
5. Change the one `return` in `newChain` to build yours.
6. If your explorer is not WhatsOnChain, change `ExplorerURL` in
   `application/prove.go`.
7. Run `make test` and `make lint`. The use cases' tests run against the fake, so
   they do not exercise yours: give it its own tests.

## No chain

Without the chain, mw's other features work the same. Two settings keep mw from
calling the port in the usual run:

- `postern_channel = "direct"` (or `MW_POSTERN_CHANNEL=direct`). Messages, cards and
  hands go straight to the backend. The chain is still used if you pass `--chain`,
  or if the Governor's newest record came by chain.
- `[events]` `chain = false` in `~/.config/mw/config.toml`. Every batch goes direct
  only, as the fallback lane does.

Stamps have no setting: each landing and vault push queues one, and the follower's
`chain-stamp` job calls `Send` once it has a key. To have no chain, supply an
implementation whose methods answer plainly:

- `Send` returns an error such as "no chain is configured". The stamp job then keeps
  each stamp pending and says so once a minute; nothing else is hurt.
- `Balance` returns 0 and no error.
- `Tx` returns `ChainTx{}` and no error, which `mw prove` prints as not known yet.

Stamps and `mw prove` then have nothing to show. Leave `postern_governor_key` unset
and the stamp job fails its pass before it calls the port.

For the other side of this, Postern has a page of the same name:
<https://github.com/Jonathan-A-White/postern/blob/main/docs/swapping-the-chain.md>.
