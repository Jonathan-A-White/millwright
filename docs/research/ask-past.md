# Research: asking a past session what happened

Answers mw-6ww.82, for the parked grilling mw-6ww.16. The Governor, 2026-09-19: "you should be
able to look at wherever Claude saves its session information if you ever need to see what
happened previously… I used to have a command in my town… that had ask past as a command."
Read-only research: no product code changed. Measured on the Laptop on 2026-10-03.

## What exists today

- **Claude Code keeps every session.** `~/.claude/projects/<dir>/<session id>.jsonl`, one
  directory per working directory, named by turning every non-alphanumeric character of the path
  into `-`. `ProjectDir` does exactly this (`infrastructure/claude/transcripts.go:47`). A Builder's
  directory is `~/.mw-worktrees/<story>`, so the transcript outlives the worktree: this Laptop
  holds transcripts for 646 Builder sessions and only 12 worktrees. Seats that run in the vault
  (Mayor, Millhand, Deputy) all share one directory, `-home-jwhite-millwright-vault`.
- **mw already reads them, cheaply.** `LiveContext` (`transcripts.go:76`) finds the newest
  transcript (`newestTranscript`, `:117`) and sums the last turn's tokens; `Tail`
  (`transcripttail.go:46`) turns the newest one into one line per thing said or done, each cut to
  200 characters (`describe`, `:75`; `oneLine`, `:104`). `mw peek` (`cmd/mw/peek.go`) shows the
  last 20 such lines, and reaches another host over the `[hands_hosts]` ssh line. Both take a
  directory and read only the newest file: neither takes a session id or a question.
- **A story knows its session id.** `runs/<story>/result-N.json` in the vault carries
  `session_id` (checked on mw-6ww.83; read leniently by `resultFile`, `application/fuel.go:185`),
  one file per attempt, and the Builder ledger line says `session <id>` (`application/next.go:1587`).
  `mw seat context` prints the first 8 characters of a session id (`application/seatcontext.go:11`).
- **A cheap, toolless headless session is already built.** `GrindArgs`
  (`infrastructure/claude/grind.go:58`) runs `claude --print --model <m> --restricted --tools
  Read --no-session-persistence --safe-mode --system-prompt … <prompt>`: no settings, no
  CLAUDE.md, nothing written back. A question to a transcript is the same shape, with no tool at all (whether `--tools` takes an
  empty list is unchecked).

## How big they are

Laptop, `find ~/.claude/projects -name '*.jsonl'`, 2026-09-17 to 2026-10-03 (nothing yet pruned):

| Set | Files | Bytes | Median | p90 | Max |
| --- | --- | --- | --- | --- | --- |
| All (subagent files included) | 1,169 | 805 MB | 477 KB | 1.5 MB | 4.9 MB |
| Builder worktrees | 646 | 456 MB | 574 KB | 1.3 MB | 3.3 MB |
| Vault (Mayor, Millhand, Deputy) | 499 | 335 MB | 360 KB | 1.7 MB | 4.9 MB |

At about 4 bytes a token, a median Builder transcript is about 140k tokens and the largest
Mayor's about 1.2M: **raw, none of it fits a cheap model's window, and a seat must never read it
raw.** Almost all of it is tool results and bookkeeping. The biggest file is 1,758 lines: 306
`user` entries hold 3.2 MB (tool results), 433 `assistant` entries 1.2 MB, the rest is
attachments and snapshots. Over a random 40 Builder transcripts (31.7 MB):

- what the model *wrote* (text blocks) is 0.7 percent of the bytes;
- a digest in `Tail`'s shape, every entry cut to 200 characters, is **2.2 percent: 17 KB, about
  4.4k tokens, per session**.

**The VPS and the desktop are not measured.** An ssh to the VPS for its numbers was declined by the
permission classifier, so I did not retry; the desktop was not tried. Each host's transcripts are
its own: a session is on the host that ran it, and a Mayor on a home that has since moved is on the
old home. Run on each: `find ~/.claude/projects -name '*.jsonl' -printf '%s\n' | sort -n | awk
'{s+=$1; m=$1} END{print NR, s, m}'`. Retention is unverified: Claude Code has a
`cleanupPeriodDays` setting (I believe 30 days by default); the oldest file here is 16 days old,
so this data cannot show it.

## Options

**A. Nothing built; a seat reads by hand.** The asker finds the file and greps. Fuel: whatever the
Mayor's own context spends, and a wrong `cat` of a 3 MB file is 800k tokens. Parts: none. This is
today, and it is why the Governor asked.

**B. `mw ask-past <target> --digest`: a free digest, no model.** Resolves the target to
transcripts and prints the `Tail`-shaped lines for the whole session (or `--since`/`--last N`).
Fuel: zero to mw; the reader pays about 4.4k tokens for a Builder session, up to ~85k for the
largest Mayor one (1,700 entries × 200 characters), which is why it takes `--last`. Parts: a
resolver, a whole-file digest (extract `Tail`'s read loop and `describe`), one cobra command, one
feature, a fake.

**C. `mw ask-past <target> "<question>"`: B, then a Haiku session answers.** The digest goes to
a toolless headless session built like `GrindArgs`, which answers in a few lines, and the asker
reads only that. Fuel: one Haiku call on about 6k tokens in and 300 out for a Builder session,
against the 1.16M tokens the session itself cost (mw-6ww.81's ledger line): well under 1 percent
of a story, and it is the cheapest model. Parts: B plus an `Asker` port, a Claude adapter beside
`grind.go`, a window cap, and a fake. A transcript is untrusted text; the answerer has no tools, so
the worst a hostile line does is give a wrong answer.

**D. Resume the session (`claude --resume <id> -p`).** The model really "remembers" and can answer
anything. But it loads the whole transcript as context (140k to 1.2M tokens, at the session's own
model) and writes a new session file. Reject: it is the fuel problem the question asked us to avoid.

## Recommendation

**C, shipped as B first.** Both are one command; step 1 is the digest alone, so the Governor can
see what a digest of a real session looks like before any model is asked anything. Build order:
(1) the resolver: a story id finds `runs/<story>/result-N.json` for its session ids, and where the
file is gone, every `*.jsonl` in `ProjectDir(~/.mw-worktrees/<story>)`; a session id or a prefix
of 8 or more characters is looked for in all project directories; `--seat mayor|millhand|deputy`
filters the vault directory by the seat named in the first prompt. A story pathed to another host
is read there over its `[hands_hosts]` line, as `mw peek` does, with a `--here` for the far end.
(2) the digest. (3) the question, with `--model haiku` by default, a window cap of 30k tokens (a
longer digest keeps its first 2k and its last 28k, and says it did), and the answer printed with
the session ids it read and nothing written. Prefer the digest's `--last N` for the Mayor's own
mid-session use: the handoff file remains the primary record, and this is for "what happened".

## Questions only the Governor can answer

1. **Who may ask?** Recommended: the Mayor, the Deputy and the Millhand, which is where a
   predecessor's reasons get lost. Not a Builder: its story is bounded and the story's bead is its
   record. Adding it to a charter needs his word.
2. **Is a Haiku answer enough, and what is the fuel cap per ask?** Recommended: Haiku, one ask at
   most 30k tokens in, no tools, no retry; a seat that wants more asks again with `--since`.
3. **Is it an `mw` command or a Clerk errand?** Recommended: an `mw` command. A command is
   testable, reaches other hosts, and costs a known amount; a Clerk costs the Mayor's own context to
   brief.
4. **May the digest reach other hosts?** It contains his dictation and anything a session printed,
   including secrets a tool result showed. Recommended: yes over ssh for the asker's terminal, never
   shipped to Postern, never written to a file.
5. **Keep transcripts longer than Claude Code does?** If the default prunes at 30 days, a story older
   than that is unanswerable. Recommended: leave it until a real ask comes up empty; then set
   `cleanupPeriodDays` in mw's own settings, because 800 MB is already 16 days of this Laptop.
6. **Name.** Recommended: `mw ask-past`, his own word.
