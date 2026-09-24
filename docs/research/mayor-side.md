# Research: the Mayor's side — reading and sending postern messages, the key on the VPS, waking on a message

Answers mw-f758y.9. Builds on mw-f758y.3's `messages.md` (the BRC-78 envelope, the on-chain wire
format, and its case for one backend poller) and mw-f758y.5's `notifications.md` (class-tagged push
payloads) — read in `/home/jwhite/postern/docs/research/`. Neither is redone here; this doc only
carries their recommendations to the Mayor's own seat, which is mw on the VPS, not a browser.
Read-only research: no product code changed in either rig. Testnet only throughout; no real key,
phrase or secret appears below, and none should ever be pasted into a bead or a commit either.

## Recommendation, up front

- **mw talks to the desktop's Go backend over the VPN, never to WhatsOnChain directly.**
  `messages.md` §4 already settled this for the Governor's phone (one poller, so the shared 3 req/s
  WhatsOnChain ceiling isn't spent twice and a restart doesn't re-scan history); the same argument
  applies unchanged to the Mayor's seat. A `mw postern inbox` that polled WhatsOnChain itself would
  be a *third* independent poller against the same limit for no benefit, and would need to keep its
  own cursor that the backend already keeps. This makes the OpenVPN link (mw-f758y.8, still
  DEFERRED) a real prerequisite of this work, not a nice-to-have — see "no throwaway work" below.
- **mw itself does the cryptography — no Node subprocess.** `github.com/bsv-blockchain/go-sdk` is
  BSV's own Go SDK and its test suite exercises BRC-78 `Encrypt`/`Decrypt`/`Verify` round-trips
  directly (confirmed by reading its PR #354 test list), so mw can build and parse the same
  `EncryptedMessage` envelope `messages.md` recommends, and sign the funding P2PKH transaction,
  in Go. This keeps mw a single static binary; the alternative (shelling out to a small Node script
  wrapping `@bsv/sdk`) adds a second language runtime to the VPS for one command, for no capability
  Go doesn't already have.
- **The Mayor's key is a plain WIF file on the VPS, 0600, never in the git vault** — the same class
  of host-local, never-synced file `.mayor-acting` already is. Its safety comes from the standing
  constraint the epic already states ("the Mayor's key never holds more than the float he gives
  it," testnet until he says otherwise), not from encryption-at-rest infrastructure: the VPS has no
  TPM, so `systemd-creds` would only wrap the key with a key stored next to it on the same disk —
  no real improvement over a plain file for the threat this key is actually exposed to, at the cost
  of a mechanism to build and operate. This is a judgment call, not a fact, and is called out again
  under "for the Governor to decide."
- **Waking the Mayor extends `contrib/mail-notify`, rather than adding a new timer.** That script
  already does exactly the job a new postern message needs — find the Mayor's live tmux pane from
  `.mayor-acting`, skip everything under load, `flock` against a second tick, type a line only when
  the pane is idle — every minute. A postern message is the same kind of event `bd mail inbox` is,
  arriving on a different transport; teaching the one script to also poll `mw postern inbox` is a
  few added lines, not a second timer unit, a second `flock`, and a second idle-pane check to keep
  in sync with the first.
- **A grilling question travels as a normal encrypted message with an unencrypted `class` tag
  alongside it in the on-chain JSON**, e.g. `{"kind":"msg","class":"decision-needed","ciphertext":
  "...","ts":"..."}` next to `messages.md`'s `kind:'msg'` shape. This is a real, open tradeoff, not
  a settled fact: the backend cannot choose a Web Push urgency or notification class for content it
  cannot decrypt, so classifying *before* the Governor opens the app means the class label itself is
  visible to anyone reading the chain (a small metadata leak: "the Mayor sent something tagged
  decision-needed at time T", never the content). The alternative — no classified push until the
  message is fetched and decrypted client-side — defeats the point of a distinct "buzzes and stays
  until dismissed" class from `notifications.md` §5. Flagged for the Governor in the story list
  below rather than decided here.

## 1. How the Mayor's mw reads and sends

`messages.md` already gives the backend a job: watch one anchor address, index `nftgate` records by
`kind`, hold a durable cursor. Its own open item #3 says the backend's "REST/websocket surface...
follows from mw-f758y's... 'The Mayor's side'" — this is that surface, from the consumer's end:

- **`mw postern inbox`** — a new command, shaped like `mw mail inbox` (`application/mail.go`,
  `cmd/mw/mail.go`): a use case with one port, `application/postern.go`'s `PosternInbox` (or
  whatever it ends up named), backed by `infrastructure/postern/`'s HTTP client — `net/http`, the
  same `*http.Client` shape `infrastructure/watch/watch.go` already uses for reachability checks,
  pointed at the backend's `GET /messages?since=<cursor>` over the VPN. It gets back envelopes
  (sender pubkey, ciphertext, `ts`, and now `class`) still encrypted; decryption happens in mw
  itself, with the Mayor's private key, never on the desktop. The backend never holds or needs the
  Mayor's key — it only ever handles ciphertext and public metadata, same as it does for the phone.
- **`mw postern send <to> -m <body> [--class <class>]`** — builds the BRC-78 envelope
  (`go-sdk`'s `EncryptedMessage.Encrypt`, sender = the Mayor's key, recipient = the Governor's known
  public key, which the vault or config would hold, not a secret), wraps it in the version-1
  `nftgate` record `messages.md` §2 defines, builds and signs the funding P2PKH transaction with the
  same library, and POSTs the raw tx to the backend's `POST /broadcast` (or straight to
  WhatsOnChain's own broadcast endpoint — broadcasting is a one-shot write, not a poll, so it does
  not carry the "one poller" argument that reading does; either is defensible, and it is a small
  enough decision to leave to whichever story implements it).
- Both commands need `MW_SEAT=mayor` the way `mw mail` does, and read the same kind of
  config the [watch]/[doctor] tables already model (a `[postern]` table: backend URL, anchor
  address, key file path) rather than new environment-variable sprawl — `infrastructure/config`
  already has the shape for this (`docs/adding-a-command.md`'s "Port method" and "Cobra" steps).

## 2. The Mayor's key on the VPS: what it is, where it lives, what limits it

A single testnet BSV private key (WIF), generated once, held as a plain file under the Mayor's own
home directory on the VPS (`~/.config/mw/postern-key`, 0600, owned by the seat's OS user) — never
committed, never synced by `mw sync`, exactly the way `.mayor-acting` is host-local free text today.
`infrastructure/config`'s existing `valueIn`/`tableIn` machinery can point a `[postern]` table at its
path without the path itself being a secret.

What limits it, in order of how much work each is:

1. **The float itself.** The epic's own standing constraint: "the Mayor's key never holds more than
   the float he gives it" — his decision, his amount, topped up by hand on testnet (a faucet) and
   later, only on his word, on mainnet. This is the real limit; everything below is defence in
   depth for a key that by design is never worth much.
2. **Testnet until he says mainnet.** No code path this doc proposes touches mainnet; a `[postern]
   chain = "testnet"` setting (or reusing whatever `chainConfig` postern's frontend already has) is
   the only gate, and it should refuse to run on `mainnet` without an explicit, separately-reviewed
   change — not an env var a Builder could flip by accident.
3. **File permission, not encryption.** 0600 stops another OS user reading it; it does not stop
   root or a compromised mw process, which is the same exposure every other file mw reads on this
   host already has (the vault's own git credentials, for one). Weighed against `systemd-creds`
   (LoadCredentialEncrypted=) below.

**Weighed and set aside:** `systemd-creds` wraps a credential with a key systemd manages, normally
backed by a TPM; the VPS is a plain cloud VPS with no TPM, so `systemd-creds` falls back to a key
kept in `/var/lib/systemd/credential.secret`, on the same disk as the credential it wraps — a real
step (learning the tool, wiring `LoadCredentialEncrypted=` into a new or existing unit, a decrypt
step in mw's own startup) for a security property that reduces to "another file on the same disk,"
given this host's hardware. Worth revisiting only if the VPS ever changes.

## 3. Waking the Mayor: extend `mw-mail-notify`, don't build a new timer

`README.md` "Telling the Mayor's window when mail arrives" already describes the mechanism a new
postern message needs verbatim: find the live pane from `.mayor-acting`, skip the tick under load,
`flock` a run against a second one, type a fixed-shape line and Enter only when the pane is idle and
not mid-turn, remember what has already been announced so it is said once. The only new work is a
third check beside `bd mail inbox` and `mw nudge`: `mw postern inbox --unread-count` (or an
equivalent quiet query that changes nothing, the way `mw mail inbox` itself only lists), compared
against what the script has already announced, typing `New postern message from <n>: <count>. Run
mw postern inbox.` on the same terms as the existing `New mail for mayor: <n> message(s)` line. This
keeps one timer, one lock, one idle check, one place that knows what "the Mayor's live pane" means —
the "few moving parts" reading of this fits `contrib/mail-notify`'s own shape better than a sibling
`mw-postern-notify` timer would.

## 4. Classified grilling questions

`notifications.md` §5's four classes (decision needed, landing to verify, alarm, message) are
proposed for the Governor's phone; a grilling question from the Mayor is the "decision needed" class
read the other way — same urgency, same "vibrate, stays until dismissed" shape, opposite direction.
Concretely: `mw postern send governor -m "<question>" --class decision-needed` puts `class:
"decision-needed"` in the same JSON push described in §1, and the backend, seeing that class on a
new record (without decrypting it — it cannot), sends a VAPID push with
`{"class":"decision-needed", ...}` per `notifications.md` §4's `payloadJSON`, letting the service
worker apply `vibrate` + `requireInteraction: true` before the Governor has opened the app at all.
The tradeoff — a class label visible on-chain to anyone watching the anchor address, in exchange for
a notification classifiable before decryption — is real and belongs to the Governor's decision, not
this doc's.

## 5. Options weighed

| Decision | Chosen | Alternative | Cost of the alternative |
|---|---|---|---|
| mw reads via backend vs. WhatsOnChain directly | via backend | direct WhatsOnChain polling from the VPS | a third independent poller against the shared 3 req/s ceiling; mw would need to keep its own cursor the backend already keeps — pure duplication |
| Crypto in Go vs. a Node helper | Go (`bsv-blockchain/go-sdk`) | shell out to a small Node script wrapping `@bsv/sdk` | a second language runtime and its own dependency tree on the VPS, for capability the Go SDK already has |
| Key storage | plain 0600 WIF file | `systemd-creds` | real setup work for a security property this TPM-less host can't actually deliver |
| Wake mechanism | extend `mw-mail-notify` | a new `mw-postern-notify` timer | a second `flock`, a second idle-pane check, a second place that reads `.mayor-acting`, to keep in step with the first forever |
| Broadcasting a sent message | backend relay or direct to WhatsOnChain (either) | — | negligible either way: a one-shot write is not the shared-poller problem reading is |

**Cost in tokens**: this doc; a Builder story to add the `Postern` port/use case/adapter/command
(shaped like `mw mail`, `docs/adding-a-command.md`'s eight steps); a Builder story on the postern
side for the backend's `/messages` and `/broadcast` endpoints; a small story to extend
`mail-notify`. None of this is large by the factory's usual story size.

**Cost in sats (testnet)**: negligible and already measured by `messages.md` — a short reply is
≈1 sat at today's accepted testnet fee rate, worst case a few sat for a long message; nothing here
changes that table, since the Mayor's send is the same transaction shape.

**Moving parts added**: one Go dependency (`bsv-blockchain/go-sdk`) in millwright; one new adapter
package (`infrastructure/postern/`); two new backend HTTP endpoints in postern's `server/`; a few
added lines in an existing script. No new timer, no new daemon, no new secret-management system.

## 6. Sources

- `/home/jwhite/postern/docs/research/messages.md` (mw-f758y.3) — envelope, wire format, one-poller
  argument, all reused above without change.
- `/home/jwhite/postern/docs/research/notifications.md` (mw-f758y.5) — class-tagged Web Push
  payloads, the four proposed classes.
- `/home/jwhite/postern/docs/research/bsv-library.md` (mw-f758y.4) — how postern consumes
  SpellForge's TypeScript BSV library; read for contrast with the Go-side option above, which needs
  none of it, since mw is not a consumer of `src/bsv`.
- `/home/jwhite/postern/src/gate/Gate.tsx`, `src/gate/index.ts`, `src/App.tsx`, `src/key/KeyVault.tsx`
  — the gate's current always-locked placeholder and the key screen reachable only at
  `/?screen=key`, read to write the story list below.
- `/home/jwhite/postern/server/cmd/postern/main.go` — the Go backend today: one `/healthz` handler,
  nothing else, confirming the message-index endpoints proposed above do not exist yet.
- millwright `README.md` "Mail" and "Telling the Mayor's window when mail arrives"; `application/
  mail.go`; `application/millhandtick.go`; `infrastructure/config/config.go`; `infrastructure/
  watch/watch.go`; `docs/adding-a-command.md`; `docs/codemap.md` — the existing patterns this doc
  extends rather than invents.
- BSV Go SDK BRC-78 support: [bsv-blockchain/go-sdk PR #354](https://github.com/bsv-blockchain/go-sdk/pull/354)
  (adds round-trip and failure-path tests for BRC-78 `Encrypt`/`Decrypt`/`Verify`, confirming the
  SDK implements the envelope `messages.md` recommends); [bsv-blockchain/go-sdk](https://github.com/bsv-blockchain/go-sdk)
  itself. Not independently built or run here — read only to confirm the capability exists before
  recommending Go over a Node helper; whoever picks up the implementing story should still spike it
  first, the same caution `bsv-library.md` gives its own npm option.

## PROPOSED STORY LIST for mw-f758y.10

Rough order; `postern` and `millwright` stories interleave because the Mayor's mw command and the
backend endpoints it calls are each other's dependency. mw-f758y.8 (the OpenVPN network) is not
today a formal dependency of mw-f758y.10 in beads, but every story below that mentions "over the
VPN" cannot be verified without it — the Governor's "no throwaway work" instruction means this
should become a real `bd` dependency, not an assumed one, before any of them starts.

1. **[postern] Serve a message index from the Go backend** — `GET /messages?since=<cursor>` and
   `POST /broadcast`, backed by one poller of the anchor address `messages.md` already specced;
   ciphertext in, ciphertext out, no key ever touches this process.
2. **[postern] The licence check the gate has never made** — read the testnet License NFT for the
   phone's known public key and show the locked screen only when it is actually absent; today
   `src/gate/Gate.tsx` always says locked regardless of licence state.
3. **[postern] Route first use to the key screen, not a query string** — `App.tsx` currently only
   reaches `KeyVault` at `/?screen=key`, typed by hand; an installed app with no key yet should land
   there itself before the gate is ever shown.
4. **[millwright] Add the `Postern` port, use case and HTTP adapter** — `mw postern inbox` /
   `mw postern send`, shaped like `mw mail` (`docs/adding-a-command.md`'s steps), talking to story 1
   over the VPN; `bsv-blockchain/go-sdk` does the BRC-78 envelope and the signed P2PKH transaction.
5. **[millwright] Give the Mayor's mw a testnet key** — generate it, fund it with a float the
   Governor sizes and sends by hand (a testnet faucet), store it as a 0600 file on the VPS outside
   the vault, wire `[postern]` config to it.
6. **[millwright] Wake the Mayor on a new message** — extend `contrib/mail-notify` to also poll
   `mw postern inbox`'s unread count and type a third line, on the same lock and idle-pane check the
   mail lines already use.
7. **[postern] Classified push for a grilling question** — the Go backend sends a VAPID push
   carrying `class` from an unencrypted on-chain field (open question above: the Governor should
   confirm he accepts that class label being visible on-chain before this ships) and the service
   worker applies `notifications.md` §5's per-class options.
8. **[postern + millwright] The demo** — the Governor sends one encrypted text from his phone; the
   Mayor's `mw postern inbox` reads and decrypts it; `mw postern send` answers; the Governor's app
   shows the reply and, for a `decision-needed` class, buzzes.

## For the Governor to decide

- Whether a `class` tag riding in the clear on-chain (§4) is an acceptable metadata leak, or whether
  classified push waits until the recipient has decrypted (losing the "buzzes before you open the
  app" property).
- The size of the Mayor's testnet float, and when (if ever) to fund a mainnet one — his decision per
  the epic's own standing constraint, not proposed here.
- Whether the plain 0600 WIF file is an acceptable resting place for the Mayor's key given this
  VPS's hardware (§2), or whether he wants `systemd-creds` anyway despite its reduced benefit here.
- Whether to broadcast a sent message through the desktop backend or straight to WhatsOnChain from
  the VPS (§1 last bullet) — a small decision, left open since either is defensible.
