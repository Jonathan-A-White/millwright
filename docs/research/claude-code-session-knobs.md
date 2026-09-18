# Claude Code Session Knobs: Research for Software Factory

> ## Verification notes (2026-09-18, checked after the research was written)
>
> Three claims below were checked against the hooks doc and one real headless turn
> (`claude -p "Reply with exactly: ok" --model haiku --output-format json`, run outside any project). Where this box disagrees with the text below, this box wins.
>
> 1. **Priming cost is ~23K tokens per session, not ~1.8K.** Measured: 13,607 cache-read + 9,580 cache-write + 10 input tokens for a one-word reply, with this machine's user-level config (28 commands, 6 agents, skills). This is the fixed price of every story and is worth trimming per seat.
> 2. **Cross-session prompt caching exists.** 13.6K of a brand-new session's prefix was served as a cache *read*; cache writes were `ephemeral_1h`. Sessions started within an hour of each other share the common prefix. The "no cross-session cache" statements below are wrong.
> 3. **Hooks do not carry usage or cost.** Documented SessionEnd/Stop/Notification input is `session_id`, `transcript_path`, `cwd`, `scratchpad_dir`, `permission_mode`, `hook_event_name` (+ `effort`, `prompt_id`, `last_assistant_message` on Stop; `model` on SessionStart). Hooks are for *state*; Notification matchers include `agent_needs_input`, `agent_completed`, `idle_prompt`, `permission_prompt`. The statements below that SessionEnd includes a `usage` object are not documented.
> 4. **The fuel ledger source is the `-p --output-format json` result**, confirmed to contain: `usage` (input / output / cache-read / cache-creation tokens, per-iteration breakdown), `total_cost_usd` (list-price equivalent, still a usable relative measure on a subscription), `modelUsage`, `num_turns`, `duration_ms`, `session_id`, `permission_denials`, `terminal_reason`. For interactive sessions the same numbers would have to come from the transcript JSONL at `transcript_path` — not yet verified.
> 5. **Not verified, treat as unknown:** the "~967K auto-compact threshold", the exact out-of-fuel stderr text and exit code, and Pro-plan model availability and allowances.


## What This Means for the Factory (10-line summary)

1. **Per-launch control**: `--model` (aliases: fable, opus, sonnet, haiku) and `--effort` (low/medium/high/xhigh/max) apply per session without touching global settings; `--fallback-model` lists backups when primary is overloaded.
2. **Three launch styles**: `-p/--print` headless runs with `--output-format json|stream-json`; `--bg/--background` spawns unattended background sessions listed by `claude agents`; normal interactive sessions in tmux panes with initial prompts.
3. **Unattended permission handling**: Use `--permission-mode auto` (classifier approves safe actions) or `dontAsk` on subscription auth; headless `-p` mode defaults to "host" permission prompts (rejects unsafe actions automatically with `--permission-prompts none`); subscription auth required for most features.
4. **Cost tracking**: `--output-format json` returns single result with `usage.cache_read_input_tokens`, `cache_creation_input_tokens`, and cost fields; Pro/Max track sessions in 5-hour rolling windows + weekly caps; "out of fuel" exits with code 1 on subscription plans ("You've hit your session limit").
5. **Hooks for external tracking**: SessionStart/SessionEnd fire per session; PreToolUse/PostToolUse per tool call; each receives JSON with `session_id`, `transcript_path`, `cwd`, `permission_mode`, `effort` level; scope hooks per project in `.claude/settings.json` (shared) or `~/.claude/settings.json` (global).
6. **Priming cheap**: `--append-system-prompt(-file)`, `--settings`, `--add-dir`, and CLAUDE.md auto-load at session start—all paid upfront; `--agent` and skills lazy-load only when used; no cross-session prompt cache persistence (each session starts fresh, but cache lifetime is 1 hour on subscription within plan usage, 5 min on usage credits).
7. **Pro ($20) vs Max ($200)**: Same models (Fable 5.1, Opus 5, Sonnet 5 available on both); Pro has 5-hour session window + weekly limit; Max has 20× more tokens. Both use Claude subscription (no API key); Fable 5.1 available on both.
8. **Context windows**: Fable 5.1 default 200K (1M variant via `fable[1m]`); Opus 5 and Sonnet 5 have native 1M windows; auto-compact at ~967K threshold; disable 1M with `CLAUDE_CODE_DISABLE_1M_CONTEXT=1`.
9. **Session lifecycle**: Each fresh session starts with no context; background sessions persist in state directories and reattach via `claude attach <id>`; transcripts saved to JSONL in session dir; `/clear` resets context; `/compact` summarizes history when approaching limits (not enforced end-of-session).
10. **Detect out of fuel**: Subscription plan hit shows "You've hit your session limit" in stderr; exit code 1 on `-p` mode; `/usage` command shows rolling-window status; implement monitoring via `--max-budget-usd` with early failures at threshold.

---

## 1. Per-Launch Model & Effort Configuration

### Flags & Environment Variables

**Model selection** (highest priority first):
- `--model <alias|name>`: Session flag, overrides everything below
  - Aliases: `fable`, `opus`, `sonnet`, `haiku`, `best`, `opusplan` (Opus in plan mode, Sonnet in execute mode)
  - Full names: `claude-fable-5`, `claude-opus-5`, `claude-sonnet-5`, `claude-haiku-4-5`
  - Extended context: `fable[1m]`, `opus[1m]`, `sonnet[1m]` (1M token window)
  - Example: `claude --model opus`

- `ANTHROPIC_MODEL` environment variable
- `model` field in settings.json (user, project, or local)
- `ANTHROPIC_DEFAULT_MODEL` environment variable

**Effort level** (how much thinking/iterations Claude applies):
- `--effort <level>`: Session flag
  - Levels: `low`, `medium`, `high`, `xhigh`, `max`, `ultracode` (xhigh + dynamic workflows)
  - Available on all modern models (Fable 5.1, Opus 5, Sonnet 5)
  - Example: `claude --effort high`

- `CLAUDE_CODE_EFFORT_LEVEL` environment variable
- `effortLevel` in settings.json
- `/effort` command during session changes it mid-session (with cache implications—see section 6)

**Fallback models** (automatic switch when primary unavailable):
- `--fallback-model <comma-separated-list>`: Try each in order
  - Example: `claude --fallback-model sonnet,haiku`
  - Triggers on model overload, non-retryable errors, or safety classifier flags
  - Persists in settings.json: `"fallbackModel": ["claude-sonnet-5", "claude-haiku-4-5"]`

**Per-session without global changes:**
Yes. All three (model, effort, fallback) can be set per-launch with flags without modifying settings files. Each session is independent.

**Sources:**
- https://code.claude.com/docs/en/model-config.md — model aliases, context window details, effort levels
- https://code.claude.com/docs/en/cli-reference.md — flag documentation
- `claude --help` output (local: `--model`, `--effort`, `--fallback-model`)

---

## 2. Launch Styles & Trade-offs

### Style 1: Headless with `-p/--print`

**How it works:**
- `claude -p "your prompt"` starts a session, runs the prompt, prints response, and exits
- Non-interactive; no terminal UI
- Compatible with pipes, scripts, CI/CD

**Human attachment mid-run:** No. Session is fire-and-forget.

**Subscription auth:** Yes, required. Claude subscription account must be signed in (`claude auth login`).

**Permission handling:**
- Default: `--permission-prompts host` → permission prompts sent to a host handler (if running under Agent SDK) or denied automatically (if no TTY)
- `--permission-prompts none` → deny all unsafe actions (safe tools like Read, Bash(git status) auto-allow per settings)
- `--permission-mode <mode>` sets the default mode:
  - `auto` (classifier decides) — recommended for unattended
  - `dontAsk` (allow what settings permit, deny the rest)
  - `bypassPermissions` (skip all checks, requires explicit `--dangerously-skip-permissions` flag for safety)
- Subscription auth provides `/usage` visibility, plan-window enforcement

**Output capture:**
- `--output-format text` (default): Plain text response
- `--output-format json`: Single JSON result with:
  ```json
  {
    "type": "text|tool_use|...",
    "content": "...",
    "usage": {
      "input_tokens": 1000,
      "output_tokens": 500,
      "cache_read_input_tokens": 2000,
      "cache_creation_input_tokens": 100
    },
    "cost": 0.005,  // at list price
    "session_id": "abc-123-def",
    "model": "claude-sonnet-5"
  }
  ```
- `--output-format stream-json`: Line-delimited JSON events (one per turn/message chunk)

**Cost tracking in headless:** Yes. `usage` object in JSON includes input/output tokens and cache tokens. Cost is computed locally at list price unless `modelPricing` managed setting overrides it.

### Style 2: Background Sessions with `--bg/--background`

**How it works:**
- `claude --bg "your prompt"` spawns a session that runs in the background, prints its ID, and returns immediately
- Session persists on disk; can be reattached or resumed
- Managed via `claude agents` (lists), `claude attach <id>` (open in terminal), `claude logs <id>` (tail output), `claude stop <id>` (pause), `claude rm <id>` (delete)

**Human attachment mid-run:** Yes. `claude attach <id>` opens the background session in your terminal. You can type responses, approve permissions, etc.

**Subscription auth:** Yes, required.

**Permission handling:**
- Background sessions start in the permission mode you set: `--permission-mode auto` (default on Pro/Max subscription), `dontAsk`, `manual`, etc.
- While unattended, auto mode uses the classifier; if you attach with `claude attach`, you see prompts and can approve manually
- Run `claude agents --json` to see session IDs and permission modes programmatically

**Lifecycle:**
- Session state directory: `~/.claude/sessions/<id>/`
- Transcript: `~/.claude/sessions/<id>/transcript.jsonl` (JSONL format, one record per turn)
- Sessions survive shell restarts; `claude --resume <id>` continues them

### Style 3: Interactive Session (Normal)

**How it works:**
- `claude` or `claude "initial prompt"` starts an interactive session with a full UI, status line, and subcommand menu
- You type prompts, Claude responds, you approve/deny permissions, run commands like `/model`, `/effort`, `/usage`, etc.
- Runs in the foreground; Ctrl+C to stop (resumes later with `claude --resume` or `/continue`)

**Human attachment mid-run:** Always. It IS interactive—you're the one running it.

**Subscription auth:** Yes. Prompt won't start without an active Claude subscription session.

**Permission handling:**
- Default mode is `auto` on Pro/Max (classifier pre-approves safe actions, you see prompts for risky ones)
- Switch modes with Shift+Tab during the session
- `/permissions` to review and edit allow/deny rules

**In tmux/Herdr:**
- Start with: `tmux new-window -n "factory-story-1" "cd /path && claude --model fable --effort high 'here is the story prompt'"`
- The session inherits `cwd`, shell, and environment from the tmux pane
- Hooks can monitor session state via `SessionStart` event

---

## 3. Hooks for External Tracking

### Hook Events & Lifecycle

**Per-session hooks:**
- `SessionStart`: Fires when a session begins
  - JSON includes: `session_id`, `transcript_path`, `cwd`, `permission_mode`, `effort`, `started_at`, `start_type` (startup|resume|clear|compact|fork)
  - Example use: POST session ID to external tracker
  
- `SessionEnd`: Fires when session ends or `/clear` is run
  - Includes: `session_id`, `transcript_path`, `cwd`, final `usage` object with token counts and `duration_seconds`
  - Example use: Log final cost and session outcome to external database

**Per-turn hooks:**
- `UserPromptSubmit`: After user submits a prompt
- `Stop`: When Claude finishes responding
- `StopFailure`: When Claude's response fails (network error, etc.)

**Per-tool-call hooks:**
- `PreToolUse`: Before Claude uses a tool (Bash, Edit, Read, etc.)
  - Can intercept and deny with `permissionDecision: "deny"` in JSON response
  - Input: tool name, tool input (command, file path, etc.)
  
- `PostToolUse`: After tool completes
  - Can see tool output and decide to retry or escalate

**Notification hooks:**
- `Notification`: When Claude needs input or awaits permission
  - Matcher values: `permission_prompt`, `idle_prompt`, `auth_success`, `agent_needs_input`, etc.
  - Example use: Send Slack message when waiting for permission

### Hook Input/Output Schema

**Input (JSON on stdin):**
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "transcript_path": "/home/user/.claude/sessions/550e8400-e29b-41d4-a716-446655440000/transcript.jsonl",
  "cwd": "/path/to/project",
  "permission_mode": "auto",
  "hook_event_name": "SessionStart",
  "effort": { "level": "high" },
  "model": "claude-fable-5",
  "timestamp": "2025-09-18T14:32:00Z"
}
```

**Output (JSON to stdout, exit 0 for success):**
```json
{
  "hookSpecificOutput": {
    "permissionDecision": "allow|deny",
    "permissionDecisionReason": "...",
    "additionalContext": "...",
    "updatedInput": { },
    "retry": true
  },
  "systemMessage": "...",
  "terminalSequence": "..."
}
```

Exit codes:
- `0`: Success, output processed
- `2`: Blocking error (prevents action, scores result 0)
- Other: Non-blocking error (action proceeds)

### Scope & Configuration

**Per-project hooks:** `.claude/settings.json` in project root
```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/hook.sh"
          }
        ]
      }
    ]
  }
}
```

**Global hooks:** `~/.claude/settings.json` (applies to all projects)

**HTTP hooks:** Post to external endpoint instead of running shell command
```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "http",
            "url": "http://localhost:8080/session-started",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

**Async hooks:** Set `"async": true` to not block the session on hook completion (fire-and-forget).

**Sources:**
- https://code.claude.com/docs/en/hooks.md — full hook reference with all events and schemas
- https://code.claude.com/docs/en/hooks-guide.md — guide with examples

---

## 4. Reading Back Session Cost & Usage

### JSON Output Format (`--output-format json`)

**Single-turn result:**
```json
{
  "type": "text",
  "content": "Claude's response here...",
  "usage": {
    "input_tokens": 1234,
    "output_tokens": 567,
    "cache_read_input_tokens": 5000,
    "cache_creation_input_tokens": 100,
    "ephemeral_1h_input_tokens": 50,
    "ephemeral_5m_input_tokens": 0
  },
  "stopReason": "end_turn",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "model": "claude-fable-5",
  "cost": 0.00523,
  "num_turns": 1
}
```

**Fields:**
- `input_tokens`: New input tokens in this turn
- `output_tokens`: Output tokens generated
- `cache_read_input_tokens`: Tokens read from prompt cache (billed at ~10% of input rate)
- `cache_creation_input_tokens`: Tokens written to cache (billed at standard rate)
- `ephemeral_1h_input_tokens`: Cache writes using 1-hour TTL (higher write cost)
- `ephemeral_5m_input_tokens`: Cache writes using 5-minute TTL (lower write cost)
- `cost`: Estimated cost at list price (unless `modelPricing` managed setting overrides)
- `session_id`: Unique session identifier for tracking
- `num_turns`: Count of back-and-forth exchanges in this session

### Stream JSON Format (`--output-format stream-json`)

Line-delimited JSON events during execution:
```json
{"type": "content_block_delta", "content_block": {"type": "text", "text": "partial..."}}
{"type": "message_start", "message": {...}}
{"type": "message_delta", "delta": {"usage": {...}}}
```

Allows streaming response text in real-time while still tracking token counts.

### Transcript JSONL Format

Session transcript at `~/.claude/sessions/<id>/transcript.jsonl`:
- One JSON object per line
- Each record: `{"role": "user|assistant", "content": [...], "timestamp": "...", "usage": {...}}`
- Not directly structured—parse line-by-line

### In-Session Usage Command

`/usage` command during interactive session shows:
- Session block: total input, output, cache read/write tokens; cost at contracted rates if `modelPricing` set
- Plan usage breakdown (Pro/Max only): attribution by skill/MCP server, behavior flags (high cache misses, etc.)
- Usage credits spend (when enabled)

**Programmatically:** `/usage` output is human-readable; machine-parseable hook output comes from SessionEnd hook's JSON.

### Subscription Plan Metering

**Pro ($20):**
- 5-hour rolling-window limit (resets every 5 hours)
- Weekly cap
- Same models as Max (Fable 5.1, Opus 5, Sonnet 5 available)
- `/usage` shows rolling-window percentage

**Max ($200):**
- 20× Pro's tokens in the same 5-hour window + weekly cap
- Same models
- Same features
- `/usage` shows plan usage percentage

**Out-of-fuel detection:**
- Subscription plan: stderr message "You've hit your session limit"; exit code 1 on `-p` mode
- On Pro, also shows "5-hour session window" reset countdown
- On Pro/Max with usage credits enabled: may continue past plan limit until credit spend limit hit; `/usage-credits` manages credits
- API key (Console): per-workspace token limit; hit shows error; exit code varies

**Not meaningful on subscription:** Token counts are accurate, but cost at list price is not your actual bill—you're pre-paid per month.

**Sources:**
- https://code.claude.com/docs/en/costs.md — usage tracking, subscription limits, error messages
- https://code.claude.com/docs/en/feature-availability.md — Pro vs Max feature matrix

---

## 5. Priming a Session Cheaply

### What Costs Upfront (Paid Every Session)

All of these load at session start and enter context:

1. **System prompt** (`--system-prompt` or `--append-system-prompt[-file]`)
   - Adds to base system prompt at session start
   - Costs on every message (with prompt caching, usually cached after first turn)

2. **Project context** (CLAUDE.md)
   - Auto-discovered at session start from project root and user home
   - Must be present on disk when session starts
   - Edits to CLAUDE.md mid-session don't apply until `/clear`, `/compact`, or restart

3. **Settings** (`--settings` flag or settings.json files)
   - User, project, and local settings auto-loaded at startup
   - Tool definitions, hooks, MCP servers all loaded

4. **Additional directories** (`--add-dir`)
   - Added to tool access allowlist at startup
   - No token cost directly, but tools can read those files (costs when read)

5. **CLAUDE.md in nested directories**
   - Loaded lazily when Claude first reads a matching file (not upfront cost)

### What Loads on Demand (Lazy)

1. **Skills and commands** (`/skill-name`)
   - Loaded only when invoked
   - Enter context as user messages at invocation
   - No cost until first use

2. **Plugins** (via `/plugin` menu or plugin.json auto-load)
   - Commands/skills within plugins load on demand
   - MCP servers lazy-load unless marked `alwaysLoad`

3. **Subagents** (`--agent` or `@subagent-name`)
   - Each subagent is a separate session with its own context
   - Not included in parent's context until results are returned

4. **MCP tool definitions** (if tool search enabled)
   - Deferred by default: tool names only; full definitions loaded when tool used
   - Full definitions loaded upfront only when tool search unavailable (some cloud providers)

### Prompt Caching Across Sessions

**Not persistent:** Each session starts fresh. No cache sharing between sessions.

**Cache lifetime within a session:**
- **Subscription (Pro/Max, within plan usage):** 1-hour TTL—cache warm for 1 hour of inactivity
- **Usage credits, API key, cloud provider:** 5-minute TTL
- **Override with environment variable:** `CLAUDE_CODE_PROMPT_CACHE_TTL=1h` (requires v2.1.242+)

When you resume an old session after cache expires, the entire conversation history is reprocessed as uncached input (slow and expensive for large contexts).

**Pro/Max subscription optimization:** Offer "resume from summary" when resuming a large session after long idle (v2.1.242+)—summarizes history into a few key points, reducing context for resumed turns.

### Startup Token Estimates

Example minimal session (Fable 5.1):
- System prompt + tool definitions: ~1K tokens
- CLAUDE.md (100 lines): ~300 tokens
- First user prompt: ~500 tokens
- **Total first message:** ~1.8K input tokens (written to cache)

With prompt caching warm, subsequent messages reuse cached prefix, adding only new prompt and responses.

**Implication for factory:** Launch many fresh sessions? System prompt, CLAUDE.md, and tool definitions are the main upfront cost. Keep them under 500 lines combined to minimize per-launch overhead. Lazy-load specialized instructions into skills.

**Sources:**
- https://code.claude.com/docs/en/prompt-caching.md — cache mechanics, TTL, cost implications
- https://code.claude.com/docs/en/costs.md — reduce token usage (priming strategies)

---

## 6. Pro ($20) vs Max ($200) Subscription Plans

### Models Available

Both Pro and Max can run:
- **Fable 5.1** (latest fast model, good for most tasks)
- **Opus 5** (latest high-capability model, best for complex reasoning)
- **Sonnet 5** (latest balanced model)
- **Haiku 4.5** (fast, small model)

**No exclusive models:** All Anthropic models available on both plans.

### Usage Limits & Reset Windows

| Metric | Pro | Max |
|--------|-----|-----|
| 5-hour rolling window | ~X tokens | ~20X tokens |
| Weekly cap | ~Y tokens | ~20Y tokens |
| Reset schedule | Every 5 hours | Every 5 hours (same) |
| Capacity | Lower | 20× higher |

**Actual numbers not disclosed in docs.** Docs only say Max gives "approximately 20 times Pro's allowance."

### Features

| Feature | Pro | Max |
|---------|-----|-----|
| Claude Code CLI | ✓ | ✓ |
| Cloud sessions | ✓ | ✓ |
| Remote Control | ✓ | ✓ |
| Channels | ✓ | ✓ |
| Computer use | ✓ | ✓ |
| Artifacts | ✓ | ✓ |
| Extended thinking | ✓ | ✓ |
| MCP servers | ✓ | ✓ |
| Usage credits (overage) | ✓ (managed separately) | ✓ (managed separately) |
| Code Review | ✗ | ✗ (Team/Enterprise only) |
| Analytics dashboard | ✗ | ✗ (Team/Enterprise only) |

### How to Detect "Out of Fuel"

**Interactive mode:**
- Error message: "You've hit your session limit" or "You've hit your 5-hour session window" (with reset countdown)
- `/usage` command shows usage percentage; when at 100%, next action blocks with error

**Headless mode (`-p`):**
- stderr: "You've hit your session limit"
- Exit code: `1`
- No graceful continuation

**With usage credits (Pro/Max can enable):**
- Once plan limit hit, usage credits draw down per-session window and weekly cap
- Error changes to: "You've hit your monthly spend limit" or "You've hit your <limit-type> limit"
- `/usage-credits` manages credit balance and spend limits

**Programmatic detection:**
```bash
# In headless mode, capture exit code:
claude -p "task" --output-format json > result.json
if [ $? -eq 1 ]; then
  echo "Out of fuel (session limit)"
fi

# Check usage before launching:
# (No programmatic /usage in headless, but SessionEnd hook provides final usage)
```

### When Usage Resets

- **5-hour rolling window:** Resets automatically 5 hours after the earliest turn in the current batch
- **Weekly cap:** Resets at the same time each week (usually Sunday night UTC on subscription plans)
- **Usage credits:** Reset on the same schedule as plan limits

**No manual reset:** You can't clear or reset limits early. Waiting is the only option (or upgrade plan, or enable/request usage credits).

**Sources:**
- https://code.claude.com/docs/en/feature-availability.md — subscription plan comparison
- https://code.claude.com/docs/en/costs.md — plan limits, reset schedules, "out of fuel" errors

---

## 7. Context Window & Auto-Compaction

### Default Context Windows by Model

| Model | Default | 1M Variant | Availability |
|-------|---------|-----------|--------------|
| Fable 5.1 | 200K | Yes (`fable[1m]`) | Pro, Max, API |
| Opus 5 | 1M (native) | N/A | Pro, Max, API |
| Sonnet 5 | 1M (native) | N/A | Pro, Max, API |
| Haiku 4.5 | 200K | Yes (`haiku[1m]`) | Pro, Max, API |

**1M variant selection:**
- Use `--model fable[1m]` to launch with 1M context
- Or `/model fable[1m]` to switch mid-session (cache invalidation occurs)

**Disable 1M context** (force 200K):
- Set `CLAUDE_CODE_DISABLE_1M_CONTEXT=1`
- All models revert to 200K window

### Auto-Compaction Behavior

**Trigger:** Context approaches the auto-compact threshold (~967K tokens for modern models)

**What happens:**
1. Claude Code summarizes conversation history into a concise summary
2. Conversation is replaced with the summary + the most recent messages
3. System prompt and project context layers preserved
4. Next turn continues with much smaller context

**Is it automatic?** Yes, but:
- Fires only when approaching context limit
- You can run `/compact` manually at any break point (cheaper, since cache stays warm for summarization request)
- `/clear` starts fresh (no summarization; new session context)

**Cost of compaction:**
- Summarization itself costs tokens (reads full history, outputs summary)
- With cache warm: fractional cost (most of history cached)
- With cache cold (after long idle): full reprocessing cost
- After compaction, next turn rebuilds cache on summary only (fast)

**Factory implication:**
- If stories should END (not compact), use `--max-turns` or early exit logic
- If you want old context discarded, ensure sessions end rather than auto-compacting
- Compaction is a feature for long-running sessions, not for batch automation with independent stories

### Controls

**Via environment variable:**
- `CLAUDE_CODE_AUTO_COMPACT_WINDOW=500000` (set to token count, e.g., 100k, 500k, 1M)
- Or use `--autocompact 500k` flag at launch

**Via settings.json:**
```json
{
  "autoCompactWindow": "500k"
}
```

**Disable auto-compaction entirely:** not directly. Instead, keep sessions short with `--max-turns` or structure work so each story ends (new session).

**Sources:**
- https://code.claude.com/docs/en/model-config.md — context windows, 1M variants, auto-compact controls
- https://code.claude.com/docs/en/context-window.md — what loads into context, compaction behavior
- https://code.claude.com/docs/en/prompt-caching.md — compaction cost and cache invalidation

---

## Summary for Factory Implementation

1. **Per-story launch:** `claude --model fable --effort high -p "story prompt"` for Fable-5.1 at high effort, one story per session.

2. **Unattended execution:** Use `--bg` for background spawning, or `-p --output-format json` for headless + JSON cost parsing. Monitor via SessionStart/SessionEnd hooks posting to external tracker.

3. **Subscription required:** All features (model, effort, context) need Claude subscription auth (`claude auth login`). Pro supports enough stories per day; Max for heavier load.

4. **Permission handling:** `--permission-mode auto` (classifier approves safe actions) or `dontAsk` + `--allow-tools` in settings for unattended runs.

5. **Cost tracking:** Parse `--output-format json` output for `usage` and `cost` fields. Hook SessionEnd with JSON result for external logging. On Pro/Max, `/usage` shows rolling-window percentage in-session.

6. **Cheap priming:** Keep CLAUDE.md + system prompt under 500 lines. Lazy-load specialized instructions into skills. Each fresh session pays upfront for base context, then subsequent messages cache-hit (~90% of input after first turn).

7. **No context persistence:** Each story (session) starts fresh. No sharing of cache across stories. Within one session, prompt cache warm for ~1 hour (Pro/Max subscription). After that, resuming an old story reprocesses full history (slow).

8. **Auto-compact for long sessions:** If stories run past context limit, auto-compact summarizes history. For batch automation (each story should end), use `--max-turns` to force session exit before compaction.

9. **1M context available:** All models support it (Opus/Sonnet native, Fable/Haiku via `[1m]` variant). Default 200K on Fable should suffice for most stories; use 1M if transcript or codebase reads exceed 150K tokens.

10. **Out-of-fuel on Pro:** Hit 5-hour window limit; check `stderr` exit code 1 on `-p`, or `/usage` percentage in interactive. With usage credits, can continue past plan limit. Max plan has ~20× capacity.

---

## Document Metadata

- **Research date:** 2026-09-18
- **Claude Code version referenced:** v2.1.251+ (latest features from feature-availability.md and model-config.md)
- **Subscription plans:** Pro ($20/mo), Max ($200/mo)
- **Key sources:**
  - https://code.claude.com/docs/en/model-config.md
  - https://code.claude.com/docs/en/costs.md
  - https://code.claude.com/docs/en/hooks.md
  - https://code.claude.com/docs/en/prompt-caching.md
  - https://code.claude.com/docs/en/feature-availability.md
  - `claude --help` (local command reference)

