# bd's timeouts, and the one that cannot be set (bd 1.3.0)

One list of the timeouts a bd command meets on a host that talks to a served Dolt
(`~/.config/mw/beads.env`), so the next friction about a timeout starts here. Read from the
bd 1.3.0 source (`github.com/steveyegge/beads` at f45b249) and checked by running the binary
on 2026-11-07.

| timeout | default | how it is set | set here |
| --- | --- | --- | --- |
| pool read deadline (per I/O, every query) | 10 s | `BEADS_DOLT_POOL_READ_TIMEOUT`, `dolt.pool-read-timeout` | not set, and **ignored by the `bd` command** (below) |
| pool write deadline (per I/O) | 10 s | `BEADS_DOLT_POOL_WRITE_TIMEOUT`, `dolt.pool-write-timeout` | not set, and ignored the same way |
| pre-push fsck | 30 s | `BEADS_FSCK_TIMEOUT` | `15m` in `beads.env` (laptop, and the member template in `application/member.go`); `docs/home-move.md` step 6 relies on it |
| `dolt push`/`pull`/`fetch` transfer | 5 min | `BEADS_CLI_TRANSFER_TIMEOUT` | not set |
| auto-push | 30 s | `dolt.auto-push-timeout` | not set |
| Dolt server read / write (server side) | 8 h (28,800,000 ms) | `read_timeout_millis`, `write_timeout_millis` in `.beads/dolt/config.yaml` | commented defaults |
| Dolt server idle (`wait_timeout`) | 30 s | the server's | bd retires its idle connections at 20 s |
| mw units' `TimeoutStartSec` | dispatch 45 min, millhand-tick 45 min, postern-mirror 30 min, report 5 min, mail-notify 4 min, usage-read 20 min, doctor 2 min, unit-failed 2 min | the unit files | the `beads.conf` drop-ins only add `EnvironmentFile=beads.env` |

## Why `bd dep add` fails at high load, and why no beads.env line fixes it

The `bd` command opens its store in `cmd/bd/main.go` from a hand-built `dolt.Config` and
calls `dolt.New`. The two pool settings are read only in `applyResolvedConfig`
(`internal/storage/dolt/open.go`), which the library and doctor paths (`NewFromConfig*`)
call and `dolt.New` does not. So in the command, both variables and both config keys are
ignored and the pool's 10 s read deadline always applies: a query that takes longer than
10 s on a loaded host (`bd dep add`'s cycle check is the first) ends with
`i/o timeout`, reported as `failed to check for dependency cycle: invalid connection`, and
the server logs the client's connection going away.

Checked, with a delay proxy (every reply from a scratch Dolt held 12 s) in front of a
scratch bd database:

- default: `bd dep add` fails after 10.2 s with `read tcp ...: i/o timeout` / `invalid connection`;
- `BEADS_DOLT_POOL_READ_TIMEOUT=30s BEADS_DOLT_POOL_WRITE_TIMEOUT=30s`: the same, after 10.25 s;
- `bd config set dolt.pool-read-timeout 30s` (in config.yaml): the same, after 10.24 s.

bd 1.3.4 (the newest release) has the same code. The fix is upstream: `dolt.New` (or
`applyConfigDefaults`) must read the two settings. Until then, setting the variables in a
`beads.env` would only look like a cure, so none is set. A `bd dep add` that hits the
deadline is retried when the host is calmer.

## mw no longer runs `bd set-state`

`bd set-state` files a closed event bead under the story and checks the new edge for a
dependency cycle: the read that met the 10 s deadline above. Nothing in mw read those beads
(10,295 of them, 15.7% of a week's new beads), so since mw-t5shinh.12 `SetStoryState` runs one
`bd update` instead: it swaps the story's `<dimension>:*` label for the new value and appends
one line to the story's own notes (`--append-notes`). No event bead and no cycle check, so that
cycle check cannot time out a state change, and the label-alone fall-back (mw-gq6.1046) is gone
since nothing is skipped. The event beads already filed stay.

For `run`, `rebase` and `mergedtests` the old labels are removed blind (every value mw knows),
so nothing is read first; for `mainred`, whose value names a bead, the label is read first,
over the kept connection or by one `bd show`.

The line's shape is fixed, so a later count can read it as the event beads were read:

```
<UTC time, 2006-01-02T15:04:05Z> <dimension>=<value> <actor> <whole reason>
```

for example `2026-11-10T14:41:05Z run=landed mw@laptop landed at 3f2a9c1`. The actor is the
Gateway's (`mw@<host>`), `unknown` if it has none. The reason is kept whole and as it was
given, so a reason with a newline runs over more lines, none of which begins with a time.

## mw tester file writes its waits after the story, not with it

`bd create --deps` runs the same cycle check inside the create, and a create whose check times out
is undone: nothing is filed. Until mw-gq6.1467 `mw tester file` made its waits (on the open stories
naming the same file and the held findings of the landing) that way, so a busy server filed
nothing, 4 tries of 4 on mw-rl10piw.48. It now files the finding held with no waits, then adds each
with one `bd dep add`, trying a wait again after 3 s, 6 s and 12 s when the failure is a passing
fault (`i/o timeout`, `invalid connection`); a wait that still fails is named in the error, which
exits non-zero, and the finding stays filed and held. `mw file` still creates a plan's stories with
`--deps` (a plan's waits are on its own stories, filed a moment before).

## An unbounded `bd list --parent` of a big parent (mw-gq6.1504)

`bd list --parent <epic> --all --limit 0` of a parent with over a thousand children hung one
seat's bd for about 25 minutes on the Laptop (2026-11-18, pid 4091466, parent mw-5r3p30) and,
with it, every `mw bd` and Builder step close behind the host's bd turn: the same class as
mw-gq6.994, where `bd ready --parent` of that epic ran past the 10 s read deadline. `mw bd` now
refuses the call when the parent has more than 500 children (`application.BdBigParentChildren`,
under half of the 1070 of the evidence): it counts them with one `bd list --parent <id> --all
--limit 501 --json`, runs nothing else, and prints the bounded forms: `mw bd list --parent <id>
--all --limit 50`, or `mw brief <map>`. bd's own default limit is 50, so a list with no limit
named is already bounded and runs as before; so does a small `--limit` and a small parent. A
parent that cannot be counted (the server does not answer) is listed as before.
