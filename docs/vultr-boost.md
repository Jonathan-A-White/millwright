# Vultr Boost: one cloud box, made and unmade with one command

`contrib/vultr-boost` makes a **Boost** (`CONTEXT.md`: the host that is not home)
on Vultr when there is work for one, and destroys it when there is not. The box
is one High Performance 8 vCPU / 16 GB machine in the hub VPS's region. Terraform
(`contrib/terraform/vultr-boost`) creates it; cloud-init runs
[`contrib/boost-bootstrap.sh`](boost-bootstrap.md) on its first boot; the script
enrols it as a WireGuard peer of the hub over ssh. Run it on the home, which holds
the age key that opens the vault's secrets ([secrets.md](secrets.md)).

```sh
contrib/vultr-boost up --dry-run   # every step and what it would run; nothing run
contrib/vultr-boost up             # terraform shows its plan and asks before it spends
contrib/vultr-boost status         # the box's state and what it costs
contrib/vultr-boost down           # destroy the box, take its peer off the hub
contrib/vultr-boost list           # every instance Vultr holds: id, label, status, server status
contrib/vultr-boost adopt          # --instance ID: import a restoring instance into the box's state
contrib/vultr-boost snapshot       # a ready-made image of a bootstrapped box
contrib/vultr-boost spend          # what Vultr's billing says the boxes cost this month
contrib/vultr-boost set-cap        # --name NAME --cap N: set the sessions a box that is up runs at once
contrib/vultr-boost address        # --name NAME: the box's WireGuard address, then its user, off the hub
```

`--name NAME` picks the box (default the tfvars `name`, else `cloud1`). The
tfvars name keeps the files below; any other box keeps its own beside them,
`hosts/vultr/NAME.tfstate.age`, `NAME.wg.key.age` and `NAME.wg.pub`, so two boxes
never share a state or a key. `up --snapshot ID` makes the box from a snapshot.

`down --instance ID` also deletes that Vultr instance by its id: it is
for an *orphan*, a box whose terraform state was lost, which `terraform destroy` cannot
find (a 404 is done). Only a 204 or a 404 is a deletion; anything else (a 409 for an instance Vultr has
locked, a 200) is a refusal and the command fails. `list` prints each instance of the
account, `id<TAB>label<TAB>status<TAB>server status`.

`--yes` on `up` or `down` passes `-auto-approve` to terraform, which otherwise
asks. `--dry-run` prints the numbered steps and changes nothing.

<!-- toc -->

- [What it costs](#what-it-costs)
- [Before the first `up`](#before-the-first-up)
- [What `up` does](#what-up-does)
- [What `down` does](#what-down-does)
- [Where each secret is](#where-each-secret-is)
- [Revoking the box's tokens](#revoking-the-boxs-tokens)
- [Elastic: boxes made and destroyed by demand](#elastic-boxes-made-and-destroyed-by-demand)
  - [A changed box_cap reaches the boxes already up](#a-changed-box_cap-reaches-the-boxes-already-up)
  - [Stories pathed to a box that goes down](#stories-pathed-to-a-box-that-goes-down)
  - [What Vultr can refuse](#what-vultr-can-refuse)
  - [The ready-made image](#the-ready-made-image)
- [What it does not do](#what-it-does-not-do)

<!-- /toc -->

## What it costs

High Performance 8 vCPU / 16 GB (plan `vhp-8c-16gb-amd`) is about **$96 a month,
$0.132 an hour**, billed for as long as the box *exists*: a stopped box bills too,
so the way to stop paying is `down`, not a shutdown. A box up twelve hours a day
is about half of that. `status` prints the monthly and hourly price from Vultr's
public price list, and what the box has cost since it was made. `up` prints the
price before terraform asks to apply.

## Before the first `up`

1. **Tokens**, in `mw secrets` (each put on the home, from a file, never an argument):
   `vultr_api_token` (required: the Vultr account's API key), and, for a box
   that can do work, `boost_github_token` (reads the vault and the rigs),
   `boost_claude_token` (`claude setup-token`) and, if the beads server has one,
   `boost_beads_password`. A kept token that is not asked for is not read.
2. **Settings**, a tfvars file in the vault at `hosts/vultr/vultr-boost.tfvars`,
   one `name = "value"` line each (the file is data to the wrapper: keep it to
   plain quoted strings). It holds no secret.

   | name | | what |
   | --- | --- | --- |
   | `region` | required | the Vultr region id of the hub VPS (`curl https://api.vultr.com/v2/regions` lists them) |
   | `wg_endpoint` | required | the hub's public `host:port`; its host is also where the wrapper ssh-es as root |
   | `beads_host` | required | the home's WireGuard address, where its beads server listens |
   | `name` | | the box's host name and peer name (default `cloud1`) |
   | `plan` | | the Vultr plan id (default `vhp-8c-16gb-amd`) |
   | `user` | | the user the Boost dispatches as (default `mw`) |
   | `beads_port`, `beads_user` | | the beads server's port (3307) and user |
   | `rigs`, `vault_repo`, `cap` | | passed to the bootstrap: `"name=url name=url"`, the vault's url, sessions at once |
   | `git_name`, `git_email` | | the git identity |
   | `ssh_authorized_key` | | a public key that may ssh in as the box's user |
   | `ssh_allowed_cidr` | | who may reach ssh on the box's public address (default anywhere) |

   The box's WireGuard address and the hub's key are not settings: the hub's
   `wg-enrol` assigns the address and prints its own key, and the wrapper passes
   both on.
3. **The hub** must have `wg-enrol` on root's PATH (or set `VULTR_BOOST_HUB_ENROL`)
   and accept the home's ssh key for root (or set `VULTR_BOOST_HUB_SSH`).
4. **Tools** on the home: `terraform`, `ssh`, `age` and `age-keygen`
   (`contrib/install-sops-age.sh`), `wg` (wireguard-tools), `jq`, `curl`, `git`.

## What `up` does

1. Reads the settings and the tokens (`mw secrets get`).
2. Opens the vault's encrypted terraform state into a private directory under
   `$XDG_RUNTIME_DIR` (else `/dev/shm`): memory, not disk. `VULTR_BOOST_RUN` names
   another tmpfs directory.
3. Makes the box's WireGuard key, once: `wg genkey`, kept encrypted in
   `hosts/vultr/wg.key.age`, its public half in `hosts/vultr/wg.pub`.
4. Over ssh, on the hub: copies `/etc/wireguard/wg0.conf` to a dated
   `wg0.conf.bak-…`, then runs `wg-enrol NAME PUBKEY`. It prints the backup's path
   and **the way back**: `ssh … 'wg-enrol --remove NAME'`, or put the backup back
   and `wg syncconf`. A peer already enrolled is left alone and no backup is made.
5. `terraform init`, then `apply`, with the tokens in `TF_VAR_*` environment
   variables only. Cloud-init makes the user, writes the bootstrap script and its
   inputs (the inputs file is mode 600 and removed when the bootstrap succeeds)
   and runs it. The bootstrap takes some minutes; its log is `~/boost-bootstrap.log`
   on the box, and the box shows in `mw` once its dispatch timer ticks.
6. Encrypts the state back into the vault, `hosts/vultr/terraform.tfstate.age`,
   commits `hosts/vultr` there, and removes the private directory.

A second `up` changes nothing it need not: the key is kept, the peer is already
enrolled, terraform finds the box and does nothing.

## What `down` does

`terraform destroy` (asking, unless `--yes`), then on the hub: a dated backup of
`wg0.conf`, `wg-enrol --remove NAME`, and the way back printed. The peer's
address, read off the hub before it goes, is then dropped from this host's
`~/.ssh/known_hosts` (`ssh-keygen -R`): the next box given that address has a
new host key, and ssh would refuse it as REMOTE HOST IDENTIFICATION HAS CHANGED.
The WireGuard key
stays in the vault for the next `up`. If the destroy fails the peer is left
alone and the state is kept, so `down` can be run again. If only the peer removal
fails, it says so and prints the command to run on the hub.

## Where each secret is

- **The Vultr token and the others** are in `secrets.enc.yaml` (sops + age). The
  wrapper reads them with `mw secrets get` into shell variables, hands them to
  terraform in its environment and to `curl` as a config on stdin. None is in a
  command line, a printed line or a file.
- **The state** holds the rendered cloud-init (so it holds the tokens). It is in the
  vault only as `terraform.tfstate.age`, age-encrypted to the home's key, and in
  the clear only in the private tmpfs directory for the length of one run.
  `*.tfstate` and `*.tfvars` are in `.gitignore`: neither belongs in this repo.
- **Vultr itself** holds the user data (the same tokens) for the life of the box,
  and the box keeps it in `/var/lib/cloud`, readable by root only.
- **The box's WireGuard key** is in `hosts/vultr/wg.key.age`, and on the box in
  `/etc/wireguard/wg0.conf`, mode 600.

## Revoking the box's tokens

After `down` the box is gone, but the tokens it was given are still good until
revoked. Revoke them at their source when a box is lost, or its user data may
have been read, and put new ones:

| Token | Revoke it at |
| --- | --- |
| `vultr_api_token` | my.vultr.com, Account, API: regenerate the key (or disable the API), then `mw secrets put vultr_api_token` |
| `boost_github_token` | github.com, Settings, Developer settings, the fine-grained token: delete it |
| `boost_claude_token` | claude.ai, Settings: revoke the token `claude setup-token` made |
| `boost_beads_password` | change it on the beads server (Dolt) |
| the box's WireGuard key | `down` removes the peer; to retire the key too, delete `hosts/vultr/wg.key.age` and `wg.pub` from the vault, and the next `up` makes a new one |

## Elastic: boxes made and destroyed by demand

With a `[cloud]` table in the home's `~/.config/mw/config.toml`, mw makes and
destroys the boxes itself. Without one it never spends: the table is the
Governor's word. Every minute the home's follower runs the cloud check (by hand:
`mw cloud check`, which does nothing on a host that is not home). It costs no tokens.

```toml
[cloud]
provider        = "vultr"   # the one provider
max_boxes       = 2         # boxes up at once, at most
monthly_cap_usd = 75        # the most the boxes may cost in a UTC month
idle_minutes    = 30        # a box with no story this long is destroyed
hourly_usd      = 0.132     # what one box costs an hour
box_cap         = 2         # sessions a box runs at once; a change reaches the boxes up (see "A changed box_cap")
up_minutes      = 60        # how long a make may run before it reads the box back from Vultr
snapshot        = "<id>"    # what contrib/vultr-boost snapshot printed; blank bootstraps in full
command         = "<full path to contrib/vultr-boost>"   # default: the millwright checkout of [rigs]

[cloud.caps]                # the sessions each host runs at once
desktop = 2
laptop  = 2
```

Each check:

1. **Counts.** The stories that wait for any host (ready, unblocked, pathed
   `host = auto`, not hitl), and the sessions free: each host of `[cloud.caps]`
   (without the table, the home alone at its `cap`) and each box up, less the
   stories claimed on it. A story pathed to one host makes no box: a box could not
   take it. A host that is asleep is counted as free all the same, so list only
   the hosts that are on, or expect fewer boxes than stories.
2. **Keeps under the cap.** When one more hour of the boxes up would pass
   `monthly_cap_usd`, every box is destroyed.
3. **Destroys the idle.** A box with no story claimed on it for `idle_minutes`,
   counted from the first check that found it so (from when it was made, until it
   takes one), is destroyed.
4. **Makes one box** when more stories wait than there are sessions free, while
   fewer than `max_boxes` are up, and the month can pay the first hour of it and
   one more hour of every box with it. The box is `cloud1`, or the next name free:
   `contrib/vultr-boost up --yes --name cloudN --snapshot <snapshot>`. A check makes
   at most one; the next check counts its sessions as free while it boots, so it
   is not made twice.

   **The check does not wait for the box.** A make is a terraform apply of six
   minutes and more, and a caller that is stopped mid-apply (a `timeout`, a closed
   session) left a half-made box that was billed and not in the book. So the check
   writes the box into the book as *being made*, prints `making cloudN in the
   background; the book says when it is up`, and starts `mw cloud make cloudN`
   (hidden) in a session and systemd scope of its own, with the deadline of
   `up_minutes` (default 60: a Vultr snapshot restore of our image took about 50
   minutes on 2026-10-15, so 20 killed every make). That process runs the wrapper
   and writes the end into the book and a `cloud` event, under the cloud lock only
   while it writes. A box being made is never destroyed as idle, and a check that
   finds one begun `up_minutes` and 15 minutes ago, with the process gone (a
   reboot), destroys it and says `failed`. Its output is in `~/.local/state/mw-dispatch/cloud-make.log` (the dispatch
   state directory).
5. **A box that does not come up** (the wrapper fails) is destroyed and tried once
   more, by the same process. When that fails too it is destroyed again and no box
   is made for an hour. A refusal that a retry cannot mend is not that: see
   [What Vultr can refuse](#what-vultr-can-refuse). Nor is a make that reaches its
   deadline: see *restoring*, below. A retry never starts while Vultr still holds an
   instance of the box's name, nor when Vultr cannot be asked.
6. **Reconciles with Vultr** (every check, first). It lists the account's instances
   (`vultr-boost list`) and destroys each one named for a box (`cloud` and a number)
   that the book does not hold, a box being made included: an *orphan*, left when a
   make was killed. It is billed and counts against Vultr's limits, and `down` finds
   no state for it. It is destroyed by its id (`down --instance`), as a `cloud` event
   `destroyed orphan cloud1 (Vultr instance <id>, <status>)`. Vultr will not delete an
   instance it has locked (a snapshot restore takes many minutes; it says 409): that is
   said once, `could not destroy ... Vultr would not delete (locked)`, and tried again at
   every check.

   **A box restoring.** A make that reaches `up_minutes` does not tear the box down and
   try again blind. It reads the instance back from Vultr (`vultr-boost list`, which
   prints each instance's server status too): if Vultr holds one, restoring from the
   snapshot (status not active, or server not ok), the book keeps the box as
   *restoring*, with the instance's id and since when (`mw status` says
   `cloud1  restoring ...`), a `cloud` event `restoring cloud1: ...` says so, and no
   second box and no retry is made. The box is billed and counts against `max_boxes`
   and its sessions count as free, as one being made does. The next check that finds
   Vultr says it is up adopts it (`vultr-boost adopt --name cloudN --instance <id>`:
   `terraform import vultr_instance.boost <id>` into the box's state, so that `down`
   finds it; the firewall group of the killed apply is left on Vultr and costs nothing)
   and writes `made cloud1`. A box still restoring `up_minutes` after the make gave up
   waiting is destroyed by its id; one Vultr no longer holds is dropped from the book.

   **What the book says of Vultr is read back, never assumed.** `up`, `down` and
   `destroyed` are written only after Vultr has been asked: `made` after the box is found
   on Vultr; `down` after the wrapper's `down` and a read of the instances (a wrapper
   that holds no state for a box exits well and destroys nothing; an instance still
   held is deleted by its id); `destroyed` only after the delete answered 204 or 404 (the
   wrapper treats every other answer, 200 included, as a refusal, and says `locked, HTTP
   409` for a 409). A box Vultr would not delete stays in the book and the event says
   `Vultr would not delete (locked)`.

   **`up` is the box at work, not Vultr's word.** A make that ends with Vultr holding
   the box writes `made cloud1 in ...; up once its first dispatch reaches the beads
   server`. A later check writes `up cloud1: its first dispatch reached the beads server
   at ...` once the box's `host.cloud1.last_sync` note, which its dispatch's sync writes
   into the one database before it claims anything, is newer than the make. A box that
   has none 20 minutes after it was made is written once as `cloud1 up, not dispatching:
   <why>` (what the note says, and where to look on the box), and `mw status` shows the
   same; until then it shows `waiting for its first dispatch`. Two boxes from cloud1's
   snapshot once both ran as cloud1 with a broken vault, and claimed nothing for an hour
   while the book said they were up (mw-5gr3k0.8).

### A changed box_cap reaches the boxes already up

`box_cap` is the home's count of a box's sessions. A box runs at its own `cap`, in its own
`~/.config/mw/config.toml`, which `boost-bootstrap.sh` wrote once from `BOOST_CAP` (the tfvars
`cap`). So that a changed `box_cap` reaches a box with no rebuild and no hand step, each cloud
check (every minute, on the home) compares the cap it last set on each box with `box_cap`, and for
a box that differs runs `contrib/vultr-boost set-cap --name cloudN --cap <box_cap>`. That reads
the box's WireGuard address off the hub (nothing there changes) and runs, over ssh as the box's
user, a small rewrite of the first `cap` line of the box's config: the other lines stay, and the
box's next dispatch reads it, no restart. Raising or lowering works alike: sessions running
finish, and a box over a lowered cap starts none until it is under it.

- Only a box that has dispatched is touched (before that it may not answer ssh); one a book
  never set a cap on is set once, so every box is counted at the cap it really runs.
- The book (`hosts/vultr/cloud.json`) keeps each box's `cap`. A set that took is a move in
  the book for each box (`cloud1: cap 2 to 3`) and one `cloud` event for the check (`box_cap 3
  set on cloud1, cloud2`). The count of free sessions uses the new cap at once.
- A box the check cannot reach keeps its old cap in the count, says so once as a `cloud`
  event (`could not set cap 3 on cloud2: ...; it is counted at the cap 2 it has`), and `mw
  status` shows under it `cap 2, not the 3 box_cap says: ...` until a later check gets through.

**`mw cloud cap N`** (N from 1 to 8; the home only, refused elsewhere) is the one command that
changes `box_cap`: it writes it in the `[cloud]` table of the home's `~/.config/mw/config.toml`,
in the vault's copy `hosts/vultr/cloud-table.toml`, and as `cap` in
`hosts/vultr/vultr-boost.tfvars` (what the next box is made with), commits the vault's two
files, and prints what it was and the way back, `mw cloud cap <old>`. It reaches no box: the next
check applies it. Editing `box_cap` by hand in the config works the same way; the command only
keeps the three places level.

To undo: `mw cloud cap <old>`; the next check sets the old cap on each box again. A box that is
being made takes the tfvars `cap`, which the command keeps level with `box_cap`.

### Stories pathed to a box that goes down

A story pathed to a host that is gone is never dispatched. Whenever a box is destroyed (idle, the
month's cap, a make that did not come up, or `mw cloud down <box>`, the home only), the cloud sets
`host=auto` on every open or deferred story whose path names it, and says so as a `cloud` event
(`re-pathed mw-x.1, mw-x.2 from cloud2 to host=auto`). A story on another host is left alone, and
so is a claimed one with a session, running or ended. A claim queued for its rig's batch has no
session (its branch is pushed, and its batch lands it): it does not keep a box busy, and when the
box goes down the claim is given back and set to `host=auto`, still queued, and said as a `cloud`
event (`handed back mw-x.1 from cloud2, queued for its batch with no session on the box, and
re-pathed it to host=auto`). `mw cloud down <box>` takes only a box's name (`cloud` and a number) and
runs under the cloud lock. `contrib/vultr-boost down`, run by hand, is a shell wrapper that
re-paths nothing: use `mw cloud down`.

### What Vultr can refuse

`vultr-boost up` can fail because Vultr says no, and no retry mends it. The adapter
names these from terraform's standard error; only the first has been seen (the wording
of the others is Vultr's API and terraform provider as best known, matched loosely).
Anything else is a plain failure: destroyed, tried once more, then held an hour.

| Vultr says (matched, lower case) | What the book and the `cloud` events say | What the check does |
| --- | --- | --- |
| `maximum monthly fee limit` | `Vultr fee limit reached: raise it at my.vultr.com or wait for the month` | Not a failure to come up: no second try, no hour's hold. The leftovers are destroyed. One card to the Governor, once, until a box comes up. No box is tried for 10 minutes, then once more, silently while it lasts. `mw status` says `Vultr fee limit reached since ...`. |
| `instance limit`, `maximum number of instances`, `reached your limit` | `Vultr instance limit reached: raise it at my.vultr.com, or destroy a box there` | One try, destroyed, no box for an hour. An event, no card. |
| `sold out`, `not available in this region`, `no capacity` | `Vultr has none of the plan left in the region: choose another region or plan in the tfvars` | One try, destroyed, no box for an hour. An event, no card. |
| `invalid api key/token`, `unauthorized ip`, `401` | `Vultr refused the API token: put a new vultr_api_token (mw secrets put), and check the API's access control at my.vultr.com` | One try, destroyed, no box for an hour. An event, no card. |
| (the instance list cannot be read) | the check says so on its output and looks for no orphan | The rest of the check goes on. |

The fee limit is Vultr's, on the account; `monthly_cap_usd` is mw's own, and the two are
separate.

**The month's spend** is the box hours the check made, each begun hour a whole
one (Vultr bills by the hour), kept in the vault in `hosts/vultr/cloud.json` with
the boxes up, and committed at each change: it moves with the vault to the next
home. Each check also asks `contrib/vultr-boost spend` for what Vultr's pending
charges say the boxes `cloud1`..`cloudN` cost, and takes that when it is more.
When Vultr cannot be asked, the book's count stands. A new UTC month starts the
count afresh. At the cap no box is made and one cloud event says `cap reached`,
once a month.

**Every move is an event**, kind `cloud`, actor `cloud@<home>`: `making cloud1: 5
stories wait for any host, 0 sessions free`, `up cloud1: made in 6m10s`, `down cloud1: idle 30m`, `failed
cloud1: ...`, `cap reached: $74.90 spent of $75.00 this month; no box is made`.
`mw events tail` shows them. **`mw status`** has a CLOUD section: the month's
spend against the cap, each box up with its age and how long it has been idle,
and the last five moves.

### The ready-made image

A box bootstrapped from Ubuntu takes some minutes before it works; one made from
a snapshot is working in about two. To make the snapshot:

1. `contrib/vultr-boost up --name cloud1`, and let the bootstrap finish
   (`~/boost-bootstrap.log` on the box; it shows in `mw status` once it dispatches).
2. `contrib/vultr-boost snapshot --name cloud1` asks Vultr for a snapshot of it
   and prints its id. Vultr takes some minutes to finish it.
3. Put the id in `[cloud] snapshot = "<id>"`, and `contrib/vultr-boost down
   --name cloud1`.

A box made from it runs the bootstrap again through cloud-init, which changes only
what is the box's own (its name, WireGuard key and address) and brings `mw` level with
main: the `host` line of the `config.toml` it carries is set to the box's name, a
broken checkout is repaired or cloned again, and its millwright clone is fast-forwarded
to `origin/main` and `mw` built from it, so the box does not run the `mw` the snapshot
was made with (mw-gq6.632). Nothing else in that `config.toml` is edited, so a snapshot made before the bootstrap wrote `[tests]`
keeps a config without it: the bootstrap names the lines it lacks, and `mw doctor
rig-tests` names the rigs. The snapshot holds
what the box held, its tokens too: delete it at Vultr (my.vultr.com, Snapshots)
when you revoke them, and make a new one. It is billed for its size while kept.

## What it does not do

- It does not run the first real `up` on its own: spending is the Governor's word,
  given by writing the `[cloud]` table.
- It does not manage a firewall beyond ssh, or backups. The box holds nothing that
  must outlive it: work is in git and beads.
- It does not log the box in to anything it was not given a token for.
