# Where Haiku 5.5 earns a place in the factory

Researched for mw-2zh33g.8 (epic mw-2zh33g, factory health measured), 2026-10-15, on the
Laptop. The Governor's ask (Postern general, 2026-10-15 04:31Z): Haiku 5.5 landed about eight
days ago; find its sweet spot of cost, speed and ability, where it has room in the factory
(grist above all), "with the proper oversight", stepping up to a stronger model when it falls
short, applied "in measured ways and wise ways ... bit by bit", with A/B tests.

Every number below comes from runs made in this story (666 grind answers, 5 story replays, 1
Tester replay, 7 Clerk-style runs) or from the factory's own records (the Builder ledger, the
mill's `grinds.jsonl`). Vendor claims are marked as such. Nothing was changed: no grind, no
seat, no charter, no config; every run was offline or in a scratch worktree, never pushed.
The raw data and the scripts are in `docs/research/haiku-5-5/`.

**Fuel spent by the trial: $15.41 list price** (grinds $14.28, story and Tester replays $1.03,
Clerk runs $0.10). The bound was ~$15. It was passed by $0.41 because the first lampas grind
runner was not stopped when I meant to stop it (I killed its shell wrapper, not the runner) and
ran the whole Sonnet bible-talk set, $7.76, beside the trimmed re-run; every spending process
was stopped the moment the tally showed it. What that cost the research: the Sonnet side of the
Clerk/triage/Deputy A/B (0 of 6 runs) and the second repetition of the Haiku side were never
run. What it bought: two runs of most lampas examples, so flake can be told from deficit.

## The one-line answers

| Candidate use | Verdict | Why (numbers below) |
|---|---|---|
| Lampas `tips` | already on Haiku 5.5 (the `haiku` alias moved under it) | 10/10 passes at low; 227 production answers in 14 days, 10 failed; nobody chose this |
| Lampas `bible-talk` (the big one: 1982 answers / 14 days) | **use Haiku at high**, via the rollout below | 85/85 passes at high vs 53/53 Sonnet; $0.008 vs $0.144 an answer; 9 s vs 6 s |
| Lampas `verse-ask` | use Haiku at high, second slice | 38/38 at high vs 38/38 Sonnet; $0.003 vs $0.043; 8 s vs 6 s |
| Lampas `verse-read` (audio-scored) | try later: Haiku at medium, after the two above | 8/8 at medium; $0.0025 vs $0.037; but 11 s vs 8 s on a grind the phone waits on |
| Lampas `find-refs` | use Haiku at low, folded into the lampas slice | 2/2 at low; $0.001 vs $0.020 |
| Cairn `sweep`, TradeTracker `item-from-photos`, `receipt-reconcile` | use Haiku at the grind's own effort (low/medium) | every example passes at every effort; $0.001 vs $0.013-0.021 |
| Legend `item-value`, `coin-rarity`, `legend-ask` | use Haiku at low/medium | every example passes; $0.001-0.002 vs $0.014-0.027 |
| Legend `coin-from-photos` | **not** Haiku | 5/7 at every effort: a token's denomination and a mint mark it does not see; Sonnet 7/7 |
| SpellForge `parent-ask`, `word-list` | use Haiku at the grind's own effort | all pass; the examples are few (2 each): widen them first |
| SpellForge `tutor-turn` (Opus high today) | not yet: try later with the tutor-turn eval | 2/2 examples at high ($0.0025 vs $0.089), but two examples and a child's tutor: the 11-case `grinds/tutor-turn.eval` must run first |
| Chores and one-file bugs (Builder stories) | **try as a trial: Haiku at high** on `chore`-formula and `files=` one-file stories | 5/5 replays landed the right fix with the gate green, at $0.013-0.087 against Sonnet's $0.10-0.25; 3-10 min against 2-8 |
| Tester stories | not Haiku at xhigh; maybe at high, later | one replay: 3 taste findings (the original's and two more), no bugs, but $0.81 and 20 min against Sonnet's $0.72 and 6 min; it guessed its fuel line |
| Clerks (lookups, a long output reduced to an answer) | try Haiku at high as the Clerk, A/B against Sonnet | 7/7 and 6/6 fields right at high on two real reductions, $0.003 and $0.031; low got 3/6 on the big one; no Sonnet run was made |
| The Deputy (bd writes from the Mayor's mail) | not yet: dry-run only, then a scratch database | the dry run wrote all 24 checked things right at low and high ($0.001); it never ran a command |
| Triage passes (the Mayor's nightly pass) | the clerical half is a Clerk's job (above); the judgement half stays with the Mayor | the clerical half: 6/6 at high, 3/6 at low, 3 min either way on 417 beads |
| `mw report`, `mw cause`, `mw status` summaries | nothing to switch | no model is spent there: they are deterministic Go |
| Mayor, Millhand, Builder on feature stories | out of scope and not recommended | not measured; the Governor's "you got to be wise" |

Haiku 5.5 at **max** is never the right effort for a grind: it thinks 10-20k tokens over a
schema answer, takes 25-90 s where medium takes 4-11 s, costs 2-5x more than Haiku at medium,
and passes no more examples (it failed two that medium passed). **xhigh** on stories and the
Tester likewise bought turns and thinking tokens, not a better result: Haiku high did the
one-file bug in 212 s for $0.013; xhigh 331 s for $0.031; max 613 s for $0.087, all three right.

## Sweep: every place a model is named today

Read on 2026-10-15 from the Laptop's checkouts of each app rig (all on `main`, clean), the vault,
`~/.config/mw/config.toml` and this rig. `claude --model haiku` on the Laptop (Claude Code 2.1.300)
already resolves to `claude-haiku-5-5` (its result JSON names the model): the one grind that
names `haiku` has been on Haiku 5.5 since the alias moved, with nobody deciding it.

| Where | What | Model | Effort | Set by |
|---|---|---|---|---|
| lampas `grinds/bible-talk.json` | grind | sonnet | medium | the rig |
| lampas `grinds/verse-ask.json` | grind | sonnet | medium | the rig |
| lampas `grinds/verse-read.json` | grind (audio-scored) | sonnet | medium | the rig |
| lampas `grinds/find-refs.json` | grind (photo) | sonnet | low | the rig |
| lampas `grinds/tips.json` | grind | **haiku** (= 5.5 now) | low | the rig |
| lampas `grinds/feedback.json` | forward to the Mayor | none | none | the rig |
| legend `grinds/coin-from-photos.json` | grind (photos) | sonnet | medium | the rig |
| legend `grinds/legend-ask.json` | grind (photos) | sonnet | medium | the rig |
| legend `grinds/coin-rarity.json` | grind | sonnet | low | the rig |
| legend `grinds/item-value.json` | grind | sonnet | low | the rig |
| legend `grinds/feedback.json` | forward to the Mayor | none | none | the rig |
| spell-forge `grinds/tutor-turn.json` | grind (photos, audio) | **opus** | high | the rig |
| spell-forge `grinds/parent-ask.json` | grind | sonnet | high | the rig |
| spell-forge `grinds/word-list.json` | grind (photo) | sonnet | medium | the rig |
| trade-tracker `grinds/item-from-photos.json` | grind (photos) | sonnet | low | the rig |
| trade-tracker `grinds/receipt-reconcile.json` | grind (photo) | sonnet | medium | the rig |
| cairn `grinds/sweep.json` | grind (photos) | sonnet | low | the rig |
| `[grist] models`, `efforts` (config, defaults) | ceiling on what a grind or a grist may ask | haiku, sonnet, opus | low, medium, high | `infrastructure/config` `DefaultGristModels`, `DefaultGristEfforts` |
| stories (path default) | Builder sessions | sonnet | high | the Mayor's charter ("Default story route is Sonnet at high effort"); `mw file` writes it per story |
| stories the Mayor routes up | Builder sessions | opus or fable | high | the Mayor, reason on the bead |
| `formulas/*.formula.json` | chore, tdd-feature, tester | none | none | formulas name no model |
| `[tester]` (config) | Tester sessions | sonnet | high | `~/.config/mw/config.toml`; defaults `DefaultTesterModel`, `DefaultTesterEffort` |
| `deputy_model` (config default) | the Deputy | sonnet | high (`application.DeputyEffort`, fixed) | config default `DefaultDeputyModel`; the Deputy charter says the same |
| `millhand_routine_model` (default) | Millhand routine wake | sonnet | high (`application.MillhandEffort`, fixed) | config default; the Millhand charter |
| `millhand_review_model` (default) | Millhand review wake | opus | high | config default; the Millhand charter |
| vault `.millhand-laptop-launch.sh` | the Laptop's Millhand by hand | opus | high | the launch script |
| vault `.mayor-launch.sh`, `bin/respawn-mayor` | the Mayor | `claude-opus-5-5` | high | the script; `MW_MAYOR_MODEL` or its second argument |
| Mayor charter | Clerks | sonnet | high | the charter (changing it is the Governor's word) |
| `application.TalkModels`, `domain.FullModelID` | `mw talk model <m>` chips and the Mayor respawn line | opus, sonnet, fable, haiku | high on respawn | code; **haiku maps to `claude-haiku-4-5-20251001`**, stale now that the alias is 5.5 (Problems found) |
| `mw report`, `mw cause`, `mw status`, the triage dump | summaries | none | | deterministic Go, no model |
| the nightly triage pass | the Mayor reads `bd` output in his own session, or hands it to a Clerk | opus / sonnet | high | practice, not config |

## Prices (vendor, for reading the tables)

List prices per million tokens, from third-party pricing pages read on 2026-10-15, not
Anthropic's own page: Haiku 5.5 $0.10 in, $0.50 out, cache read $0.01, for prompts up to 100k
tokens (5x those above 100k); Sonnet 5.5 $2 in, $10 out, cache read $0.20, cache write $2.50
(5 min) or $4 (1 h). The costs in the tables are Claude Code's own `total_cost_usd`, which the
factory already uses as the one number that compares two models (`application.SessionResult.CostUSD`);
on the Governor's Max plan it is not a bill. Checked against the ledger: mw-gq6.179's result file
(30 in, 5,194 out, 444,061 cache read, 27,455 cache write) prices to exactly $0.2506 at Sonnet's
$2/$10/$0.20/$4, so Claude Code bills the 1-hour cache write.

## Method

**Grinds.** A scratch Go program (`docs/research/haiku-5-5/replay-runner.go.txt`, built against this
rig's `application` and `infrastructure/claude` packages) replays every example under
`grinds/examples/<kind>/` of an app rig exactly as the mill runs a grist: the mill's preamble (with
its random tag) then the grind's instructions as the system prompt, the answer schema as
`--json-schema`, the same `claude --print` flags (`infrastructure/claude.GrindArgs`), photos
copied into a private directory, and for `verse-read` the recording scored first with the local
scorer (`mw grist score --engine local`) and given as `reading_result`, as the mill does. Each
answer is held to the grind's schema (`application.SchemaViolations`) and the example's `expect`
(`application.CheckExpect`): the same checks `mw grist smoke` makes. It never touches the backend.
Every grind with examples was run on its own model at its own effort, and on Haiku 5.5 at the
grind's own effort, at high and at max; the second lampas runner, running unmeant beside the first,
gave most lampas examples a second run. Opus `tutor-turn` was run only at its own effort (two
examples). Cold starts throughout, 3 sessions at once, on a Laptop whose 1-minute load sat at 15-17
of 20 cores for the whole trial (other Builders at work): wall seconds are high for every model
alike, so read `api s` (the model's own time) for speed and `wall s` for what a phone would wait.
The production medians from `grinds.jsonl` (14 days) are given for today's model as a check.

**Stories.** Three small landed millwright stories were replayed from their starting commit (the
parent of their landing) in a scratch worktree or a clean clone cut at that commit, with the same
boot file their Builder had (charter, rig memory, story) except for a formula section telling
the session it was a replay: never run bd or mw check, commit on the detached HEAD, write the
closing comment to a file. Same flags as `mw dispatch` gives a Builder (`--permission-mode auto`,
`--permission-prompts none`, the attribution-off settings). The gate was then read from the
session's own `make test` log and the acceptance commands re-run by hand in the worktree. The
first two replays shared the rig's repository, so later history was visible in `git log --all`;
the 298 replay noticed the landed commit's subject and says it did not open it. The last three
ran in clones with nothing after the start commit.

**Tester.** The Tester story mw-5r3p30.151 (Sonnet high, $0.72) was replayed on Haiku xhigh from a
detached lampas worktree with the tester formula's steps, `mw tester file` forbidden and FINDINGS
written to a file.

**Clerk, triage, Deputy.** Three Clerk-sized errands with deterministic truth computed by script:
(a) 43 real ledger rows reduced to seven figures (counts, a sum, an id); (b) the clerical half of a
triage pass over the 417 open work beads of this vault (counts per rig, claimed ids, missing path
keys, duplicate titles, oldest P1s); (c) a Deputy dry run: a realistic Mayor FILE AND RELEASE mail
turned into the exact bd command lines, checked by 24 regexes (held create, type, label, priority,
parent, body and acceptance files, exact title, seven keys one per command, dependency, dated
comment, open last, read back, mail reply, `--actor root`, no push). No tools, structured output
for (a) and (b). Planned: Haiku low, high, max and Sonnet high, twice each. Run before the stop:
Haiku low and high once each on all three, Haiku max on (a); the Haiku max triage run was killed
unfinished and the Sonnet side never ran.

## Grinds: pass rate, seconds and cost per answer

Full table with every grind in `docs/research/haiku-5-5/grind-tables.md`; every run in
`grind-runs.jsonl`. "Runs" counts examples times repetitions. Production = `grinds.jsonl`, last 14
days, answered grists, median total seconds (warm pool, so faster than a cold start here).

| app / grind | model:effort | runs | pass | api s (median) | wall s | $ / answer | out tokens | production: answers, failed, median s |
|---|---|---|---|---|---|---|---|---|
| lampas bible-talk | sonnet:medium (today) | 53 | 53 (100%) | 4.2 | 5.9 | 0.1440 | 250 | 1982, 69, 5.1 s |
| | haiku:medium | 85 | 84 (99%) | 5.8 | 7.8 | 0.0076 | 921 | |
| | **haiku:high** | 85 | 85 (100%) | 7.1 | 9.3 | 0.0078 | 1321 | |
| | haiku:max | 82 | 82 (100%) | 44.1 | 45.2 | 0.0125 | 10820 | |
| lampas verse-ask | sonnet:medium (today) | 38 | 38 | 4.2 | 5.7 | 0.0427 | 296 | 634, 39, 4.6 s |
| | haiku:medium | 38 | 37 (97%) | 5.9 | 7.5 | 0.0026 | 1189 | |
| | **haiku:high** | 38 | 38 | 6.6 | 8.3 | 0.0027 | 1348 | |
| | haiku:max | 38 | 36 (95%) | 46.5 | 47.8 | 0.0078 | 11676 | |
| lampas verse-read | sonnet:medium (today) | 9 | 9 | 6.5 | 8.2 | 0.0371 | 640 | 266, 12, 8.6 s |
| | haiku:medium | 8 | 8 | 9.1 | 11.1 | 0.0025 | 1794 | |
| | haiku:high | 8 | 8 | 13.5 | 15.2 | 0.0031 | 2858 | |
| | haiku:max | 6 | 6 | 88.3 | 89.4 | 0.0128 | 22658 | |
| lampas tips | **haiku:low (today)** | 10 | 10 | 2.7 | 4.4 | 0.0010 | 367 | 217, 10, 3.4 s |
| | sonnet:low | 10 | 10 | 2.8 | 4.2 | 0.0182 | 181 | |
| | haiku:high / max | 10 / 10 | 10 / 10 | 3.4 / 24.3 | 5.0 / 25.5 | 0.0011 / 0.0039 | 596 / 6096 | |
| lampas find-refs | sonnet:low (today) | 2 | 2 | 2.6 | 4.8 | 0.0202 | 152 | 12, 2, 4.8 s |
| | haiku:low / high / max | 2 each | all | 2.3 / 2.2 / 3.7 | 4.4 / 4.2 / 5.5 | 0.0010 / 0.0010 / 0.0012 | | |
| legend coin-from-photos | sonnet:medium (today) | 7 | 7 | 6.6 | 8.7 | 0.0266 | 871 | 139, 0, 9.6 s |
| | haiku:medium / high / max | 7 each | **5 / 5 / 5 (71%)** | 7.5 / 10.4 / 64.5 | 9.0 / 11.9 / 66.5 | 0.0018 / 0.0022 / 0.0094 | | |
| legend legend-ask | sonnet:medium (today) | 4 | 4 | 6.0 | 8.2 | 0.0265 | 566 | 26, 0, 7.5 s |
| | haiku:medium / high / max | 4 each | all | 7.7 / 10.0 / 55.6 | | 0.0017 / 0.0018 / 0.0078 | | |
| legend item-value | sonnet:low (today) | 4 | 4 | 2.5 | 4.8 | 0.0135 | 226 | 140, 0, 4.0 s |
| | haiku:low / high / max | 4 each | all | 2.8 / 4.0 / 21.7 | | 0.0008 / 0.0009 / 0.0032 | | |
| legend coin-rarity | sonnet:low (today) | 2 | 2 | 3.1 | 4.7 | 0.0146 | 350 | 12, 0, 5.2 s |
| | haiku:low / high / max | 2 each | all | 3.4 / 5.9 / 39.1 | | 0.0009 / 0.0011 / 0.0053 | | |
| spellforge tutor-turn | opus:high (today) | 2 | 2 | 9.1 | 11.0 | 0.0891 | 860 | 70, 0, 12.7 s |
| | haiku:high / max | 2 each | all | 8.3 / 33.2 | 12.7 / 34.8 | 0.0025 / 0.0061 | 1276 / 8544 | |
| spellforge parent-ask | sonnet:high (today) | 2 | 2 | 3.3 | 4.7 | 0.0132 | 258 | 28, 0, 5.1 s |
| | haiku:high / max | 4 / 2 | all | 3.1 / 10.9 | | 0.0007 / 0.0017 | | |
| spellforge word-list | sonnet:medium (today) | 2 | 2 | 3.4 | 4.9 | 0.0137 | 216 | 29, 0, 5.3 s |
| | haiku:medium / high / max | 2 each | all | 3.0 / 2.4 / 6.7 | | 0.0008 / 0.0008 / 0.0010 | | |
| trade-tracker item-from-photos | sonnet:low (today) | 5 | 5 | 3.5 | 5.1 | 0.0152 | 256 | 103, 0, 5.1 s |
| | haiku:low / high / max | 5 each | all | 2.5 / 2.4 / 5.6 | | 0.0008 / 0.0009 / 0.0012 | | |
| trade-tracker receipt-reconcile | sonnet:medium (today) | 2 | 2 | 4.3 | 5.8 | 0.0212 | 503 | 19, 0, 7.1 s |
| | haiku:medium / high / max | 2 each | all | 4.6 / 4.0 / 18.9 | | 0.0013 / 0.0011 / 0.0032 | | |
| cairn sweep | sonnet:low (today) | 3 | 3 | 3.0 | 4.6 | 0.0126 | 252 | 45, 0, 5.0 s |
| | haiku:low / high / max | 3 each | all | 2.4 / 2.5 / 5.8 | | 0.0007 / 0.0009 / 0.0009 | | |

Across all 666 runs: Sonnet (today's model) 143 runs, 100% pass; Haiku high 178 runs, 99%;
Haiku medium 146, 97%; Haiku low 26, 100%; Haiku max 171, 98%.

**Every Haiku failure (10 of 523 runs):**

- legend/coin-from-photos/subway-token, every effort (3 runs): `denomination` "Unknown", wanted
  token/NYC/transit/subway. Haiku does not read the token's legend; Sonnet does.
- legend/coin-from-photos/washington-quarter-1944-s, every effort (3 runs): `mintMark` null,
  wanted "S". Haiku does not find the small mint mark in the photo; Sonnet does.
- lampas/bible-talk/rusty-offer, haiku:medium (1 of its 2 runs at medium failed; high and max
  passed): the answer did not say "rusty". A reading-of-the-memory miss, not a schema one.
- lampas/verse-ask/profile-slow-hands-on, haiku:medium (1 of 2 runs failed, the other passed):
  the answer taught the word without the try-it-aloud turn the profile asks for. Flake at medium.
- lampas/verse-ask/bridge-quite-a-bit, haiku:max (both runs failed; medium and high passed): the
  long-thinking answer explained the grammar and skipped the "you have quite a bit still to
  meet" bridge the example wants. More thinking, worse instruction-following here.

Where Haiku fails it is the two photo-reading legend examples (small print on a coin) and, at
medium or max, an occasional miss of one rule in a 98 KB instruction set. At high on text grinds
it missed nothing in 85 + 38 + 8 + 10 + 2 runs.

**Speed.** Haiku 5.5 is not faster than Sonnet 5.5 here: at the grind's own effort its model time
is 1.1-1.5x Sonnet's (it writes 3-5x the output tokens, thinking included), at high 1.5-2x, at max
8-14x. For a phone waiting on `bible-talk`, Haiku high means about 7 s of model time against
Sonnet's 4 s on a cold start; the mill's warm pool takes 1.5 s off either. That is the one cost of
the switch and the thing the slice must measure on the phone, not just here.

**Cost.** The grind's whole cost is the system prompt. `bible-talk`'s instructions are 98 KB, about
35k tokens; the mill's preamble carries a fresh random tag in the system prompt for every grist
(`gristTag` in `application/gristgrind.go`), so no two sessions share a cache prefix and every
answer writes the whole prompt to cache: 35,083 cache-write tokens a run, $0.140 of Sonnet's
$0.144 median. At 1982 answers in 14 days that is about $285 of list-price Sonnet a fortnight on
one grind, of which $277 is the cache write. Haiku's cache write is cheaper ($0.0076 an answer, the
same 35k tokens), which is why its answers cost 1/19 of Sonnet's. Moving the tag out of the system
prompt (it is only needed around the request in the user message) would let runs within the
cache window read the prompt instead, at $0.20/M for Sonnet: about $0.01 an answer, a 14x cut with
no model change. That is filed below as the first thing to do, before any Haiku slice, because it
changes both sides of the A/B.

## Stories: three landed stories replayed on Haiku 5.5

Originals from the ledger and `runs/<id>/result.json`; replays from their result files; the
gate from each session's own `make test` log and my re-run of the acceptance commands. Diffs,
commits and closing comments are in `docs/research/haiku-5-5/story-*`.

| story | kind | original (Sonnet) | Haiku 5.5 replay | gate | right fix? |
|---|---|---|---|---|---|
| mw-gq6.298 untrack six .pyc, ignore `__pycache__` | chore-sized bug, 2 files, formula tdd-feature | sonnet/medium: $0.102, 149 s, 8 turns, 1.4k out, 1 commit | xhigh: $0.025, 230 s, 19 turns, 17k out, 2 commits | make build, test, lint: pass (its logs) | yes: the same .gitignore rule and `git rm --cached`; both acceptance commands pass |
| mw-gq6.404 check-gate-jobs ignores an ambient JOBS | one-file bug, `files=scripts/check-gate-jobs.sh` | sonnet/high: $0.152, 500 s, 19 turns, 3.3k out, 1 commit | high: $0.013, 212 s, 14 turns, 7.7k out; xhigh: $0.031, 331 s, 24 turns, 25k out; max: $0.087, 613 s, 29 turns, 62k out | high: make test 47 ok, lint ok; xhigh: make test ok on the second run (one flaky tmux test under load, passed alone), lint ok; max: make test ok, lint ok | all three: `env -u JOBS` in `var()`, the landed fix; `JOBS=4 scripts/check-gate-jobs.sh` exits 0 in each worktree (run by me). xhigh adds a regression line; max also clears MAKEFLAGS and proves `make lint JOBS=4` too |
| mw-gq6.179 mail notifier types the inbox total as new | chore formula, `contrib/mail-notify` + test | sonnet/high: $0.251, 159 s, 17 turns, 5.2k out, 1 commit (the test only: `count` was already the new count by then) | xhigh: $0.067, 443 s, 48 turns, 43k out, 2 commits (test first, then the wording "N new message(s)" the story's DO quotes, plus the doc comment and one test fixture) | make build, test (1m22s, all ok), lint: pass (its log); `go test ./contrib` passes here | defensible: it did what DO said word for word; Sonnet's Builder judged the count already right and added only the test. Its closing comment also noted the twelve wording-dependent tests it had to touch |
| mw-5r3p30.151 Tester of the Reader-gaps fix (lampas) | tester formula | sonnet/high: $0.719, 382 s, 30 turns, 18.8k out, 1 taste finding | xhigh: $0.812, 1195 s, 66 turns, 100k out, 39 shots, 3 taste findings, 0 bugs | nothing committed (tester-cwd clean; no worktree and no server left running) | it followed all five steps and six adversarial moves on the landed commit and on main, found the original's woven-space finding and two more (verse-number tap box opens a 34 px gap; a double tap closes the sheet), measured gaps to the pixel; but its "Tester fuel" line is a guess ("~210000, estimate") where the formula wants the counter |

Reading: on chores and one-file bugs Haiku 5.5 did the work, at high as well as at xhigh, for
1/4 to 1/12 of Sonnet's list cost, with the rig's gate green and the same fix. Its sessions take
more turns and far more output tokens (thinking), so wall time was similar or longer, not
shorter. Max and xhigh added cost and minutes without changing the fix. On the Tester, a long
tool-driving session, Haiku xhigh was thorough but neither cheaper nor faster than Sonnet high,
and it took a liberty with the fuel line that mw would read. Three stories is a trial's worth
of evidence, not a verdict: the rollout slice below is sized to settle it.

## Clerk, triage and Deputy dry run

Truth computed by script; a run's score is fields right over fields asked (ledger 7, triage 6)
or checks passed over 24 (Deputy). `docs/research/haiku-5-5/clerk-tables.md`, prompts' heads,
truth files and every answer are beside this note.

| errand | haiku:low | haiku:high | haiku:max | sonnet:high (today's Clerk) |
|---|---|---|---|---|
| ledger reduction (43 rows, 39 KB, 7 figures) | 7/7, $0.006, 13 s | 7/7, $0.003, 17 s | 7/7, $0.012, 65 s | not run (stopped at the fuel bound) |
| triage clerical half (417 beads, 129 KB, 6 answers) | **3/6**, $0.041, 190 s (miscounted non-epics, the per-rig counts and the no-rig list; got the sorted ids, duplicates and oldest P1s right) | 6/6, $0.031, 196 s | killed unfinished after 18 min | not run |
| Deputy dry run (24 checks) | 24/24, $0.001, 5 s | 24/24, $0.0015, 10 s | not run | not run |

Reading: Haiku at high did both reductions without a slip; at low it lost the counts on the big
one. Both efforts took three minutes on the 129 KB triage input, which is the Clerk's real
cost: the Mayor waits. Without the Sonnet runs this says Haiku high is a candidate Clerk, not
that it matches today's Clerk; the first Clerk slice is that A/B. The Deputy dry run shows Haiku
reads the Mayor's conventions and writes the seven keys, the hold, the comment and the release in
the right order; it does not show what happens when bd answers back, which is where the Deputy's
real risk is (a wrong `--set-metadata` on a live story). The Deputy stays on Sonnet until a
scratch beads database exists to trial it against.

## The step-up: how a Haiku answer is checked and handed to Sonnet

Named as slices (to build), not built here.

**Grinds (slice G0, the mechanism).** A grind file may name `"model": "haiku"` and a new
`"fallback": {"model": "sonnet", "effort": "medium"}`. The mill already judges every answer
(`gristOutcome`: refusal, no structured output, schema `required` fields) and the smoke judges
every example (`SchemaViolations`, `CheckExpect`). The step-up is: when a Haiku answer is
`failed` or `refused` by `gristOutcome`, or fails the full `SchemaViolations` check (not only
`required`), the mill runs the same grist once more on the fallback, in the same private
directory, and answers with that; the `grinds.jsonl` line records `model`, `model_from:
"fallback"` and `fallback_reason`. Cost of a step-up is one Haiku answer wasted ($0.001-0.008)
plus the Sonnet answer. Turning Haiku off for a kind: `mw grist stats --kind <k>` already reads the
last N runs; a new `mw grist fallbacks` reads the fallback share, and the dispatch tick (home only)
sets a bd kv note `grist.haiku-off.<app>.<kind>` when **3 or more of the last 20 answers** of that
kind stepped up, or any 2 in a row; the mill reads the note and runs the kind on the fallback
outright until the Mayor clears it (`mw grist haiku on <app>/<kind>`), with one mail to the
Mayor when it trips. The `expect` checks run only on examples, so the live step-up cannot read
them; the smoke after every landing still does, and a Haiku kind whose smoke fails holds the rig
as any smoke failure does (mw-gq6.412). What this does not catch: a Haiku answer that fits the
schema and is wrong in content (the legend mint mark). For that the slice measures, not the
mechanism: the A/B below plus the app's own signal where one exists (Lampas's thumbs on a tutor
answer; `feedback` grist).

**Stories (slice S0, the mechanism).** `mw next` already refuses a branch whose gate fails and
`mw retry` already re-dispatches a story as attempt 2 (`attempts` metadata). The step-up is a
path rule: a story whose path says `model=haiku` and whose attempt 1 ends `tests-fail`,
`session-failed`, `uncommitted-work` or `merged-tests-fail` is retried once as attempt 2 with the
path's `fallback_model` (sonnet) and `fallback_effort` (high), written on the bead as a RETRY
comment naming the reason, and the ledger row of attempt 1 keeps `haiku/high` so `mw report` can
count Haiku's hit rate. A Haiku story that lands is scored by the Tester trial where one runs and
by bugs filed against it within a week (`mw cause ... --fix`), which `mw report` already counts
per landing. Turning Haiku off: when **2 of the last 10** Haiku attempts on a rig stepped up, or
a bug is caused by a Haiku landing, `mw dispatch` routes new haiku-path stories of that rig on
the fallback and mails the Mayor; back on by his word (`mw path set` or a kv note cleared).

**Clerks (slice C0).** A Clerk is started by the Mayor inside his session; the check is the
Mayor's own read of the report, as the seats README already says ("check its report before
relying on it"). The step-up is procedural: the Mayor's Clerk prompt asks for a self-check line
(the counts re-derived a second way) and the Mayor re-asks Sonnet when the two disagree or the
answer is not in the shape asked. Measured by the A/B below; turned off by the Mayor after **2
wrong reports in 10**.

## Rollout: slices, one use at a time, each an A/B with a bar and a way back

Smallest and most checkable first. A slice starts only when the one before met its bar. Every
slice names the inputs, how they are split, what is read, how long it runs, the bar against
today's model, and the automatic step back. Nothing below is built or changed by this story.

**Slice 0 (not Haiku, but first): move the mill's random tag out of the system prompt.** Keep
the preamble constant; put the tag only in the user message around `GRIST INPUT`. Measure
`cache_write` tokens per answer in `grinds.jsonl` (today ~35k on bible-talk) before and after.
Bar: the median Sonnet bible-talk answer costs under $0.03 (from $0.144). Step back: none
needed, it is a prompt layout change; the smoke after landing proves answers unchanged. Why
first: it cuts today's grist bill ~10x and makes the Haiku A/B compare answers, not caches.

**Slice G0: the grind step-up mechanism** (`fallback` in the grind file, the retry, the
`grinds.jsonl` fields, `mw grist fallbacks`, the kv note and the off switch). Built in
millwright, trialled with the smoke on a grind that stays on Sonnet. Bar: `make test`, the smoke
passes, a forced Haiku failure (a stand-in `claude` that returns no structured output) steps up
once and is counted. No model change yet.

**Slice G1: Lampas `tips` is already on Haiku 5.5: make it a decision.** Read the last 20 days of
`grinds.jsonl` for tips (217 answered, 10 failed, median 3.4 s). The failed share (4.4%) is
higher than bible-talk's (3.4%) and verse-ask's (5.8%) on Sonnet, in the same range. A/B: for
2 weeks alternate tips grists by txid parity between haiku:low and sonnet:low (the grind file
names `"model": "haiku"`, `"ab": {"model": "sonnet"}` from G0's shape); read failed share,
median seconds and `mw grist smoke lampas --kind tips` daily. Bar: Haiku's failed share within 2
points of Sonnet's and its median seconds within 1 s. Step back: G0's off switch, or the Mayor
writes `"model": "sonnet"` in the grind file.

**Slice G2: Lampas `bible-talk` on Haiku at high.** The biggest grind, the biggest saving
($0.144 to $0.008 an answer before slice 0; after slice 0 about $0.01 to $0.002), 53 examples
of coverage, 85/85 here. A/B for 2 weeks or 400 answers, whichever first: alternate by txid
parity between haiku:high and sonnet:medium; read (1) failed+refused share from `grinds.jsonl`,
(2) step-ups (G0), (3) `wall_s` and `model_s` medians per model, (4) the Governor's own thumbs
on tutor answers in Lampas if the app records them, else his word, (5) the smoke daily on both
arms (G0 lets the smoke force a model). Bar: failed share within 2 points of Sonnet's, step-ups
under 3%, median seconds at most 3 s above Sonnet's, no bible-talk bug filed against a Haiku
answer in the window, 53/53 smoke on Haiku each day. Step back: G0's off switch (3 of 20) during
the trial; after it, the grind file.

**Slice G3: the small photo-free grinds together:** Cairn `sweep`, TradeTracker
`item-from-photos` and `receipt-reconcile` (photos, but every example passed), Legend
`item-value`, `coin-rarity`, `legend-ask`, SpellForge `parent-ask` and `word-list`, Lampas
`find-refs` and `verse-ask`. One slice, one week each app, same A/B as G2 at each grind's own
effort (verse-ask at high). Bar as G2. These have 2-7 examples each: the slice first adds
examples until each grind has at least 5 (a Builder story per app), since 2/2 proves little.
Legend `coin-from-photos` is excluded: Haiku missed 2 of 7 (token legend, mint mark) at every
effort; it stays on Sonnet, and the two examples it missed become the first items of a
"photo-fine-print" smoke set that any later model must pass.

**Slice G4: `verse-read` on Haiku at medium**, after G2 passed, because the reader waits on it
and Haiku was 2-3 s slower; same A/B, bar as G2 with the seconds bar tightened to 2 s.
`tutor-turn` (Opus) waits for the 11-case `npm run tutor:eval` to be run on Haiku
(`TUTOR_EVAL_MODEL=haiku`, about $0.10) and for the Governor's word, since it is a child's tutor.

**Slice S0: the story step-up mechanism** (path `fallback_model`/`fallback_effort`, the one
automatic retry on the fallback, the ledger keeping attempt 1's model, `mw report` counting Haiku
attempts and step-ups, the per-rig off switch). Bar: `make test`; a fake harness that fails
attempt 1 shows attempt 2 on the fallback and the ledger rows read right.

**Slice S1: chores and one-file bugs in millwright on Haiku high.** Inputs: every new story in
rig millwright with formula `chore`, or type bug with a `files=` key naming one file. Split: the
Mayor files them as today; `mw dispatch` alternates by bead-id hash between `haiku/high` and
`sonnet/high`, writing which on the bead. Run length: 20 stories (10 each) or 3 weeks. Read:
landed on attempt 1 (gate green), step-ups, cost and minutes from the ledger, bugs filed against
the landing within 7 days (`mw cause`), the Mayor's VERIFIED rate. Bar: Haiku's attempt-1 landing
rate within 10 points of Sonnet's, no more bugs caused per landing, cost per landing under half.
Step back: S0's rule (2 of 10 step-ups, or a caused bug, routes the rig back to Sonnet and mails
the Mayor). Then S2: the same on lampas, where the gate is heavier (npm ci, Playwright) and the
Builder's turns cost more wall time.

**Slice C1: Haiku high as the Clerk, A/B.** The Mayor's next 10 Clerk errands of the reducing
kind (comments read, a `bd list` reduced, a ledger counted) run twice, Sonnet high and Haiku
high, same prompt, the Mayor comparing the two reports and writing a line on
`seats/mayor/procedures.md` (or a bead) per errand: same / Haiku wrong / Sonnet wrong, with the
two costs from the subagent's usage. Bar: Haiku wrong on at most 1 of 10 where Sonnet was right,
and never on a count. Then the Mayor asks the Governor for the charter line, since the Clerk's
model is in his charter.

**Slice D1: the Deputy, dry runs only.** Build a scratch beads database (a `bd init` under
/tmp, as `infrastructure/beads` tests do) and replay 10 real Mayor mails from the Deputy's
handoffs on Haiku high against it, diffing the resulting beads against what the Sonnet Deputy
did (the handoff records the keys). Bar: 10/10 identical. Only then a live A/B, and only with
the Governor's word, because the Deputy writes to the real database.

**Not sliced:** the Tester (Haiku xhigh cost more and took three times as long as Sonnet; a trial
at high can follow S1 if S1 passes), the Millhand review wake, the Mayor, feature stories.

## Problems found (for the Mayor to file)

1. **The mill's preamble tag defeats prompt caching** (`gristTag`, `application/gristgrind.go`;
   also `gristWarmSpec.call` for warm sessions): every grist writes its whole system prompt to
   cache. Sonnet bible-talk: 35,083 cache-write tokens, $0.140 of a $0.144 answer, about $277 of
   $285 list price a fortnight. Fix: constant preamble, tag only in the user message. Acceptance:
   two consecutive replays of one example show the second with `cache_read` near 35k and
   `cache_write` near 0; the smoke unchanged. (Slice 0 above.)
2. **`domain.FullModelID(haiku)` is `claude-haiku-4-5-20251001`** while the `haiku` alias is 5.5:
   `mw talk model haiku` would respawn the Mayor on the old model. Fix: `claude-haiku-5-5`, and
   the test that pins it. Acceptance: `respawnMayorLine(haiku)` names `claude-haiku-5-5`.
3. **Lampas `tips` moved to Haiku 5.5 without a decision** when the alias moved. Not a bug, but
   the factory has no record of which Haiku its grinds ran on: `grinds.jsonl` keeps the alias,
   not the model id the result JSON names (`modelUsage` keys). Fix: record the id in the
   `grinds.jsonl` line (`model_id`). Acceptance: a line from a haiku grind carries
   `claude-haiku-5-5`.
4. **Haiku misses fine print in coin photos** (legend `coin-from-photos`: subway token
   denomination, 1944-S mint mark) at every effort. Nothing to fix today; it is the reason that
   grind stays on Sonnet and the two examples should be a named "fine print" set.
5. **mw-gq6.179 landed as a test-only commit**: `contrib/mail-notify` already counted new
   messages (`count=$(echo "$new" | wc -l)`, line 366 today) when the story was filed; the Mayor's
   CAUSE line pointed at the inbox total. Nothing to fix; a note that the cause reading was stale.
6. **The Tester formula's fuel line is unverifiable by the session**: the Haiku Tester wrote an
   estimate. `mw next` could read the result JSON's `total_cost_usd` for the Tester row instead of
   asking the session; today the ledger already does for Builders.
7. **A replay of a landed story in a shared worktree sees the landed fix in `git log --all`.**
   Any later A/B replay (S1's baseline of past stories) must run in a clone cut at the start
   commit, as the last three replays here did (`git clone`, `checkout --detach`, delete `main`,
   `reflog expire`, `gc --prune=now`).

## Files

- `docs/research/haiku-5-5/grind-tables.md`: every grind, every model:effort, and every failure.
- `docs/research/haiku-5-5/grind-runs.jsonl`: all 666 runs (answers themselves not kept).
- `docs/research/haiku-5-5/replay-runner.go.txt`: the offline replay runner (a scratch `main`
  built against this rig with a `replace` directive; not part of the build).
- `docs/research/haiku-5-5/make-story-run.sh`, `make-tester-run.sh`, `run-clerk.sh`: how the
  replays and the Clerk runs were started.
- `docs/research/haiku-5-5/story-*.diff`, `*.commits`, `*.closing.md`: what Haiku committed and
  said on each story replay; `tester-*.FINDINGS.md`: the Haiku Tester's findings.
- `docs/research/haiku-5-5/clerk/`: the Clerk prompts' heads, schemas, truth files and every
  answer with its cost and time; `clerk-tables.md`: the grades.
