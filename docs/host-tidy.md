# mw host tidy: the caches of a host that only grow

A host's tools keep caches they never shrink fast enough for a factory. Go's
build cache keeps an entry for 5 days after its last use, and a host's Builders
write about 16 GB of it a day: the laptop's `~/.cache/go-build` reached 110 GB
(2026-11-19, mw-gq6.1537). `mw host tidy` deletes what such a cache can lose.
Its age is PROVISIONAL, for the Governor to confirm.

| Command | What it acts on | Bound | The act |
| --- | --- | --- | --- |
| `mw host tidy go-cache` | The files Go made in `GOCACHE` (else `~/.cache/go-build`): `<hex>-a` and `<hex>-d` in a two-hex-digit directory | Last used (modification time, which Go touches on each use) over 1 day ago | Delete the file; Go rebuilds what it needs |

It never touches Go's `README` and `trim.txt`, a file of another name, or
anything outside the cache. `--dry-run` says what it would delete and deletes
nothing.

## Running it

`mw-host-tidy.timer` runs `mw host tidy go-cache` daily at 05:10 UTC and 20
minutes after boot, on every host that enables it:

```
sh scripts/install-units.sh --enable mw-host-tidy
```

`mw doctor go-cache-size` names a host whose cache is over 30 GB (the check's
`Warn`), and its cure is the same tidy.

## Other caches that only grow (mw-gq6.1537's sweep)

| Cache | Tidy |
| --- | --- |
| `~/.cache/go-build` | `mw host tidy go-cache`, and the doctor's `tmp-leftovers` (`go clean -cache` over 2 GB when no go process is live: on a busy host it never runs) |
| `~/.cache/uv` | None |
| `/tmp` test leftovers | `tmp-leftovers` removes only dolt spool files, `/tmp/bd` and stale `claude-0` sessions; no tidy for a test's own leftovers |
| `~/.mw-worktrees` | `mw host sweep` removes the worktree of a story closed for over an hour; no age rule for a worktree of a story not closed |
