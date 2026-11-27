# Deploying a built site from an `[after_landing]` line

A rig whose product is a static site (postern's web app) is shipped by its
`[after_landing]` command. Two things go wrong with a plain `rsync` into the
live directory, and both did on 2026-10-02: the command was stopped at the
five-minute limit while `rsync` was copying, so `index.html` went up and the
script it names did not (the page was black, every asset URL fell back to
`index.html`), and `<dir>.prev` was a copy of that broken tree, so there was
nothing to roll back to. This page is the fix: a longer limit for that rig, and
a deploy that cannot leave the live tree half done.

## The limit, per rig

An after-landing command is stopped after five minutes unless the host's
`~/.config/mw/config.toml` says otherwise for that rig:

```toml
[after_landing_limit]
postern = "20m"     # a Go duration: 90s, 20m, 1h. Default for a rig not named: 5m
```

A value that is not a duration, or is not longer than zero, is refused by name
when `mw next` starts, instead of being read as the default. The limit applies to
that rig's `[after_landing]` line wherever mw runs it (a landing, the
self-update of the factory rig).

The limit is where a command is called slow: it is waited for up to twice the limit
(`application.AfterLandingStretch`), and one that ends of itself in that time is
reported with `it ran 7m0s, past its limit of 5m0s, and finished by itself`, not
stopped. A rig whose deploy is longer than the default 5 minutes names its own limit
here.

A command that is stopped at twice its limit is never quiet. The mail to the Mayor
opens with `after landing STOPPED at the limit: the site may be half-deployed`,
the comment on the story starts with it, and the report's one line says
`stopped after 40m0s, still running` (for a limit of 20m).
The line also says the stop is at twice the rig's limit and names the table:
`set [after_landing_limit] <rig> = "<time>" in the config file to give this rig longer`.

## `contrib/site-deploy <dist> <host> <dir>`

`<dist>` is the local build, `<host>` the ssh name of the machine that serves the
site (or `local` for a directory on this machine), `<dir>` the absolute path the
web server serves. It:

1. rsyncs `<dist>` to `<dir>.next` with `--link-dest=<dir>`, so an unchanged file
   is a hard link to the live one and only what changed travels; `<dir>` is not
   touched;
2. checks that `<dir>.next/index.html` exists and that every `/assets/...` file it
   names is in `<dir>.next`;
3. swaps on the host with renames, `<dir>` to `<dir>.prev`, then `<dir>.next` to
   `<dir>`, so the live tree is always one complete build and `<dir>.prev` is the
   last complete one. Rolling back is `mv <dir> <dir>.bad && mv <dir>.prev <dir>`.

A copy that fails or is killed, or a tree that fails the check, exits non-zero
before the swap and leaves `<dir>` and `<dir>.prev` as they were; the next run
clears a stale `<dir>.next`. It needs `rsync` on both ends and, for a host other
than `local`, ssh that works without a prompt. `<dir>` and `<host>` may hold only
plain characters, as they are put into commands run over ssh.

### The postern line

The command runs in the rig's checkout, so name the script by its path. On the
Laptop, with `vps` the ssh name of the server (the one the old line used):

```toml
[after_landing]
postern = "npm ci && npm run build && ~/millwright/contrib/site-deploy dist vps /var/www/postern"

[after_landing_limit]
postern = "20m"
```

This replaces the old `ssh vps 'rsync -a --delete /var/www/postern/ /var/www/postern.prev/'
&& rsync -az --delete dist/ vps:/var/www/postern/` pair: the swap makes the
`.prev` copy itself. The line is changed by the Mayor or the Millhand on the
host after this lands; a story does not edit host config.
`contrib/sitedeploy_test.go` holds the cases: a good copy, a copy killed midway,
a page naming a missing asset, a first deploy, and a remote host through ssh.

## Two hosts, two lines: `after-landing-agrees`

An `[after_landing]` line is per-host config, so a host that lands a rig runs
*its* line, not the Laptop's. On 2026-11-27 the Laptop's lampas line gained
`npm run lxx:ship -- --out dist/data-lxx` and the desktop's never did; the
desktop then landed a batch of lampas, deployed it without that step and the
live site lost its Septuagint. Nothing had said the two lines had parted.

`mw doctor after-landing-agrees` says it. On every sync a host leaves a note,
`host.<name>.after_landing`, holding one `<rig> <sha256>` line per rig it has an
`[after_landing]` line for: the fingerprint of the line, never the line (a line
may name a host or a path). The check compares this host's fingerprints with each
other Builder host's, rig by rig. It is **faulty** when they differ for a rig,
and its text names the rig and both hosts:

```
lampas: the [after_landing] line differs between desktop (this host) and laptop; see each host's line with `grep -A30 '^\[after_landing\]' ~/.config/mw/config.toml` and make them equal (docs/site-deploy.md)
```

A host with no line for a rig is not compared for it, and a host that has left no
note yet is not compared at all. The check only reports; it changes nothing.

**When it fires:**

1. On each host named, run the `grep` the text gives and compare the rig's two
   lines.
2. Decide which is right (usually the one that gained a step) and put that line
   in the other host's `~/.config/mw/config.toml`, under `[after_landing]`. The
   Mayor or the Millhand does this on the host; a story does not edit host
   config.
3. The next `mw sync` on that host leaves the new fingerprint, and the next
   `mw doctor` reads ok. There is no switch to turn the check off for one rig:
   if two hosts' lines are meant to differ, say so to the Mayor rather than
   leave the alarm up.

The factory rig's `millwright` line counts too: a host with no `millwright` key
runs `make build` (`config.DefaultMillwrightAfterLanding`), and that is the line
fingerprinted.
