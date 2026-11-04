# mw grist tidy: what the mill keeps, and for how long

The mill keeps a record of every grind under `grist_state_dir` (default
`~/.local/state/mw/grist`). The photos and recordings in it are the apps'
users' own, and they grew by about 28 MB a day. `mw grist tidy` deletes them
on the Governor's retention (mw-gq6.692, "Do A"): **30 days**. The age is the
constant `GristKeepAge` in `application/gristtidy.go`; changing it is a change
to that file and to this page together.

| # | What it acts on | Bound | The act |
| --- | --- | --- | --- |
| 1 | The `attachment-N.<ext>` files of a run in `runs/<txid>/` | The run's `received` time in `timing.json` is 30 days ago or more | Delete those files |
| 2 | A grist's directory `forwarded/<txid>/` (the pictures of a forward) | 30 days or more since the forward: the newest time of the directory and its pictures | Delete the directory, whole |

Row 1 keeps the run's small record: `input.json` (the request), `scorers.json`,
`answer.json` (the answer text), `timing.json` (the timings and ids) and
`sender.json` (the key a forward's status goes back to). `mw grist runs`,
`mw grist stats`, `mw grist timing` and the room check read those and are
not changed by the sweep. A run whose attachments are already gone is not
listed again. A directory of `runs/` with no `timing.json` is not a run and is
left alone.

Nothing younger than 30 days is touched: a run exactly 30 days old goes, one a
second younger stays.

The sweep prints what it deleted (each run or forward, its files and size) and
the total, and writes one line for each to the Millhand's tick log
(`tidy: grist: ...`, under mw tidy's prefix so the tick's counts read past
it). `mw grist tidy --dry-run` lists what it would delete and deletes nothing.
The Millhand's tick runs it after `mw tidy` on a host with a `[grist]` table
(the host that runs the mill) and says `grist tidy deleted N items (size)` in
its line.

It never deletes anything outside `runs/<txid>/attachment-*` and
`forwarded/<txid>/`, and refuses a name that is not one path segment.
