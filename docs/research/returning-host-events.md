# Research: what a returning host reads to catch up

Answers mw-6ww.83, for the parked grilling mw-6ww.70. The Governor, 2026-10-03: "use bsv to
communicate, so if a machine comes back on it's own or with a dr, it can see what happened and
not get confused." Read-only research: no product code changed.

## What exists today

- **One log, one file, one writer.** `~/.local/state/mw/events/log.jsonl` on the home, written
  only by the home's follower (`cmd/mw/events.go:83`). `Append` numbers each event under a flock
  (`infrastructure/eventlog/eventlog.go:52-107`); `Since(seq)` is a linear scan
  (`eventlog.go:160-187`). Ours holds 9,925 events, 1.6 MB, in about 44 hours (2 days): 6,039
  `bead_changed`, 2,808 `job`, 827 `mail`, 30 `handover`, 5 `control`. Read raw it is about 400k
  tokens; a day's share is about 200k. A seat must never read it raw.
- **Every reader keeps its own cursor and starts at the head.** The follower's `follow.json` is a
  bd-audit time, not a seq (`application/eventfollow.go:77`), and its first run appends nothing
  (`:98-103`, `:398`). The nudger's seat cursors (`eventnudge.go:113`), the control cursor
  (`eventcontrol.go:138`) and the springer (`eventspring.go:176-181`) all start at the head, so a
  cancel "from before it began is history" (`docs/events.md:129`). The shipper's `ship.json` holds
  `Shipped`, the fallback batches still to go on chain, and the emergencies sent out of order
  (`eventship.go:36-52`).
- **Nothing crosses hosts.** A Boost's dispatch reads its own host's log (`dispatch.go:436`), so
  a pause or cancel written on the home never reaches it: "Only the home's log is read"
  (`docs/events.md:133`; `README.md:1133`).
- **The Governor's `events` records cannot be read by a host.** `seal` encrypts to
  `GovernorKey` (`eventship.go:379,389`), `Cipher.Encrypt` takes one recipient
  (`postern.go:223-226`), and the inbox skips every record whose `To` is not its own key
  (`postern.go:1331`). `Messages(since)` does return every indexed record, so a host can fetch
  them (`infrastructure/postern/http.go:97`), just not open them.
- **Seqs are not unique across homes.** A move onto a dead home starts a fresh log at seq 1, and
  "an app that reads by seq must start again from 0" (`docs/home-move.md:289-298`). Events carry
  no log id, so a returning host holding seq 9,000 cannot tell a restored log from a new one.
- **Stories already catch up from beads.** A dead pane with a lapsed lease is reclaimed, a
  landed one closed (`dispatch.go:1127-1176`); a handover also lands in the vault's acting file
  (`docs/events.md:87-93`) and a cancel in the story's run label (`events.md:129-131`).

## What is only in the log

Bead changes, cards, mail, messages and `hands_ran` are rebuilt from beads (`eventfollow.go`'s
feed is bd's audit). Only three things are log-only: **pause-host / resume-host** (folded from the
whole log on every dispatch, `eventcontrol.go:38-60`), **open emergencies** (an alarm and its
`clears`, `events.md:213`), and **job passes** (informational). So "what happened while I was
gone" is two levels, paused or not and which alarms are open, plus whatever beads hold.

**What a returning host must not replay:** a `cancel` (kills a session), a `handover` (ends a
`mw events wait`, `events.md:90`), a `job scheduled` (springs a pass), an emergency (a second
alarm, a second chain record), a `mail` (a second nudge). These are edges: replaying them acts
twice. A signed hands step is guarded by its own `used` file, not by the log
(`windows-hands.md`), and stays out of scope.

## Options

**A. Beads only.** The returning host reads beads and the vault and nothing else; the two log-only
levels move to bd kv notes (`pause.<host>`, `alarm.<seq>`). Fuel: zero. Parts: two notes, one
reader. Fails when beads cannot be reached, which is the dead-home case the Governor named, and
whether kv notes ride the Dolt push to GitHub is unchecked. It is also not BSV.

**B. Pull the stream over ssh.** The host runs `mw events tail --since N` on the home
(`[hands_hosts]` ssh exists). Fuel: zero. Parts: a per-host cursor, a log id, ssh. Fails when the
home is dead, and replays edges unless the reader is built to skip them.

**C. Stream on BSV to the hosts.** Seal each batch to every host's key as well. Fuel: zero, but
satoshis multiply by the hosts (`chain_daily_cap` is 500 records a day), and `ship.json` (90 KB
already) needs a per-recipient copy, with its pending ranges and the out-of-order `Urgent` list.
Parts: a new class, a per-host sender state, a reader on each host, a cursor, gap fill from
`from`/`to` ranges, dedupe by (log id, seq). Full fidelity, and the most moving parts. Every
replay hazard above comes back through the reader.

**D. Snapshot on BSV.** The home's follower folds the log into one small record, a level not a
stream: `{log id, head, home, paused hosts, open emergencies, ts}` (under 1 KB), and seals it to
each host's key. It goes direct and on chain on every change and on the `[events] heartbeat` (hourly).
A returning host fetches `Messages(since)`, opens the newest
snapshot signed by the home, and acts on levels. Fuel: zero tokens, since the fold and the read are
code; the seat that is told gets about 300 tokens, not 400k. Satoshis: roughly 25 records a day per
host plus changes, about 10 percent of the cap for two hosts. Parts: a fold (a pure function beside
`PausedHost`), one class, one reader, a log id file beside `log.seq`.

## Recommendation

**D**, built in three steps: (1) a log id written beside `log.seq`, new on every fresh log, which
also fixes the seq collision for the Governor's app; (2) the fold, tested against the real log of
9,925 events; (3) the seal-and-send and the reader. Edges never travel, so there is nothing to
replay by construction; gaps do not matter, because the newest snapshot wins and an old or
duplicate one is dropped by `ts`; a different log id means "start from this snapshot". Beads stay
the truth for stories (A's content), and BSV carries only what beads cannot, which is also what
works when the home is dead. B and C are rejected for their moving parts. Keep the log itself on
the home as it is.

## Questions only the Governor can answer

1. **Which facts must a returning host get from BSV rather than beads?** Recommended: pause or
   resume, open emergencies, and who is home. Stories, claims, cards and mail stay in beads.
2. **Whose snapshot may a host believe?** A host that returns after a home move must not obey the
   old home. Recommended: only a snapshot signed by the key of the host the vault's `home` file
   names, and a move signed by him.
3. **Chain or direct?** Chain is durable through a DR but costs satoshis. Recommended: both on
   every change and hourly (about 10 percent of the cap).
4. **After a DR with the log lost, may seqs restart at 1 under a new log id?** It changes the
   app's contract (protocol §22). Recommended: yes, new log id, and the app resets its cursor when
   it changes.
5. **What does a host do when it finds no snapshot newer than two hours?** Recommended: dispatch
   nothing, raise an alarm, and after the next heartbeat work from beads alone and say so.
