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

## The twelve steps

Each is printed as `==> [n/12] name: what`, checks before it acts, and says
`skip:` when the machine is already as the step wants it, so a second run
changes nothing. `--dry-run` prints `would:` where a run would act, and writes,
installs and starts nothing.

1. **packages**: `git tmux jq ripgrep python3 curl ca-certificates gh make xz-utils wireguard-tools` (apt, root).
2. **node**: Node 22+ from nodejs.org at `BOOST_NODE_VERSION`, its tarball checked against the published `SHASUMS256.txt`, unpacked under `~/.local/lib` and linked into `~/.local/bin`.
3. **git**: your identity; a `credential.https://github.com.helper` that reads `$GH_TOKEN` when git asks (so no token is in git's config); `~/.config/mw/github.env`.
4. **wireguard**: `/etc/wireguard/wg0.conf` (mode 600) with this host's address and the hub as the one peer, `wg-quick@wg0` enabled and up. An existing key in `wg0.conf` is kept.
5. **rig**: clones millwright if the script is not inside it, then runs `scripts/install.sh`, which installs Go and bd at the versions in `scripts/pins.env`, builds `mw` and links it into `~/.local/bin`. It is skipped when `install.sh --dry-run` has nothing to do.
6. **claude**: Claude Code through npm into `~/.local`; `~/.config/mw/claude.env` (`CLAUDE_CODE_OAUTH_TOKEN`).
7. **vault**: a plain `git clone` of the vault. Not `mw init --join`, which would also bootstrap a beads database of its own; a Boost keeps none.
8. **rigs**: every rig in `BOOST_RIGS`.
9. **config**: `~/.config/mw/config.toml` with `host`, `vault`, `cap`, `beads_sync = "auto"`, `beads_server_host` and the `[rigs]` table, only if there is none. An existing one is never edited; the lines it lacks are listed.
10. **beads**: `~/.config/mw/beads.env` (mode 600): `BEADS_DOLT_SERVER_HOST`, `_PORT`, and the user and password if given. This is what makes `bd` reach the home's server instead of making a second, empty database.
11. **profile**: a marked block in `~/.profile` that puts `~/.local/bin` on `PATH` and reads the three env files.
12. **dispatch**: `~/.config/mw/dispatch.env` (the `PATH` line, if absent; the host owns it), a systemd drop-in `mw-dispatch.service.d/boost-env.conf` naming the env files (no secret in it), `loginctl enable-linger`, then `scripts/install-units.sh --enable mw-dispatch`.

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
- It does not update anything already present: an existing checkout is not pulled, an existing `config.toml` and `dispatch.env` are not edited, an existing Node 22+ or `claude` is left alone.
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
