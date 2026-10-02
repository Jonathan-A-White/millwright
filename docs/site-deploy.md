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

A command that is stopped at its limit is never quiet. The mail to the Mayor
opens with `after landing STOPPED at the limit: the site may be half-deployed`,
the comment on the story starts with it, and the report's one line says
`stopped after 20m0s, still running`.

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
