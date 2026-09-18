# Herdr capabilities vs. what our Go dispatcher still has to build

Research date: 2026-09-18. Source: `herdrdev/herdr` GitHub repo (Rust, Apache-2.0, default branch
`master`, read via `gh api`, current HEAD as of today; latest tagged release v0.9.1, 2026-09-16)
and https://herdr.dev/docs/ (read via the `docs/next/website/src/content/docs/*.mdx` sources in
the repo, which are the doc pages' source of truth). No Herdr binary was installed or executed;
this is read-only source/doc research.

## What this means for the factory (read this first)

Herdr is a legitimate, fast-moving, close match for the "runner" layer of the factory: it already
gives you a persistent multi-pane terminal server with detach/reattach, a scriptable CLI + JSON
Unix-socket API (protocol 22) to create workspaces/panes, launch Claude Code in them with a cwd,
send input, read output, and get `idle/working/blocked/done/unknown` agent state via
regex-based screen-scraping (not Claude Code hooks — hooks only carry session-id for resume).
It has first-class Git-worktree support (`worktree create/open/remove`, literally wraps `git
worktree`) that will happily adopt worktrees `bd worktree` or plain `git worktree` already
created. Multi-machine (`--machine <label>`) works over normal SSH with its own bridge, and
"phone" access is just SSH + the TUI, no app. It does **not** give you Beads/issue-tracker
integration, a task queue, agent-to-agent messaging beyond one-agent-drives-another-via-CLI, cost
tracking, or scheduling — that's still ours to build. Given the tiny 1 vCPU/1 GB VPS target, treat
the resource-footprint numbers below as a real risk (issues show idle-client and multi-agent CPU
load); prototype against Herdr on the VPS specifically before committing. Recommendation: build
the Go dispatcher behind a small "runner" interface and talk to Herdr's Unix socket directly
(not the CLI, not by shelling out), with plain `tmux` as a fallback backend — do not depend on
Herdr's CLI-skill/agent-automation layer, which is designed for an LLM to drive interactively, not
for a headless supervisor.

## 1. Core model: workspaces, panes, sessions, persistence, tmux relationship

Herdr is **not** built on tmux and does not run inside it by default; it replaces tmux for this
use case, while remaining "tmux-style" in feel (prefix keys, splits) and able to run *as* a guest
inside tmux if you nest it (agent detection then does not look inside a tmux instance launched
from within a Herdr pane — it just sees `tmux` as the pane's foreground process). It is a Rust,
single-binary background **server** plus one or more terminal **clients**: `herdr` starts/attaches
a client to a local background server automatically; you never manage sockets by hand
(`docs/next/website/src/content/docs/concepts.mdx`, `how-to-work.mdx`).

Hierarchy: **session** (a named server namespace; default session is what plain `herdr` attaches
to; `herdr session list/attach/stop/delete` for independent extra servers) → **workspace** (top-level
project container, e.g. one per repo/task) → **tab** (a layout inside a workspace) → **pane** (a real
terminal/PTY). IDs are opaque and scoped to one server: `w1` (workspace), `w1:t1` (tab), `w1:p1`
(pane). An **agent** is "a process Herdr recognizes inside a pane" — a pane can exist with or
without an agent in it.

Persistence / detach-reattach (`session-state.mdx`): `ctrl+b q` detaches the client; the server and
all panes/processes keep running. Reattach with `herdr`. `herdr server stop` ends the server and
kills its pane processes. On a full server restart (crash, machine reboot, `server stop`), original
processes do **not** survive — confirmed explicitly in the README ("herdr restores the saved
layout and can resume supported agent sessions; the original processes do not survive") — but
Herdr restores workspace/tab/pane/cwd/layout/focus from a `session.json` snapshot, replaced panes
come back as fresh shells in their saved directory, and for a documented list of agents (Claude
Code integration v6+, Codex, etc.) with the official integration hook installed, Herdr can reissue
the agent's native resume command (`claude --resume <id>`) to get the conversation back. Optional
`pane_history` (off by default, disk-persisted terminal scrollback, disabled by default because it
can capture secrets) and an experimental, opt-in "live handoff" (`herdr update --handoff`,
`herdr --remote host --handoff`) can transfer *live* PTYs/processes across a server replacement,
batched over 64 panes per batch — this is the only path where processes truly survive a server
swap.

**Client/server architecture and resource footprint**: multiple clients can attach to the same
server and view different workspaces/tabs independently; each pane's terminal size follows
whichever client is viewing it. There is no official footprint number published in docs/README
(**couldn't verify** an official RAM/CPU spec). From real-world GitHub issues (data, not vendor
claims, so treat as inference/risk signal, not a guarantee):
- "[Headless server CPU remains high with multiple attached clients and working agents](https://github.com/herdrdev/herdr/issues/1862)"
  and "[Headless server CPU scales with host process count (60–100% on busy multi-user hosts)](https://github.com/herdrdev/herdr/issues/1399)" (both closed/fixed, but indicate the server does
  polling/rendering work proportional to attached clients and pane/process count).
- "[Idle attached clients add steady main-thread CPU on the headless server (4 idle 199x53 clients
  ≈ +40% of a core), lagging the active client](https://github.com/herdrdev/herdr/issues/3822)" (closed) — a
  concrete number, useful as an upper-bound sanity check for a 1 vCPU box with several idle
  watchers (e.g., a phone SSH session left open).
- "[client transport: a stalled (not closed) client pins a blocked writer + 2 FDs per client; CPU
  stays elevated](https://github.com/herdrdev/herdr/issues/3612)" (closed in 0.9.1 changelog, 30s
  timeout added for non-progressing observers).
No memory-footprint numbers were found anywhere (docs, changelog, or issues). Given "2-6 concurrent
story sessions" each with a live Claude Code pane, plus a watching phone/laptop client, on a 1 GB
VPS, this CPU-under-load pattern is a real risk to validate before depending on Herdr as the VPS
runner.

## 2. Agent awareness — how Claude Code state is detected

**Not** Claude-Code hooks for lifecycle state. Detection is layered (`docs/.../agents.mdx`,
confirmed by the manifest source):
1. Herdr identifies the **foreground process** in the pane (native OS process-group detection on
   Unix; descendant-scanning heuristics on Windows).
2. It reads the **live bottom-buffer terminal screen snapshot** (not scrollback) and evaluates a
   bundled **TOML rule manifest** per agent kind against it — regex over specific screen regions
   (`osc_title`, `bottom_non_empty_lines(N)`, `prompt_box_body`, `after_last_horizontal_rule`,
   `whole_recent`, OSC progress sequences) to classify `idle` / `working` / `blocked`. This is
   genuine terminal-output screen-scraping, with priorities and `not:`/`any:`/`all:` combinators to
   avoid false positives (e.g. distinguishing a real permission prompt from user-typed text that
   contains similar words). Full Claude Code manifest:
   `src/detect/manifests/claude.toml` (repo). Example rules: a working-spinner regex over Braille/
   half-circle glyphs in the OSC title, a `"esc to interrupt"` live-turn regex, a `"do you want to
   proceed?"` + Yes/No line-shape regex for `blocked` (Bash permission prompts), an
   idle rule keyed on the `❯` prompt-box glyph.
3. **Claude Code specifically has a hook integration** (`herdr integration install claude`, hook v10)
   but per `integrations.mdx` it is explicitly **session-identity only**: "The hook reports Claude
   Code session identity to the local Herdr socket on session start. Claude Code state comes from
   Herdr's screen manifest detection" — i.e. lifecycle state (idle/working/blocked) is *never*
   hook-derived for Claude Code, only the session-id-for-resume is. (Agents that ARE hook-authoritative
   for lifecycle are Pi, OMP, Kimi Code CLI, OpenCode, Kilo Code CLI, MastraCode — Claude Code is not
   in that list.) CHANGELOG history shows Herdr deliberately *removed* trust in Claude Code's
   post-tool-use/subagent-completion hooks for `working` state because they produced false
   "working" states (0.5.12, 0.7.4 changelog entries) — i.e. they tried hook-based state for Claude
   Code and walked it back in favor of screen-scraping.
4. States exposed: `idle`, `working`, `blocked`, `done` (idle-but-unseen, a per-client/per-CLI "seen"
   flag distinguishes `idle` from `done`), `unknown` (present but not confidently classified — never
   treated as proof of completion). `herdr agent explain <target>` dumps which manifest rule/region
   matched, for debugging misclassification.
5. Events/queries exposed: `agent.get` (poll one agent), `agent.wait --until <states> --timeout`
   (server-owned, event-driven wait, does not busy-poll), `agent.read` (pane text, with an
   alternate-screen-history helper for full-screen apps like Claude Code that scrolls the app's own
   viewport to pull more transcript), plus event subscriptions `pane.agent_detected` and
   `pane.agent_status_changed` (see §3).
6. Remote manifest auto-update: Herdr polls herdr.dev for manifest patches to *existing* known
   agents and hot-reloads them without a restart (disable with `[update] manifest_check = false`);
   adding a brand-new agent kind still needs a Herdr binary update. Local override files at
   `~/.config/herdr/agent-detection/<agent>.toml` always win.

## 3. Control surface — CLI and Unix-socket API

**Protocol / schema**: Herdr ships a versioned JSON-Schema document, `herdr api schema
[--json|--output PATH]`. Decoded from the repo (`docs/next/api/herdr-api.schema.json`, 277 KB):
top-level `"protocol": 22`, `"schema_version": 1`, and five schema roots — `request`,
`success_response`, `error_response`, `event`, `subscription_event`. So yes — there is a real,
versioned wire protocol ("protocol 22" is exact and current), and a full JSON Schema for it, but
**no Go client exists**; it's request/response JSON-lines over a Unix domain socket (path from
`$HERDR_SOCKET_PATH` inside a managed pane, or `crate::session::active_api_socket_path()`
otherwise) that we'd write a thin client for ourselves (trivial: one JSON object per line in, one
per line out, `{"id":...}` correlated). There is no official SDK for any language, Rust included —
the CLI itself is the only shipped client.

**Method surface** (from `socket-api.mdx`'s "Raw methods" table plus the schema's method-params
structs — ~90+ methods total), the ones relevant to a dispatcher:

| Need | CLI | Raw socket method |
|---|---|---|
| Create a workspace (project container) with cwd/env | `herdr workspace create --cwd DIR --label L [--focus]` | `workspace.create` (`cwd`, `label`, `focus`, `env`... via tab/pane create) |
| Create a tab/pane with cwd+env | `herdr tab create --cwd DIR --env K=V`, `herdr pane split --cwd DIR --env K=V --direction right\|down` | `tab.create`, `pane.split` (both take `cwd`, `env: {k:v}`, `focus`) |
| Launch a command in a pane | `herdr pane run <pane_id> "cmd"` | `pane.send_text` + implicit Enter, or use `agent.start` for a recognized agent |
| Start Claude Code itself in a pane | `herdr agent start <name> --kind claude --pane <id> [--timeout ms] -- <claude args>` | `agent.start` (`AgentStartParams{name, kind, pane_id, args, timeout_ms}`) — requires an *existing* pane at an idle shell prompt; never creates/splits layout |
| Send a prompt to the agent | `herdr agent prompt <target> "text" [--wait] [--timeout ms] [--until STATE...]` | `agent.prompt` (`AgentPromptParams{target, text, wait:{until,timeout_ms}}`) — atomically submits text+Enter and can start a wait in the same request |
| Send raw input / keys | `herdr pane send-text`, `herdr pane send-keys`, `herdr agent send-keys <target> esc\|ctrl+c\|...` | `pane.send_text`, `pane.send_keys`, `pane.send_input`, `agent.send_keys` |
| Read pane output | `herdr pane read <id> --source visible\|recent\|recent-unwrapped\|detection [--lines N] [--format text\|ansi]`, `herdr agent read <target> ...` | `pane.read`, `agent.read` (`.result.read.text`) |
| Wait for output to match | `herdr pane wait-output <id> --match TEXT \| --regex RE [--timeout ms]` | `pane.wait_for_output` |
| Query agent state | `herdr agent get <target>` | `agent.get` |
| Wait for a state change | `herdr agent wait <target> --until idle\|working\|blocked\|done\|unknown [--timeout ms]` | `agent.wait` (server-owned, event-driven, pins the specific agent instance so a pane replacement can't spoof completion) |
| Subscribe to state/output changes | (no direct CLI; socket only) | `events.subscribe` with a `Subscription` list including `pane.agent_status_changed`, `pane.agent_detected`, `pane.output_matched` (with its own match/timeout), `pane.exited`, `workspace.*`, `tab.*`, `pane.*`, `layout.updated`; also `events.wait` for one-shot waits keyed on an `EventMatch` (e.g. `pane_agent_status_changed`) |
| One-time state bootstrap for a cache | `herdr api snapshot` | `session.snapshot` (full workspace/tab/pane/agent dump; docs explicitly recommend opening `events.subscribe` *first*, buffering, then snapshotting, then replaying buffered events, to avoid a bootstrap race — good pattern to copy) |
| Close a pane / tab / workspace | `herdr pane close`, `herdr tab close`, `herdr workspace close [--group]` | `pane.close`, `tab.close`, `workspace.close` (`close_group` required to close a workspace that still has linked worktree workspaces open) |
| Report custom lifecycle state (for non-native agents / our own signals) | `herdr pane report-agent <pane> --source custom:x --agent name --state working\|idle\|blocked`, `herdr pane release-agent` | `pane.report_agent`, `pane.report_agent_session`, `pane.release_agent` |

Every mutating command's success response is JSON on stdout; `--json` is accepted (harmlessly, on
some subcommands it's a no-op since JSON is already the only output). CLI failures are JSON on
stderr with exit code 1 (server-level error) or exit code 2 (CLI syntax error) — easy for a Go
dispatcher to parse either way. `docs/next/website/src/content/docs/agent-automation.mdx` and the
bundled Claude-Code **skill** at `skills/herdr/SKILL.md` are written specifically to teach an
*interactive LLM* how to drive this surface conversationally (ID discovery via JSON responses,
`--current`/`HERDR_PANE_ID` conventions, safety rules like never running `herdr server stop` from
inside a session) — useful as a reference for semantics, but it's aimed at a chat agent, not a
headless supervisor process; our dispatcher should talk to the socket directly rather than shelling
CLI text out and re-parsing prose.

## 4. Worktree support

`herdr worktree {list,create,open,remove}` (CLI: `src/cli/worktree.rs`; socket: `worktree.create`
/`worktree.open`/`worktree.remove`/`worktree.list`) is a real, literal wrapper around `git
worktree`, tied one-to-one to a Herdr workspace:
- `worktree create --branch NAME [--base REF] [--path PATH]`: creates the branch (from `--base` or
  `HEAD`) if it doesn't exist, or checks it out if it does; runs the equivalent of `git worktree
  add`; defaults the checkout location to `<worktrees.directory>/<repo>/<branch-slug>` (config
  default `~/.herdr/worktrees`, `[worktrees] directory = "..."`, source-generated slugs like
  `worktree/brave-river-1a2b` if you don't name a branch); then opens it as a **new Herdr
  workspace**, grouped under the parent repo's workspace in the sidebar.
- `worktree open (--path PATH | --branch NAME)`: opens an **already-existing** checkout (one Herdr
  did not necessarily create) as a workspace, or refocuses it if already open. This is the
  interop path: point Herdr at a worktree `bd worktree` or plain `git worktree add` already made,
  and it will adopt it without needing to have created it. `worktree list` similarly enumerates
  existing Git worktree checkouts for a repo/cwd from the OS filesystem, not from Herdr's own
  state.
- `worktree remove`: runs `git worktree remove` against the linked workspace; **never deletes the
  branch**; needs `--force` for a dirty checkout or one with submodules.
- `workspace close --group` closes a parent workspace and all its linked worktree workspaces
  together (Herdr-side bookkeeping only — never deletes checkouts/branches); without `--group` it
  refuses (`workspace_group_close_required`) if worktree children are still open, protecting you
  from accidentally orphaning a Herdr view of live work.
- Git ownership check: if the repo is owned by a different user/SID than the Herdr process (a
  realistic scenario if the dispatcher runs as a different user, or on the VPS root vs. a service
  account), Git refuses by default; pass `--trust-repository` per-command to bypass (it does not
  touch global Git config).

**Interaction with `bd worktree` / plain `git worktree`**: since Herdr's model is "a Herdr workspace
with worktree provenance metadata" layered on top of an ordinary Git worktree directory, and
`worktree open` explicitly supports adopting a pre-existing checkout, this composes cleanly: let
`bd worktree` (or `git worktree add` directly) own worktree creation/naming/cleanup as the source of
truth, and have the dispatcher call `herdr worktree open --path <dir>` (or the raw
`worktree.open` socket call) purely to get a Herdr workspace/pane wrapped around it for
observability — you don't have to use Herdr's own `worktree create` branch-naming scheme at all.

## 5. Remote machines (`herdr --machine`, saved SSH) and phone access

Three distinct remote modes, all over plain OpenSSH — no custom daemon/agent required beyond Herdr
itself (`how-to-work.mdx`, `persistence-remote.mdx`, `connecting-machines.mdx`):

1. **SSH-then-run** (`ssh host` then `herdr`): simplest, tmux-equivalent path. Both ends need
   Herdr installed; the whole server+UI runs remotely; your local terminal just renders it. This is
   explicitly the recommended **phone** path.
2. **`herdr --remote <host>`** (client-local, one remote session): your local Herdr client attaches
   to one remote server over SSH; panes/processes live remotely, UI/theme/keybindings render
   locally; enables bridging local desktop clipboard-image paste to the remote pane (not possible
   in mode 1, since there the whole thing including "your desktop" is the remote box). Needs a
   compatible `herdr` binary on the remote `PATH` (or Herdr will offer, interactively, to install
   one — never non-interactively). Supports named remote sessions (`--session NAME`), a custom
   local build (`HERDR_REMOTE_BINARY=...`), and a control-socket/keepalive layer Herdr manages
   inside a temp SSH config unless `[remote].manage_ssh_config = false`.
3. **`herdr --machine <label-or-id>`** (v0.9.1, new): controls a **saved SSH machine profile**
   directly from the CLI *without an open TUI window* — e.g.
   `herdr --machine build-vps agent list`, `herdr --machine build-vps pane split ...`. This is the
   one most relevant to a headless dispatcher that wants to fan commands out to a remote Herdr
   server from a script. Requirements: both machines' Herdr installs must support "machine API
   forwarding" (a compatibility capability, not just any recent version); the remote server must
   already be running; forwarding **never** installs/starts/restarts the remote server and **never**
   silently falls back to Local — a failed remote command is a hard failure, not a local no-op.
   Remote worktree paths must be absolute or `~`/`~/...`; plugin link paths must be absolute.
   `herdr machine add <ssh-target> --label L [--remote-session NAME]` (interactive, may
   install/upgrade the remote package after confirmation) creates the saved profile; `machine
   list/rename/remove/enable/disable` manage it. Saved profiles store only an id/label/target/
   session/enabled flag — no credentials; auth is 100% delegated to OpenSSH (`ssh-add` for
   passphrase keys in non-interactive contexts).

**Behavior when the link drops**: `connecting-machines.mdx` documents this in detail — automatic
reconnect with exponential backoff up to 2 minutes (a healthy minute resets it to fast-retry);
health-probes when the connection goes quiet so a broken link doesn't stay "Online" indefinitely;
disconnected machines are skipped in navigation; last-known state is shown dimmed/stale and input
is disabled until a fresh reconnect; other connected machines are unaffected by one machine's
outage; a background (non-interactive) reconnect will never answer prompts, install/update/restart
a server, or perform handoff — only an explicit interactive `herdr --remote <target>` run can do
that "Attention"-clearing work. This directly matters for the "flaky train wifi" laptop scenario:
losing/regaining connectivity is a designed-for case, not an edge case, but note it never
auto-heals a server-version mismatch or install gap without a human at an interactive prompt.

**WSL2 as local vs. remote side**: WSL2 Ubuntu is just "Linux x86_64" to Herdr for all of this —
supported as both a local client machine and as an SSH-reachable remote host in the
multi-machine/`--remote`/`--machine` matrix (`connecting-machines.mdx`: "Linux, macOS, and Windows
clients connecting to Linux or macOS servers on x86_64 or aarch64, or Windows servers on x86_64").
There's no WSL-specific carve-out in the remote-machines docs; WSL-specific notes exist only in the
Windows-cursor-rendering section (see §6) and in scattered issues (see below).

**"Work from your phone" doc** (`how-to-work.mdx`, section literally titled "Work from your
phone"): no app, no web dashboard. Install any SSH client (their doc name-drops **moshi** for
iPhone), `ssh you@server`, run `herdr` — same persistent session, TUI adapts to narrow screens.
That's the entire mechanism; it rides on mode 1 above, so it inherits detach/reattach and doesn't
depend on `--remote`/`--machine` at all.

## 6. WSL2 specifics, root, low-memory hosts

No dedicated WSL2 doc page exists; WSL2 is mentioned only incidentally:
- `windows-beta.mdx`: on native Windows *and* WSL2, Herdr defaults `host_cursor = "auto"` to a
  **drawn** (cell-content) cursor rather than the terminal-native cursor, because ConPTY-style
  repaint causes native-cursor flicker/jumps under a multiplexer; this breaks IME composition
  anchoring for CJK input (`[ui] host_cursor = "native"` to opt back in, trading back the flicker).
  Also: "This [Kitty graphics] path has been exercised with Windows WezTerm hosting Herdr through
  WSL" — i.e. one of their own test configurations is Herdr running *inside WSL2*, hosted by a
  Windows terminal emulator.
- Real issues (GitHub search, all data points, several already closed/fixed by 0.9.1 but showing
  what's been fragile): clipboard bridging is the recurring WSL2 pain point — "[WSL herdr --remote
  cannot bridge Windows clipboard images](https://github.com/herdrdev/herdr/issues/3376)" (closed),
  "[fix: read Windows clipboard images in WSL](https://github.com/herdrdev/herdr/issues/3572)"
  (closed), "[WSL: image paste in local panes fails until wl-clipboard/xclip are installed](https://github.com/herdrdev/herdr/issues/3984)"
  (**open**) — install `wl-clipboard`/`xclip` in the WSL2 Ubuntu image if clipboard-image paste
  matters. Also OSC-sequence leakage onto the WSL2 shell prompt under specific terminal
  combinations (Alacritty, WezTerm) — closed but indicates WSL2's ConPTY/X11-less terminal chain
  has needed several rounds of fixes. The 0.9.1 changelog itself lists "Linux process discovery
  stays responsive around stuck WSL agents and avoids repeatedly scanning an ever-growing process
  tree" (issues #2179/#3621/#3674) — i.e. WSL agent process trees have caused real Linux-process-
  discovery slowdowns that needed dedicated fixes, as recently as this release cycle.
- **Running as root**: **couldn't verify** — no README/docs/CHANGELOG passage addresses running the
  Herdr server as root explicitly (issue search for "root" surfaced only path/`HERDR_PLUGIN_ROOT`/
  cwd-casing bugs, not user-privilege concerns). Nothing suggests it's disallowed, but nothing
  confirms it's supported/tested either — worth a smoke test on the VPS before relying on it,
  especially since a from-scratch VPS is commonly provisioned as root-only initially.
- **Low-memory hosts**: **couldn't verify** — no explicit "N MB minimum" guidance anywhere in docs
  or CHANGELOG. See §1 for the closest available signal (CPU-under-load issues; no RAM numbers
  found at all). Given the 1 GB VPS target, this is the single biggest unverified risk in this
  whole report — recommend an actual install+load test rather than trusting inference here, but per
  the ticket's read-only-research constraint that test is out of scope for this document.

## 7. Built-in orchestration features that overlap with what we'd build

Herdr's own framing (README: "agent-native — agents drive herdr through the cli and socket api:
they can spawn panes, prompt each other, and wait until another agent is genuinely blocked") is
explicitly about **one agent orchestrating others through the same primitives a human/script would
use** — not a separate orchestration subsystem. Concretely, from `agent-automation.mdx` and the
schema:

- **No task queue.** There is no concept of a work item, ticket, backlog, or queue in the API — a
  caller (script or agent) is responsible for deciding what work goes where. `agent.view.set` is
  the closest thing to a queryable "board": a plugin/script can install a declarative filter+sort
  projection over the live agent list (by status, workspace, arbitrary reported metadata token)
  for the sidebar/mobile view, but it's a *display* projection, not a task queue or execution
  scheduler — it doesn't dispatch work, just changes what's shown and in what order.
- **Agent-to-agent "messaging"** is really "one caller uses `agent.prompt`/`pane.send_*` on another
  agent's pane, and `agent.wait`/`events.subscribe` to notice when it's blocked" — the skill doc
  literally teaches an agent to `herdr pane split` a sibling, `herdr agent start` a helper kind, and
  `herdr agent prompt reviewer "..." --wait`. There's no mailbox, no structured message schema, no
  broadcast — it's "control another agent's terminal like a human would."
- **No prompts/templates system**, no model-selection abstraction (each `agent start --kind
  <kind> -- <native args>` just passes through to that CLI's own flags, e.g. `-- -m gpt-5.4` for
  Codex — model choice is 100% delegated to the underlying agent CLI), and **no cost tracking** or
  token/spend accounting anywhere in the docs, CHANGELOG, schema, or issue search (a search for
  "token telemetry" surfaced only a closed, unmerged third-party RFC issue proposing this as a
  plugin idea — confirming it does not exist upstream).
- **No scheduling** (cron-like or otherwise) — Herdr reacts to commands; it does not run anything
  on a timer itself.
- **No Beads/issue-tracker integration or notion of one** — confirmed by both doc read-through and
  a targeted GitHub code/issue search for "beads" (only an unrelated OSC-8-link issue matched) and
  "issue tracker" (nothing). Its only "issue" concept is the plugin `link_handlers` feature, which
  can regex-match a GitHub issue/PR URL clicked in a pane and fire a plugin action — not a tracker
  integration.
- **Plugins** (`herdr-plugin.toml`, `plugin.link/list/enable/disable/action.invoke`, event hooks on
  Herdr's own lifecycle events like `worktree.created`) are the extensibility seam Herdr expects
  third parties to build task-queue/scheduling/cost-tracking-style features on top of, rather than
  shipping them itself.

All of the above — task assignment/queueing, cross-story-session policy (concurrency limits,
priority), Beads read/write, cost/usage accounting, and any timer-driven behavior — is squarely
"still ours to build," per the table below.

## 8. Maturity / risk

- **Release cadence**: extremely fast for a project this young. CHANGELOG history runs from
  `0.4.6` (2026-04-09) to `0.9.1` (2026-09-16) — roughly 5 months, ~40 tagged releases in that
  window (multiple releases in some weeks, e.g. five between 0.6.0 and 0.6.10 in three weeks), plus
  near-daily "preview" builds shown in the GitHub Releases list (10 preview builds visible in just
  the last month of history alongside the last 3 stable tags). Stars/forks (39k stars, 3k forks per
  `gh api repos/herdrdev/herdr`) suggest real adoption, not a toy project, but the pace also means
  the CLI/API surface is still shifting.
- **Breaking-change history**: real and recurring, but well-flagged. CHANGELOG has explicit
  `### Breaking Changes` (and twice `### Breaking Changes Please Read`) sections at multiple past
  versions (0.5.x, 0.6.x, 0.7.5, 0.8.x eras) — examples: default prefix key changed `ctrl+s`→
  `ctrl+b`; `keys.quit` removed in favor of `keys.detach`; keybinding trigger syntax overhaul;
  legacy vt100 terminal backend removed in favor of Ghostty; single-process `--no-session` mode
  removed entirely; and, most relevant to us, **client/server wire-protocol version bumps that
  require stopping and restarting any already-running server** (e.g. "The client/server protocol is
  now version 8. Stop and restart any running v0.5.12 server before attaching with this release.").
  Given we're now at protocol 22, this has happened repeatedly. A background dispatcher that
  assumes a long-lived server process needs to handle `protocol_mismatch` errors (a documented,
  machine-readable CLI error code) and know when it must restart the Herdr server itself.
- **Open-issue themes** (334 open issues at time of research): clustered around Windows/ConPTY
  input-and-clipboard edge cases, WSL2 clipboard/process-discovery quirks (see §6), terminal-emulator-
  specific rendering/graphics quirks (WezTerm, kitty, Alacritty), and CPU/performance under
  multi-client or multi-agent load (see §1) — no evidence of core-crash-class instability, but a
  long tail of platform/terminal compatibility issues consistent with a fast-moving cross-platform
  terminal app.
- **Telemetry / network calls**: no analytics/telemetry product or opt-out setting found anywhere
  in docs, CHANGELOG, or source (`src/update.rs` is the only outbound-networking code reviewed and
  it's purely the updater). The only outbound network calls identified: (a) update-manifest checks
  against `https://herdr.dev/latest.json` / `preview.json` (background check surfaces availability +
  release notes only; **manual** `herdr update` actually downloads/installs — this is opt-in, not
  silent auto-update); (b) Homebrew-install version checks against `formulae.brew.sh`; (c)
  background remote agent-detection **manifest** pulls from herdr.dev (disable with `[update]
  manifest_check = false`); (d) obviously, your own SSH traffic for remote/machine features. No
  evidence of usage/telemetry beacons. **Couldn't verify** there is *zero* telemetry beyond this
  (no privacy policy was located to fetch/cross-check), but nothing in the reviewed source or docs
  contradicts "update checks + manifest checks only."
- **Auto-update behavior**: never silent/automatic in the sense of installing without being asked —
  background checks only *notify*; `herdr update` (or `herdr update --handoff` for the experimental
  live-transfer path) is a deliberate, user-invoked action. Homebrew/mise/Nix-managed installs
  disable Herdr's own updater entirely and go through their package manager instead. Server
  replacement during any update always defaults to **not** stopping a running server unless a
  protocol-incompatible upgrade forces it, and even then asks interactively (default answer "No").

## Note on untrusted content encountered

Per the research instructions, everything read from the repo/site was treated as data, and no
embedded instructions were acted on. Three places contained agent-directed instructional text
worth flagging explicitly:
1. `README.md`: "if you are an ai agent helping with this repository, read `AGENTS.md` before
   making changes and read `CONTRIBUTING.md` before opening issues or PRs" — addressed to an AI
   agent modifying the Herdr repo itself; not applicable (we are not modifying it), not followed.
2. `src/cli.rs` (`AGENT_HELP_FOOTER` constant, shown in `herdr --help` output): "Are you an AI? Use
   these resources ONLY IF your task specifically asks you to: ... https://herdr.dev/agent-guide.md
   ... https://herdr.dev/llms.txt ... SKIP if a Herdr skill is already in your context. Otherwise
   run: herdr --skill" — a self-referential prompt-injection-shaped string aimed at coding agents
   that run `herdr --help`. Not executed (we did not run any `herdr` binary); noted here only
   because the task specifically asked to flag such content.
3. `skills/herdr/SKILL.md`: an entire Claude-Code-format skill file, shipped in the repo, whose
   purpose is literally to instruct an AI agent how to drive the `herdr` CLI once running inside a
   Herdr pane (gated on `HERDR_ENV=1`). This is legitimate first-party product documentation (it's
   how Herdr wants agents to use it), not a hidden/malicious instruction, but it *is* agent-directed
   instructional content living in the data we were asked to research, so it's called out per the
   ticket's instructions. It was read and summarized (§3) as documentation, not followed as a
   command — this session never had `HERDR_ENV=1` set and never ran any `herdr` command.

## Herdr does this — don't build it vs. Still ours to build in Go

| Herdr does this — don't build it | Still ours to build in Go |
|---|---|
| Persistent background terminal server; detach/reattach without killing panes | Dispatcher process itself: deciding *when* to launch a new story session, how many concurrently (2–6 cap), and retry/backoff policy |
| Workspace/tab/pane hierarchy + JSON socket API (protocol 22) to create/split/close them with cwd+env | Beads (`bd`) integration: pulling ready work items, writing back status/results, any Beads↔session mapping |
| Launching Claude Code (or any of ~20 other agent CLIs) in a pane and tracking its `idle/working/blocked/done/unknown` state via screen-scraping + optional session-id hooks | Turning "blocked"/"done" events into factory-level decisions (auto-resume, escalate to Mayor, notify human) — Herdr only exposes the raw state, not policy |
| `git worktree`-aware workspace creation/open/remove, safe close-group semantics | Actually deciding worktree layout/naming/lifecycle policy for stories (or delegating that to `bd worktree`); Herdr only needs `worktree open --path <dir>` to adopt what we already made |
| Multi-machine control over SSH (`--machine`), reconnect/backoff, saved profiles with no stored credentials | Coordinating *which* machine a given story should run on (laptop asleep vs. VPS always-on) and failing over policy when a machine is unreachable |
| "Work from your phone" via plain SSH + adaptive TUI — no app needed | Any richer mobile UX (push notifications on blocked/done, a dashboard) beyond "open an SSH client" |
| Event subscriptions (`events.subscribe`) for pane/agent/workspace state changes, one-shot `events.wait` | The actual event-driven orchestration loop that watches those events and acts (spawn next story, alert Mayor, update Beads) |
| Detach-safe live persistence; snapshot restore of layout after a crash/reboot; optional native agent session resume | Task-level durability/idempotency — knowing a story survived a Herdr restart is not the same as knowing its Beads state is consistent |
| Plugin/event-hook extension seam (`herdr-plugin.toml`) if we ever want deeper Herdr-side hooks | Task queue, priority/scheduling, agent-to-agent structured messaging, prompt/template management, model-selection policy, cost/token tracking — none of this exists in Herdr at all |
| Version/protocol negotiation, update-manifest checks, live-handoff for zero-downtime server upgrades | Our own compatibility handling: reacting to `protocol_mismatch`, deciding when to restart the Herdr server ourselves, or vendoring a specific version to avoid drift |

## Recommendation: CLI vs. socket vs. runner-agnostic

**Build the Go dispatcher behind a small internal "runner" interface (create-session, run-command,
send-input, read-output, get-state, wait-for-state, close-session), with two backends: a Herdr
socket client as the primary implementation, and plain `tmux` (via its own control-mode or
`tmux send-keys`/`capture-pane`) as a fallback/portable backend.** Concretely:

- **Talk to Herdr's Unix socket directly, not the CLI.** The socket protocol is versioned,
  schema'd (protocol 22, full JSON Schema available via `herdr api schema`), and every operation we
  need (create workspace/pane with cwd+env, `agent.start`/`agent.prompt`/`agent.send_keys`,
  `pane.read`/`agent.read`, `agent.wait`, `events.subscribe`, `worktree.open`, `pane.close`) is a
  first-class method with structured JSON params/results. Shelling out to the CLI and parsing text
  would mean re-parsing the same JSON the CLI itself parses, plus process-spawn overhead per call
  on a 1-vCPU box, plus fragility if CLI flag/output shape changes across Herdr's fast release
  cadence — whereas the wire schema is explicitly designed for exactly this (`herdr api schema`
  exists *for* external tool authors). Go has no first-party client, but the protocol is simple
  newline-delimited JSON with correlated `id`s — a ~200-line Go client is a reasonable one-time
  cost, and decoding the JSON Schema gives us compile-time-checkable request/response structs for
  free.
- **Keep it behind an interface, not a hard dependency**, because: (a) Herdr's protocol has broken
  compatibility (protocol-version bumps forcing server restarts) multiple times in 5 months and will
  likely do so again; (b) the 1 GB VPS resource-footprint risk (§1) is unverified and could force a
  fallback to something lighter for the always-on box even if Herdr is great on the laptop; (c) a
  runner-agnostic interface lets us prototype/ship against `tmux` immediately (well-understood,
  already present everywhere, zero new moving parts) and swap in Herdr once its VPS footprint and
  our operational comfort with its release cadence are validated, without touching dispatcher logic.
- **Don't build on Herdr's CLI-skill / agent-automation layer as designed** — that layer
  (`skills/herdr/SKILL.md`, `agent-automation.mdx`) is explicitly written for an *interactive LLM*
  reasoning about IDs and safety rules turn-by-turn, not for a deterministic headless supervisor;
  our dispatcher wants direct, typed socket calls, not agent-facing prose conventions.

## Gaps / couldn't verify

- No official RAM footprint number for the Herdr server (idle or under N agents); only indirect CPU
  numbers from GitHub issues (§1). Needs a real install+test on the target VPS, which this
  read-only ticket didn't authorize.
- No confirmation either way on running the Herdr server as root (§6) — not addressed in docs, not
  flagged as unsupported in issues either.
- No official minimum-hardware/low-memory guidance page exists (searched docs and CHANGELOG).
- No privacy policy/telemetry disclosure page was located to cross-check the "no telemetry beyond
  update/manifest checks" conclusion in §8; that conclusion is based on absence of evidence in the
  docs, CHANGELOG, and the update/networking source file reviewed, not on a positive statement from
  Herdr.
- Did not exhaustively read every `src/` module (e.g. `src/pane_graphics_files.rs`,
  `src/kitty_graphics/*`, `src/ghostty/*`) since they're irrelevant to the dispatcher questions;
  focused reading was on `src/cli*`, `src/api/*`, `src/detect/*`, `src/remote/*`, `src/worktree.rs`,
  and the doc/CHANGELOG/schema sources listed above.
