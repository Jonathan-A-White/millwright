# Keeping each Builder host's Claude Code level with the home's

A model can need a newer Claude Code than a host has (`claude-fable-5-2` needs
2.1.321 or later), and a session started there fails. A host whose Claude Code
falls behind the home's was unseen until mw-gq6.1059; now two things watch it.

<!-- toc -->

- [What watches](#what-watches)
- [Bringing a host level by hand](#bringing-a-host-level-by-hand)
- [The way back](#the-way-back)
- [What else the snapshot freezes](#what-else-the-snapshot-freezes)

<!-- /toc -->

## What watches

- **`mw doctor harness-level`**, on the home. Every sync leaves the host's
  Claude Code version as the note `host.<host>.harness` (beside `mw_commit`);
  the home reads those for the Boost, the hosts `[cloud.caps]` names and the
  boxes the cloud book holds up, and compares each with its own `claude
  --version`. A host older than the home is one faulty line:
  `cloud1 has claude 2.1.300, behind the home's 2.1.322: from the home run
  contrib/vultr-boost set-harness --name cloud1 --version 2.1.322`. A host that
  left no version, or left something that is no version, is said `unread` and
  never guessed (cannot-tell, unless another host is behind). The check only
  reports: there is no cure to run, the host is brought level by hand, below.
  It says what a host's last sync saw, so a host just brought level reads right
  after its next sync.
- **A new box is brought level before its first dispatch.** A box made from the
  snapshot comes up on the snapshot's Claude Code. The cloud check, before it
  sets the box's dispatch timer (`set-pace`), runs `contrib/vultr-boost
  set-harness --name cloudN --version <the home's>`. If the box is behind, one
  `npm install -g --prefix /home/mw/.local @anthropic-ai/claude-code@<version>`
  runs on it and the cloud check records the move, `cloud1: claude 2.1.300 to
  2.1.322 (the home's)`. A box already level runs no install. A failed install
  leaves the box up on the version it had, sets its timer all the same and says
  one line, `cloud1: claude stays at 2.1.300, not 2.1.322: npm install failed:
  ...`; it is never a box that cannot start a session, and the doctor's check
  then names it until a person brings it level.

## Bringing a host level by hand

Read the version to reach on the home: `mw version` prints `claude 2.1.322`.

**A cloud box** (Claude Code is an npm package under the prefix `/home/mw/.local`;
npm's own prefix on a box is the node folder, so a bare `npm install -g` puts a
copy where the PATH never reaches):

```sh
contrib/vultr-boost set-harness --name cloud1 --version 2.1.322
# the same by hand:
ssh mw@<the box's address> 'bash -lc "npm install -g --prefix /home/mw/.local @anthropic-ai/claude-code@2.1.322"'
```

The address comes from `contrib/vultr-boost address --name cloud1` (the home
resolves no `cloud1.mw` name). npm warns that the package's postinstall is not
covered by allowScripts; the binary runs all the same. `--dry-run` prints the
steps and runs none.

**The Laptop and the desktop** (the native install: versions side by side under
`~/.local/share/claude/versions`, the link `~/.local/bin/claude` pointing at
one):

```sh
ssh desktop 'bash -lc "claude install 2.1.322"'
```

**Check:** `mw version` on the host prints `claude 2.1.322`; its next sync
leaves the note, and `mw doctor harness-level` on the home is ok.

## The way back

- A box: install the version it had, the same command with the old number
  (`contrib/vultr-boost set-harness` installs only when the box is *behind*
  the number given, so use the ssh form for a downgrade):
  `ssh mw@<address> 'bash -lc "npm install -g --prefix /home/mw/.local @anthropic-ai/claude-code@2.1.300"'`.
- A native host: the old version is still on disk, so point the link back:
  `ssh desktop 'ln -sfn ~/.local/share/claude/versions/2.1.310 ~/.local/bin/claude'`.
- The change itself: revert the commit. Boxes then come up on the snapshot's
  version again and the doctor is quiet about it.

## What else the snapshot freezes

See the closing comment of mw-gq6.1059 for how each of mw, bd, Go, Node and a
rig checkout's tools is kept level today, and which are not.
