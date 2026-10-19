# A member's own factory

A **member** is another person who shares the licence holder's machine and plan but
keeps their own factory: their own Linux user, vault, beads database, Dolt server and
Mayor, and their own Claude login. The holder (the Governor, for the first member) keeps
tabs: every epic the member's Mayor files is **released only on the holder's approval**,
a decision card the holder taps from their own account, and the holder's key has the
cockpit role on the member's backend. Nothing of the holder's own factory changes.

This page is the whole setup of one member, in order. A step marked **BY HAND** is one
`mw` cannot do (it needs root, a login, money or a remote); the others are done by
`mw member init`, run as the member's user. The names below are the first member's:
`luke`, a vault at `~/luke-vault`, Dolt on port 3308, a backend at
`http://10.88.0.2:8788`, the host `laptop`.

Words with a fixed meaning (Seat, Session, Story, Path, Formula, Rig, Host, Fuel,
Governor, Mayor, Builder) are defined in [`CONTEXT.md`](../CONTEXT.md). The starter kit
the member's seat files come from is `template/` ([setting-up-a-vault.md](setting-up-a-vault.md)
is its guide).

## 1. The Linux user — BY HAND

As root on the machine:

```sh
sudo adduser luke
```

## 2. Keep the user's units running — BY HAND

The Dolt server below is a systemd **user** unit. Without linger, the user manager, and so
Dolt, stops when the member logs out. As root:

```sh
sudo loginctl enable-linger luke
```

`mw member init` checks it (`loginctl show-user luke -p Linger`) and names this step at the
end of its report when it is not set.

## 3. The tools — BY HAND

As `luke`, install what [setting-up-a-vault.md](setting-up-a-vault.md) lists under
*Prerequisites*: `git`, `tmux`, `dolt` and `bd` at the pinned versions, and the `mw` binary on
the member's `PATH` (build it with `make build`, or copy the holder's `bin/mw`). `dolt` must
be on the `PATH`, or named with `--dolt`.

## 4. Claude, on the member's own login — BY HAND

As `luke`, install Claude Code and log in with the member's own plan:

```sh
claude
```

The member's Mayor runs on this login. The holder's login is never copied here, and the member's
never goes to the holder's user.

## 5. Make the factory

As `luke` (not as the holder, and not with `sudo`):

```sh
mw member init --name luke --vault ~/luke-vault --dolt-port 3308 \
    --holder "Jonathan (the Governor)" --backend http://10.88.0.2:8788
```

`mw member init --help` documents every flag. It checks everything it can before it writes
anything (a free port, an empty vault directory, a dolt on the `PATH`), and it writes:

| What | Where |
|-|-|
| the config file: `vault`, `host` (default `laptop`), `cap = 1`, `postern_backend` | `~/.config/mw/config.toml` |
| bd's connection to the member's Dolt, with a generated password (mode `0600`) | `~/.config/mw/beads.env` |
| a Dolt user unit on the chosen port, bound to `127.0.0.1`, enabled and started | `~/.config/systemd/user/dolt-beads.service` |
| the Dolt data directory the unit serves | `~/.local/share/mw/dolt-beads` |
| the vault: a git repository, one commit, laid from the starter kit (`seats/`, `CLAUDE.md`), its beads database made on that server | `~/luke-vault` |

The Dolt data lives outside the vault on purpose: a `.beads/dolt` directory that `bd` did not
make itself is taken for a legacy workspace and refused. The beads database is made with
`bd init` against the server that is already running (`--server --external`), never `bd migrate`:
`bd bootstrap` only picks up a database that exists somewhere, and a new vault has none.

The Mayor's charter, `seats/mayor/charter.md`, gains a `## Family` section after *Who you
serve*: it names the licence holder, says that every epic is released only on the holder's
approval (a decision card they tap from their own account), and that the holder's key has the
cockpit role here. The member may tweak the rest of the seat files; the Family section is the
holder's line, and only the holder approves a change to a charter.

A second run is refused (`already a member factory at <file>`) and touches nothing.

## 6. Let the member's shell find bd — BY HAND

`mw member init` writes `beads.env` but not the member's shell profile. As `luke`, add to
`~/.profile`:

```sh
set -a; . ~/.config/mw/beads.env; set +a
```

and log in again. Check it from the vault:

```sh
cd ~/luke-vault && bd list
```

## 7. A private remote for the vault — BY HAND

Make a private repository for the vault on a git host the member can push to, then:

```sh
git -C ~/luke-vault remote add origin <url>
git -C ~/luke-vault push -u origin main
```

## 8. The member's backend, and the holder's cockpit key — the rest of the family plan

Not made by `mw member init`; each is a hand step or a story of its own: the member's Postern
backend unit (the `http://10.88.0.2:8788` the config file names), the nginx route and
certificate for the member's app, the holder's key with the cockpit role on that backend (the
Family section already tells the member's Mayor to expect it), and the holder's dispatcher
serving the member's vault so the Builders run on the holder's machinery and plan.

## The way back

```sh
mw member remove
```

run as `luke` stops and disables `dolt-beads.service` and **deletes nothing**: the config file,
`beads.env`, the Dolt data and the vault stay where they are, to be removed by hand if they are
not wanted. To make the factory again, remove `~/.config/mw/config.toml`,
`~/.config/mw/beads.env`, `~/.config/systemd/user/dolt-beads.service` and the vault first.

## Limits

- Dolt's `root` account has no password, as on the holder's own server, so any user on the
  machine can reach the member's database over `127.0.0.1:3308`. The `beads` user's password
  keeps bd honest; it does not keep another local user out.
- Pick a port that no other user's Dolt uses (the holder's is 3307). `mw member init` refuses a
  port that already answers.

See `features/member.feature`.
