# Research: just-in-time context (a side load)

Answers mw-6ww.181.1, for the grilling ticket mw-6ww.181. The Governor, Postern general 2026-11-17
14:26Z (direct:fe037fd8...): "how do you get just the right information at the right time and not
too much and not too little", by "a side load that tells you", with more "if you wanted it ... with
the appropriate targeted searches", and some of it "in the tools that you use and the tools tell you
how you're using it ... not the right way". Read-only research: no product code and no rule file
changed. Every URL below was opened on 2026-11-17. The Mayor's sheet, `seats/mayor/procedures.md`
in the vault, was read whole at vault commit 3425e8c87 (20,271 bytes, 97 lines); its rules are named
here by section and by line (L70 is line 70 of that version). Bytes are `wc -c`; tokens are bytes
over 2.4, the ratio `docs/seat-boot.md` measured for these files.

<!-- toc -->

- [The answer in short](#the-answer-in-short)
- [What the standing context costs today](#what-the-standing-context-costs-today)
- [Prior art](#prior-art)
  - [The web side load: Third Voice, Sidewiki, Hypothesis](#the-web-side-load-third-voice-sidewiki-hypothesis)
  - [Claude Code hooks](#claude-code-hooks)
  - [Skills, path rules and subagents](#skills-path-rules-and-subagents)
  - [Retrieval and just-in-time context](#retrieval-and-just-in-time-context)
  - [Tools that say how you use them wrongly](#tools-that-say-how-you-use-them-wrongly)
  - [Context rot and right-sized context](#context-rot-and-right-sized-context)
- [Where a rule can live](#where-a-rule-can-live)
- [What millwright already has](#what-millwright-already-has)
- [The rules of procedures.md, one by one](#the-rules-of-proceduresmd-one-by-one)
- [The other seats](#the-other-seats)
- [Postern's seats and Wicket](#posterns-seats-and-wicket)
- [Risks](#risks)
- [What it would be measured by](#what-it-would-be-measured-by)
- [Options](#options)
- [Recommendation](#recommendation)
- [Questions to grill him on](#questions-to-grill-him-on)

<!-- /toc -->

## The answer in short

- A rule can live in three places. **In the tool**: the command refuses, warns, or prints the next
  step (a tool guard). **Beside the act**: the harness adds a few lines when the seat does a thing
  (a hook side load). **In the standing context**: the charter and the boot sheet, read every
  sitting.
- The test is what a machine can see. If the tool can see the breach, the rule is the tool's. If
  only the act can be seen, it is a side load's. If nothing can be seen (a duty, an authority,
  whose word counts), it stays.
- The Mayor's sheet holds 68 rules. By that test 32 move to tool guards (10,335 bytes), 12 to hook
  side loads (3,868 bytes) and 24 stay (4,865 bytes). The table is below, every rule in it.
- Four of the 32 already have their guard and are still read every sitting: 2,024 bytes that can
  go with no build at all.
- The Fuel saved is modest: at most about 5.5K tokens of a sitting that runs to 180K. The larger
  gains are that a rule met at the act is followed more reliably than one of 68 read an hour
  before, and that the sheet stops growing (it grew 42% in the last month).
- The web's side loads died of three parties with different interests, an empty sidebar and spam.
  The one that lives, Hypothesis, lives where one closed group writes and reads. That is our case.
- Claude Code's hooks can carry a side load today, with one catch: a side load hung on an act
  arrives after the seat has already written that act.
- Recommended: guards first, cutting each rule's text as its guard lands; and the side-load hook
  built in shadow, counting for a week before it adds a line to any seat's context.

## What the standing context costs today

| What a Mayor holds every sitting | Bytes | Tokens, about |
|-|-|-|
| Charter (system prompt) | 5,881 | 2.5K |
| Boot set: the newest handoff (cap 7,000) | 4,330 to 4,979 on 2026-11-17 | 2K |
| Boot set: the sheet's Boot, Rules that bite every sitting, Who is talking | 7,927 | 3.3K |
| The sheet's Standing asks of the Governor, read at his first message | 10,409 | 4.3K |

- The sheet costs 18,336 bytes, about 7.6K tokens, in every sitting, before any work. Handoffs ran
  10 to 22 a day from 2026-11-10 to 16 (mean 16), so about 122K tokens a day in first reads, and
  every later turn of a sitting reads it again from cache.
- It grows and is not cut. The sheet was 14,325 bytes on 2026-10-16 (`docs/seat-boot.md`) and is
  20,271 now, 32 vault commits later; its own third line says "Under 14 KB". Standing asks went
  from 6,944 to 10,409 bytes. They sit outside the boot set's 15,000-byte ceiling, which
  `mw seat handover` enforces, so nothing stops them.
- The read is in the way of the rule. Boot step 2 says to read Standing asks "at the first message
  of his, before the receipt line", and the first ask is a receipt within a minute.
- A Builder pays the same way. This story's boot file is 22,995 bytes, and 5,701 of them are the
  text of the Verify and Closing comment steps, which a Builder reaches at the end.

## Prior art

### The web side load: Third Voice, Sidewiki, Hypothesis

- **Third Voice** (1999 to April 2001). A browser plug-in: a sidebar to write notes on any page
  and read other people's, a small red triangle on a page that had notes, the notes kept on Third
  Voice's servers and never on the page's own site
  ([Wikipedia, Third Voice](https://en.wikipedia.org/wiki/Third_Voice)). Site owners campaigned
  against it ("Say No to TV" called it "Web graffiti"), a new visitor mostly found no notes,
  spammers and cross-site scripting found it, and it "couldn't generate enough advertising revenue
  to stay in business" ([Wikipedia, Web annotation](https://en.wikipedia.org/wiki/Web_annotation)).
- **Google Sidewiki** (September 2009 to December 2011). The same idea as a sidebar of the Google
  Toolbar, comments ranked by votes and by the writer's past. Site owners had no say; Jeff Jarvis
  wrote that it "takes comments away from my blog and puts them on Google"
  ([Wikipedia, Google Sidewiki](https://en.wikipedia.org/wiki/Google_Sidewiki)). His "sidewalk or
  something" may be this one: a guess of this survey, his to confirm.
- **Hypothesis** (2011 to today). Open source, a layer of commentary over pages others publish. A
  note is public, shared with a group, or private. It lives on classes: a teacher and students
  annotate one reading inside the college's own system, and the college pays. Over five million
  annotations by May 2019 ([Wikipedia, Hypothes.is](https://en.wikipedia.org/wiki/Hypothes.is);
  its own page sells exactly that today: [web.hypothes.is/about](https://web.hypothes.is/about/)).
- **The W3C Web Annotation Data Model** (Recommendation, 23 February 2017), the standard that
  grew up beside it. An annotation is a *body* about a *target*; a *selector* picks out
  the part of the target (a quote with the text before and after it, a position, a CSS or XPath
  path); a *state* says which version of the target it was written for
  ([w3.org/TR/annotation-model](https://www.w3.org/TR/annotation-model/)).

What carries over:

1. **The failures were social, not technical.** Three parties (the page's owner, the note's
   writer, the reader) wanted different things, and anyone could write. Hypothesis works where one
   closed group writes and reads for one purpose. Here the Governor approves a rule, the Mayor
   places it and a seat reads it: one party.
2. **An empty or noisy sidebar is worse than none.** For a seat, a side load that is not about the
   act is a distractor, and distractors cost accuracy (below). Aim narrow: few acts, few lines.
3. **Anchoring is the hard part.** A note is lost when its page changes; a rule hung on a command
   is lost when the command is renamed, or missed when the same end is reached another way.
4. **The red triangle.** Third Voice showed a mark first and the notes on a click. A side load can
   be one line and a topic name, with the body one `mw seat read` away.
5. **Body, target, selector, state** is the record a side load needs: the lines, the act, how the
   act is matched, and where the rule came from and when.

### Claude Code hooks

From the [hooks reference](https://code.claude.com/docs/en/hooks) and the
[hooks guide](https://code.claude.com/docs/en/hooks-guide):

- A hook is a command (or an HTTP call, an MCP tool, a model question) that the harness runs at a
  named moment. There are over thirty; the ones that matter here are `SessionStart`,
  `UserPromptSubmit`, `PreToolUse`, `PostToolUse` and `Stop`.
- A hook is matched by tool name, and narrowed by an `if` in permission-rule form: the reference's
  example `"Bash(git *)"` runs it only for git commands. It calls `if` best-effort.
- `PreToolUse` can answer with `permissionDecision` (`allow`, `deny`, `ask`), a
  `permissionDecisionReason` that a denied seat is shown, an `updatedInput` that replaces the
  call's input, and `additionalContext`: a "String added to Claude's context alongside the tool
  result". `PostToolUse` has `additionalContext` too. For `UserPromptSubmit` and `SessionStart`,
  what the hook prints is added beside the prompt or before the first one. A `Stop` hook can keep
  the seat from going idle, with a reason it is shown.
- Not every moment can carry lines. `PermissionDenied` fires only when auto mode refuses a call,
  and its one documented answer is whether the seat may try again.
- **The catch.** A `PreToolUse` side load lands beside the result. The seat has written the call
  by then, so the lines cannot shape the act they hang on unless the hook also denies it.
- Each field is capped at 10,000 characters. A field put at the wrong level of the JSON is ignored
  without an error, so a broken side load fails silently. A command hook's default time limit is
  600 seconds.
- A hook costs no context: "Zero, unless hook returns additional context"
  ([features overview](https://code.claude.com/docs/en/features-overview)). What it adds stays in
  the conversation for the rest of the sitting.
- Here: mw passes a session's settings inline (`--settings`), and one hook already rides there, the
  `PermissionRequest` hook that tells an unattended seat "Nobody is watching this seat ..."
  (`infrastructure/claude/claude.go`, `DenyHookCommand`). That is a side load, in use. For a
  story's `--print` session the reference implies `PreToolUse` hooks run (its `defer` decision
  exists only there); it was not tried here.

### Skills, path rules and subagents

- **Skills** ([skills](https://code.claude.com/docs/en/skills)). At the start only each skill's
  name and description are in context; "full skill content only loads when invoked". A description
  is cut at 1,536 characters in the listing, and a loaded body "stays there across later turns".
  Anthropic calls the shape *progressive disclosure*, "a well-organized manual that starts with a
  table of contents, then specific chapters, and finally a detailed appendix"
  ([Agent Skills](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills)).
  The model chooses whether to load one, so delivery is not certain. A Builder's session has the
  Skill tool taken out (`StoryDisallowedTools`).
- **Path rules** ([memory](https://code.claude.com/docs/en/memory)). A file in `.claude/rules/`
  with a `paths:` line loads only "when Claude works with matching files": on a Read, Write or
  Edit of one. This is a side load anchored to a file, built in.
- **Subagents** ([sub-agents](https://code.claude.com/docs/en/sub-agents)). A side piece of work
  runs "in its own context and returns only the summary". It keeps bulk out, not rules in: the
  Mayor's Clerk reading a picture (Rule 30) is this.
- The same pages say where each belongs: an instruction in a standing file "is a request, not a
  guarantee" (the features overview), and "A real guardrail needs to be deterministic"
  ([Steering Claude Code](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more)).

### Retrieval and just-in-time context

- Retrieval-augmented generation pairs what a model holds with what it looks up, "parametric and
  non-parametric memory" ([Lewis et al., 2020](https://arxiv.org/abs/2005.11401)). Active retrieval
  goes further: "actively decide when and what to retrieve across the course of the generation",
  looking up when the next sentence is unsure
  ([Jiang et al., 2023](https://arxiv.org/abs/2305.06983)). That is his side load: retrieval set
  off by what you are about to do.
- Anthropic's context engineering post (29 September 2025) names the aim, the "smallest possible
  set of high-signal tokens", and the "just in time" way: keep light pointers (paths, stored
  queries, links) and load at the moment of use. It also says "runtime exploration is slower than
  retrieving pre-computed data" and recommends a hybrid
  ([Effective context engineering](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)).
- Deferred tools are the measured case. Holding tool definitions back until searched for cut the
  standing cost from about 77K to 8.7K tokens and *raised* accuracy (Opus 4 from 49% to 74%,
  Opus 4.5 from 79.5% to 88.1%): less standing context did the work better, not only cheaper
  ([Advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use)).
- The factory already pulls this way: `mw seat boot` reads an index and `mw seat read <seat>
  <topic>` one section; `mw memory query <rig> <term>` reads facts. Both are pull: the seat must
  think to ask. A side load is the push twin of the same index.

### Tools that say how you use them wrongly

- **Poka-yoke** (Shigeo Shingo, 1960s): "any mechanism in a process that helps an equipment
  operator avoid mistakes", either a *warning* or a *control* that makes the mistake impossible
  ([Wikipedia, Poka-yoke](https://en.wikipedia.org/wiki/Poka-yoke)).
- **Compilers.** A rustc diagnostic says what is wrong and where, keeps the fix in a separate
  help line, and carries a code whose long form is one `--explain` away. A lint has a level
  (allow, warn, deny), and even deny is an error the user can still opt out of
  ([rustc dev guide](https://rustc-dev-guide.rust-lang.org/diagnostics.html)).
- **git's hints.** A hint is shown at the moment, "alongside instructions on how to squelch it",
  and each can be switched off by name ([git-config, advice](https://git-scm.com/docs/git-config)).
- **For models, measured.** SWE-agent's edit command refuses an edit that fails a linter:
  "Invalid edits are discarded, and the agent is asked to try editing the file again." With the
  guard 18.0% of tasks were solved, without it 15.0%. Its rule: "Guardrails mitigate error
  propagation and hasten recovery" ([Yang et al., 2024](https://arxiv.org/abs/2405.15793); the
  numbers are in [the paper's text](https://arxiv.org/html/2405.15793)).
- Anthropic's advice for a tool's errors: "clearly communicate specific and actionable
  improvements, rather than opaque error codes or tracebacks"
  ([Writing tools](https://www.anthropic.com/engineering/writing-tools-for-agents)).

### Context rot and right-sized context

- Chroma tested 18 models (14 July 2025): "performance grows increasingly unreliable as input
  length grows", and one distractor is enough to lower it. The same questions over about 300
  focused tokens against about 113K: "significantly higher performance on focused prompts"
  ([Context Rot](https://www.trychroma.com/research/context-rot)).
- Place matters: "performance is often highest when relevant information occurs at the beginning
  or end of the input context", and it "significantly degrades" in the middle
  ([Liu et al., 2023](https://arxiv.org/abs/2307.03172)). A rule read at boot is in the middle by
  the hundredth turn. A rule that arrives at the act is at the end.
- Count matters: with 500 instructions at once the best of 20 models followed 68%, with a bias to
  the earlier ones ([Jaroslawicz et al., 2025](https://arxiv.org/abs/2507.11538)).
- Claude Code's own line: "target under 200 lines per CLAUDE.md file. Longer files consume more
  context and reduce adherence", and such files are "context, not enforced configuration. To
  block an action regardless of what Claude decides, use a PreToolUse hook instead"
  ([memory](https://code.claude.com/docs/en/memory)).
- One caution for the cure: keep the start of the context stable and only ever append, because
  the cache is what makes a long sitting affordable; and saying a thing again at the end of the
  context helps it hold
  ([Manus](https://manus.im/blog/Context-Engineering-for-AI-Agents-Lessons-from-Building-Manus)).
  A side load appends, so it keeps the cache. A boot sheet that changed with the act would not.

## Where a rule can live

| | It checks the act | It says something beside the act |
|-|-|-|
| **In the tool** (`mw`) | `mw postern send` refuses base64 (mw-gq6.1432) | `mw postern inbox` ends with the line that answers him |
| **In the harness** (a hook) | a hook denies a Bash call ending in `&` | a hook adds two lines after a landing |
| **In the standing context** | nothing checks | the seat remembers |

The table of rules below says **tool guard** for the first row, **hook side load** for the second
and **stays** for the third. Three questions sort a rule:

1. Does the act go through a command of ours, and can that command see the breach in its own
   arguments or in state it can read? Then it is a tool guard. It costs no context until it fires,
   it cannot be forgotten, it is tested, and it works in any harness.
2. If not, is there an act or an event a machine can see, at which the harness can add lines (a
   Bash shape, a file read, a context size, the seat going idle)? Then it is a hook side load.
3. If not, it stays: a duty with no act, an authority, whose word counts, the order of boot.

Because of the catch above, a side load has three ways to be in time. In order of preference:

- **Hang it on the event before the act.** The rules for answering him belong on the output that
  shows his message, not on `mw postern send`. Best done in the tool: no hook is needed.
- **Stop the first try.** For a rare and costly act (closing a window by hand, a smoke that pages
  his phone) the hook denies the first call with the rule as its reason. It costs one more turn.
- **Beside the result, for next time.** Only for an act done many times in a sitting. The weakest.

## What millwright already has

- **Guards, many.** About 1,100 lines of `application/`, outside its tests, speak of refusing, and
  the house style is already the prior art's: say what is wrong, then the fix, and write nothing first
  ("Every refusal comes before the write", `application/pathset.go`). A sample:

| Command | What it does | Where |
|-|-|-|
| `mw path set` | warns of a criterion no Builder can run; refuses `--hold` with no reason, a closed or started story, a release with neither `--after` nor `--hold` | `application/pathset.go` (`notRunnableWarning`, :110, `StaysHeldNoAfterLine`) |
| `mw postern send` | warns over 80 words, refuses over 120 (`--long`); refuses a flat post beside his unanswered one; holds a decision card when his queue is over 8 (`--why-now`) | `application/postern.go:2644`, `posternflat.go:46`, `reviewhold.go`, `dampers.go:42` |
| `mw bd` | waits up to 120 s for the host to be quiet, then runs bd; a story session's `bd` is already this | `application/bdquiet.go`, `infrastructure/sessionshell` (`WithBdShim`) |
| `mw note` | fills `{now}` with the time itself | `application/note.go:15` |
| `mw file`, `mw tester file`, `mw dispatch` | refuse an epic missing what its rig requires, a guest rig with no ask, a story with no acceptance, a bug with no Sweep | `application/epicrules.go`, `guest.go`, `domain/plan.go`, `domain/sweep.go` |
| `mw seat handover` | refuses while the boot set is over its ceiling, naming the file to shorten | `application/seatboot_manifest.go:477` |
| `mw next`, `mw check` | refuse a commit signed as a machine's work, a story with no closing comment, an unproved grind example | `application/next.go`, `batchland.go:800` |

- **The paste rules of mw-gq6.1432** (in progress; `application/pasterules.go` is not in this
  checkout yet): one table of named rules run by `mw postern send`, `mw talk say` and
  `mw hands add`, each with its reason, an `--allow <rule> --allow-why` written on the bead, and
  the help printed from the same table. This is the right shape, and the rest of this page leans
  on it.
- **A boot index and reads by need** (`seats/<seat>/boot.toml`, `mw seat boot`, `mw seat read`,
  `docs/seat-boot.md`). `mw memory query` logs each use (`application/memory.go`, `logQuery`);
  `mw seat read` logs nothing, so nobody knows which topics are read and which never are.
- **Saved prompts** (`/do`, `/later`, `/groom`): his word brings the instructions with it. A side
  load keyed on him, in use.
- **One hook**, the unattended seat's, as above.
- **Formula steps**: closing one step makes the next ready. Their text is all in the boot file
  from the start, though the tool that closes a step could print the next one.

What is missing is the way back. When a guard lands, the rule's text stays in the sheet: L70 says
"Since mw-t5shinh.26 ... `mw postern send --class decision-needed` holds such a card itself", and
its 1,148 bytes have been read at every sitting since. And no guard counts its hits.

## The rules of procedures.md, one by one

68 rules: Boot 9, Rules that bite every sitting 17, Who is talking 5, Handing off 1, Standing
asks 36. (Read by need is an index, not rules.) "Exists" means the guard is in the code today.

| Rule | Where it would move | How, and why |
|-|-|-|
| Boot 6 (L12): never `bd mail reply`; no `--file` on `bd mail send` | tool guard | both are in the arguments `mw bd` already reads; it can refuse with the form that works |
| Boot 8, Rule 8 (L14, L29): rig memory over 8000 bytes is cut before the next un-hold | tool guard | `mw status` names the rig; the un-hold itself knows the rig and can warn |
| Rule 1 (L21): ONE `bd`, `go` or `mw sync` at a time | tool guard | exists for bd (`mw bd`); a seat's session has no shim yet, so the rule asks the Mayor to prefer it |
| Rule 1a (L22): no `bd dolt push` on the home while the backup is halted | tool guard | `mw bd` knows the home; a rule with an end date belongs in code that is deleted with it |
| Rule 3 (L24): `bd comment` takes no `-m`; the grep that reads a closing comment | tool guard | an argument form is the tool's to catch; the grep is a command waiting to be written |
| Rule 5 (L26): every written time comes from `date -u` | tool guard | exists in `mw note` (`{now}`); the same word in `mw postern send` and in comments ends the rule |
| Rule 7 (L28): hold, edit, un-hold; new beads held; a hold has a reason | tool guard | half exists in `mw path set`; the rest is `mw bd` refusing an edit of an open story |
| Rule 11 (L31): verify a landing before releasing what waits on it | tool guard | the VERIFIED word is on the bead, so a release can name the unverified landing above it |
| Rule 28 (L34): one `mw talk wait` only; `--repathed` | tool guard | a lock in `mw talk wait` replaces the `pgrep`; none exists today |
| Rule 31 (L37): a Windows paste is ONE line | tool guard | mw-gq6.1432, its row windows-paste-one-line |
| Ask L61: a receipt to every message within a minute | tool guard | the commands that show his message end with the answering line, `--re` and his txid filled in |
| Ask L64: a hands step is one whole `mw hands add` | tool guard | `mw hands add` checks its own arguments (it refuses a second step of one name today) |
| Ask L65: a grilling question is a decision card, A/B, on an OPEN bead | tool guard | the send can refuse a card with no recommendation, under two options, or on a closed bead |
| Ask L66: link all a post names | tool guard | rows for the paste table: a short bead id, a bare commit, a list with no blank line after |
| Ask L70: his queue's limit of 8 holds for questions | tool guard | exists since 2026-11-10 (`reviewhold.go`); 1,148 bytes still read. One line stays for the two cards mw does not hold |
| Ask L71: feedback from an app | tool guard | the mail is mw's own; its foot can carry the two lines, outside the FEEDBACK lines |
| Ask L72: his outside things; closed only on his word | tool guard | the filing can refuse a to-do with no `Open:` line; the close can ask for his words |
| Ask L73: every bug carries a Sweep | tool guard | exists (`domain/sweep.go`; `mw dispatch` passes over a bug without one) |
| Ask L74: fold three or more bugs of one cause | tool guard | `mw status` can name the pile; the 1,702 bytes are a weekly procedure for loop.md's Fold |
| Ask L75: every failed story gets a cause | tool guard | a retry of a failed story with no `mw cause` can be refused |
| Ask L80: a parked want | tool guard | exists as the saved prompt `/later`: his word brings it |
| Ask L81: code in ONE fenced block; a paste names a new PowerShell | tool guard | mw-gq6.1432, two of its rows |
| Ask L83: answer in HIS thread with `--re` | tool guard | exists (`posternflat.go:46`); 504 bytes still read |
| Ask L84: answer a card in its thread; the grep for a short txid | tool guard | `--re` can take the short txid and widen it itself |
| Ask L85: HOW TO CHECK IT on every landing he can see | tool guard | exists: the Builder's formula asks for it, `mw next` refuses a story with no closing comment, `mw lint-checkit` checks the labels |
| Ask L86: VERIFIED is the comment's first word | tool guard | `mw verify` writes the word; `mw bd` can refuse a comment where a time comes first |
| Ask L88: a demo closes on his word | tool guard | the same close-on-his-word check as L72 |
| Ask L89: factory-internal demos get the label `digest` | tool guard | the verifying command can add the label |
| Ask L92: Talk mode's command lines | tool guard | `mw talk wait` can print the `mw talk say` line with the talk and turn filled in |
| Ask L93: the Deputy by default while he is active | tool guard | `mw bd` knows the seat and when he last wrote; a Mayor's bead write then gets a warning |
| Ask L95: `mw postern inbox` for every message | tool guard | `mw talk wait` can name the picture itself |
| Boot 2 (L8): which topic to read when | hook side load | this is the side load's own table, held today in the seat's head |
| Boot 5 (L11): closing the old window by hand | hook side load | rare, and a bare `tmux kill-window`: stop the first try and say the conditions |
| Boot 7 (L13): the first sitting after a night | hook side load | a line at the session's start, only while the night's stale.md is unread |
| Rule 2 (L23): no rig tests beside a sync | hook side load | `go test` and `make test` are not ours; a hook on those two can deny and name `contrib/mw-heavy` |
| Rule 12 (L32): hand off at about 180K | hook side load | a hook is given the transcript's path and can measure it, then say so once |
| Rule 27 (L33): never a trailing `&` | hook side load | a shape of the Bash call; only the harness sees it |
| Rule 29a (L35): `mw grist smoke` pages his phone | hook side load | it needs judgement (has every cause landed?): stop the first try with the question |
| Rule 30 (L36): a picture is read by a Clerk | hook side load | the act is a Read of an image file |
| Handing off (L57) | hook side load | read by need already; the Rule 12 side load names it at the moment |
| Ask L68: a story stopped by what is not its own code | hook side load | beside the output that reports the failed story |
| Ask L87: VERIFIED from his use; every screenshot against `bin/mayor-unverified` | hook side load | the same act as Rule 30 |
| Ask L91: the Talk line is always open | hook side load | when the seat goes idle a hook can see no `mw talk wait` alive, and say so |
| Rule 4 and Who is talking, all five (L25, L48 to L52) | stays | whose word counts must be known before any text arrives; a side load comes in the very channel these rules say to doubt |
| Boot 1, 3, 4, 9 (L7, L9, L10, L15) | stays | the order of boot, needed before the first act |
| Rule 10; Asks L62, L67, L76, L79 | stays | authority and how to read him: the bead wins, who releases, who sets priorities, `/do` by default |
| Asks L63, L69, L77, L82, L90, L96 | stays | duties with no act to hang on: clear what waits on the Mayor, never the bottleneck, read the health report, update the cards, no dead time, read all he sends |
| Rule 6 and Ask L78 (L27, L78): a harness refusal ends that road; a refusal needed again | stays | the moment can be seen, but the harness's denial hook can only allow a retry, not add lines; a rule whose miss parks the window for hours stays until a way to deliver it is proven. A hook can count denials meanwhile |
| Ask L94: mail brings the Deputy up by itself | stays | a fact, not a rule; it could simply go |

**24 rules stay**: the last six rows, 6 + 4 + 5 + 6 + 2 + 1. **32 move to a tool guard** and **12
to a hook side load**: 68 in all.

| Where | Rules | Bytes of the sheet | Of them read every sitting |
|-|-|-|-|
| tool guard | 32 | 10,335 | 10,335 (56%) |
| hook side load | 12 | 3,868 | 2,956 (16%); Handing off is read by need already |
| stays | 24 | 4,865 | 4,865 (27%) |

These are upper bounds. A moved rule often leaves a line behind, and a side load that fires puts
its lines back for the rest of that sitting. Already cuttable, because the guard exists: L70, L83,
L73 and L85, 2,024 bytes; with mw-gq6.1432 landed, L37 and L81 too, 2,745 bytes.

## The other seats

- **Builder.** Its rig memory is already a set of notes with an anchor: most facts carry a file
  path as their subject (`[application/pathset.go] ...`). Today a Builder is asked to pull ("before
  touching one, run mw memory query"). A side load would push a fact the first time the Builder
  reads or edits that file, which is exactly what a path rule does. The boot render would then
  hold only the facts with no file to hang on, and the 8,000-byte budget would stop being the limit
  on what a rig can remember. Second: the step texts (5,701 bytes here) could arrive from the
  command that closes the step before, at the end of the context where a rule holds best.
- **Deputy and Millhand.** Nothing was measured for them here. The Deputy's work is nearly all
  `mw` and `bd` commands, so guards serve it most; its crib of exact command lines (3,089 bytes)
  is the kind of text a tool's own messages replace.

## Postern's seats and Wicket

Postern's seats are the ones whose words reach him on his phone: the Mayor (posts, cards, Talk),
and the Deputy writing on the Mayor's word. All of it leaves through a few commands,
`mw postern send`, `mw talk say`, `mw hands add`, `mw card send`: one narrow door, which is why
the paste rules fit there.

Wicket is his tool at work. All this survey knows of it is his own words: it "is based on"
Postern, and "at work we've started having I think it's like a say tool ... that has different
filters built in"; and from a plan of 2026-10-22, "Wicket itself is outside the factory". Nothing
here was checked against Wicket: this session cannot see it, and should not.

| | Shared: the same shape serves both | Not shared |
|-|-|-|
| The say path | one check run by every command that speaks to a person: named rules, each with its reason, its fix and an override that is written down. Wicket's say tool has this already; mw-gq6.1432 brings it here | the rows. Ours name PowerShell, base64 and 80 words on his phone; Wicket's are his work's |
| The side load | a record of four parts: the lines, the act, how the act is matched, where the rule came from | the acts. Ours are `mw` and `bd` commands and his messages |
| Delivery | in the tool, which works in any harness | the hook half needs a harness with hooks; which one Wicket's seats run in is not known here |
| The measure | hits and overrides for each rule; bytes of standing context | every number, message and rule text: work's stay at work, his stay here |
| Who approves a rule | | here the Governor; there his team |

So the half that carries is the tool half, and Wicket is ahead on it. What could go back to his
work is the sorting test, the timing catch, and the counting. Whether anything may cross in either
direction is his to say.

## Risks

- **A side load that never arrives, silently.** A wrong field or a matcher that matches nothing
  gives no error. Cure: count every match from the first day, and keep the standing line until the
  count shows the side load arriving.
- **Too late.** The catch above. Cure: the three ways to be in time, the tool first.
- **It piles up.** What a side load adds stays all sitting. A sitting that sets off every rule
  holds them all again. Cure: once a sitting for each rule, a few lines each, never on a common
  act (`bd show`, a grep), and a total for a sitting.
- **Authority.** A side load arrives as text the harness inserted, the kind Who is talking says to
  doubt. Cure, as a rule that stays: a side load can remind and can stop, and can never grant,
  approve or stand for his word. Its lines come only from the vault's table, never from a bead, a
  mail or a message.
- **False refusals, in front of him.** Cure: every guard has a name and an override whose reason
  is written down, as mw-gq6.1432 has; a new one warns before it refuses; overrides are counted,
  and a guard that is mostly overridden is wrong.
- **One harness.** The hook's shape is Claude Code's and it changes (a field of `PreToolUse` is
  already deprecated). Cure: the hook is one settings line calling one `mw` command, with the
  table and the logic in mw, and a test against one real session.
- **Two copies drifting.** Cure: the tool's table is the rule; the sheet names nothing the table
  holds.
- **The cut never happens.** L70 and L83 show it. Cure: a guard's story ends by naming the lines
  of the sheet that can now go.

## What it would be measured by

- **Bytes of standing context** for each seat at each sitting: the boot set and Standing asks.
- **Hits for each guard and side load**, by name. None in a month: retire it. Many: it earns its
  keep, and its standing line can go.
- **Overrides** for each guard.
- **Misses**: how often he has to say a rule again, and `mw cause ... process` records.
- **Reads by need**: `mw seat read` and `mw memory query`, by topic.

Bytes show the cost and misses show the worth. The health report has neither today.

## Options

A sketch of option B's table, to make it concrete. Nothing is built, and the names are his to
choose:

```toml
# seats/mayor/sideload.toml
[[rule]]
name = "talk-line-open"             # procedures.md Ask L91
on   = "Stop"
when = "no mw talk wait is alive"
say  = "The Talk line is always open: arm mw talk wait before you go idle."

[[rule]]
name = "picture-to-a-clerk"         # Rule 30 and Ask L87
on   = "PreToolUse"
tool = "Read"
path = "*.jpg"
stop = "first"                      # deny the first try; say is the reason
say  = "A Clerk reads his picture and reports in words; compare it with bin/mayor-unverified first."
```

| | A: guards first | B: A, and a side-load hook, in shadow first | C: the harness's own skills and path rules |
|-|-|-|-|
| What | Finish mw-gq6.1432. Grow its table; give `mw bd` a table of refused forms; next-step lines in the outputs that show his message; cut each rule's text when its guard lands. Count hits | One command, `mw sideload`, fed by a table in the vault and called by one hook line in the settings mw already passes. A week counting only; then on, rule by rule | Write the long procedures as skills and the rig memory as `.claude/rules` files |
| Tokens saved a Mayor sitting | 0.8K at once with no build (2,024 bytes); 1.1K when mw-gq6.1432 lands; up to 4.3K with all 32 | A's, and up to 1.2K more, less what fires; and Standing asks need no longer be read whole before the first receipt | Little for one-line rules: a skill's description is always in context and is as long as the rule. Real only for long procedures, which `mw seat read` serves already |
| Build | mw-gq6.1432 (M, in progress), then about 8 small stories, grouped by table | One M (the command, the table, once a sitting, the count), two S (the wiring and the shadow switch; the report), then the Mayor's table | S: files only. But a second home for rules, outside mw's tests and counts |
| Risk | Low. Sure to fire, tested, any harness. A false refusal is the one to watch | Medium. One harness, the timing catch, silent misses, piling up; all four are what the shadow week measures | Medium to high for a rule that must hold: the model chooses whether to load it. Builders have the Skill tool off |
| Serves Wicket | Yes, as a shape | Only if its harness has hooks | Only on Claude Code |

## Recommendation

**A now, and B's shadow week beside it; C only as one trial.**

1. Cut the four rules whose guard has landed (L70, L83, L73, L85) to a line each or to nothing:
   2,024 bytes, today, no build. It is the Mayor's sheet, and the asks in it are his: question 2.
2. Let mw-gq6.1432 land, cut L37 and L81, and make it the pattern: one table, named rules, the
   reason, the override written down, the help printed from the table.
3. Count: a guard's hits and overrides by name, and `mw seat read` by topic. Small, and every
   later choice rests on it.
4. Work down the tool guard rows in order of bytes and of misses: L70's leftover, L74 (to loop.md),
   Rule 7, L65, Rule 1 for seats, the answering line for L61.
5. Build the side-load command and run it in shadow for a week on the Mayor: it adds nothing to
   any context and counts which of the 12 rules would have fired, how often, and how many bytes.
   Switch on rule by rule only where the numbers say it saves.
6. One trial of C's idea through B's hook: a Builder's rig memory by file path, on one rig.

Why this order: the tool half is sure, cheap, and the half that Wicket shares. The hook half is
where the unknowns are, and a week of counting costs almost nothing and removes most of them.
Whether cutting or moving a rule needs his yes each time is question 3.

## Questions to grill him on

1. Is the sorting test right: in the tool when the tool can see the breach, beside the act when only the act can be seen, in the standing sheet only when nothing can be seen?
2. When a guard lands, may the Mayor cut that rule's text from the sheet at once, your standing asks included, or does a line stay for two weeks first?
3. Is moving a rule into a tool a change to the factory's rules that needs your yes each time, or is it the Mayor's to do as long as the rule's words and reason do not change?
4. Should a new guard refuse from its first day, or warn for a week and then refuse?
5. May a side load stop an act (deny the first try and say why), or may it only add lines beside it?
6. Do you want the side-load hook built now in shadow, counting only, so that switching it on is decided by numbers?
7. Which is the measure you care about more: fewer times you have to say a rule again, or fewer bytes at boot?
8. Is "sidewalk" Google Sidewiki (2009 to 2011), or another one you remember?
9. What does Wicket's say tool filter today, and which of its filters would you want here?
10. Which harness do Wicket's seats run in, and does it have hooks?
11. May the design (the sorting test, the table's shape, the shadow numbers) go to your work, and may anything come back, or must the two stay apart?
12. Of the 24 rules that stay, are there any you would rather delete than keep?
13. Should the Builder's rig memory by file path be tried on one rig first, and which?
