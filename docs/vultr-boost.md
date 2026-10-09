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
contrib/vultr-boost snapshot       # a ready-made image of a bootstrapped box
contrib/vultr-boost spend          # what Vultr's billing says the boxes cost this month
```

`--name NAME` picks the box (default the tfvars `name`, else `cloud1`). The
tfvars name keeps the files below; any other box keeps its own beside them,
`hosts/vultr/NAME.tfstate.age`, `NAME.wg.key.age` and `NAME.wg.pub`, so two boxes
never share a state or a key. `up --snapshot ID` makes the box from a snapshot.

`--yes` on `up` or `down` passes `-auto-approve` to terraform, which otherwise
asks. `--dry-run` prints the numbered steps and changes nothing.

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
`wg0.conf`, `wg-enrol --remove NAME`, and the way back printed. The WireGuard key
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
box_cap         = 2         # sessions a box runs at once (the bootstrap's cap)
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
5. **A box that does not come up** (the wrapper fails) is destroyed and tried once
   more. When that fails too it is destroyed again and no box is made for an hour.

**The month's spend** is the box hours the check made, each begun hour a whole
one (Vultr bills by the hour), kept in the vault in `hosts/vultr/cloud.json` with
the boxes up, and committed at each change: it moves with the vault to the next
home. Each check also asks `contrib/vultr-boost spend` for what Vultr's pending
charges say the boxes `cloud1`..`cloudN` cost, and takes that when it is more.
When Vultr cannot be asked, the book's count stands. A new UTC month starts the
count afresh. At the cap no box is made and one cloud event says `cap reached`,
once a month.

**Every move is an event**, kind `cloud`, actor `cloud@<home>`: `up cloud1: 5
stories wait for any host, 0 sessions free`, `down cloud1: idle 30m`, `failed
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
what is the box's own (its name, WireGuard key and address). The snapshot holds
what the box held, its tokens too: delete it at Vultr (my.vultr.com, Snapshots)
when you revoke them, and make a new one. It is billed for its size while kept.

## What it does not do

- It does not run the first real `up` on its own: spending is the Governor's word,
  given by writing the `[cloud]` table.
- It does not manage a firewall beyond ssh, or backups. The box holds nothing that
  must outlive it: work is in git and beads.
- It does not log the box in to anything it was not given a token for.
