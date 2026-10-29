# What a seat reads at boot, and what it costs (mw-gq6.445)

Every session pays for everything it is given before it does any work: the
harness's own system prompt, the seat's charter, the kickoff, and then whatever
the kickoff sends it to read. That first stretch is the seat's *boot*. This
page measures it for each seat from real transcripts, lists what each seat
reads with its size, and cuts it by hierarchy: a short index at boot, the rest
one command away (`mw seat boot <seat>` and `mw seat read <seat> <topic>`),
nothing lost. The Governor asked for this on mw-266lp8 (2026-10-15): "ways of
decreasing the boot cost without losing anything important ... a very simple
instruction where you can find what you need easily and go from there".

<!-- toc -->

- [How it was measured](#how-it-was-measured)
- [Before: what each seat reads, and what it costs](#before-what-each-seat-reads-and-what-it-costs)
- [The cuts](#the-cuts)
- [After: projected](#after-projected)
- [The commands](#the-commands)
- [Proposed manifests, for the Mayor to place](#proposed-manifests-for-the-mayor-to-place)
- [What this story did not change](#what-this-story-did-not-change)
- [The Mayor's boot under a ceiling (mw-gq6.755)](#the-mayors-boot-under-a-ceiling-mw-gq6755)

<!-- /toc -->

## How it was measured

Claude Code keeps a transcript of every session, one JSON line a turn, and each
assistant turn records the context it was given: `input_tokens` plus
`cache_creation_input_tokens` plus `cache_read_input_tokens`. The Mayor, the
Deputy and the Millhand all run in the vault, so their transcripts are together
under `~/.claude/projects/-home-<user>-millwright-vault/`; a Builder's are under
`~/.claude/projects/-home-<user>--mw-worktrees-<story>/`. A seat is told from
its kickoff's first words. `scripts/seat-boot-cost.py` prints, for each
transcript, the context of its first fourteen turns and the tool calls each
made, as numbers and tool names only:

```sh
cd ~/.claude/projects/-home-$USER-millwright-vault
python3 -I ~/millwright/scripts/seat-boot-cost.py mayor $(ls -t *.jsonl | head -12)
```

"After boot reads" below is the context of the first turn after the kickoff's
reads are done; "at the first real action" is the turn that arms a wait, reads
mail or starts on the story. Measured 2026-10-16 on the Laptop, on the sessions
of 2026-10-15 and 16.

Bytes to tokens: the Mayor's five boot reads (36.7K bytes) raised its context
by 15.6K tokens, so these files cost about **2.4 bytes a token** (dense
markdown, full of ids and paths), not the 4 of plain prose. Projections below
use 2.4.

## Before: what each seat reads, and what it costs

| Seat | Sessions | First turn | After boot reads | At the first real action |
|-|-|-|-|-|
| Mayor | 7 (after handoffs 409-415) | 23.0-25.2K | 37.9-40.1K | 42.5-48.1K |
| Deputy | 6 (sittings 688-693) | 22.3-22.5K | 23.9-24.8K | 24.3-25.0K |
| Millhand (laptop) | 4 (wakes 108-111) | 23.3-23.8K | 26.2-28.6K | 29.1-32.1K |
| Builder, a millwright story | 6 | 23.3-26.2K | 37.6K (the one that read all three docs) | - |
| Builder, a lampas story | 3 | 78.3-79.6K | 80.7-86.2K | - |
| Clerk | 16 Mayor transcripts with briefs | ~18-19K (estimated) | - | - |

The first turn's context is mostly the harness's own system prompt and tools,
read from cache and not mw's to cut: 16.2-16.3K for the Mayor and the Deputy
(interactive), 12.7K for the Millhand, 10.2-10.7K for a Builder (`--print`).
What mw adds at the first turn is the charter, the kickoff and the vault's or
rig's `CLAUDE.md`, 6-7K tokens for the Mayor.

What each seat reads at boot, with sizes (bytes, 2026-10-16):

- **Mayor.** Charter 5,881 (system prompt); kickoff 1,482. Then, as the kickoff
  says: `seats/mayor/vision.md` 2,666; the newest handoff ~5,000 (415: 5,029);
  `seats/mayor/procedures.md` 14,325, of which Boot 1,880, Rules that bite
  every sitting 3,393, Read by need 666, Who is talking 643, Handing off 533,
  Standing asks of the Governor 6,944; `seats/mayor/pruning.md` 4,188;
  `~/millwright/CONTEXT.md` 10,518. Then `mw brief mw-6ww mw-gq6 mw-0om`
  6,630 and `docs/codemap.md` 10,234. Total read after the kickoff: ~53.5K
  bytes, ~22K tokens, in four to six turns.
- **Deputy.** Charter 4,414 (system prompt); the default kickoff. Then the
  newest handoff ~1,750, `ls seats/deputy/`, and in every sitting measured
  `mw mail --help` (and often `mw events --help`, `mw --help`): about 2-2.5K
  tokens re-read each sitting to find the same four commands. Some sittings
  read the previous handoff too ("then handoff 692, unchanged except as
  below").
- **Millhand.** Charter 6,306 (system prompt); kickoff 1,082. Then
  `~/.config/mw/config.toml`, `seats/millhand/hosts/laptop/boot.md` 5,057 (of
  which First steps 1,268, Standing procedures 1,152, VPS health rule 849,
  Gotchas 830), the newest handoff ~1,400, the timers.
- **Builder.** The boot file mw writes (`runs/<story>/boot.md`): 17,067-18,709
  for the millwright stories measured, that is the charter 4,358, the rig
  memory 7,835 and the story 5-6.5K; the kickoff ~3,500; the rig's `CLAUDE.md`
  (millwright 1,637). Then, as millwright's `CLAUDE.md` asks, `CONTEXT.md`
  10,518, `docs/codemap.md` 10,234 and `docs/adding-a-command.md` 1,150: 9.4K
  tokens more. A lampas story's Builder starts at 78-80K because lampas's
  `CLAUDE.md` is **140,191 bytes** (whisper-hid's is 55,185; every other
  rig's is 0.9-8.5K): some 55K tokens on every lampas story, read again from
  cache on every turn of it.
- **Clerk.** The Mayor's brief to a Clerk (the Agent tool) is 518-5,752
  characters, 0.2-1.5K tokens. A Clerk's own turns are kept apart from the
  Mayor's transcript and were not found under the vault's directory, so its
  boot is estimated: the harness's prompt (~16K) plus the vault's `CLAUDE.md`
  1,818 and `AGENTS.md` 3,793 (~1.6K) plus the brief.

## The cuts

The hierarchy: at boot a seat reads only what every sitting needs, in one
call, and ends with a one-screen index of the rest; anything on the index is
one more call away, by heading. Procedures split by trigger: what fires every
sitting is in the boot set, what fires at a handoff, a release or a prune is a
topic read then.

1. **A manifest a seat: `seats/<seat>/boot.toml`.** It names, in order, the
   files, sections and commands the seat reads at boot, each with a byte cap,
   and the files whose headings it reads by need. `mw seat boot <seat>` prints
   the boot set and the index in one output; `mw seat read <seat> <topic>`
   prints one section whole. Built in this story, with the manifests below
   proposed for the Mayor to place in the vault (the vault is the Mayor's; a
   Builder does not write it). What is not in a manifest is not read, so the
   boot cost is what the manifest says, and a change to it is a diff of one
   file.
2. **The Mayor's kickoff becomes one instruction:** run `mw seat boot mayor`,
   then follow its Boot section. Out of its boot: `pruning.md` (read when rule
   8 fires), `CONTEXT.md` (the glossary, read by section when filing or
   grilling: `mw seat read mayor work`), the codemap (when planning a
   millwright story), procedures' Handing off (at the handoff), Standing asks
   (when releasing; 6,944 bytes), Read by need (the index replaces it). In:
   vision, the newest handoff, Boot, Rules that bite every sitting, Who is
   talking, the brief. Whether the glossary's People and offices and Work
   sections (5,315 bytes, ~2.2K tokens) stay in the boot set is the Mayor's
   call; both numbers are below.
3. **The Deputy stops re-reading help.** Its boot set is the newest handoff and
   `mw mail inbox --as deputy`; a new `seats/deputy/crib.md` of the exact
   commands it uses every sitting (`mw mail inbox`, `read`, `reply -m`, a bead
   created held, `mw todo due`, `mw events wait`) becomes its topics, read by
   name instead of by `--help`. A Deputy handoff stands alone, as the charter
   says: no "then read the previous one".
4. **The Millhand boots from the section that fires**, `First steps, in order`
   of its host's `boot.md`, the newest handoff and the timers; The host, the
   VPS health rule, Standing procedures and Gotchas are topics.
5. **A Builder's biggest cost is a rig's `CLAUDE.md`, not the vault.** lampas's
   140K-byte `CLAUDE.md` is paid on every lampas story and every turn of it:
   cut to a one-screen index with the rest read by need (a lampas story; the
   same shape as this one). For millwright, the Builder's `CLAUDE.md` sends it
   to the glossary and the codemap whole (21K bytes); a `seats/builder/boot.toml`
   of topics (below) lets a Builder read one section of either with
   `mw seat read builder <topic>` when it needs it. The rig memory is already
   hierarchical for a rig kept as facts (`mw memory query`, `mw memory demote`
   after an eval); millwright's own memory is still the flat `millwright.md` at
   7,835 of 8,000 bytes, so migrating it (`mw memory migrate millwright`) is
   what makes demotion by eval apply to it. Every `mw memory query` is logged
   (rig, terms, hits, the story from `$MW_STORY`) to the host's
   `~/.local/state/mw/memory-queries.jsonl`, host state so that a Builder's
   query leaves the vault clean; `mw memory stats <rig> [--since 7d]` counts
   them, and a facts rig's boot ends its memory with one line naming the ten
   most queried subjects (then those with most facts) and the query command
   (mw-1ljpo9.8).
6. **Every session in the vault reads `AGENTS.md`'s beads block** if the
   harness reads `AGENTS.md`: 3,793 bytes telling it to use `bd prime`, `bd
   remember` and to push before stopping, all three against ADR 0003 and the
   vault's `CLAUDE.md`. Shrinking it to the minimal block ADR 0003 names saves
   ~1.2K tokens a session and a contradiction.
7. **History stays out of boot**, as ADR 0003 has it: the ledger, `history.md`
   (153K) and the archives are never read at boot, and the index makes a
   section of any of them reachable by name rather than by `grep`.

## After: projected

Projected from running the proposed manifests against copies of the real seat
files on 2026-10-16 (`mw seat boot mayor` printed 16,538 bytes with a stand-in
for the brief; the real brief is 6,630), at 2.4 bytes a token, added to the
measured first turn.

| Seat | Boot reads before | Boot reads after | At the first real action, before -> after |
|-|-|-|-|
| Mayor | ~53.5K bytes, ~22K tokens, 4-6 turns | 23.2K bytes, ~9.7K tokens, 1 call | 42.5-48.1K -> ~32.7K (~35K with the glossary's People and Work kept) |
| Deputy | handoff + help, ~2.5-3K tokens, 3-4 turns | 2.3K bytes, ~1K tokens, 1 call | 24.3-25.0K -> ~23.3K |
| Millhand | ~7.5K bytes + timers, ~3.5K tokens, 2-3 turns | 4.0K bytes, ~1.7K tokens, 1 call | 29.1-32.1K -> ~25.1K |
| Builder, millwright | 21.9K bytes after the boot file, ~9.4K tokens | by section on need | 37.6K -> ~30K |
| Builder, lampas | `CLAUDE.md` 140K bytes, ~55K tokens | an index, ~4K bytes | 78-80K -> ~27K (a lampas story) |
| Clerk | brief + vault files | unchanged; -1.2K if `AGENTS.md` shrinks | ~18-19K -> ~17K |

The Mayor's saving is ~13K tokens a boot (28% of its context at the first
action) and three to five turns; every later turn of the sitting reads that
much less from cache. Once the manifests are in the vault, the "after" is
measured the same way as the "before": `scripts/seat-boot-cost.py` on the next
sittings' transcripts.

## The commands

```sh
mw seat boot mayor                 # the boot set, then the index
mw seat read mayor                 # the index alone
mw seat read mayor handing-off     # one section, whole
mw seat read mayor hand            # a prefix no other topic shares
```

`mw seat boot <seat>` prints a title line, then each part of the manifest under
a `==> <part> <==` line (the form `head` and `tail` use for many files, which no
heading of a part's own can be mistaken for), then `==> Read by need <==` and
the index, one line a file: `- <file>: <slug>, <slug>, ...` (the slug is the
heading's own words, so the heading is not printed twice; mw-gq6.755). A part past its
cap is cut at a line end and ends `[cut at <cap> of <size> bytes]`. A part that
cannot be read is said in its place, `[not read: <why>]` or `[failed: <why>]`
after what a command printed, the rest is printed all the same, and the command
exits non-zero counting them. A seat with no manifest is refused.

A topic's slug is its heading's first five words before any aside in
parentheses or after a colon, lower-case and hyphenated: `Handing off (at
~180K by ...)` is `handing-off`, `Since 2026-09-21: Builders propose` is
`since-2026-09-21`. A heading that repeats across the topic files gets `-2`,
`-3`. `mw seat read` takes the slug, or a prefix no other slug shares, and
refuses an unknown or ambiguous one naming what there is. A section runs from
its heading line to the line before the next heading of its level or higher,
so a `###` under a `##` is read with it; a `#` inside a fenced code block is
not a heading. Both commands read files only; `boot` also runs the manifest's
commands, each in the vault with `sh -c`, for at most `--timeout` (2m).

The manifest:

```toml
cap = 8000                         # bytes a part may print, 0 none; default 8000
ceiling = 15000                    # bytes the whole boot set may print, 0 none (the default):
                                   # past it the set is printed, then said to be over, and the
                                   # command exits non-zero (mw-gq6.755)

[[boot]]                           # one of file, newest or command; in order
file = "seats/mayor/vision.md"     # from the vault's root; {host} and {seat} are filled in
cap = 3000                         # this part's own cap

[[boot]]
newest = "seats/mayor/handoffs"    # the newest .md of the directory, by name

[[boot]]
file = "seats/mayor/procedures.md"
section = "Boot"                   # one heading, by its text or its slug

[[boot]]
rig = "millwright"                 # a file of a rig's checkout, from the config's [rigs]
file = "CONTEXT.md"
section = "Work"

[[boot]]
command = "mw brief mw-6ww"        # run in the vault; what it prints is the part

[[topic]]                          # a file whose headings are read by need
file = "seats/mayor/procedures.md"

[[topic]]
rig = "millwright"
file = "docs/codemap.md"
```

It is read by `application.ParseSeatManifest` (`application/seatboot_manifest.go`)
and the use cases `SeatBootSet` and `SeatRead` there; the vault adapter is
`infrastructure/vault/seatboot.go`, a rig's files `infrastructure/rig/rigfiles.go`
(`Docs`), the commands `infrastructure/shellcmd`. Tests:
`application/seatboot_manifest_test.go` and `cmd/mw/seat_boot_test.go`.

## Proposed manifests, for the Mayor to place

Each was run as written (with a `printf` in place of the command) against
copies of the real files; the sizes above are from those runs. The Mayor
commits them to the vault as `seats/<seat>/boot.toml`; the kickoffs that send a
seat to run them are the Mayor's own files too (`seats/<seat>/kickoff.md`).
Charters are not touched.

`seats/mayor/boot.toml`:

```toml
# What a Mayor reads at boot, in order (mw seat boot mayor), and by need (mw seat read mayor <topic>).
cap = 8000

[[boot]]
file = "seats/mayor/vision.md"

[[boot]]
newest = "seats/mayor/handoffs"

[[boot]]
file = "seats/mayor/procedures.md"
section = "Boot"

[[boot]]
file = "seats/mayor/procedures.md"
section = "Rules that bite every sitting"

[[boot]]
file = "seats/mayor/procedures.md"
section = "Who is talking"

[[boot]]
command = "PATH=$PATH:$HOME/millwright/bin timeout 120 mw brief mw-6ww mw-gq6 mw-0om"

[[topic]]
file = "seats/mayor/procedures.md"

[[topic]]
file = "seats/mayor/pruning.md"

[[topic]]
file = "seats/mayor/filing.md"

[[topic]]
file = "seats/mayor/verifying.md"

[[topic]]
file = "seats/mayor/approvals.md"

[[topic]]
file = "seats/mayor/box.md"

[[topic]]
file = "seats/mayor/laptop.md"

[[topic]]
file = "seats/mayor/priorities.md"

[[topic]]
rig = "millwright"
file = "CONTEXT.md"

[[topic]]
rig = "millwright"
file = "docs/codemap.md"
```

To keep the glossary's two core sections in the boot set, add before the
command:

```toml
[[boot]]
rig = "millwright"
file = "CONTEXT.md"
section = "People and offices"

[[boot]]
rig = "millwright"
file = "CONTEXT.md"
section = "Work"
```

`seats/mayor/kickoff.md`, proposed: "You are booting into the Mayor seat of
millwright. Your charter is in your system prompt. Boot now with one call,
`PATH=$PATH:$HOME/millwright/bin mw seat boot mayor`: it prints vision, the
newest handoff, the Boot, Rules and Who is talking sections of
seats/mayor/procedures.md and the brief, then the index of what you read by
need with `mw seat read mayor <topic>`. Then follow the Boot section's steps 4
to 8 in order ..." with the rest of today's kickoff (the warm-then-hot handover
and the mail) as it is.

`seats/deputy/boot.toml`:

```toml
[[boot]]
newest = "seats/deputy/handoffs"

[[boot]]
command = "MW_SEAT=deputy mw mail inbox --as deputy"

[[topic]]
file = "seats/deputy/crib.md"
```

with `seats/deputy/crib.md` new, one `##` a job (Mail, Beads, To-dos, Waiting)
holding the exact command lines today's sittings re-derive from `--help`.

`seats/millhand/boot.toml`:

```toml
[[boot]]
file = "seats/millhand/hosts/{host}/boot.md"
section = "First steps, in order"

[[boot]]
newest = "seats/millhand/hosts/{host}/handoffs"

[[boot]]
command = "systemctl --user list-timers --no-pager | grep -E 'mw-|NEXT'"

[[topic]]
file = "seats/millhand/hosts/{host}/boot.md"
```

`seats/builder/boot.toml` (topics only; a Builder's boot set is the boot file
mw writes):

```toml
[[topic]]
rig = "millwright"
file = "CONTEXT.md"

[[topic]]
rig = "millwright"
file = "docs/codemap.md"

[[topic]]
rig = "millwright"
file = "docs/adding-a-command.md"
```

## What this story did not change

- Charters: none. Nothing above needs a charter line; the kickoffs and
  manifests are the Mayor's files.
- `mw seat up`'s default kickoff still sends a seat to its charter and newest
  handoff; a seat with a manifest is told to run `mw seat boot <seat>` by its
  own `kickoff.md`. Having `mw seat up` say so itself when the seat has a
  manifest is a one-line follow-up.
- lampas's `CLAUDE.md` and the vault's `AGENTS.md`: proposed, not edited; both
  belong to other files than this story's.

## The Mayor's boot under a ceiling (mw-gq6.755)

The Mayor's boot set printed 40.6K bytes on 2026-10-25 (a Mayor at 48-58K of
context before any work). Where it went: the vision 2.7K, the newest handoff
5.2K, Boot 2.9K, Rules that bite every sitting 4.0K, Who is talking 0.7K,
Standing asks 9.0K, the glossary's two sections 5.4K, the brief 7.7K (cut at
8,000), the index 2.6K. The charter-adjacent minimum is the handoff, Boot,
Rules, Who is talking, the brief and the index; the vision, Standing asks and
the glossary are topics (`mw seat read mayor vision`, `... standing-asks`,
`... work`), and Boot says when to read each. The index is one line a file now
(0.96K, from 2.6K), and `ceiling = 15000` in the manifest keeps the set from
growing back. Measured on copies of the real files with the brief held to 1K,
the set is ~14.8K; the handoff, Boot and Rules are 12.1K of that on their own,
so the brief is what a ceiling of 15K leaves room for the least of.
