# Give way: the Windows half

The desktop is also another person's PC, and the factory runs inside its WSL. `mw giveway on` (see the README,
"A host that gives way to the person at its screen") freezes the factory there and `mw giveway off` lets it
go on. The Windows half is one script, `contrib/windows/giveway.ps1`, and three scheduled tasks of the
account that owns the WSL distro (the default of `--account`) that run it. They are installed by one paste that mw prints.

Windows PowerShell 5.1, no module beyond what Windows ships. The distro is `Ubuntu-24.04`, its user
`jwhite`, and mw is `/home/jwhite/.local/bin/mw` (the three are at the top of the script).

<!-- toc -->

- [What happens at unlock and at lock](#what-happens-at-unlock-and-at-lock)
- [The paste](#the-paste)
- [The mode, and the way back](#the-mode-and-the-way-back)
- [The two logs](#the-two-logs)
- [Which event does what](#which-event-does-what)
- [What is NOT covered](#what-is-not-covered)
- [Checking it](#checking-it)

<!-- /toc -->

## What happens at unlock and at lock

Each task runs `giveway.ps1 -Event unlock|lock|tick`. The mode is one word in the file `mode` beside the
script: `freeze` (the default, and what a missing or unreadable file means) or `shutdown` (the fallback).

| Event | `freeze` mode | `shutdown` mode |
|---|---|---|
| unlock | At once: `mw giveway on --by windows-unlock --note windows_free_mb=<Windows' Available MBytes> --note windows_ms=<ms since the script's process started>`. Then the state file says `unlocked`. Then, after the freeze and not before, as root in WSL: `sync` and drop the page cache (so the guest hands memory back to Windows); 10 s later `mw giveway mark` with `windows_free_mb` again. If a lock came meanwhile, the drop and the mark are skipped. | `mw giveway on`, the state file says `unlocked`, then `wsl.exe --terminate Ubuntu-24.04`. |
| lock | The state file says `locked`; `mw giveway off --by windows-lock` (continues what was stopped, retries the sessions that died). | The state file says `locked`; WSL is started again as the task `Start WSL (millwright)` does (that task is started; if it is missing, a hidden `sleep infinity` is); the script waits up to 2 minutes until WSL answers; then `mw giveway off`. |
| tick (every 5 minutes) | When the state file says `unlocked` and a session is active at the console (`quser` shows `console` and `Active`, and the lock screen, `LogonUI`, is not up): `mw giveway on --by windows-tick`, a renew. Otherwise nothing. | Nothing: a renew would start WSL again. |

The unlock task fires on workstation unlock and console connect of ANY user's session, and at logon of any
user. The lock task fires on workstation lock and console disconnect of any user's session. The tick task
repeats every 5 minutes. A marker not renewed for `giveway_stale` (15 minutes by default) is lifted by the
next `mw dispatch` tick, so a missed lock costs at most that.

**No window.** The task's action is `conhost.exe --headless powershell.exe -NoProfile -NonInteractive
-ExecutionPolicy Bypass -File ...giveway.ps1 -Event <event>`, not `powershell.exe -WindowStyle Hidden`
(that flag still flashes a console for a moment). `conhost.exe --headless` needs Windows 10 build 19041 or
later, or Windows 11; the install paste refuses to register anything on an older build. The Laptop is build
26200; the desktop's build is read by the paste. `wsl.exe` is started from the same hidden console.

**A failure of `wsl.exe`** (WSL down, the distro missing) is written to the log and the script exits 0: no
error box. Note that `wsl.exe -d <distro> --exec` starts a distro that is down, so an unlock with WSL down
starts it and freezes nothing.

**`-DryRun`** prints, for the event and the mode, each command it would run, in order, and runs none; it
writes no state and no log. A tick in a dry run assumes `unlocked` and someone at the console.

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
writes the `mode` file (`freeze`); registers `millwright give way (unlock)`, `millwright give way (lock)` and
`millwright give way (tick)` under that account (interactive logon, normal rights); and prints the three
tasks with their state and next run (the unlock and lock tasks have none: they run on their events).

Nothing in mw runs the paste. It does not change `.wslconfig`, and it leaves `millwright-wsl-flag-watch` and
`Start WSL (millwright)` as they are.

## The mode, and the way back

```sh
mw host giveway --mode-line shutdown   # the paste that writes the word 'shutdown' to the mode file
mw host giveway --mode-line freeze     # and the one that writes 'freeze' again
mw host giveway --uninstall-line       # the way back
```

The way back removes the three tasks and the folder `C:\ProgramData\millwright\giveway` (not
`C:\ProgramData\millwright`, where the other scripts live), and then runs `mw giveway off --by
windows-uninstall` in WSL so that a host left frozen, or with WSL down in `shutdown` mode, is not left so.

## The two logs

- **Windows: `C:\ProgramData\millwright\giveway\giveway.log`**, one line per run (renamed to `.1` at 256 KB):
  `<UTC time> <event> <mode> <step> exit=<code> ms=<ms> ... total_ms=<ms>`. The steps are `on`, `state=`,
  `drop-caches`, `mark`, `off`, `renew`, `terminate`, `start-wsl`, `wsl-answers`. An `exit=-1` is a
  `wsl.exe` that could not be run; `FAILED(...)` is an exception. Read it with
  `Get-Content C:\ProgramData\millwright\giveway\giveway.log -Tail 20`. `state` and `mode` are beside it.
- **WSL: `~/.local/state/mw/giveway.log`** (`mw giveway status` prints the last 5 lines): one JSON line per
  `on`, `off`, renew, `expired` and `mark`, with `took_ms`, the sessions and processes stopped,
  `MemAvailable`, `load1` and the `--note` values (`windows_free_mb`, `windows_ms`, and the `by` of the
  caller: `windows-unlock`, `windows-lock`, `windows-tick`).

Right at unlock: the Windows line's `on` exit=0 and its `ms` a second or two at most, and the WSL line of
event `on` by `windows-unlock` within a couple of seconds of the unlock, with a `mark` ten seconds later whose
`windows_free_mb` is higher than the `on` line's. After the lock: an event `off` by `windows-lock`.

## Which event does what

| Windows event | Task that fires | What the host does |
|---|---|---|
| unlock | unlock | freezes (or shuts WSL down) |
| lock | lock | goes on (or starts WSL, then goes on) |
| logon (any user) | unlock | as unlock |
| logoff | none | nothing at once. After a logoff the sign-in screen is up, so the tick stops renewing, and the marker lapses after `giveway_stale` |
| console connect | unlock | as unlock |
| console disconnect | lock | as lock |
| remote connect / disconnect (RDP) | none | nothing: a person at a remote session is not read as "at the screen" |
| sleep, wake | none | nothing. Windows holds WSL still while asleep; after wake the lock screen asks for the password and unlock fires, or the tick renews if the session is still active |
| a restart | none (logon fires the unlock task after it) | WSL comes back with the marker file still on disk; the next `mw dispatch` tick lifts it when it is older than `giveway_stale` |

## What is NOT covered

- Her PC asleep or hibernating: nothing runs, and nothing needs to; WSL does not run either.
- WSL already down when an unlock comes: `wsl.exe --exec` starts it, `mw giveway on` freezes nothing, and the
  WSL keeper (`windows-hands.md`) is what keeps WSL up.
- A Windows restart (see the table), and a logoff: the host stays frozen for up to `giveway_stale`.
- Grace not signed in: the tasks are interactive tasks of her account, so with no session of hers they do
  not run. A different user at the screen while hers is signed in (fast user switching) is covered by
  console connect and disconnect.
- Remote desktop sessions, and anything the person does in WSL itself.
- The factory's own processes that `mw giveway on` does not stop (its Found-not-fixed list is on the first
  story, mw-2891blf.1).

## Checking it

Not yet run on the desktop. At the desktop, after the paste: lock the PC (Win+L), wait a minute, unlock it.
No window flashes, and browsing is as quick as with the factory off. Then read the two logs above.
