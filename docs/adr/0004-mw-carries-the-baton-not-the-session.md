# mw carries the baton, not the session

When a story's session ends, a Claude Code session-end hook runs `mw next`, which closes out the story (acceptance check, ledger line, fuel from the session's result), asks beads what is now ready for this host, and launches fresh sessions for it up to the concurrency cap. The Governor's earlier factory had each temporary worker look up the next story, spawn its successor and then close itself; we keep the self-propelling, daemon-free feel of that relay but move the baton out of the model, because a worker that dies or runs dry before spawning its successor stalls the chain silently, because deciding "what is next" is a free graph query that should not cost tokens, and because a relay is inherently serial while `mw` sees the whole frontier and so gets parallel and hybrid modes from the dependency graph alone.

## Consequences

A session that ends without firing its hook leaves a stale claim; `mw sweep` (a zero-token query, run by hand or on a timer) finds it. Sessions never need to know what comes after them. A story cannot be dispatched unless its path names a rig and a target branch.
