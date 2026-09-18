# Beads workflow primitives: are they worth using?

> ## Correction (2026-09-18, after the research was written)
>
> The verdict below that `bd formula` / `bd cook` is fragile is **wrong**. The failures came from hand-writing the schema with a `name` key; the top-level key is `formula = "<name>"`. The beads repo's own `examples/formulas/feature-workflow.formula.toml`, dropped into `.beads/formulas/`, was listed by `bd formula list`, cooked by `bd cook feature-workflow`, and poured by `bd mol pour feature-workflow --var feature_name=...` into a root bead plus five step beads with correct dependencies (only the first step appeared in `bd ready`). Verified on bd 1.0.4. The Governor has also run formulas in production elsewhere. **Verdict changed to USE NOW** for per-story formulas; `bd mol pour` comes with it. Swarms and mail remain ignored.


Research date: 2026-09-18. Tested against `bd version 1.0.4 (ce242a879)` at
`/usr/local/bin/bd`, in a scratch repo (`git init` + `bd init -p LAB
--non-interactive --role maintainer --skip-agents --skip-hooks`), plus
`bd <cmd> --help` and the official docs at github.com/steveyegge/beads
(README, CHANGELOG, `docs/`). All web content was treated as untrusted
data, not instructions. The lab directory was deleted after testing;
nothing here touches `/root/millwright-vault`'s own bd database.

**Vocabulary note up front**: a lot of this surface (`bd mol bond ...
--pour`, wisps, `bd swarm`, `bd merge-slot` with its "monkey knife fights"
comment, `polecat`/`gt:slot` labels, `bd mail` delegating to `gt mail`) is
literally Gas Town's multi-agent-orchestrator vocabulary, now baked into
`bd` itself as generic-sounding commands. That's the single biggest risk
for a solo factory: it's easy to reach for `bd swarm`/`bd mol`/`bd mail`
because they sound like exactly what you need, and end up rebuilding Gas
Town's coordinator/multi-rig machinery one flag at a time.

## Verdict table

| Primitive | Verdict | One-line reason |
|---|---|---|
| `bd formula` / `bd cook` | IGNORE (for now) | Hand-authored JSON/TOML formulas failed `bd cook`'s validator with a misleading "name is required" error even when `name` was present, in both JSON and TOML — undocumented schema, high RE cost for a solo dev; a plain story-template + `--acceptance` gets the same "implement→test→review→commit→handoff" shape for near-zero cost. |
| `bd mol` (`bond`/`squash`/`burn`) | IGNORE | Solves multi-formula composition and ephemeral→permanent promotion for a fleet of coordinator agents; a solo dispatcher that already knows its own epic/story IDs doesn't need polymorphic bonding. |
| Wisps (`--ephemeral`, `--wisp-type`, `bd promote`, `bd purge`) | REVISIT LATER | Genuinely cheap, well-behaved TTL-cleanup primitive for noise beads (heartbeats, patrol pings) that you don't want cluttering `bd list`/git history — but the factory doesn't have that noise yet; adopt only once dispatcher health-check beads start accumulating. |
| `bd swarm` / `--mol-type swarm` | IGNORE | Empirically just creates one extra "molecule" bead wrapping the epic (`mol_type=swarm`, optional `coordinator` field) so multiple coordinator agents can discover work to pick up; `bd ready --parent <epic>` + `bd blocked --parent <epic>` already give the same status for a dispatcher that owns the epic ID directly. |
| `bd gate` (+ `--waits-for`, `--waits-for-gate`) | REVISIT LATER | Real, working async-block primitive (`human`/`timer`/`gh:run`/`gh:pr`/`bead` types), tested clean in the lab — useful once the factory needs "wait for CI" or "wait for human sign-off" steps, but adds a bead + a `bd gate check` polling habit the dispatcher doesn't need yet. |
| `bd ready --gated` | IGNORE (as-is) | Only surfaces *molecules* whose gates closed (returned empty in a non-molecule epic); not useful unless you also adopt `bd mol`. |
| `bd merge-slot` | USE NOW | Exactly the "only one worktree merges at a time" lock a concurrent dispatcher needs, built from one bead with `acquire`/`check`/`release` and a holder/waiter queue — tested clean, no Go locking code required. **Caveat**: the slot bead is a plain `task`-type issue with a `gt:slot` label, so it will show up in `bd ready`/`bd list` unless the dispatcher filters it out (see gotcha below). |
| `bd set-state` / `bd state` | USE NOW | Cheap, generic `dimension:value` label + audit-event pair (tested: `bd set-state <id> build=running` → `bd state <id> build` → `running`) — good fit for dispatcher-owned status (`build:running`, `health:failing`) without inventing a bespoke label scheme in Go. |
| `bd audit` | REVISIT LATER | Append-only JSONL log of agent interactions for later fine-tuning/dataset use; real and cheap to call, but pure overhead unless you actually plan to mine `.beads/interactions.jsonl` later. |
| `bd remember` / `bd memories` / `bd recall` | USE NOW, sparingly | Works as documented, but **every memory is injected into every `bd prime` call for every session, forever** (measured: one memory added ~240 bytes / ~60 tokens to `bd prime` output) — treat it as a small, curated "house rules" file, not a scratchpad, or the fixed per-session tax grows unbounded. |
| `bd kv` | USE NOW | Plain per-repo key/value store, not injected into `prime`, good for config flags the dispatcher/planner need to share (e.g. `factory.mode=solo`) without a memory-injection cost. |
| `bd create --graph <json>` | USE NOW (schema below), with real limits | Real, working batch-create-with-dependencies path — but the JSON schema is **entirely undocumented** in the official docs; I reverse-engineered it from error messages (see below). It does **not** support `--acceptance`/`--design`/`--skills`/`--estimate`/`--spec-id` per node — those fields are silently dropped. **Also: `--dry-run` does not actually skip writing for `--graph`** — it created real issues in testing. Verify this on the actual `bd` build before trusting `--dry-run` in a planner. |
| `bd create -f <markdown>` | REVISIT LATER | Very flat: only `##` headings become issues (title=heading, body=description); a top-level `#` epic heading is silently ignored (no epic auto-created), no parent/dependency parsing, no acceptance-criteria field population. Usable for a flat batch of independent stories only if the planner also emits a `## Acceptance Criteria` section in each body (lint checks that literal heading in the description when the structured field is empty). |
| `bd epic status` / `close-eligible` | USE NOW | Exactly two subcommands; `status` gives a free "N/M children closed" rollup, `close-eligible` auto-closes epics whose children are all done — cheap epic housekeeping the dispatcher would otherwise hand-roll. |
| `bd prime` | USE NOW, monitor size | Measured 4.5 KB (~1,100 tokens) in full CLI mode, ~0.76 KB (~190 tokens) in `--mcp` mode on a near-empty DB — both bigger than the "~50 tokens" MCP-mode claim in `--help`. Paid at every session start; grows with every `bd remember`. Worth checking real size again once the factory's actual `.beads/PRIME.md`/memories are in place. |
| `bd stale` / `bd blocked` / `bd ready --explain` | USE NOW | All three are pure DB queries with `--json`, zero AI involved — exactly the "is anything stuck" health check the ticket asks about; a cron/shell script can `jq` these directly (see recommendation below). |
| `bd mail` | IGNORE | Confirmed via `--help`: it's a thin delegator that does nothing until `BEADS_MAIL_DELEGATE`/`mail.delegate` points at an external tool (Gas Town's `gt mail`); no messaging is implemented in `bd` itself. |
| `bd preflight` | IGNORE | Checklist is specific to the beads project's own contribution workflow (Go fmt, `nix vendorHash`, `.beads/issues.jsonl` pollution) — not a generic PR checklist for an arbitrary repo. |
| `bd lint` + `--validate` | USE NOW | Tested clean: checks that each issue type has its required sections (task/feature/bug need "Acceptance Criteria", epic needs "Success Criteria") either as a structured field (`--acceptance`) or a literal `## Acceptance Criteria` heading in the description. Cheap CI-style gate for story quality before dispatch. |
| `--estimate`, `--acceptance`, `--design`, `--spec-id` on `create` | USE NOW | All four are real structured fields on direct `bd create` (verified via `--json` output), separate from `description`. |
| `--skills` on `create` | IGNORE as a field | Verified: `--skills` is **not** a queryable field — it's just appended as a `## Required Skills` text block inside `description`. Don't rely on it for filtering; use a label instead if you need to query by skill. |

## What this means for the factory (≤10 lines)

Adopt the primitives that are single cheap beads doing one job with no
hidden agent/coordinator concept: `bd merge-slot` for serializing merges
across concurrent worktrees, `bd ready --claim` for atomic work-claiming,
`bd set-state`/`bd state` for dispatcher-owned status, `bd kv` for shared
config, `bd lint`/`--validate` plus `--acceptance`/`--design`/`--estimate`
for story quality, and `bd stale`/`bd blocked`/`bd ready --explain --json`
for a zero-AI, cron-able "is anything stuck?" check — **skip an AI seat for
that entirely**. Skip `bd mol`/`bd swarm`/`bd mail`/`bd formula` for now:
they exist to let *multiple coordinator agents* discover and divide work
across rigs, which a single Go dispatcher that already knows its own epic
IDs doesn't need — adopting them imports Gas Town's machinery by another
name, which is the exact failure mode this project is trying to avoid.
Use `bd create --graph` for the planner's batch output using the
reverse-engineered schema below, but keep acceptance criteria embedded as
markdown in each node's `description` rather than relying on unsupported
per-node fields, and don't trust its `--dry-run`.

---

## Primitive-by-primitive detail

### `bd formula` + `bd cook`

**What it does**: `bd formula` manages YAML/JSON/TOML workflow templates
searched from `.beads/formulas/`, project-local, user, or `$GT_ROOT`
directories. `bd cook` "cooks" a formula file into a **proto** (a template
epic with a `template` label and child issues for each step), either in
compile mode (keeps `{{var}}` placeholders, for planning) or runtime mode
(substitutes `--var` values, for actual instantiation). The proto can then
be `bd mol pour`ed (persistent) or `bd mol wisp`ped (ephemeral).

**Minimal example attempted**: a 5-line JSON formula
(`{"name":"mini","steps":[{"id":"a","title":"A"}]}`) and an equivalent
TOML file both failed identically:
```
$ bd cook .beads/formulas/mini.formula.json
Error: resolving formula: formula validation failed:
  - formula: name is required
```
This reproduced with `--dry-run`, with and without `--mode=runtime`, with
a `title` field added, and with the "reserved-name" variable renamed —
i.e. the validator rejects a formula that already has a non-empty `name`
field, in both JSON and TOML. This is either a real bug in v1.0.4's
formula loader or an undocumented schema requirement (e.g. a wrapper
object) that isn't discoverable from `--help` or error messages alone. I
could not get a hand-authored formula to `bd cook` successfully in the
time budgeted for this ticket — **couldn't verify** the exact working
schema.

**What Go code it would save**: a template-expansion step (turn "the
recurring 5-step story shape" into 5 linked beads) that the dispatcher or
planner would otherwise write itself — maybe 50-100 lines of Go, or
equivalently a shell function that shells out to `bd create` 5 times with
`--deps`.

**Cost**: high right now — an undocumented, apparently fragile schema
(TOML "preferred" per docs, JSON supported but broken in my tests),
requiring either digging into `bd`'s Go source or finding a working
example formula in the repo's test fixtures (not fetched for this
ticket). The `formulas.md`/`cli-reference/formula.md`/`cook.md` docs
describe the concept clearly but never show a complete, copy-pasteable
formula file.

**Verdict: IGNORE (for now)** — a plain story template (a markdown/heredoc
you paste into `bd create -f` or a 5-line shell loop with `--deps`) gives
the same "implement → test → self-review → commit → handoff" shape today,
with zero schema risk. Revisit only if the working formula schema turns
up (e.g. by reading `bd`'s source, or the project publishes examples), and
only if the same 5-step shape recurs across enough epics that avoiding
5 `bd create --deps ...` lines is worth the token cost of a session
having to compose/inspect formula files.

**Direct answer to the ticket's specific question**: no — for a shape as
simple as implement→test→review→commit→handoff, a story with a fixed
description template + `--acceptance` text is enough. A formula only pays
for itself if the shape has real branching/variables (e.g. different
step counts per language, or steps that differ by story type) — this one
doesn't.

### `bd mol` (molecules): `bond`, `squash`, `burn`

**What it does**: A "proto" is an uninstantiated template epic; a
"molecule" is what you get after `bd mol pour`/`wisp` instantiates it into
real, linked issues. `bd mol bond A B` polymorphically combines two
protos/molecules (sequential/parallel/conditional), including a "Christmas
Ornament pattern" (`--ref arm-{{name}}`) for spawning readable per-worker
child IDs — explicitly designed for a fleet of named worker agents
("polecats") bonding sub-molecules onto a shared patrol bead. `bd mol
squash` condenses a molecule (typically an ephemeral one) into a permanent
"digest" issue and clears its ephemeral flag; `bd mol burn` deletes a
molecule outright with no trace, for abandoned/test runs.

**Minimal example**: not run — `bond`/`squash`/`burn` all require an
existing proto or molecule, and given `bd cook` couldn't be gotten
working (above), building one by hand for this alone wasn't worth the
extra bd invocations under the "keep experiments small, another agent is
using bd" constraint. `bd mol --help` and `bd mol bond --help` were read
in full instead.

**What Go code it would save**: dependency-wiring logic for combining
multiple template epics into one bigger DAG, and a promote/discard
lifecycle for scratch work.

**Cost**: the whole vocabulary (proto/mol/bond/squash/burn/distill,
"solid→liquid→vapor" phase language) is real conceptual overhead for a
solo dev, and it's downstream of formulas already being IGNORE'd.

**Verdict: IGNORE** — this is fleet-of-agents composition tooling
(the `--ref`/`--var` "Christmas Ornament" pattern is literally for naming
per-worker sub-molecules). A solo dispatcher creates one epic with
`bd create --graph` and doesn't need to bond/squash/burn anything.

### Wisps (`--ephemeral`, `--wisp-type`, `bd promote`, `bd purge`)

**What it does**: a wisp is a normal issue with `Ephemeral=true` — stored
locally, excluded from git/Dolt sync — intended for operational noise
(heartbeats, patrol pings, GC reports, recovery/error/escalation events)
that has no audit value. `bd purge` permanently deletes closed wisps
(optionally by age/pattern, dry-run by default without `--force`). `bd
promote <wisp-id>` converts a wisp into a permanent, Dolt-versioned bead
in place (same ID, labels, deps, comments preserved) if it turns out to
matter after all.

**Minimal example**: not run directly (would have required `--ephemeral`
create + `bd purge --force`, skipped to conserve bd invocations since it's
a straightforward, low-risk primitive already well-covered by `--help`
and docs). Per the beads CHANGELOG (1.3.0, 2026-09-15), the ephemeral/tier
semantics were still being actively hardened as of this research (a
non-empty `wisp_type` now forces `ephemeral=true` at creation on every
code path) — this is a young, still-moving part of the schema.

**What Go code it would save**: a TTL-cleanup cron job for
dispatcher-generated noise beads (session-start pings, health-check
results) that would otherwise accumulate forever in `bd list`/git history.

**Cost**: low once you have noise to clean up (`--wisp-type` is a fixed
enum: heartbeat/ping/patrol/gc_report/recovery/error/escalation); zero
value if you don't.

**Verdict: REVISIT LATER** — the factory doesn't yet generate the kind of
operational noise (heartbeats, patrol beads) that wisps exist to contain.
Once the dispatcher starts writing a bead per session-start/health-check,
revisit; until then it's an unused knob.

### `bd swarm` / `--mol-type swarm`

**What it does (tested)**: `bd swarm validate <epic>` checks an epic's DAG
for cycles/orphans/disconnected subgraphs and reports "ready fronts"
(parallel waves) — pure read, no state created; ran clean against a
1-node epic and correctly reported it swarmable. `bd swarm create <epic>`
creates one extra bead: a `molecule`-typed issue with `mol_type=swarm`,
an optional `--coordinator` address, and a `relates-to` dependency back to
the epic. `bd swarm status <epic-or-swarm-id>` then buckets the epic's
children into Completed/Active/Ready/Blocked.

**Minimal example**:
```
$ bd swarm create LAB-407
✓ Created swarm molecule: LAB-c7b
   Epic: LAB-407 (Epic P)   Max parallelism: 1   Waves: 1
$ bd swarm status LAB-c7b
Ready: ○ LAB-ypr   (same info bd ready --parent LAB-407 would show)
```

**What Go code it would save**: a "which coordinator agent should pick up
this epic" discovery mechanism, and DAG-sanity checks (`validate`) that
would otherwise be hand-rolled graph code.

**Cost**: `validate` is genuinely free/useful; `create`/`status` add a
bead and a layer of indirection for a status view that `bd ready --parent
<epic>` + `bd blocked --parent <epic>` already give directly, once you
already know the epic ID (which the dispatcher does — it made the epic).

**Verdict: IGNORE** — `bd swarm validate <epic>` is worth keeping as an
optional planner-side sanity check before dispatch (cycles/orphans are
real failure modes), but `swarm create`/`status`/the `coordinator` field
solve multi-coordinator discovery, which a single Go dispatcher doesn't
need.

### `bd gate` (+ `--waits-for`, `--waits-for-gate`, `bd ready --gated`)

**What it does**: a gate is an issue that blocks another issue like any
dependency, but resolves asynchronously instead of by a normal `bd close`.
Types: `human` (needs manual `bd gate resolve`), `timer` (auto-expires),
`gh:run`/`gh:pr` (GitHub Actions/PR-merge watchers), `bead` (waits for a
cross-rig bead to close). `--waits-for`/`--waits-for-gate` are flags on
`bd create`, not `bd gate` — they set up an N-of-M "fan-out" gate (wait
for all-children or any-children of a spawner issue).

**Minimal example (tested, worked cleanly)**:
```
$ bd gate create --type=human --blocks LAB-ypr --reason "waiting on design review"
✓ Created gate LAB-sgp (type: human)
$ bd gate list
⏳ Open Gates (1): ○ LAB-sgp - human
$ bd gate resolve LAB-sgp --reason "reviewed"
✓ Gate resolved: LAB-sgp
```
`bd ready --gated --json` returned `{"count":0,"molecules":[]}` against
this non-molecule epic — it only looks at molecules, so it's a no-op
unless you've also adopted `bd mol`.

**What Go code it would save**: a "wait for CI to go green" or "wait for
a human approval" polling/webhook mechanism — real, non-trivial glue code
the dispatcher would otherwise write (a GitHub Actions watcher, a Slack
approval bot, etc.).

**Cost**: low per-gate (one bead, clean CLI), but it establishes a polling
habit (`bd gate check`) and a dependency type the dispatcher has to know
about when deciding what's "ready."

**Verdict: REVISIT LATER** — genuinely solves a real, specific problem
(blocking on GitHub CI or a human sign-off without inventing a webhook
receiver), but the factory doesn't yet have a CI-gated or human-gated
step described in this ticket. Adopt `gate create --type=gh:pr` the day
the dispatcher needs to wait on a real PR merge; don't build it in ahead
of need.

### `bd merge-slot`

**What it does**: one exclusive-lock bead per rig (`<prefix>-merge-slot`,
labeled `gt:slot`) with `acquire`/`check`/`release`/holder+waiter-queue
semantics, explicitly built to stop concurrent agents from creating
cascading merge conflicts ("monkey knife fights" per its own `--help`).

**Minimal example (tested end-to-end, worked exactly as documented)**:
```
$ bd merge-slot create
✓ Created merge slot: LAB-merge-slot
$ bd merge-slot acquire
✓ Acquired merge slot: LAB-merge-slot   Holder: lab
$ bd merge-slot check
○ Merge slot held: LAB-merge-slot   Holder: lab
$ bd merge-slot release
✓ Released merge slot: LAB-merge-slot
```

**Gotcha found**: the merge-slot bead is an ordinary `task`-type issue
with priority 0 and no special exclusion from `bd ready`/`bd list`. In
testing, `bd ready --claim --json` (which a dispatcher would use to grab
the next story) **claimed the merge-slot bead itself** as if it were a
work item, because it's the highest-priority "ready" task in the DB. A
dispatcher using `bd ready --claim` for story dispatch must exclude it
explicitly, e.g. `bd ready --claim --exclude-label gt:slot` or by
filtering on issue type/parent.

**What Go code it would save**: a distributed lock/mutex the dispatcher
would otherwise implement itself (a lockfile, a DB row with
compare-and-swap, etc.) to serialize "land this worktree's commits"
across 2-6 concurrent sessions.

**Cost**: very low — one bead, four commands, no AI needed, and it
composes with the existing worktree-per-story model exactly as described
in the ticket.

**Verdict: USE NOW** — this is the single most directly-applicable
primitive in the whole list: concurrent worktrees landing commits is
exactly the "several workers, one merge target" problem it exists to
solve, and it costs one shell call per land, not an AI seat. Just make
sure the dispatcher's `bd ready --claim` query excludes `gt:slot`.

### `bd set-state` / `bd state`

**What it does**: `bd set-state <id> <dim>=<value> [--reason]` atomically
writes a `<dimension>:<value>` label (fast lookup) and an underlying event
bead (source of truth / audit trail) in one call; `bd state <id> <dim>`
reads the current value back.

**Minimal example (tested)**:
```
$ bd set-state LAB-ypr build=running --reason "dispatcher started session"
✓ Set build = running on LAB-ypr   Event: LAB-ypr.1
$ bd state LAB-ypr build
running
```

**What Go code it would save**: a bespoke "story status" label scheme
plus manual event logging that the dispatcher would otherwise implement
by hand (adding/removing labels itself, writing its own history log).

**Cost**: trivial — one call to set, one to read, both `--json`-able.

**Verdict: USE NOW** — good fit for dispatcher-owned lifecycle state
(`build:running`, `build:failed`, `health:stuck`) that's richer than
`bd`'s built-in status enum but doesn't need a whole custom schema.

### `bd audit`

**What it does**: appends structured entries (`bd audit record --kind
llm_call|tool_call|label ...`) to `.beads/interactions.jsonl`, an
append-only, git-versionable log intended for "why did the agent do
that?" debugging and later SFT/RL dataset generation. `bd audit label`
attaches a good/bad label to a prior entry.

**Minimal example**: not run (writes a git-tracked file; skipped to avoid
polluting the throwaway lab repo's git history for a feature that's easy
to fully characterize from `--help` alone).

**What Go code it would save**: a structured logging format for
agent-session provenance the dispatcher would otherwise define itself.

**Cost**: low to call, but it's dead weight unless you actually plan to
mine the log later (fine-tuning, retrospective debugging of a bad
session).

**Verdict: REVISIT LATER** — worth turning on once you actually want to
answer "why did session X do Y," not before.

### `bd remember` / `bd memories` / `bd recall` / `bd kv`

**What they do**: `bd remember "insight" [--key k]` stores a persistent,
cross-session note that's **automatically injected into every `bd prime`
call** (confirmed by measurement below); `bd memories [search]`
lists/searches them; `bd recall <key>` fetches one by key. `bd kv
set/get/clear/list` is a separate, general-purpose per-repo key-value
store for config-like values — **not** injected into `prime`. Per the
CHANGELOG (1.3.0, 2026-09-15), memories are actually stored as rows in the
same underlying config/kv plane as `bd kv`, just under a "memory"
semantic label; both are scoped per-repo (one `.beads/` DB = one project),
not per-issue or global.

**Minimal example (tested)**:
```
$ bd kv set factory.mode solo && bd kv get factory.mode
solo
$ bd remember "dispatcher uses 2-6 worker slots" --key dispatcher-slots
$ bd recall dispatcher-slots
dispatcher uses 2-6 worker slots
```
Measured cost: adding that one memory grew `bd prime`'s output from 4,518
to 4,760 bytes (+242 bytes, ~60 tokens) by appending a `## Persistent
Memories (1)` section with the full text.

**What Go code it would save**: a project-notes file the dispatcher/Mayor
would otherwise have to remember to read at session start (this is
explicitly what `bd prime` + `bd remember` replace — the beads README
tells agents to use `bd remember` "do not create MEMORY.md files").

**Cost**: `bd kv` is free (not injected anywhere). `bd remember` has a
**permanent, compounding token cost**: every memory is paid for at every
session start, forever, until `bd forget`ed. There's no forgetting
mechanism triggered automatically — this is 100% policy-driven.

**Verdict: USE NOW, sparingly** for `bd remember` (a handful of durable
"house rules" the Mayor/dispatcher should never forget — e.g. "always run
tests with -race", "worktrees live under .worktrees/") — treat it as a
small, curated constant, not a running log. **USE NOW** freely for `bd
kv` (dispatcher config, feature flags) since it has no prime-time cost.

### `bd create --graph <json>` (schema)

**What it does**: batch-creates a set of issues plus their dependency
edges from one JSON file, in one write — the natural fit for "planner
turns a conversation into an epic of stories with dependencies."

**The exact schema is undocumented** in the official docs (confirmed:
`docs/cli-reference/create.md` has only the one-line flag description;
no schema/example JSON exists anywhere under `docs/`). I reverse-engineered
the working shape from `bd`'s own validation error messages (which
reference the real Go struct name `GraphApplyNode`):

```json
{
  "nodes": [
    {
      "key": "epic1",
      "title": "Epic One",
      "type": "epic"
    },
    {
      "key": "story1",
      "title": "Story One",
      "type": "task",
      "parent_key": "epic1",
      "priority": 1,
      "description": "do the thing",
      "labels": ["backend", "urgent"]
    }
  ],
  "edges": [
    { "from_key": "story1", "to_key": "epic1", "type": "blocks" }
  ]
}
```
Verified fields on a node: `key` (required, local id for wiring
edges/parents), `title` (required), `type`, `priority` (int, not string —
`"1"` fails with a Go unmarshal-type error), `description`, `parent_id` /
`parent_key` (either works; creates a real parent-child dependency,
confirmed via `bd children`), `labels` (must be a JSON array of strings,
not a comma-string — confirmed via type-mismatch error). Verified fields
on an edge: `from_key`/`to_key` (or presumably `from_id`/`to_id`), `type`
(defaults sensibly; `"blocks"` confirmed working end-to-end via `bd dep
list`).

**Fields that do NOT work on a graph node** (confirmed by testing:
supplying them with a deliberately wrong JSON type produced no type
error, meaning the field doesn't exist in the struct, unlike `priority`
which does): `estimate`, `estimated_minutes`, `acceptance`,
`acceptance_criteria`, `skills`, `design`, `spec_id`, `deps`,
`depends_on`, `blocks`. These are silently dropped if you include them —
only plain `bd create --acceptance ...`/`--design ...`/`--estimate ...`
(direct CLI, one issue at a time) populates those structured fields.

**Gotcha found**: `bd create --graph plan.json --dry-run` **did not
dry-run** — it created real issues in the database every time I ran it
(verified via `bd count`/`bd list` before and after). `--dry-run` on plain
`bd create -f markdown` correctly refuses to run at all
("--dry-run is not supported with --file flag"), so `--graph`'s silent
failure to honor `--dry-run` is a real, easy-to-miss gap — a planner
should never trust `--graph --dry-run` as a preview without re-verifying
on the actual `bd` build in use.

**What Go code it would save**: exactly the batch epic+stories+deps
creation call the Mayor needs to emit — this is the one write path that
directly replaces bespoke Go DB-writing code, if the planner emits a JSON
object shaped like the above.

**Cost**: moderate — the schema had to be reverse-engineered (no docs,
inconsistent error messages), fields are more limited than hoped
(no acceptance/design/estimate per-node), and `--dry-run` is unsafe to
rely on. Once known, the schema itself is simple and cheap to emit.

**Verdict: USE NOW**, with the schema above, but: keep acceptance
criteria as a `## Acceptance Criteria` heading inside each node's
`description` text (which `bd lint`/`--validate` does recognize) rather
than assuming a dedicated field will carry it through `--graph`; and
never treat `--graph --dry-run` as a safe preview.

### `bd create -f <markdown>`

**What it does**: batch-creates issues from a markdown file. Tested:
```markdown
# Epic Three
## Story Three A
Do thing A.
## Story Three B
Do thing B.
```
produced **two independent task issues** ("Story Three A", "Story Three
B") with no parent epic created (`# Epic Three` was silently ignored,
confirmed via `bd children`/`bd show`) and **no dependency between them**.
This is a much flatter format than `--graph`: no headings-to-hierarchy,
no checkbox-as-dependency syntax, nothing beyond "each `##` heading
becomes one issue, body text becomes its description." This isn't
documented anywhere either (confirmed no schema doc exists under
`docs/cli-reference/create.md` beyond the one-line flag description) —
verified empirically.

**What Go code it would save**: minor — a way to paste a quick flat list
of stories without dependencies (e.g. copy-pasting a conversation's
bullet list). Not a real batch-DAG tool.

**Cost**: low to use, but easy to be surprised by (expecting hierarchy or
deps that don't happen).

**Verdict: REVISIT LATER** — fine for genuinely flat, independent
one-offs; use `--graph` instead the moment stories have dependencies or
belong to an epic, which is the Mayor's normal case per the ticket.

### `bd epic` subcommands

**What it does**: exactly two commands exist — `bd epic status [--eligible-only]`
(completion rollup: "N/M children closed") and `bd epic close-eligible
[--dry-run]` (auto-closes epics whose children are all done). There is no
`bd epic create`/`list`/`children` — epics are just `bd create -t epic`
plus `--parent` on children, per the docs' own quickstart example.

**Minimal example (tested)**:
```
$ bd epic status LAB-407
○ LAB-407 Epic P
   Progress: 0/1 children closed (0%)
```

**What Go code it would save**: a "roll up child completion into epic
status" query and an auto-close-when-done sweep, both of which the
dispatcher would otherwise compute from `bd list --parent` output.

**Cost**: essentially free — two thin, correct commands.

**Verdict: USE NOW** — cheap epic housekeeping; run `bd epic
close-eligible` periodically (or after each story closes) instead of
writing that rollup logic in Go.

### `bd prime`

**What it does**: prints AI-oriented workflow context (session-close
checklist, memory injection, etc.) meant to be run at the start of every
session/hook. Supports a full CLI-reference mode and a smaller `--mcp`
mode; a `.beads/PRIME.md` file overrides the default content entirely.

**Measured** (near-empty lab DB, one memory added mid-test):
- `bd prime` (full/CLI mode): **4,518 bytes** baseline, **4,760 bytes**
  after adding one `bd remember` entry — roughly **1,100-1,190 tokens**
  at a rough 4 bytes/token estimate.
- `bd prime --mcp`: **756 bytes**, roughly **190 tokens** — noticeably
  more than the "~50 tokens" the tool's own `--help` claims for MCP mode.

Since this is paid at the start of every one of the dispatcher's 2-6
concurrent fresh coding sessions, it's worth re-measuring against the
factory's real `.beads/PRIME.md` and actual memory count before deciding
whether to force `--mcp` mode, trim `PRIME.md`, or write a smaller custom
override.

**What Go code it would save**: a bootstrap-context string the dispatcher
would otherwise assemble itself (workflow rules, close-out checklist) —
`bd prime` gives that for free, with the memory injection as a bonus.

**Cost**: real and recurring (paid every session), but the alternative
(no priming, or hand-rolled priming) isn't obviously cheaper, and
`PRIME.md`/`--mcp` give two independent ways to shrink it.

**Verdict: USE NOW**, but treat its size as a budget line item to
monitor, and prefer `--mcp` mode or a custom `.beads/PRIME.md` if the
default 1,100-token full-mode output is more than the dispatcher wants to
pay per session.

### `bd stale` / `bd blocked` / `bd ready --explain`

**What they do**: `bd stale [--days N] [--status ...]` lists
issues untouched for N days (default 30); `bd blocked [--parent id]`
lists currently-blocked issues; `bd ready --explain --json` returns
structured `{"blocked":[...,"blocked_by":[...]],"ready":[...]}` reasoning
for every issue. All three are pure reads over the existing DB — no AI
involved — and all support `--json`.

**Minimal example (tested)**:
```
$ bd ready --explain LAB-sgp --json
{"blocked":[{"id":"LAB-g4b","blocked_by":[{"id":"LAB-uxb", ...}], ...}],
 "ready":[{"id":"LAB-2mr", ...}, ...]}
```

**What Go code it would save**: exactly the "is anything stuck" health
check described in the ticket — a DB query across statuses/timestamps
that the dispatcher or a cron job would otherwise write in Go/SQL.

**Cost**: zero beyond the `bd` call itself.

**Verdict: USE NOW** — this directly answers the ticket's question:
**yes, "stuck detection" should be a zero-token shell/cron check**, e.g.
```
bd stale --status in_progress --json | jq 'length'
bd blocked --json | jq 'length'
bd ready --explain --json | jq '.blocked | length'
```
run on a timer, alerting only past a threshold — no AI seat needed to
answer "is anything stuck."

### `bd mail`

**What it does**: confirmed via `--help` — `bd mail` is a **pure
delegation shim**. It does nothing on its own; it forwards to whatever
external command is set via `BEADS_MAIL_DELEGATE`/`BD_MAIL_DELEGATE` env
var or `bd config set mail.delegate "..."` (the examples in its own
`--help`, e.g. `bd mail send mayor/ -s "Hi"`, are examples of Gas Town's
separate `gt mail` tool's syntax, not something `bd` implements).

**Minimal example**: not run — there is nothing to instantiate without an
external mail tool configured, and installing one is out of scope
("don't install anything").

**What Go code it would save**: nothing, as shipped — it would only save
code if you also built or adopted an external mail-capable tool to
delegate to.

**Cost**: zero to ignore.

**Verdict: IGNORE** — provides no functionality by itself; only relevant
if/when the factory grows a genuine inter-agent messaging need serious
enough to justify writing or adopting a delegate tool, which is well
beyond a solo dispatcher's current requirements.

### `bd preflight`

**What it does**: prints (or `--check`s) a PR-readiness checklist —
tests run, lint clean, `gofmt`, `.beads/issues.jsonl` pollution, nix
`vendorHash` freshness, version mismatches.

**Minimal example**: not run (would just report on the beads-lab repo
itself, which has no Go/nix files, so the check is meaningless there).

**What Go code it would save**: nothing generic — this checklist's
specific items (Go formatting, Nix `vendorHash`) are contributor checks
for the `bd` project's own repo, not a general "is this PR ready" tool.

**Cost**: negligible to ignore.

**Verdict: IGNORE** — not a generic primitive; it's beads'-own CI
checklist, not reusable for an arbitrary factory repo unless that repo
happens to share beads' exact Go+Nix toolchain.

### `bd lint` + `--validate`

**What it does**: checks that each issue type has its required narrative
sections — bug needs "Steps to Reproduce" + "Acceptance Criteria",
task/feature need "Acceptance Criteria", epic needs "Success Criteria",
chore needs nothing. It accepts either the structured field (e.g.
`acceptance_criteria` set via `--acceptance`) **or** a literal `##
Acceptance Criteria` heading in the free-text `description`.
`--validate` on `bd create` applies the same rule at creation time.

**Minimal example (tested)**:
```
$ bd create "Direct Test" -d "body" --acceptance "criteria X" ...
$ bd lint LAB-2n1
✓ No template warnings found (1 issues checked)
$ bd create -f plan.md   # (no acceptance field, no heading)
$ bd lint LAB-5mm
⚠ Missing: ## Acceptance Criteria
```

**What Go code it would save**: a story-quality gate the dispatcher would
otherwise write itself before handing a story to a coding session (don't
dispatch a story with no acceptance criteria).

**Cost**: essentially free — one call, clear output, `--json`-able.

**Verdict: USE NOW** — run `bd lint` (or `bd create --validate`) as a gate
before dispatch; it's the cheapest possible check that a story is
actually specified well enough for an AI session to self-review against.

**Direct answer to the ticket's question**: yes, "plain story template +
acceptance criteria" is enough on its own — `bd lint`/`--validate`
already give you the enforcement mechanism for that template without
needing a formula at all.

### `--estimate`, `--skills`, `--acceptance`, `--design`, `--spec-id` on `create`

**What they do / verified**: `--estimate` (int minutes), `--acceptance`
(string), `--design` (string) and `--spec-id` (string) are all real,
independently queryable fields — confirmed via `bd show --json`, e.g.
`{"design":"use JWT","spec_id":"spec-42", ...}`. **`--skills` is not** a
structured field: passing `--skills "go,testing"` produced no `skills`
key in `--json` output; instead it appended a literal `## Required
Skills\ngo,testing` block onto the end of `description`.

**What Go code it would save**: a handful of typed fields on the story
object (estimate, spec link, design notes, acceptance text) that the
planner would otherwise have to encode into freeform description text and
parse back out.

**Cost**: zero beyond remembering the `--skills` gotcha.

**Verdict: USE NOW** for `--estimate`/`--acceptance`/`--design`/
`--spec-id`; **treat `--skills` as description text, not a filter field**
— use a label (`-l skill:go`) instead if the dispatcher needs to route
stories by required skill.

---

## Gaps / couldn't verify

- **`bd cook`'s actual working JSON/TOML formula schema** — every
  hand-authored attempt (JSON and TOML, with/without `id`/`name`/`title`,
  with a renamed variable to rule out a name collision) failed identically
  with `formula: name is required` even when a `name` field was present.
  This may be a real bug in v1.0.4, or an undocumented schema shape (e.g.
  a wrapper key) that isn't visible from `--help`/error text. Needs either
  reading `bd`'s Go source or finding a working example formula file from
  the project's own repo/tests (not fetched for this ticket).
- **CHANGELOG version numbers for when `formula`/`mol bond`/`swarm`/
  `merge-slot` were introduced** — a delegated research pass could only
  reliably fetch and quote the most recent portion of CHANGELOG.md (up
  through the 1.2.x/1.3.0 entries, dated through 2026-09-15); older
  version attributions (v0.3x-v0.4x range) came from third-party
  release-note mirrors, not a direct CHANGELOG.md quote, and are not
  presented as verified facts above.
- **Whether `--graph`'s `from_id`/`to_id` (issue-ID-based edges, as
  opposed to the tested `from_key`/`to_key`) work identically** — not
  separately tested; `from_key`/`to_key` against in-batch `key`s is
  confirmed working end-to-end (verified via `bd dep list`).
- **`bd audit record`/`bd gate discover`/`bd gate add-waiter`/`gh:run`
  and `gh:pr` gate types end-to-end** — read from `--help` and docs only,
  not executed, since they need either a real GitHub Actions run/PR or a
  git-tracked audit file, neither of which fit a disposable lab repo.
- **`bd batch`** (not in the ticket's list, but surfaced during research
  as possibly relevant): a real, documented command that runs
  `close`/`update`/`create`/`dep add|remove` from a script in one Dolt
  transaction, explicitly built to reduce write amplification from
  looping `bd` invocations — not tested here, but worth a follow-up look
  if the dispatcher/planner end up issuing many small `bd` writes per
  epic instead of one `--graph` call.
