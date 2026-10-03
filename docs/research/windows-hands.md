# Research: hands steps on the desktop's Windows side

Answers mw-6ww.86. The Governor, 2026-10-03 12:46Z, on mw-6ww.60: "I need a way of being able to
thumbprint approve Windows activity on the desktop so that it can happen when I'm away but that it
can't happen without me approving it." Read-only research: no product code changed.

## What already holds, and what is missing

A hands step tap in Postern makes a fresh passkey assertion, then signs §17's
`hands-approve/v1\n<sha256>\n<approved_at>\n` with his secp256k1 key (`@bsv/sdk`, DER). mw checks
it (`application/posternrun.go`) and runs it; a root step goes to `mw-hands-root`, which checks it
all again as root against root-owned `/etc/mw-hands/governor.pub` and `/etc/mw-hands/host` and
records the approval under `flock` before running (`infrastructure/handsroot/helper.go`). The
signature check is Go (`handsroot.VerifyApproval`, `bsv-blockchain/go-sdk`, pure Go), so it builds
for Windows unchanged.

Missing, on the desktop: any way in to Windows. Facts from `hosts/desktop.md` and mw-6ww.60:
`/etc/wsl.conf` has interop and automount off (the seal, `desktop-move.md` step 0.4); Windows
OpenSSH is not installed; WireGuard `10.88.0.3` lives inside WSL, so when WSL dies the desktop
vanishes from the network (03:27Z on 2026-10-03, ssh and ping both timed out). The Windows account
is `grace`; WSL distros are per Windows user, so only a process running as `grace` sees
`Ubuntu-24.04`. Postern's backend and `mw postern inbox --apply` run on the Laptop now.

So the one rule every option below must keep: **the step runs only after a Windows-side program,
installed by his hands, checks his signature itself.** WSL is never trusted: a step for Windows is
for a new host name, `desktop-win`, so an approval for `desktop` (WSL) never runs on Windows nor the
reverse. And because the first job is to bring a dead WSL back, the way in must not pass through WSL.

## The Windows helper all three share

`mw-hands-win.exe`: `infrastructure/handsroot` built with `GOOS=windows`, with three parts ported:

- **trusted files**: `C:\ProgramData\mw-hands\governor.pub` and `host` (`desktop-win`). Refuse
  unless the owner is `Administrators` or `SYSTEM`, no ACE grants write to any other SID, neither
  file nor directory is a reparse point (link or junction), and the directory passes the same
  test: the Windows form of "owned by root, not group- or world-writable".
- **replay guard**: `C:\ProgramData\mw-hands\used`, ACL `SYSTEM` and `Administrators` only, held
  with `LockFileEx` from read to write, the approval recorded and flushed before the step starts;
  plus the five-minute age check against the Windows clock (not WSL's, which steps back).
- **running**: `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command <run>`
  inside a Job object with kill-on-close, so the limit stops everything it started. A step's `run`
  and `way_back` for `desktop-win` are PowerShell.

The binary lives in `C:\Program Files\millwright\` (admin-writable only). A later upgrade is itself
a signed step whose text names the new binary's sha256, so his signature binds the bytes.

## Option A: Windows OpenSSH, a forced command to the helper

- **Bootstrap, once, as admin at the desk** (one paste the Mayor writes): install WireGuard for
  Windows with a tunnel of its own (new peer `desktop-win` from `contrib/wg-enrol` on the VPS hub; `10.88.0.5` below stands for its address)
  as a boot-time service (`wireguard /installtunnelservice`); `Add-WindowsCapability` the
  OpenSSH server, `sshd` automatic; a local admin account `mw-hands` with a long random password
  nobody records, denied local and RDP logon; copy the helper, `governor.pub` and `host` in with the
  ACLs above, comparing the key's fingerprint with the Me screen and the binary's
  `Get-FileHash` with the hash the Laptop printed. `sshd_config`: `ListenAddress 10.88.0.5`,
  `PasswordAuthentication no`, and `Match User mw-hands` with `ForceCommand` naming the helper,
  `PermitTTY no`, no forwarding. One key in `administrators_authorized_keys`:
  `restrict,from="10.88.0.2,10.88.0.3",command="…mw-hands-win.exe" ssh-ed25519 … mw-hands@laptop`.
  Firewall: drop the default any-address `OpenSSH-Server-In-TCP` rule; allow 22 only on the WG
  interface from those addresses.
- **Re-verifies on Windows**: the helper, run by sshd in `mw-hands`'s session. A Windows OpenSSH
  session for an administrator gets the full, elevated token (no UAC split), so the helper and the
  step run elevated with no further hop. Confirm with `whoami /groups` (High Mandatory Level) in
  the first step.
- **Replay guard**: the helper's `used` file; mw's own `hands.approval.*` note before that.
- **governor.pub**: `C:\ProgramData\mw-hands\`, writable by `SYSTEM` and Administrators only.
  Every admin on Windows is root: if `grace` is an admin, malware that wins a UAC prompt in her
  session could rewrite it. That is the same trust Linux gives root, no more.
- **When WSL is dead**: everything here runs without it: WireGuard service, sshd, helper. The
  Laptop reaches `10.88.0.5` while `10.88.0.3` is gone.
- **Attack surface**: sshd (a SYSTEM service) listening on the WG address only; the Laptop's
  `mw-hands` key, usable only to start the helper; then his signature. Both are needed. A broken
  forced command would be an admin shell, hence `restrict`, `from=` and the firewall rule as well.
- **mw changes**: almost none. The runner's root branch already pipes the request JSON on stdin over
  the `[hands_hosts]` prefix (`desktop-win = "ssh -i ~/.ssh/mw-hands-win mw-hands@10.88.0.5"`); the
  forced command ignores the `sudo -n …` it sends. A user step sends no stdin, so the helper refuses
  it: fail closed. mw should still refuse `as: user` for `desktop-win` with a plain reason.

## Option B: an unprivileged ingress and an elevated task watching a drop folder

- **Bootstrap**: WireGuard and sshd as in A, but the login account `mw-drop` is not an admin. Its
  forced command (`mw-hands-win.exe submit`) writes stdin to `C:\ProgramData\mw-hands\inbox`
  (`mw-drop` may create files there, nothing more), starts the task `millwright-hands`, and waits
  for the result in `outbox`, streaming it back, so mw still sees one synchronous run. The task
  runs `mw-hands-win.exe work` as `SYSTEM` (highest privileges); its security descriptor grants
  `mw-drop` the right to start it and nothing else (to be tried at the desk; failing that, the task
  also fires every minute and `submit` waits for it).
- **Re-verifies on Windows**: the SYSTEM worker, which trusts nothing `submit` says.
- **Replay guard**: the worker's `used` file; the inbox file is deleted after reading, either way.
- **governor.pub**: as in A.
- **When WSL is dead**: runs, as A. Steps that need `grace`'s WSL register tasks as `grace`.
- **Attack surface**: smaller than A where it matters: a broken forced command is a non-admin shell
  that can only drop files. Larger in moving parts: the inbox and outbox ACLs, the task's descriptor,
  a second process to keep in step.

## Option C: WSL interop back on, plus an elevated task

- **Bootstrap**: re-enable interop and automount in `/etc/wsl.conf` (undoing step 0.4); register
  the SYSTEM task and helper of B; mw inside WSL writes requests to
  `/mnt/c/ProgramData/mw-hands/inbox` and starts the task with `schtasks.exe /run`.
- **Re-verifies on Windows**: the SYSTEM worker, as B.
- **Replay guard**: as B.
- **governor.pub**: as A.
- **When WSL is dead**: nothing runs. It cannot be the way the WSL keeper arrives or a dead WSL is
  brought back, which is the whole first need.
- **Attack surface**: the worst. Every process in WSL (each Builder, each `npm install` script) gets
  `grace`'s unelevated Windows token and her whole `C:` drive, signature or not. The seal was put
  there on purpose.

(A fourth, not costed here: the helper as a SYSTEM Windows service listening on the WG address over
TLS, with no sshd and no admin account; the smallest listener, but a new transport in mw's runner
and a service wrapper to write.)

## Recommendation: Option A

A is the Linux design already reviewed and running, carried across: a key that can only start the
helper (in place of `sudoers NOPASSWD`), a helper that checks his signature itself, root-owned key
and host files, a locked used-approval file. It needs the least new code (the helper's Windows
port, a `desktop-win` host, no runner change), keeps WSL sealed, and works with WSL dead. If he
wants the network-facing account unprivileged, B is the upgrade path, on the same helper.

The first three steps, each a `desktop-win` root step he taps:

1. **`wsl-keeper`**: register `millwright-wsl-keeper` with principal `grace`, logon type S4U (no
   password stored), triggers at startup, at logon, on unlock and every 5 minutes; its action is
   the watchdog of `hosts/desktop-wsl-watchdog.ps1` (start `sleep infinity` if the distro is not
   running; `wsl --shutdown` and start after two failed checks in a row). Add `vmIdleTimeout=-1`
   under `[wsl2]` in `grace`'s `.wslconfig`. Print `whoami /groups` and the task's first run result.
   Way back: `Unregister-ScheduledTask millwright-wsl-keeper`, remove the line. Risk: `wsl.exe` may
   refuse to run from a non-interactive S4U task; if the first run fails so, ask him (question 5).
2. **`power-log`** (read-only): Kernel-Power and Power-Troubleshooter events since 2026-10-01,
   `powercfg /a`, `powercfg /requests`, the keeper and `wifi-watch` logs: what killed WSL at lock,
   the read mw-6ww.60's option B wanted.
3. **`power-fix`**: written from step 2's output, e.g. `powercfg /change standby-timeout-ac 0`,
   `hibernate-timeout-ac 0`, and the Wi-Fi adapter's "allow the computer to turn off this device"
   off.

## Questions only the Governor can answer

1. **Any signed step, or an allow-list of commands?** Recommended: any signed step. His tap on the
   full text is the gate; an allow-list sends every new chore back to the desk, the thing he wants
   to stop doing.
2. **Which Windows account do steps run as?** Recommended: a new local admin `mw-hands`, never
   `grace`'s own login. Steps that need her WSL register tasks as `grace`.
3. **If `grace` is an administrator, does she stay one?** An admin session can rewrite
   `governor.pub` after a UAC prompt. Recommended: yes for now, since she is his own account at
   the desk; revisit (a separate admin, `grace` standard) once A works.
4. **A (ssh key plus his signature, helper elevated) or B (unprivileged ingress, about a day more
   work)?** Recommended: A now, B later if the grilling asks for it.
5. **If `wsl.exe` will not run under S4U, may the keeper task store `grace`'s password (LSA)?**
   Recommended: yes, for that one task only; otherwise WSL is held only while she is signed in.
6. **Which hosts may call in?** Recommended: the Laptop and the desktop's WSL (`10.88.0.2`,
   `10.88.0.3`), wherever Postern's hook runs if home moves; never the VPS.
7. **May a signed step change the hands path itself (helper, key, sshd)?** Recommended: the helper
   by a step naming its sha256, yes; `governor.pub` only at the desk, a rule the Mayor keeps, since
   any elevated step could change it.
8. **The approval window**: recommended five minutes, as on Linux.
