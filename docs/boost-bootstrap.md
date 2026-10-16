# Boost bootstrap: a fresh machine becomes a Boost

`contrib/boost-bootstrap.sh` turns a fresh Ubuntu 24.04 machine into a **Boost**
(`CONTEXT.md`: the host that is not home; it builds, dispatching against the
home's beads server, whenever it is on). The desktop was set up by hand over
many sittings (`hosts/desktop.md` in the vault); a cloud box or the 2020 Lenovo
needs the same in one command.

```sh
cp contrib/boost-bootstrap.env.example ~/boost.env && chmod 600 ~/boost.env   # fill in the blanks
contrib/boost-bootstrap.sh --dry-run --env-file ~/boost.env   # every step, none run
contrib/boost-bootstrap.sh           --env-file ~/boost.env   # the real run
rm ~/boost.env
```

Run it as root (a cloud box usually is) or as a user with passwordless `sudo`;
without either it stops at the first step that needs root and says so. Run from
a checkout of the rig it uses that checkout; run as a loose file it clones the
rig to `$MW_HOME` (default `~/millwright`). It runs as the user the Boost will
dispatch as, because the timer, the env files and the tokens are that user's.

## Inputs

Environment variables, or a small env file of `NAME=VALUE` lines
(`--env-file FILE` or `BOOST_ENV_FILE`). The file is read as data, never run, and
is refused unless group and others cannot read it (`chmod 600`). A variable in
the environment wins over the same name in the file.
`contrib/boost-bootstrap.env.example` lists every name.

| Name | | What it is |
| --- | --- | --- |
| `BOOST_HOST` | required | this host's name, the `host =` line of its `config.toml` |
| `BOOST_WG_ADDRESS` | required | its WireGuard address, `10.88.0.x` (no mask) |
| `BOOST_WG_HUB_PUBKEY` | required | the hub's public key |
| `BOOST_BEADS_HOST` | required | the home's address on WireGuard, where its Dolt server listens |
| `BOOST_WG_PRIVATE_KEY` | | else made here by `wg genkey` and kept in `wg0.conf`; the public key is printed for `wg-enrol` |
| `BOOST_WG_ENDPOINT` | | the hub's public `host:port` (default: `wg-enrol`'s) |
| `BOOST_BEADS_PORT`, `BOOST_BEADS_USER`, `BOOST_BEADS_PASSWORD` | | the Dolt server's port (3307), user and password |
| `BOOST_RIGS` | | `"name=git-url name=git-url"`: the rigs to clone, to `BOOST_RIGS_DIR/name` |
| `BOOST_RIGS_DIR`, `BOOST_VAULT`, `BOOST_VAULT_REPO` | | where the rigs and the vault go, and the vault's url |
| `BOOST_CAP` | | sessions at once in `config.toml` (default 2) |
| `BOOST_GITHUB_TOKEN` | | a token that can read the vault and the rigs |
| `BOOST_CLAUDE_TOKEN` | | a Claude Code token (`claude setup-token`) |
| `BOOST_GIT_NAME`, `BOOST_GIT_EMAIL` | | the git identity |
| `BOOST_NODE_VERSION` | | the Node to install when Node 22+ is absent (default 24.21.0) |
| `MW_HOME` | | where the rig is cloned when the script is not inside it |

Tokens are passed in; the script never fetches, mints or logs in for one.

## The thirteen steps

Each is printed as `==> [n/13] name: what`, checks before it acts, and says
`skip:` when the machine is already as the step wants it, so a second run
changes nothing. `--dry-run` prints `would:` where a run would act, and writes,
installs and starts nothing.

1. **packages**: `git tmux jq ripgrep python3 curl ca-certificates gh make xz-utils wireguard-tools` (apt, root).
2. **node**: Node 22+ from nodejs.org at `BOOST_NODE_VERSION`, its tarball checked against the published `SHASUMS256.txt`, unpacked under `~/.local/lib` and linked into `~/.local/bin`.
3. **git**: your identity; a `credential.https://github.com.helper` that reads `$GH_TOKEN` when git asks (so no token is in git's config); `~/.config/mw/github.env`.
4. **wireguard**: `/etc/wireguard/wg0.conf` (mode 600) with this host's address and the hub as the one peer, `wg-quick@wg0` enabled and up. An existing key in `wg0.conf` is kept.
5. **rig**: clones millwright if the script is not inside it, then runs `scripts/install.sh`, which installs Go and bd at the versions in `scripts/pins.env`, builds `mw` and links it into `~/.local/bin`. It is skipped when `install.sh --dry-run` has nothing to do.
6. **claude**: Claude Code through npm into `~/.local`; `~/.config/mw/claude.env` (`CLAUDE_CODE_OAUTH_TOKEN`).
7. **vault**: a plain `git clone` of the vault. Not `mw init --join`, which would also bootstrap a beads database of its own; a Boost keeps none. Its `.beads` is then set to mode 700: a clone makes it at the umask's mode, and bd warns `recommended: 0700` on every call at any other (mw-gq6.451).
8. **rigs**: every rig in `BOOST_RIGS`.

   A checkout already there (5, 7 and 8; a box made from a snapshot has them all) is checked: for object files that are empty, then `git status` and `git fsck --connectivity-only`. A broken one is repaired: its empty objects removed, `git fetch`, `git reset --hard origin/<its branch>`. When that does not mend it, it is moved aside to `<dir>.broken-<time>` and cloned again, with its old `.git/config` copied in. The log says `repaired ...` or `re-cloned ...`. A snapshot of cloud1 once carried a vault whose HEAD was a zero-byte object, and every dispatch on the boxes made from it failed before claiming (mw-5gr3k0.8). The checkout the script is run from is only checked; what is wrong with it is a note.
9. **config**: `~/.config/mw/config.toml` with `host`, `vault`, `cap`, `beads_sync = "auto"`, `beads_server_host`, the `[rigs]` table, and a `[tests]` line for each rig it cloned (and a `[changed_specs]` line where there is one), only if there is no config. In an existing one only the `host` line is ever edited: it is set to `BOOST_HOST` when it names another host, as a config a snapshot carries does, and every other line is kept. The other lines it lacks are listed. The `[tests]` and `[changed_specs]` lines are copied, as written, from the vault's `hosts/boost-config.toml` (below), never typed per box. A rig that file does not name is said, not guessed: without its `[tests]` line `mw next` gates the rig with `make test`, which a rig with no Makefile refuses (mw-gq6.453). `mw doctor rig-tests` names such a rig on a Boost later.
10. **browsers**: for each cloned rig whose `package.json` depends on `@playwright/test` (lampas's `[changed_specs]` and postern's `gate:shots` drive a browser), `npx playwright@<the version its package-lock.json holds> install --with-deps chromium`, run in the rig, so the browser is the revision that rig's own Playwright wants. It needs root for the system libraries. Skipped when that version's chromium was installed before.
11. **beads**: `~/.config/mw/beads.env` (mode 600): `BEADS_DOLT_SERVER_HOST`, `_PORT`, and the user and password if given. This is what makes `bd` reach the home's server instead of making a second, empty database.
12. **profile**: a marked block in `~/.profile` that puts `~/.local/bin` on `PATH` and reads the three env files.
13. **dispatch**: `~/.config/mw/dispatch.env` (the `PATH` line, if absent; the host owns it), a systemd drop-in `mw-dispatch.service.d/boost-env.conf` naming the env files (no secret in it), a timer drop-in `mw-dispatch.timer.d/boost-pace.conf` (`OnCalendar=` then `OnCalendar=*:0/5`), `loginctl enable-linger`, then `scripts/install-units.sh --enable mw-dispatch`, and, when the step changed anything, one `systemctl --user start --no-block mw-dispatch.service`. The timer's own pace is hourly, the home's fallback for an event its follower missed; a Boost has no follower, so without the timer drop-in a box just made waited up to an hour for its first dispatch (mw-5gr3k0.7).

### hosts/boost-config.toml

The one file in the vault that says what a Boost's `[tests]` and `[changed_specs]` hold: the same lines as the home's `config.toml`, one per rig, each on one line.

```toml
[tests]
lampas = "npm ci --no-audit --no-fund && TZ=UTC npm test && npm run typecheck && npm run lint && npm run build"
postern = "npm ci --no-audit --no-fund && TZ=UTC npm test && ... && npm run gate:shots"

[changed_specs]
lampas = "npm run build && TZ=UTC npx playwright test --project=shots {specs}"
```

It is written by `mw config export-boost`, run on the home: it rewrites the file from the
home's `[tests]` and `[changed_specs]` (nothing else of the config), names each line it
brings level, and commits the file in the vault (mw-gq6.456). When a rig's line changes in
the home's `config.toml`, run it again, or the next Boost gets the old line; `mw doctor
boost-config`, on the home, says when the two differ, naming each rig. Only the rigs in
`BOOST_RIGS` are copied, in the order of `[rigs]`.

The home's other per-rig tables are not copied, on purpose (a Boost's `mw` still follows
main: a host with the millwright checkout and no `millwright` key in `[after_landing]` runs
`make build` for it, README "How each host's mw is rebuilt"): `[after_landing]` and
`[after_landing_limit]` run on the host whose checkout a landing moved (a deploy from the
home), `[heavy_net]` only matters on a metered network, which a Boost is not, and `[tester]`
springs the Tester's stories from the home's dispatch.

## Where the secrets go

Only to files of mode 600, under `~/.config/mw` (mode 700 when the script makes
it) and `/etc/wireguard/wg0.conf`: `github.env`, `claude.env`, `beads.env`,
`wg0.conf`. A file is written through a temp file made mode 600, then moved
into place. No secret is put in a command line, in git's config, in the unit
drop-in, in `config.toml` or in anything the script prints, and nothing is
written into the rig or the vault. `scripts/check-boost-bootstrap.sh` holds all
of that, with fake tokens, in `make lint`.

## What it does NOT do

- It never holds the **age key** or a **BSV key**: those stay with the Governor and are put in place by hand.
- It never logs in to anything, never fetches a token, and never reads a secret from anywhere but what it is given.
- It does not enrol the host on the WireGuard hub: it prints the `wg-enrol NAME PUBKEY` line to run there. The tunnel is up but silent until the hub has the key.
- It does not create the Dolt user for this host on the home, or set `beads_sync` on the home; `mw doctor beads-server` says whether `bd` reaches the database.
- It does not update anything already present: an existing checkout is not pulled (one that is broken is repaired, above), an existing `config.toml` is not edited but for its `host` line, an existing `dispatch.env` is not edited, an existing Node 22+ or `claude` is left alone.
- It does not change the home, arm any timer but `mw-dispatch`, or point any story at this host: that is `host = <name>` on the story's path.
- It does not run on anything but Ubuntu (it refuses another OS with nothing changed) and has not been run on a real fresh machine yet; its test uses stand-in commands and a fake root.

## After it

Open a new login shell, give the hub the public key it printed, and check:

```sh
mw doctor beads-server
mw status
```

`bd` reads the Dolt server's address from `beads.env`; a `bd` run before step 10
or without it opens a second database under `.beads/embeddeddolt`, which `mw
doctor` flags.
