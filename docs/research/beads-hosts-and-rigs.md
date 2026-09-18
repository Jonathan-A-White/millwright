# Beads: one database, two hosts, many rigs

Research ticket answered by reading `bd <cmd> --help` (bd v1.0.4, Dolt backend), running
small experiments in a scratch dir (git init + `bd init -p <prefix> --non-interactive
--role maintainer --skip-agents --skip-hooks`), and reading the upstream
[steveyegge/beads](https://github.com/steveyegge/beads) README, `docs/` tree, and
`CHANGELOG.md`. All experiments ran one `bd`/`dolt` process at a time; the lab dir has
been deleted.

## What this means for the factory (read this, skip the rest if short on time)

- **Use one Dolt database for the whole factory, not one per rig.** Put `.beads/` in a
  dedicated factory control repo (this repo already does this — see `/root/millwright-vault/.beads`
  and `CLAUDE.md`'s "issues live in a local Dolt DB" line). Tag every story with
  `metadata: {"rig": "<repo-name-or-path>", ...}`. Ordinary `bd dep add ... blocks`
  works across rigs for free because they're just rows in one table — no `bd repo`,
  hydration, or `bd ship`/`external:` needed. Upstream docs say this explicitly:
  *"You DON'T need multi-repo if: working solo on your own project."*
- **Sync the two hosts with `bd dolt push` / `bd dolt pull` against a Dolt remote
  living on the same git remote you already use for the factory repo** (`git+ssh://` to
  a private GitHub repo works — Dolt writes to `refs/dolt/data`, a separate ref from
  your source branches, so yes, a plain private GitHub repo works, no DoltHub or
  sql-server required). This is the *only* durable cross-machine sync path;
  `.beads/issues.jsonl` is an export, not sync — never rely on it for reconciliation.
- **Stay in embedded mode** (the default) on both hosts — zero persistent processes,
  fits the 1 GB VPS. Its cost: single-writer file lock, so never run two `bd` write
  commands against the same `.beads/` concurrently (matches the hard rule already
  given for this task).
- **`--claim` is atomic only against one physical database.** Two hosts that both
  claim the same issue while offline will produce a genuine Dolt row-level merge
  conflict on the next `bd dolt pull` — confirmed by reproducing it. Resolving it in
  embedded mode requires dropping to raw `dolt sql`/`dolt conflicts resolve` (`bd
  doctor` and `bd sql` both refuse to run in embedded mode) — plan for this friction,
  don't assume `bd doctor --fix` will save you.
- **Dispatcher's per-story routing (`rig`, `model`, `effort`) is just `metadata` JSON.**
  Confirmed working end-to-end: create, update, and `bd ready --metadata-field
  k=v --json` filtering. Consider aligning `model`/`effort` with beads' own advisory
  convention keys `execution_suggested_model` / `execution_reasoning_effort` for
  future interop.
- **Gaps / couldn't verify:** exact RSS of a running `dolt sql-server` (didn't dare
  start one on this 300 MB-free box); concurrent-writer safety in embedded mode beyond
  what the docs state (didn't run two `bd` writers in parallel, per hard rule).

---

## 1. Sync between two machines

### Commands surveyed (`--help`)

- `bd federation` — peer-to-peer sync between independent Dolt "towns" (separate
  databases, each owning its own copy), via `add-peer`/`sync`/`status`. Requires
  *server mode* (SQL peer connections); not for a single embedded solo database.
- `bd dolt` — remote management + push/pull. `bd dolt remote add <name> <url>`,
  `bd dolt push`, `bd dolt pull`. This is the documented cross-machine sync path.
- `bd vc` — local git-like operations (commit/merge/status) on the Dolt history,
  branch-local, not cross-machine.
- `bd branch` — list/create Dolt branches (analogous to git branches inside one Dolt db).
- `.beads/issues.jsonl` (auto-exported after every write, throttled to 60s) — **not**
  the sync channel. Upstream is explicit: *"issues.jsonl is an export for viewers and
  interchange... not the source of truth, not a full database backup, and cannot
  safely reconcile deletes or pruning."* (`docs/getting-started/sync-setup.md`)
- `bd backup` — a *separate* Dolt backup destination (filesystem path or DoltHub URL),
  for off-machine disaster recovery, not day-to-day two-host sync:
  ```
  bd backup init <path>     # filesystem or DoltHub
  bd backup sync            # push
  bd backup restore [path]
  ```

### Can the remote be a plain private GitHub repo?

**Yes.** From `docs/architecture/dolt.md`:

```
# DoltHub (public or private)
bd dolt remote add origin https://doltremoteapi.dolthub.com/org/beads
# S3
bd dolt remote add origin aws://[bucket]/path/to/repo
# GCS
bd dolt remote add origin gs://[bucket]/path/to/repo
# Git SSH (GitHub, GitLab, etc.)
bd dolt remote add origin git+ssh://git@github.com/org/repo.git
# Local file system
bd dolt remote add origin file:///path/to/remote
```

> **Sharing a Git repo**: Dolt stores data under `refs/dolt/data`, separate from
> standard Git refs (`refs/heads/`, `refs/tags/`). You can safely point a `git+ssh://`
> remote at the same repository as your project source code.

`bd init` even auto-detects `git remote get-url origin` and wires it as the Dolt
remote automatically, so on a normal GitHub-backed repo, `bd dolt push` "just works"
against the same private repo you already use for code — no DoltHub account, no
`dolt sql-server`. **This is the simplest robust setup** for one person, two hosts.

### Experiment: `file://` remote as a stand-in for a private GitHub repo

Verified push/clone/pull mechanics with a local `file://` remote (network-equivalent
to `git+ssh://` for Dolt's purposes — same remote abstraction):

```
$ bd dolt remote add origin file:///.../remote-store
Added remote "origin" → file:///.../remote-store (SQL + CLI)

$ bd dolt push
Pushing to Dolt remote...
Push complete.

# fresh "second host" clone:
$ bd init -p lab --non-interactive --role maintainer --skip-agents --skip-hooks \
    --remote file:///.../remote-store
✓ Bootstrapped from remote: file:///.../remote-store
✓ bd initialized from git remote!
$ bd list --json   # shows all issues + dependencies from host A, verbatim
```

Day-to-day workflow per upstream docs (`docs/getting-started/sync-setup.md`):

```
Machine A                          Machine B
bd create "New task" -p 1
bd dolt push
                                    bd dolt pull
                                    bd update bd-a1b2 --claim
                                    bd close bd-a1b2 --reason "Done"
                                    bd dolt push
bd dolt pull
bd list                            # sees the closed task
```

Rules called out explicitly: always use `bd dolt ...`, never raw `dolt` while a server
is running (embedded has no server, so raw `dolt` is safe there when no `bd` process
is active — verified below); commit (`bd dolt commit`) before pulling if you have
uncommitted changes, or pull fails with "cannot merge with uncommitted changes";
push before switching machines — unpushed work only exists locally.

Sources: [`docs/architecture/dolt.md`](https://github.com/steveyegge/beads/blob/main/docs/architecture/dolt.md),
[`docs/getting-started/sync-setup.md`](https://github.com/steveyegge/beads/blob/main/docs/getting-started/sync-setup.md),
[`docs/core-concepts/sync-concepts.md`](https://github.com/steveyegge/beads/blob/main/docs/core-concepts/sync-concepts.md)

---

## 2. Offline divergence: how conflicts merge, what claims guarantee

Dolt merges **at cell/row granularity**, not line-based — different rows, or
different columns of the same row, merge automatically with no conflict. The **same**
column of the **same** row modified differently on both sides is a genuine conflict
that Dolt cannot auto-resolve.

### Experiment A — non-conflicting divergence (should auto-merge)

Both hosts create *new*, independent issues offline (hash IDs mean no PK collision):

```
# host A (offline): bd create "Created offline on host A" --actor hostA  -> lab-m5q
# host B (offline): bd create "Created offline on host B" --actor hostB  -> lab-mpd
# host A pushes, host B pulls:
$ bd dolt pull
Pulling from Dolt remote...
Pull complete.
$ bd list --json | grep '"id"'
    "id": "lab-mpd"
    "id": "lab-m5q"
    ...
```
Clean auto-merge, no conflict. This is exactly what hash-based IDs
(`docs/core-concepts/hash-ids.md`) are for: *"No coordination needed between
creators... merge-friendly across branches."*

### Experiment B — real conflict: both hosts claim the same issue offline

```
# host A: bd update lab-act --claim --actor hostA   -> assignee=hostA, status=in_progress
# host B (not yet synced): bd update lab-act --claim --actor hostB -> assignee=hostB, status=in_progress
# host A pushes first:
$ bd dolt push   # succeeds
# host B pulls:
$ bd dolt pull
Error: merge origin/main: Error 1105: Merge conflict detected, @autocommit transaction
rolled back. @autocommit must be disabled so that merge conflicts can be resolved
using the dolt_conflicts and dolt_schema_conflicts tables before manually committing
the transaction. Alternatively, to commit transactions with merge conflicts, set
@@dolt_allow_commit_conflicts = 1
```

Pull fails outright and rolls back — host B keeps its own local state (`assignee:
hostB`) until the conflict is resolved. **This is not automatic "last write wins."**

Trying to resolve it through `bd` itself, in embedded mode:

```
$ bd doctor
Note: 'bd doctor' is not yet supported in embedded mode.
$ bd sql "select * from dolt_conflicts"
Error: 'bd sql' is not yet supported in embedded mode
```

**Neither of the two tools the official merge-conflict runbook
(`docs/recovery/merge-conflicts.md`) recommends work in embedded mode.** The only path
that worked was dropping to the raw `dolt` CLI directly against
`.beads/embeddeddolt/<db>/` (safe when no `bd`/server process is holding it):

```
$ cd .beads/embeddeddolt/lab
$ dolt sql -q "set @@dolt_allow_commit_conflicts=1; call dolt_merge('origin/main');"
| conflicts | message         |
| 1         | conflicts found |
$ dolt sql -q "select * from dolt_conflicts"
| table  | num_conflicts |
| issues | 1             |
# inspect dolt_conflicts_issues: shows base/our/their columns per row —
# our_assignee=hostB, their_assignee=hostA, base_assignee=NULL
$ dolt sql -q "call dolt_conflicts_resolve('--theirs', 'issues');"
$ dolt add -A && dolt commit -m "merge: resolve conflict on lab-act"
$ bd show lab-act --json   # bd sees the resolved row correctly
$ bd dolt push             # succeeds; back in sync
```

**Answer to the ticket's question:** conflicts are resolved via Dolt's native
cell/row-level 3-way merge (base/ours/theirs), not by beads. Hash IDs eliminate
create/create conflicts entirely. Genuine value conflicts only arise when both hosts
mutate the *same row's same columns* offline (classically: both claim, or both
close-with-different-reason, the same issue) — and resolving those in the factory's
default embedded mode requires raw `dolt sql`, since `bd doctor`/`bd sql` refuse to
run there. `bd federation sync` (a different, heavier mechanism, see §1) does offer
`--strategy ours|theirs` for automatic bulk resolution, but that's peer-to-peer
federation between independent towns, not the embedded push/pull path recommended
for a solo dev.

**`--claim` atomicity** (`bd update --claim`, `bd ready --claim`): per
`docs/multi-agent/coordination.md`, *"`--claim` is atomic: when multiple agents pull
from the same ready queue, the first claim wins, and repeating a claim you already
hold is idempotent."* This is a SQL-transaction-level guarantee against **one physical
database** (one host's embedded engine, or one shared `dolt sql-server`). It is
**not** a cross-host guarantee — two hosts can both "win" a claim locally while
disconnected; the collision only surfaces later as the Dolt merge conflict
demonstrated above. Practical rule for the factory: only run the dispatcher (which
claims stories) on one host at a time, or accept occasional manual conflict
resolution when both fire close together.

Sources: [`docs/recovery/merge-conflicts.md`](https://github.com/steveyegge/beads/blob/main/docs/recovery/merge-conflicts.md),
[`docs/multi-agent/coordination.md`](https://github.com/steveyegge/beads/blob/main/docs/multi-agent/coordination.md),
[`docs/core-concepts/hash-ids.md`](https://github.com/steveyegge/beads/blob/main/docs/core-concepts/hash-ids.md),
own experiments above.

---

## 3. Many rigs: one database vs. one database per repo

### What `bd repo` actually configures

`bd repo add <path>` appends to `repos.additional` in `.beads/config.yaml` (version
controlled):

```yaml
repos:
  primary: "."
  additional:
    - "../lab2"
```

`bd repo sync` then reads **each additional repo's `.beads/issues.jsonl` export** and
**imports (upserts)** those issues into the *local* database, tagging them with
`source_repo`. Confirmed by experiment:

```
$ bd repo add ../lab2
Added repository: ../lab2
$ bd repo sync --verbose --json
Imported 1 issue(s) from ../lab2
$ bd list --json   # now shows lab2-yi5 alongside lab-native issues
```

This is a **one-way, upsert-only, read-side hydration** — it is explicitly documented
as unable to detect deletes/pruning (same caveat as the JSONL-is-not-sync warning in
§1), and the resulting cross-repo `bd dep add` edge (tested: `bd dep add lab-dm0
lab2-yi5` succeeded and showed up in `bd show`) lives **only in the hydrating repo's
own database** — it is not written back into `lab2`'s database. Query `lab2` directly
and it knows nothing about that dependency.

### `--repo` and "auto-routing" on `bd create`

`--repo <path>` on `bd create` is a **write-time override that targets another repo's
actual database** — not a label. Confirmed:

```
$ bd create "Routed to lab2 path story" --type task --repo ../lab2 --json
{"id": "lab2-ax5", ...}
$ cd ../lab2 && bd list --json | grep '"id"'
    "id": "lab2-ax5"     # really landed in lab2's own Dolt db
    "id": "lab2-yi5"
```

Caveat found by experiment: passing a value that does **not** exactly match a
configured `repos.additional` entry (e.g. `--repo lab2` instead of the configured
`../lab2`) silently produced a bogus success response (`{"id": "lab-w1x", ...}`) for
an issue that then existed in **neither** database — `bd show` failed in both repos.
Treat `--repo` as requiring the exact string used in `config.yaml`; don't trust the
success output without a `bd show` follow-up.

"Auto-routing" (`routing.mode: auto`) is **not** about picking which of many rigs a
story belongs to — it's a maintainer-vs-contributor mechanism for OSS fork workflows:
route your own planning beads to a private `~/.beads-planning` repo so they never
land in (and pollute) an upstream project you're contributing to. Role is detected
from git remotes (`origin`/`upstream` divergence) or set explicitly via `bd config
set beads.role`. This machinery exists for exactly the scenario upstream calls out as
irrelevant to a solo dev:

> **You DON'T need multi-repo if:** working solo on your own project... **You DO need
> multi-repo if:** contributing to OSS, fork workflows, multiple work phases, multiple
> personas. — `docs/multi-agent/multi-repo-migration.md`

### Model A (one shared database, `rig` as metadata) vs. Model B (one database per rig + `bd ship`/`external:`)

| | Model A: single DB, `metadata.rig` tag, ordinary `blocks` deps | Model B: one DB per rig, `bd repo` hydration and/or `bd ship` + `external:<project>:<capability>` |
|---|---|---|
| Cross-rig dependency granularity | Fine-grained, story-to-story `bd dep add` (any dependency type) | `external:` refs only resolve at the **capability** level — blocked until *some* closed issue in the target project carries `provides:<capability>`. Coarser than "this story blocks that story." |
| Setup per rig | None — rigs don't need `bd init` at all | Every rig needs its own `bd init`, own Dolt db, own remote/sync cadence |
| Cross-host sync overhead | One `bd dolt push`/`pull` cycle for the whole factory | N independent push/pull cycles, one per rig's database, each subject to the single-writer/conflict behavior in §2 |
| Visibility of the dependency graph | Uniform: `bd ready`/`bd blocked` in the one DB sees everything | Only the *hydrating* repo sees hydrated cross-repo deps; querying a satellite rig's own DB directly shows nothing about it |
| Data leakage if a rig repo is later open-sourced or handed off | None — planning data was never in the rig repo | None either, if you never `bd init` the rig — but the built-in tooling assumes you would |
| What breaks | An AI session running *inside* a rig's working tree has no local `.beads/`; it must be told where the store is (`BEADS_DIR=/path/to/factory/.beads` or `bd --db .../factory/.beads/...`, or `-C`) to log comments/discovered work back to the right place | `bd repo sync`'s upsert-only import can silently retain issues the source deleted/pruned; hub vs. satellite dependency-graph views can disagree; doubles the moving parts for sync and conflict handling from §1/§2 |

**Recommendation for this factory: Model A.** The ticket's planner wants ordinary
`blocks` dependencies between stories that live in different rigs — that is
precisely the case `external:<project>:<capability>` is *not* built for (it's a
coarse "capability shipped" gate, see `bd ship --help` and
`docs/cli-reference/ship.md`), and precisely what a single shared database gives you
for free. This also matches how `/root/millwright-vault` is already set up per its own
`CLAUDE.md` ("issues live in a local Dolt DB... `.beads/issues.jsonl` is a passive
export") — i.e., the factory repo itself is the one shared store; rigs are plain code
checkouts with no beads awareness. The one thing to wire up deliberately: any AI
session or hook running inside a rig's own working tree that needs to talk to beads
(e.g., to add a `discovered-from` issue) should point at the factory's `.beads` via
`BEADS_DIR` or `--db`/`-C`, rather than relying on `.beads` auto-discovery (which will
find nothing, since the rig has no local store).

Sources: [`docs/multi-agent/routing.md`](https://github.com/steveyegge/beads/blob/main/docs/multi-agent/routing.md),
[`docs/multi-agent/multi-repo-migration.md`](https://github.com/steveyegge/beads/blob/main/docs/multi-agent/multi-repo-migration.md),
[`docs/cli-reference/ship.md`](https://github.com/steveyegge/beads/blob/main/docs/cli-reference/ship.md) (per `bd ship --help`, not separately re-fetched),
own experiments above.

---

## 4. Per-story routing metadata: `rig`, `model`, `effort`

All confirmed by experiment.

**Create with metadata:**
```
$ bd create "Test story A" --type task \
    --metadata '{"rig":"x","model":"fable-5.1","effort":"high"}' --json
{
  "created_at": "2026-09-18T12:04:22.372712306Z",
  "created_by": "root",
  "id": "lab-dm0",
  "issue_type": "task",
  "metadata": {"effort": "high", "model": "fable-5.1", "rig": "x"},
  "priority": 2, "schema_version": 1, "status": "open",
  "title": "Test story A",
  "updated_at": "2026-09-18T12:04:22.372712306Z"
}
```

**`bd show <id> --json` shape** (array of one object):
```json
[
  {
    "id": "lab-dm0",
    "title": "Test story A",
    "status": "open",
    "priority": 2,
    "issue_type": "task",
    "created_at": "2026-09-18T12:04:22Z",
    "created_by": "root",
    "updated_at": "2026-09-18T12:04:22Z",
    "metadata": {"rig": "x", "model": "fable-5.1", "effort": "high"}
  }
]
```

**Update metadata later** — both a single-field patch and a full replace work:
```
$ bd update lab-dm0 --set-metadata rig=y --json          # patches one key, keeps others
$ bd update lab-dm0 --metadata '{"rig":"z","model":"sonnet-5","effort":"low"}' --json
                                                           # full replace, drops unlisted keys
```
(`--unset-metadata <key>` also exists per `bd update --help`, not separately tested.)

**`bd ready --metadata-field rig=x --json` filters correctly:**
```
$ bd ready --metadata-field rig=x --json     # while rig=x -> returns the issue
$ bd ready --metadata-field rig=y --json     # after changing rig to y -> [] (empty, confirms filter is live, not stale)
```
`bd ready --json` shape adds ready-queue fields (`dependency_count`,
`dependent_count`, `comment_count`) on top of the `show` shape.

**`--claim` via `bd ready`:**
```
$ bd ready --claim --json
[{ "id": "lab-dm0", "status": "in_progress", "assignee": "root",
   "started_at": "2026-09-18T12:05:10Z", "metadata": {...}, ... }]
```

**Note on convention:** upstream already has an advisory metadata convention for
exactly this purpose (`docs/core-concepts/metadata.md`): `execution_agent_type`,
`execution_suggested_model`, `execution_reasoning_effort`, `execution_mode`,
`execution_parallel_group`. These are read by "parent/orchestrator agents... before
spawning subagents." Nothing requires using these exact keys — metadata is arbitrary
JSON — but the dispatcher may want `model`/`effort` to line up with
`execution_suggested_model`/`execution_reasoning_effort` in case any bd-native or MCP
tooling already reads them. `rig` has no existing convention key; it's a fine custom
key (reserved prefixes to avoid: `bd:` and leading `_`).

Sources: `bd create --help`, `bd update --help`, `bd ready --help`, own experiments,
[`docs/core-concepts/metadata.md`](https://github.com/steveyegge/beads/blob/main/docs/core-concepts/metadata.md).

---

## 5. `--shared-server` / `--server` vs. embedded

| | Embedded (default) | Server (`--server`) | Shared server (`--shared-server`) |
|---|---|---|---|
| Process model | Dolt engine runs **in-process inside each `bd` invocation**; no process persists between commands (confirmed: `ps aux \| grep dolt` was empty between our `bd` calls) | A separate, long-running `dolt sql-server` process your `bd` client connects to over TCP (127.0.0.1:3307) or a Unix socket | One long-running `dolt sql-server` for **all** projects on the machine, at `~/.beads/shared-server/`, port 3308 by default; each project gets its own database (must have unique prefix — colliding prefixes are detected and refused, not silently merged) |
| Writers | **Single-writer, enforced by file lock.** Concurrent `bd` write commands against the same `.beads/embeddeddolt/` will hit "database is locked" | Multiple concurrent writers (multiple agents/sessions) | Multiple concurrent writers, shared across projects |
| Memory footprint on a 1 GB host | Effectively zero at rest; a transient bump only for the duration of each `bd` call (this box has ~300 MB free and ran every experiment above without issue) | One persistent Go process (`dolt sql-server`) running continuously — **couldn't verify exact RSS**; deliberately did not start one on this box to avoid risking OOM with only ~300 MB free. Upstream's own stated motivation for shared-server mode is "reduced resource usage" vs. one server per project, implying non-trivial per-server overhead | Same persistent-process cost as server mode, but only **one** instance regardless of how many projects/rigs exist — the right choice if you ever do need a server on this VPS |
| When you need it | Solo dev, one writer at a time (matches the factory's own hard rule: run `bd` one at a time) | "Multiple agents writing simultaneously," "orchestrator multi-rig setups," "federation with remote peers" (`docs/architecture/dolt.md`) | Same triggers as server mode, but with several projects/rigs, to avoid one `dolt sql-server` per rig |
| Tooling gaps observed | **`bd doctor` and `bd sql` both refuse to run** ("not yet supported in embedded mode") — confirmed by experiment. Conflict resolution (§2) requires dropping to raw `dolt` CLI | `bd doctor`/`bd sql` presumably work (not tested — would require starting a persistent server on a memory-constrained box) | same as server mode |

**Concurrent bd processes in embedded mode:** the docs are direct — *"Embedded mode is
single-writer (enforced via file lock). If you need concurrent access, switch to
server mode"* (`docs/architecture/dolt.md`, "Lock Contention" section). This matters
directly for the dispatcher: if it ever launches two AI sessions that both need to
write to the *same* rig's/factory's database at the same moment (e.g., both claiming
different stories at once), embedded mode will serialize them via the file lock (one
waits or errors, not corrupts) rather than truly parallelize. **Couldn't verify**
whether a second embedded writer blocks-and-waits vs. errors immediately — didn't
run two `bd` writers concurrently, per the hard rule for this task. If the factory's
dispatcher genuinely needs several sessions writing to the shared store at once, plan
to move to `--shared-server` rather than assume embedded mode's file lock behaves
gracefully under contention.

Sources: `bd init --help`, `bd dolt --help`, own experiments,
[`docs/architecture/dolt.md`](https://github.com/steveyegge/beads/blob/main/docs/architecture/dolt.md)
("Modes of Operation", "Lock Contention (Embedded Mode)", "Shared Server Mode"
sections).

---

## 6. `bd worktree` and git worktrees

`bd worktree` (`create`/`info`/`list`/`remove`) is a convenience wrapper around `git
worktree` that also gets beads config right; per `--help`: *"Worktrees automatically
share the same beads database as the main repository via git common directory
discovery — no manual redirect configuration needed."*

Per `docs/reference/worktrees.md`, the underlying model (current beads; an older
`sync.branch`-based worktree scheme has been removed):

- **All worktrees of one repo share one `.beads/` workspace** (found via git's common
  directory, unless overridden with `BEADS_DIR`). There is no per-worktree database.
- Issue data lives in Dolt (`refs/dolt/data`), separate from whichever git branch is
  checked out in a given worktree — so switching branches in one worktree doesn't
  touch beads state at all.
- For "many parallel stories in one rig," this means: spinning up N git worktrees for
  N concurrent AI sessions on the same rig gives them **all the same underlying
  Dolt database** — i.e., the same single-writer-in-embedded-mode constraint from §5
  applies *across* worktrees of one rig, not just within one working directory.
  Upstream's own guidance: *"For ordinary single-user worktree use, run commands
  directly. For true multi-writer workflows across machines or agents, sync
  frequently with `bd dolt pull`/`push`, and coordinate through the tracker to avoid
  working the same issue concurrently."*
- An `BEADS_DIR` env var lets you point *any* number of code worktrees/clones at one
  external beads workspace that isn't co-located with any of them — the same
  mechanism the factory would use under Model A (§3) for a rig's working tree to talk
  to the factory's central `.beads/`.

Not separately re-tested with real `git worktree add` in the lab (out of scope for
memory-constrained testing) — took upstream's documented behavior at face value here,
cross-checked against the single-writer facts already confirmed empirically in §5.

Source: [`docs/reference/worktrees.md`](https://github.com/steveyegge/beads/blob/main/docs/reference/worktrees.md),
`bd worktree --help`.

---

## Gaps / couldn't verify

- Exact memory (RSS) of a running `dolt sql-server` or `--shared-server` process on
  this VPS — did not start one, to avoid OOM risk with ~300 MB free.
- Whether a second embedded-mode `bd` writer blocks-and-retries or fails immediately
  under lock contention — did not run two `bd` processes concurrently (hard rule).
- `bd doctor`/`bd sql` behavior in server mode — not tested (would require a
  persistent server).
- `bd federation`'s cell-level vs. whole-table conflict behavior in practice — read
  `--help` and docs only; did not stand up two federated "towns" (would need real
  network peers or a second local server).
- Real `git worktree add` + `bd worktree create` mechanics — read docs only, not
  re-run in the lab.
