# Research: search over every To do card, done ones and years back

Answers mw-va88wh.7 (the research question filed on mw-va88wh.1: "which store, which index,
which API, and the smallest first slice that finds an old address in under a second?"). His words,
Talk d1274929 turn 9: search "has to be solid, even of old things", and "very easy to talk to you
about". Read at millwright `5a576af` and postern `b9bbbbf`. A docs-only story: no product code
changed, the live beads database was only read, and the scripts that made every number below are
beside this file in `docs/research/todo-search/` (`gen.py` makes the fixture, `bench.py` times it).

## Inventory

Where the words of a card live, and what Postern's Search tab reaches today. A card is an open bead
labelled `hitl:todo` (postern `src/todo/lists.ts:9,152`). Live scale, read 2026-10-16 with
`bd sql`: 29,392 beads (27,747 closed), 11,901 comments (12.8 MB), 16.3 MB of descriptions, and
only **33 `hitl:todo` cards ever, 6 of them closed**. His board is small today; the whole store is
not, and "years back" means this table, not a shorter list.

| Where a card's words live | Where it is kept | What reaches the phone | Does Search find it? (file:line) |
| --- | --- | --- | --- |
| Title | bead `title` | in the view for every open card and for a closed one for 7 days (`application/posternview.go:29`, `:242`) | Yes: `src/model/search.ts:59-63` (id, title, summary, labels, path). Closed cards only while the view still lists them |
| Notes (the bead's description: his edit "notes" is the description, `application/posterntodo.go:72-74`; the `Open:` and `Text:` lines are in it, `src/todo/links.ts:97`) | bead `description` | only the **first 280 runes**, plain text, as `summary` (`application/posternview.go:61`, `:1688`; `src/todo/lists.ts:128`) | The summary, yes (`search.ts:62`). The rest only after he has opened the bead and its detail was stored (`:72-80`, `src/cockpit/SearchScreen.tsx:84`) |
| Comments | bd `comments` table | not in the view; one bead at a time through the bead detail ask (`GET /api/beads/{id}`, protocol §12), each cut at 16,000 runes | Only for beads he has opened (`search.ts:81-86`). mw itself reads comments for beads open or closed within 31 days (`infrastructure/beads/snapshot.go:63`) |
| The message that made it | the `todo_add` message (protocol §13) in the phone's message store and the server's opaque JSONL; the card only gets a comment "ADDED by the Governor via postern, txid …" (`application/posterntodo.go:64`) | the message is on the phone | Yes, every decrypted message (`search.ts:88-`), as a hit on its thread, not on the card |
| `todo_rank`, `todo_due`, `todo_important`, `todo_size`, `todo_then` | bead metadata (`application/todorank.go:15`, `tododue.go:16,20`, `todosize.go:18-19`) | as fields of the view bead (`application/posternview.go:204-215`) | No. `todo_then` is free text and is never searched |
| bd `notes` column and mw's own notes | `issues.notes` (6.4 KB across all 29,392 beads) and `config` rows `kv.*` (`snapshot.go:70`) | no | No, and not card words |
| A closed card | stays in beads forever; leaves the board at once (`src/todo/lists.ts:152`, `isClosed`) and the view after 7 days | gone after 7 days | Not found, unless he opened it once |

What the Search tab is, in one line: a substring test over what the phone already holds
(`search.ts:34`, `containsAll`), so an address written in a comment on a card closed last year is reachable
only if he opened that card. That is the gap.

## Candidates

Fixture: `gen.py` writes N cards as the JSONL `bd export` prints. Seed 7, created 2023-01 to 2026-10,
70% closed, 40% with one to three comments, an `Open:` line on 30%, labels `hitl` and `hitl:todo`,
a `todo_rank`. **Ten "old address" cards are planted**, closed between 2023-03 and 2025-04: six hold
the address in the description, four hold it only in a comment (Quarry Road, Tanner Mill Road,
Lantern Wharf, Salt Marsh Drive). Two fixtures, same generator:

- **2,400 cards** (3.2 MB; 993 with comments) for the candidates that must be built in a real bd.
- **30,000 cards** (13.6 MB of descriptions, 12.6 MB of comments, 12,017 commented cards), the size of
  the live store (16.3 MB and 12.8 MB), for the index.

Ten queries, the same for every run (phrases, as typed): `harbor lane`, `quarry road`, `04101`,
`juniper street`, `cobbler's lane`, `lantern wharf`, `address` (common word), `passport photo`
(common words), `forwarding address`, `zzqxv` (no match). **Cold** is the first run after the data
files were evicted from the page cache (`posix_fadvise DONTNEED`, no root needed); against the live
server the page cache cannot be evicted, so its "cold" column is just the first run. **Warm** is the
median of seven runs after it. Machine: this Builder's host (cloud2, 8 vCPU, 16 GB), Python 3.12.3's
SQLite 3.45.1, bd 1.3.0.

### (a) Beads' own store

Two ways in, both read-only.

**(a1) `bd query` on a fixture bd** (bd's embedded Dolt in a scratch directory, with a clean
environment so it cannot reach the live server):

```sh
python3 -I docs/research/todo-search/gen.py 2400 7 > /tmp/cards2400.jsonl
mkdir -p /tmp/fxbd /tmp/fxhome && cd /tmp/fxbd && git init -q .
env -i HOME=/tmp/fxhome PATH=/usr/bin:/bin bd init --prefix fx --non-interactive --quiet
env -i HOME=/tmp/fxhome PATH=/usr/bin:/bin bd import /tmp/cards2400.jsonl      # 35 s, 235 MB on disk
python3 -I docs/research/todo-search/bench.py bd /tmp/fxbd/.beads
# each query runs:  bd query 'title="Q" OR description="Q" OR notes="Q"' --all --limit 0 --json
```

| query | hits | cold ms | warm ms (median of 7) |
| --- | --- | --- | --- |
| `harbor lane` | 1 | 1205.6 | 433.6 |
| `quarry road` | 0 (comment only) | 481.2 | 429.5 |
| `04101` | 1 | 481.3 | 438.2 |
| `juniper street` | 1 | 498.6 | 440.5 |
| `cobbler's lane` | 1 | 534.4 | 456.8 |
| `lantern wharf` | 0 (comment only) | 490.4 | 444.0 |
| `address` | 39 | 509.3 | 443.5 |
| `passport photo` | 5 | 503.0 | 473.6 |
| `forwarding address` | 1 | 494.2 | 447.4 |
| `zzqxv` | 0 | 521.9 | 466.7 |
| **median of ten** | | **500.8** | **443.7** |
| **slowest** | | **1205.6** | **473.6** |

It works, it needs nothing built, and it is a half-second a query on 2,400 cards, of which nearly
all is the process starting and Dolt opening (an empty-result run costs the same). It cannot see
comments at all: `bd search`/`bd query`/`bd list` filter only title, description, notes (so two of
the ten planted addresses are unreachable). It has no ranking and no snippet. One more trap found
on the way: `bd query 'description=a AND description=b'` keeps only the **last** condition on a
field (checked: `description=zzzz AND description=lane` returns the cards that hold "lane").

**(a2) Dolt SQL over the served database** (the Home host's external server, `bd sql`, SELECT
statements only, against the live store itself, 29,392 beads and 11,901 comments):

```sh
python3 -I docs/research/todo-search/bench.py live /home/mw/millwright-vault
# each query runs:
#   select id from issues where lower(concat_ws(' ', title, description, notes)) like '%Q%'
#   union select issue_id from comments where lower(text) like '%Q%'
```

The collation is `utf8mb4_0900_bin` (`show create table issues`), so a plain `LIKE` is
case-sensitive: `lower()` is needed, and it makes the scan a full one. The live store holds none of
the planted cards, so its hits are only for `address`.

| query | hits (live) | first run ms | warm ms (median of 7) |
| --- | --- | --- | --- |
| `harbor lane` | 0 | 3672.8 | 3949.1 |
| `quarry road` | 0 | 3867.7 | 3631.5 |
| `04101` | 0 | 3468.3 | 3176.7 |
| `juniper street` | 0 | 3149.6 | 3279.8 |
| `cobbler's lane` | 0 | 3358.1 | 3265.4 |
| `lantern wharf` | 0 | 3282.3 | 3361.0 |
| `address` | 453 | 3821.6 | 3552.2 |
| `passport photo` | 0 | 3520.6 | 3359.6 |
| `forwarding address` | 0 | 3413.5 | 3687.2 |
| `zzqxv` | 0 | 3353.4 | 3221.0 |
| **median of ten** | | **3440.9** | **3360.3** |
| **slowest** | | **3867.7** | **3949.1** |

A bare `select 1` through `bd sql` costs 1.8 s from this host (bd start-up and the hop to
`10.88.0.2:3307`), so the scan itself is about 1.5 s. Run on the Home host the baseline would be
smaller; that was not measured. It reaches comments, but a full scan on every keystroke is out.
`MATCH ... AGAINST` needs a `FULLTEXT` index, which means writing to the live schema: not tried and
not to be.

Live probes made, all reads: the timing script above; `select status, count(*) ... group by status`;
`select count(*), sum(length(...)) from issues` and `from comments`; `show create table issues`;
`bd list --all --limit 0 --json` (12.0 s, 44 MB, the one read `Snapshot` makes,
`infrastructure/beads/snapshot.go:32`); `select issue_id, id, created_at, text from comments`
(5.0 s, 14.8 MB); `bd list --all --limit 0 --json --label hitl:todo` (2.4 s, 33 cards);
`... --updated-after 2026-10-15T00:00:00Z` (4.2 s, 3,057 beads in one day). Before and after:
`git -C /home/mw/millwright-vault status --short .beads` printed nothing both times; `bd dolt status`
says the server is running, unchanged; `dolt_status` lists one working change, the `config` table,
which is the factory's own notes being written all day (the bead count moved from 29,387 to 29,392
while this ran, by other sessions).

### (b) SQLite FTS5 kept by mw on the Home host

One file. One FTS5 row per text field of a card (title, description, each comment), the rowid being
`card number * 64 + slot` so a changed card's rows are one rowid range and a refresh needs no scan.
The table is `fts5(id UNINDEXED, kind UNINDEXED, body, tokenize='unicode61 remove_diacritics 2')`;
a query is `select id, kind, snippet(doc, 2, '[', ']', '…', 12) from doc where doc match ? order by
rank limit 20` (ranked, with a snippet).

```sh
python3 -I docs/research/todo-search/gen.py 30000 7 > /tmp/cards30000.jsonl
python3 -I docs/research/todo-search/bench.py fts /tmp/cards2400.jsonl
python3 -I docs/research/todo-search/bench.py fts /tmp/cards30000.jsonl
python3 -I docs/research/todo-search/bench.py fts /tmp/cards30000.jsonl trigram
```

2,400 cards (build 0.11 s, file 4.6 MB):

| query | hits | cold ms | warm ms (median of 7) |
| --- | --- | --- | --- |
| `harbor lane` | 1 | 2.3 | 0.1 |
| `quarry road` | 1 | 1.9 | 0.1 |
| `04101` | 1 | 1.7 | 0.0 |
| `juniper street` | 1 | 1.8 | 0.1 |
| `cobbler's lane` | 1 | 2.1 | 0.1 |
| `lantern wharf` | 1 | 1.9 | 0.1 |
| `address` | 20 (capped) | 5.5 | 0.2 |
| `passport photo` | 10 | 4.8 | 0.4 |
| `forwarding address` | 2 | 2.2 | 0.1 |
| `zzqxv` | 0 | 1.3 | 0.0 |
| **median of ten** | | **2.0** | **0.1** |
| **slowest** | | **5.5** | **0.4** |

30,000 cards, the live store's size (build 1.5 s, file 58.3 MB):

| query | hits | cold ms | warm ms (median of 7) |
| --- | --- | --- | --- |
| `harbor lane` | 1 | 34.9 | 0.1 |
| `quarry road` | 1 | 6.4 | 0.1 |
| `04101` | 1 | 5.7 | 0.1 |
| `juniper street` | 1 | 35.5 | 0.1 |
| `cobbler's lane` | 1 | 10.4 | 0.2 |
| `lantern wharf` | 1 | 36.5 | 0.1 |
| `address` | 20 (capped) | 43.2 | 0.6 |
| `passport photo` | 20 (capped) | 36.1 | 2.8 |
| `forwarding address` | 2 | 7.7 | 0.1 |
| `zzqxv` | 0 | 5.5 | 0.1 |
| **median of ten** | | **22.6** | **0.1** |
| **slowest** | | **43.2** | **2.8** |

All ten planted addresses are found, the four comment-only ones included (a match query for each
of the ten streets, at 30,000 cards, returns exactly its card; the ten timed queries above name six
of them). Other measures, same runs: as-you-type
prefix `harbo*` 5.4 ms at 30,000; refreshing one changed card 2.5 ms at 30,000 (a first try keyed
by the `UNINDEXED` id column scanned the table and took 576 ms, hence the rowid scheme). The
`trigram` tokenizer (substring match inside words, so `041` finds `04101`) at 30,000: median
cold 32.6 ms, warm 0.4 ms, slowest 102.7 / 15.4 ms, file **155.8 MB**, build 6.6 s, so it is a
choice for later and not for the first slice.

The same file from Go, with the pure-Go driver `modernc.org/sqlite v1.60.1` (a throwaway module
in `/tmp`, not this repo): the same query on the same files, first of eight runs after one
eviction, 2,400 cards at most 4.7 ms, 30,000 cards at most 41.6 ms (`address`), warm at most 5.2 ms
(`passport photo`); a program that only opens it is a 10.2 MB binary against `mw`'s 22.2 MB today.
This is a new dependency (`go.mod`, and a credit in the README, which `credits_test.go` insists
on); `mattn/go-sqlite3` needs cgo and the `sqlite_fts5` build tag. A search
without a dependency would be an inverted index written by hand over `database/sql`-less files; not
measured here.

Filling it. The only read mw needs is what `Snapshot` already makes. Full rebuild from the live
store: `bd list --all` 12.0 s + all comments 5.0 s + the insert 1.5 s, about 19 s, fine nightly and
on demand. For `hitl:todo` cards alone: 2.4 s for 33 cards. An incremental refresh by
`updated_at` watermark **misses new comments**: checked on the fixture, `bd comments add` leaves
the bead's `updated_at` unchanged, so a comment on a closed card needs its own watermark on
`comments.created_at`.

### (c) Postern's own view index, extended to closed cards

The view already carries each bead as id, title, labels, 280-rune summary and dates
(`application/posternview.go:1686-1690`). Extending it to closed cards means the view grows by a
card's worth per card ever made. Measured with `bench.py view` (JSON as the view writes it,
gzipped as §11 says, then base64 and the BRC-78 envelope):

| cards in the view | JSON | gzip | on the wire per fetch | phone scan (node, in memory), ten queries |
| --- | --- | --- | --- | --- |
| 2,400 | 1,270 KB | 308 KB | about 410 KB | 1.7 to 6.7 ms |
| 30,000 | 15,887 KB | 3,842 KB | about 5.1 MB | 14.4 to 45.5 ms |

Today's view is "about 300 KB" (protocol §10). The scan is fast; the cost is the bytes the phone
fetches whenever the view changes (at most every 30 s, protocol §11), and what the bytes leave out.
The summary is the first 280 runes of the description, so the four comment-only planted addresses
are not there (`quarry road` and `lantern wharf` found 0), and what is past the cut is lost (the word `address` is in 40 of the 2,400 cards, in a title,
the notes or a comment; the view finds 8). (c) cannot reach comments without sending them, and sending every comment
would make the view's JSON about 28 MB at the live size.

### Where the candidates stand

| | reaches comments | 10 queries, median cold / warm | slowest cold | needs | live database |
| --- | --- | --- | --- | --- | --- |
| (a1) `bd query`, 2,400 | no | 501 / 444 ms | 1,206 ms | nothing | read |
| (a2) Dolt SQL, 29,392 | yes | 3,441 / 3,360 ms | 3,868 ms | nothing | read, full scans |
| (b) FTS5, 2,400 | yes | 2.0 / 0.1 ms | 5.5 ms | a SQLite driver | read to fill it |
| (b) FTS5, 30,000 | yes | 22.6 / 0.1 ms | 43.2 ms | a SQLite driver | read to fill it |
| (c) view index, 30,000 | no (280 runes) | n/a, 14 to 46 ms scan | 5.1 MB a fetch | nothing | read |

## The phone's ask

Postern's server holds opaque ciphertext (postern `server/internal/index/store.go`: an append-only
JSONL replayed into memory), so it cannot search; the query has to be answered where the plaintext
is, on the Home host. Three ways for the phone to get an answer, with what each costs.

**Path 1: a `search` record, answered from the Home host's index.** The same pattern as a grist
(protocol §19): the phone encrypts `{"action":"search","q":"harbor lane","limit":20}` to the Mayor's
key and posts it directly (§9, one HTTP round trip); mw already holds the stream open
(`infrastructure/postern/http.go:644`, `GET /api/events`, the Mayor's key may read it, §10), so the
`message` event wakes it at once; it fetches the record, decrypts it, queries FTS5, and sends back
a record `{"re": "direct:<txid>", "hits": [...]}` to the Governor's key; the phone hears the
`message` event and reads it. The server learns that a search happened and its size, never the words.

| | bytes | seconds |
| --- | --- | --- |
| request: plaintext 70 B | script about 0.5 KB, the POST body about 1.0 KB (it is the script in hex) | |
| answer, `harbor lane`, 1 hit | 217 B plaintext, about 0.7 KB script | |
| answer, `address`, 20 hits with snippets | 3.7 KB plaintext, about 5.3 KB script | |
| answer, 50 hits | 9.6 KB plaintext, about 13.2 KB script | |
| work on the Home host (measured) | | decrypt and encrypt about 1 ms each (assumed; not timed), query 0.1 to 43 ms at 30,000 cards, `mw` start 0.02 s |
| the whole round (modelled, not measured on a phone) | about 6 KB for a 20-hit search, up and down | 0.4 to 0.8 s: phone encrypt 20 ms, POST 1 round trip (100 ms assumed on mobile), event to mw 20, fetch 20, the work above 45, post 20, event to the phone 50, fetch 100, decrypt 20; add two round trips if the radio or TLS has gone idle |

The pieces to build exist: the record kinds and the stream client in mw, the Search tab on the
phone; what does not exist is a `search` action and its answer in protocol §13/§19, and a
`message` handler in mw that is not the Mayor's inbox.

**Path 2: a bigger view snapshot.** Nothing to ask; the phone searches what it holds. Bytes: the
view grows from about 300 KB to about 410 KB at 2,400 cards and 5.1 MB at 30,000, on every view
change, whether or not he searches. One fetch of the 2,400-card view carries as many bytes as
about 50 twenty-hit searches (6 KB each, up and down) would. Seconds: the scan is 2 to 46 ms once loaded; the load on a
cold app is the download (about 4 s for 5.1 MB at 10 Mbit/s, a modelled figure). It cannot reach
comments or past 280 runes, and nothing in it is "cold on the back end".

**Path 3, rejected: `GET /api/search?q=` like the bead detail ask (§12).** One round trip and the
server runs `mw postern search` as it runs `mw postern bead`, but then the query words sit in the
URL and in the server's process list, which breaks the rule that the server holds only what it
cannot read.

**What "hot on the phone, cold on the back end" is.** Hot is what the view already holds: open
cards and anything closed in the last 7 days, searched locally with no wait, as today
(`search.ts`). Cold is everything older, in the Home host's index, asked for by a `search` record
when he taps "Search older cards" or when the local search finds nothing. In bytes: hot costs the
300 KB the phone already carries; cold costs 1 KB up and 1 to 13 KB down per question, and the
phone's own store does not grow. In seconds: hot is under 50 ms; cold is the 0.4 to 0.8 s modelled
above, of which the index is under 50 ms.

## Recommendation and first slice

**Store: one SQLite file on the Home host. Index: FTS5, `unicode61 remove_diacritics 2`, one row
per card field and comment, filled from beads.**

Why, in under 100 words: it is the only candidate that reaches comments and years back and answers
in milliseconds (22.6 ms median cold, 0.1 ms warm at the live store's size) against half a second
to three and a half seconds for beads' own store, which cannot see comments or rank at all. The view
cannot hold comments without growing to megabytes. The index is a cache of beads, so it can be
deleted and rebuilt (about 19 s for the whole store), and the live database is never written. Cost:
one new Go dependency, `modernc.org/sqlite`.

**The smallest first slice** is the index and the command, without the phone: `hitl:todo` cards
only, which is 33 cards now and read in 2.4 s, so a full rebuild on each run needs no watermark
and none of the incremental trap above. The phone's `search` record is the second slice (in the
postern rig as well); widening to every bead and to messages is the third.

> **Story sketch**
>
> - Title: `mw todo search`: find a To do card by any word of its title, notes or comments, closed
>   ones and years back, from an FTS5 file on the Home host
> - Path files: `application/cardsearch.go`, `infrastructure/cardindex/`, `cmd/mw/todosearch.go`,
>   `features/todo_search.feature`, `docs/codemap.md`, `README.md` (the Credits line for the driver),
>   `go.mod`, `go.sum`
> - Size: M (the dependency and the codemap and credits checks make it wider than the code)
> - Acceptance criteria:
>   1. **An old address is found in under a second:** on a fixture of 2,000 or more `hitl:todo`
>      cards, some closed years ago, `mw todo search "harbor lane"` prints the closed card whose
>      comment holds that address, with the snippet, in under a second, cold and warm, as a
>      feature scenario with a stated time bound.
>   2. A comment added to a closed card after the index was built is found by the next search.
>   3. Searching never writes to beads (the fake tracker's `Asked()` shows no change calls).
>   4. Deleting the index file is harmless: the next search rebuilds it.
>   5. `go build ./... && go test ./...` and `make lint` pass; `docs/codemap.md` names the new port.

Left open, for the stories after: the index's home path and refresh trigger (on each search while
the file is older than a minute, or from `mw-view-follow`); whether `trigram` is wanted (156 MB at
30,000 cards) for inside-a-word search; how a `search` answer is shaped in protocol §13; and the
second brain (mw-fr6r7e.1), which builds on the same file.

Not measured, said plainly: the end-to-end seconds from a real phone (the table above is a model
whose network figures are assumptions); Dolt SQL from the Home host itself (this host reaches the
server over the network); the pure-Go driver at 30,000 cards beyond the single query run; and the
fixtures are synthetic English-like words, so the ranking quality on his real text is untested.
