# Give way: the Windows half

The desktop is also another person's PC, and the factory runs inside its WSL. `mw giveway on` (see the README,
"A host that gives way to the person at its screen") freezes the factory there and `mw giveway off` lets it
go on. The Windows half is one script, `contrib/windows/giveway.ps1`, and four scheduled tasks of the
account that owns the WSL distro (the default of `--account`) that run it. They are installed by one paste that mw prints.

Windows PowerShell 5.1, no module beyond what Windows ships. The distro is `Ubuntu-24.04`, its user
`jwhite`, and mw is `/home/jwhite/.local/bin/mw` (the three are at the top of the script).

<!-- toc -->

- [What happens at unlock, at lock and at logoff](#what-happens-at-unlock-at-lock-and-at-logoff)
- [The paste](#the-paste)
- [The mode, and the way back](#the-mode-and-the-way-back)
- [The two logs](#the-two-logs)
- [Which event does what](#which-event-does-what)
- [What the home shows](#what-the-home-shows)
- [What is NOT covered](#what-is-not-covered)
- [Checking it](#checking-it)

<!-- /toc -->

## What happens at unlock, at lock and at logoff

Each task runs `giveway.ps1 -Event unlock|lock|logoff|tick`. The mode is one word in the file `mode` beside the
script: `freeze` (the default, and what a missing or unreadable file means) or `shutdown` (the fallback).

| Event | `freeze` mode | `shutdown` mode |
|---|---|---|
| unlock | At once: `mw giveway on --by windows-unlock --note windows_free_mb=<Windows' Available MBytes> --note windows_ms=<ms since the script's process started>`. Then the state file says `unlocked`. Then, after the freeze and not before, as root in WSL: `sync` and drop the page cache (so the guest hands memory back to Windows); 10 s later `mw giveway mark` with `windows_free_mb` again. If a lock came meanwhile, the drop and the mark are skipped. | `mw giveway on`, the state file says `unlocked`, then `wsl.exe --terminate Ubuntu-24.04`. |
| lock | The state file says `locked`; `mw giveway off --by windows-lock` (continues what was stopped, retries the sessions that died). | The state file says `locked`; WSL is started again as the task `Start WSL (millwright)` does (that task is started; if it is missing, a hidden `sleep infinity` is); the script waits up to 2 minutes until WSL answers; then `mw giveway off`. |
| logoff | After a pause of 3 s (the event comes as the session ends): if a session of some user is still at the screen, nothing (the tick keeps the give way going); otherwise as lock, by `windows-logoff`. | The same, then as lock. |
| tick (every 5 minutes) | When a session of ANY user, on the console or remote, is active at the screen (`quser` shows `Active` for it, and no lock screen, `LogonUI`, is up in that session) and WSL is running: `mw giveway on --by windows-tick`, a renew. Otherwise nothing. | Nothing: a renew would start WSL again. |

The unlock task fires on workstation unlock, console connect and remote connect (remote desktop) of ANY
user's session, and at logon of any user. The lock task fires on workstation lock, console disconnect and
remote disconnect of any user's session. The logoff task fires on an event-log trigger, because Task
Scheduler has no logoff trigger: event 4 of the log `Microsoft-Windows-User Profile Service/Operational`
(provider `Microsoft-Windows-User Profiles Service`), "Finished processing user logoff notification", which is
written after the session has ended; it is on by default (Security 4647 needs the Logoff audit policy). The
tick task repeats every 5 minutes. A marker not renewed for `giveway_stale` (15 minutes by default) is lifted by the
next `mw dispatch` tick, so a missed lock costs at most that.

**No window.** The task's action is `conhost.exe --headless powershell.exe -NoProfile -NonInteractive
-ExecutionPolicy Bypass -File ...giveway.ps1 -Event <event>`, not `powershell.exe -WindowStyle Hidden`
(that flag still flashes a console for a moment). `conhost.exe --headless` needs Windows 10 build 19041 or
later, or Windows 11; the install paste refuses to register anything on an older build. The Laptop is build
26200; the desktop's build is read by the paste. `wsl.exe` is started from the same hidden console.

**A failure of `wsl.exe`** (WSL down, the distro missing) is written to the log and the script exits 0: no
error box.

**WSL down.** `wsl.exe -d <distro> --exec` starts a distro that is down, and nothing runs the factory while
WSL is down. So before it calls `wsl.exe` for an unlock, a lock (freeze mode), a logoff or a tick, the
script looks for the WSL 2 virtual machine's process (`vmmemWSL`, or `vmmem` on an older WSL): none, and it
logs `WSL is not running: ... not started` and calls nothing. The look takes about 15 to 30 ms (asking
`wsl.exe` for its running distros takes about 400, which would delay the freeze). If the look itself
fails, the script goes on as if WSL were up. In `shutdown` mode the lock still starts WSL: that is its point.
A VM that is up with the distro stopped inside it is not told apart from a running one.

**`-DryRun`** prints, for the event and the mode, each command it would run, in order, and runs none; it
writes no state and no log. The two checks are assumed: `-AssumeWsl up|down` (default `up`) for the WSL check
and `-AssumeScreen occupied|empty` for the screen (default `occupied` for a tick, `empty` for a logoff).

## The paste

On the desktop, in a NEW admin Windows PowerShell as the account that owns WSL (the default of `--account`; the prompt is `PS C:\Users\<that account>>`). On any host that has mw:

```sh
mw host giveway --install-line
```

It prints one block of text that begins with a comment saying what it does and its way back. Paste the whole
block. It refuses to run under any account but that one (`--account <name>` changes it) and on a Windows
older than build 19041; it makes the folder `C:\ProgramData\millwright\giveway`; downloads the script from the
public repo pinned at the commit this mw was built from and refuses it unless its SHA-256 is the embedded
copy's (`--inline` instead carries the script in the paste, base64, for a host that cannot reach the repo);
writes the `mode` file (`freeze`); registers `millwright give way (unlock)`, `millwright give way (lock)`, `millwright give way (logoff)` and
`millwright give way (tick)` under that account (interactive logon, normal rights); and prints the four
tasks with their state and next run (all but the tick have none: they run on their events).

Nothing in mw runs the paste. It does not change `.wslconfig`, and it leaves `millwright-wsl-flag-watch` and
`Start WSL (millwright)` as they are.

## The mode, and the way back

```sh
mw host giveway --mode-line shutdown   # the paste that writes the word 'shutdown' to the mode file
mw host giveway --mode-line freeze     # and the one that writes 'freeze' again
mw host giveway --uninstall-line       # the way back
```

The way back removes the four tasks and the folder `C:\ProgramData\millwright\giveway` (not
`C:\ProgramData\millwright`, where the other scripts live), and then runs `mw giveway off --by
windows-uninstall` in WSL so that a host left frozen, or with WSL down in `shutdown` mode, is not left so.

## The two logs

- **Windows: `C:\ProgramData\millwright\giveway\giveway.log`**, one line per run (renamed to `.1` at 256 KB):
  `<UTC time> <event> <mode> <step> exit=<code> ms=<ms> ... total_ms=<ms>`. The steps are `on`, `state=`,
  `drop-caches`, `mark`, `off`, `renew`, `terminate`, `start-wsl`, `wsl-answers`, and the notes
  `WSL is not running: ...` and `someone else still at the screen: nothing`. An `exit=-1` is a
  `wsl.exe` that could not be run; `FAILED(...)` is an exception. Read it with
  `Get-Content C:\ProgramData\millwright\giveway\giveway.log -Tail 20`. `state` and `mode` are beside it.
- **WSL: `~/.local/state/mw/giveway.log`** (`mw giveway status` prints the last 5 lines): one JSON line per
  `on`, `off`, renew, `expired` and `mark`, with `took_ms`, the sessions and processes stopped,
  `MemAvailable`, `load1` and the `--note` values (`windows_free_mb`, `windows_ms`, and the `by` of the
  caller: `windows-unlock`, `windows-lock`, `windows-logoff`, `windows-tick`).

Right at unlock: the Windows line's `on` exit=0 and its `ms` a second or two at most, and the WSL line of
event `on` by `windows-unlock` within a couple of seconds of the unlock, with a `mark` ten seconds later whose
`windows_free_mb` is higher than the `on` line's. After the lock: an event `off` by `windows-lock`.

## Which event does what

| Windows event | Task that fires | What the host does |
|---|---|---|
| unlock | unlock | freezes (or shuts WSL down) |
| lock | lock | goes on (or starts WSL, then goes on) |
| logon (any user) | unlock | as unlock |
| logoff (any user) | logoff | goes on at once, unless a session of another user is still at the screen (then it stays frozen and the tick keeps it so) |
| console connect | unlock | as unlock |
| console disconnect | lock | as lock |
| remote desktop connect | unlock | as unlock |
| remote desktop disconnect | lock | as lock |
| a person under another account, the owner's session signed in (disconnected) | unlock and lock, of any user's session | as unlock and lock: the owner's session runs the task |
| a person under another account, no session of the owner | none can run | see "What is NOT covered": WSL is down then, and nothing is frozen because nothing runs |
| unlock with WSL down | unlock | nothing is called, WSL is not started, nothing is frozen |
| sleep, wake | none | nothing. Windows holds WSL still while asleep; after wake the lock screen asks for the password and unlock fires, or the tick renews if the session is still active |
| a restart | none (logon fires the unlock task after it) | WSL comes back with the marker file still on disk; the next `mw dispatch` tick lifts it when it is older than `giveway_stale` |

## What the home shows

A host that gives way keeps its claims (the heartbeats go on), but its sessions are silent for as long as the person
is at the screen. So that the home does not take that silence for a fault, the host tells it (mw-2891blf.3).

- **The note.** After `mw giveway on` has made its last stop (never before: the freeze makes no beads call), the host
  writes the tracker's note `host.<host>.gives_way` = `<since RFC3339> <by> <seen RFC3339>`, under a timeout of 5
  seconds. A failure (the tracker down, slow or erroring) is the `note` field of the `on` line in `giveway.log`
  (`failed: ...`) and changes nothing else; the next renew writes it again. A renew rewrites it no oftener than every
  10 minutes (`seen` is the time of the write). `mw giveway off` clears it after the host has gone on, and so does the
  expired lift of a stale marker (`note` is `cleared`, or `failed to clear: ...`).
- **What the home does with it.** While a note stands, every place the home judges a session quiet, stuck,
  stranded or starved passes over the stories claimed on that host: the quiet alarm (`mw nudge`: no "in progress N min"
  and no "<host> last synced N min ago"), `mw sweep` (no "stuck" comment, no `run=stuck`; the report names them as
  frozen), `mw status` (no STRANDED line, no NO SESSION line, the host is not called asleep, no STUCK CLAIMS entry),
  the DAMPERS starved-rig alarm, `mw stuck`, `mw doctor`'s `stuck-claims` check and the claim no host names that a
  dispatch gives back.
- **The line.** `mw status` says once, under the host in OTHER HOSTS (and at the top of its own report on the host
  that gives way): `<host> gives way since <MM-DD HH:MMZ> (<n> stories frozen)`. `<n>` is the stories claimed there.
- **A note that is not believed.** A note not rewritten for twice `giveway_stale` (30 minutes by default) is stale: a
  host that was switched off, crashed or lost its WSL leaves its note behind. `mw status` says `<host>: gives-way note
  stale, last written <time>: not believed` and every alarm behaves as it does with no note.

## What is NOT covered

- Her PC asleep or hibernating: nothing runs, and nothing needs to; WSL does not run either.
- WSL already down when an unlock comes: `wsl.exe --exec` starts it, `mw giveway on` freezes nothing, and the
  WSL keeper (`windows-hands.md`) is what keeps WSL up.
- A Windows restart (see the table): the host stays frozen for up to `giveway_stale`.
- **Nobody of the owner's account signed in (mw-2891blf.15, left named).** The tasks are interactive tasks of
  that account, so with no session of it (signed off) none runs. This does not freeze the factory because
  there is none to freeze: WSL is held only while that account is signed in (`docs/research/windows-hands.md`,
  question 5; the keeper `Start WSL (millwright)` is an interactive task too), so with the account signed
  off WSL is down. That is reasoned, not shown on the desktop. A different user at the screen while the
  owner's session is signed in but disconnected (fast user switching) is covered: the owner's session runs the
  tasks. What was NOT done: an `S4U` principal (a task without a session, no stored password) would run
  without that session, but `wsl.exe` under `S4U` is untested, and swapping the interactive tasks for it
  could break the case that works; a SYSTEM task cannot call `wsl.exe` for the owner (a distro is per user),
  and a marker it writes cannot be read from a sealed WSL (no `/mnt/c`). If the desktop ever keeps WSL up
  with the owner signed off (an `S4U` keeper), this needs doing: make the four tasks `S4U` after trying
  `wsl.exe` under it, and never store a password.
- Anything the person does in WSL itself.
- The factory's own processes that `mw giveway on` does not stop (its Found-not-fixed list is on the first
  story, mw-2891blf.1).

## Checking it

Not yet run on the desktop. At the desktop, after the paste: lock the PC (Win+L), wait a minute, unlock it.
No window flashes, and browsing is as quick as with the factory off. Then read the two logs above.
