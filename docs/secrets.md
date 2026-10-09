# Secrets

The factory keeps the tokens it hands to machines in the vault, in
`secrets.enc.yaml`, encrypted with [sops](https://github.com/getsops/sops) to
one [age](https://github.com/FiloSottile/age) recipient, which the vault's
`.sops.yaml` names. The file is committed and synced like any other vault file:
its names are in the clear, its values never are. The one key that opens it
lives on the home.

    mw secrets put <name>    # the value on stdin, never an argument
    mw secrets get <name>    # the value to stdout, only when stdout is not a terminal
    mw secrets list          # names only

## What is stored

Only tokens a machine must be given to do factory work, one per name. Each row
says what the token is, what uses it, and how to revoke it at its source. A
token gets its row here before it is put.

| Name | What it is | Used by | Revoke it at its source |
| --- | --- | --- | --- |
| `vultr_api_token` | the Vultr account's API key (epic mw-5gr3k0) | `contrib/vultr-boost` on the home ([vultr-boost.md](vultr-boost.md)), to make and unmake a Boost box | my.vultr.com, Account, API: disable the API or regenerate the key, then put the new one |
| `boost_github_token` | a GitHub token that reads the vault and the rigs (optional) | `contrib/vultr-boost`, into the box's cloud-init | github.com, Settings, Developer settings: delete the token |
| `boost_claude_token` | a Claude Code token from `claude setup-token` (optional) | `contrib/vultr-boost`, into the box's cloud-init | claude.ai, Settings: revoke the token |
| `boost_beads_password` | the beads server's password, if it has one (optional) | `contrib/vultr-boost`, into the box's cloud-init | change it on the beads server |

Nothing else is kept here. The claude login, GitHub credentials, the Postern
and mill keys and WireGuard keys are each where their own docs say, not in this
file.

## Who holds the key

- **The home** holds the age key at `~/.config/mw/age.key`, mode 600 (or the
  path `age_key_file` in `~/.config/mw/config.toml` or `MW_AGE_KEY_FILE` names).
  Only the home runs `mw secrets get`.
- **The Governor** keeps one offline copy of the key (printed, or on a drive
  kept off any network), so a home lost with its disk does not lose the vault's
  secrets.
- **No other host** holds it: not the Boost, not the VPS, not a cloud box. A
  cloud box is given the one token it needs, piped from `mw secrets get` on the
  home, never the key.

`mw doctor`'s age-key check holds this: on the home it warns when the key is
missing (once the vault keeps secrets) or is not a plain file of mode 600; on
any other host it warns when a key is there at all. It has no cure.

## Setting it up, once

On the home, with `mw` and the vault in place:

1. `sh contrib/install-sops-age.sh` installs the pinned sops, age and
   age-keygen into `~/.local/bin`, each checked against a sha256 pinned in the
   script. A second run changes nothing.
2. `age-keygen -o ~/.config/mw/age.key && chmod 600 ~/.config/mw/age.key`
   makes the key. It prints the public key, the recipient (`age1…`).
3. In the vault, write `.sops.yaml` with that recipient, and commit it:

       creation_rules:
         - path_regex: secrets\.enc\.yaml$
           age: age1…

   Nothing else goes in it: an `encrypted_regex` or `unencrypted_suffix` there
   could leave values in the clear.
4. Make the Governor's offline copy of `~/.config/mw/age.key`.
5. `mw secrets put <name> < file` for each token; each put commits
   `secrets.enc.yaml` alone, and the next `mw sync` carries it.

## When the home moves

The key goes with the home. Copy it to the new home over the WireGuard link
(`scp -p`), check it reads mode 600 there, then remove it from the old home.
Until both are done the age-key check warns on one host or the other.

## When the key may have leaked

Git keeps every past version of `secrets.enc.yaml`, and a leaked key opens all
of them. So a new key alone is not enough:

1. Revoke every token in the table above at its source, and make new ones.
2. Make a new key (step 2 above), put its recipient in `.sops.yaml`, and
   re-encrypt to it in the vault:
   `SOPS_AGE_KEY_FILE=<old key> sops updatekeys --yes secrets.enc.yaml`, then
   `SOPS_AGE_KEY_FILE=<new key> sops rotate -i secrets.enc.yaml` for a new data
   key; commit both files.
3. `mw secrets put` each new token, replace the Governor's offline copy, and
   destroy every copy of the old key.

## What mw never does with a value

A value passes between `mw` and `sops` on a pipe only: on sops' stdin when it
is put, on its stdout when it is read. It is never a command-line argument (a
process listing would show it), never a temp file, never printed to a terminal,
and never written to a log, an event, a bead or a mail. When sops fails, what
it said is passed on with the value cut out of it. `mw secrets list` prints
names, never values. sops is run with every `SOPS_` setting of the caller's
environment removed and shown one identity, the key file.
