# Research: the desktop is also Grace's PC

Answers mw-gq6.399. The Governor, 2026-10-13 ~13:22Z (Talk d1274929 turn 4): his wife says the
desktop is slow to browse the web when she uses it, and he is "sure that's all the message that we're
giving it". Research, one `.wslconfig` paste, and one new mw setting.

## What is happening

Read on the desktop over ssh, 2026-10-13 09:44 (local) and 13:2x UTC:

| Fact | Reading |
|---|---|
| CPU | `Intel(R) Core(TM) i7-10700`: 8 cores, 16 threads (`/proc/cpuinfo`, `nproc` = 16) |
| WSL2 VM | 16 CPUs, `MemTotal` 12.2 GB, swap 8 GB, kernel `6.18.33.2-microsoft-standard-WSL2` |
| Load | 8.75 at the Mayor's first look; 9.93, 14.85, 15.02 (1, 5, 15 minutes) later: the VM kept every thread busy for a quarter of an hour |
| Builders | `nice -n 10` (`builder_nice`), 4 at once (`cap = 4`, set 2026-10-10) |
| Windows side | Not readable from the guest: `/etc/wsl.conf` has `[interop] enabled=false` and `[automount] enabled=false`, so no `powershell.exe` from WSL. Windows' RAM and whether a `.wslconfig` exists are **unknown to this note** |

The cause is that the VM is a Hyper-V guest. Its 16 vCPUs are threads of one Windows process
(`vmmemWSL`) that Windows schedules against Grace's browser at **normal priority**. `nice` is a Linux
scheduler setting: it only orders the guest's own processes among themselves, and the guest sees
no one else to yield to, so Windows sees a VM asking for all 16 threads, flat out. In-guest `nice`
cannot fix this, and no mw setting inside WSL can either. Two things can: fewer vCPUs given to the
VM (`.wslconfig`), and fewer things running inside it (`cap`, and the new `builder_jobs`).

## 1. `.wslconfig` for Grace's account

`%UserProfile%\.wslconfig` (the Windows account WSL runs under, `grace` per
[windows-hands.md](windows-hands.md)) is read when the WSL VM starts, so it takes effect only after
`wsl --shutdown`.

**`processors=8`.** Half the threads. The VM can then never ask Windows for more than 8 threads' worth
of CPU, so the browser always has the other 8. The vCPUs are not pinned to particular threads: this
bounds how much of the PC the VM can use, not which threads. If the page is still slow with it,
lower it to 6 (leaves 10); a Builder's test run takes longer, the PC's owner does not notice.

**`memory=10GB`, only if the file has no `memory=` line.** The guest used about 5 GB at load 10 with
four builds plus 2.7 GB of cache, and it sees 12 GB. Memory is not what makes browsing stall (CPU is);
this just hands 2 GB back if nobody has chosen a number. The swap of 8 GB the guest shows is not a
WSL default for a 12 GB VM, which suggests a `.wslconfig` already exists with its own `memory=` and
`swap=`: the paste therefore **keeps every line already in the file** and adds a `memory=` only when
there is none. (That is an inference, not a reading.)

The paste is written for a **new Windows PowerShell** (`PS C:\Users\grace>`), not WSL. It copies the
file aside first, changes only the `[wsl2]` section, shows the result, then waits for Enter before the
restart, because `wsl --shutdown` ends every session on the desktop and takes it off the network
for about a minute (WireGuard 10.88.0.3 lives inside WSL; see
[windows-hands.md](windows-hands.md)). Do it when no story is running there (`mw status`), and
let the Mayor know. It starts the distro again afterwards with `sleep infinity`, so it does not stay
off until someone opens a terminal. It has not been run on Windows: the author had no PowerShell to
run it in. Read the printed file before pressing Enter.

```powershell
$path = Join-Path $env:USERPROFILE '.wslconfig'
$backup = "$path.bak-" + (Get-Date -Format 'yyyyMMddTHHmmss')
$had = Test-Path $path
if ($had) { Copy-Item $path $backup }
$lines = [System.Collections.Generic.List[string]]::new()
if ($had) { foreach ($l in (Get-Content $path)) { $lines.Add($l) } }
$start = -1
for ($i = 0; $i -lt $lines.Count; $i++) { if ($lines[$i].Trim() -ieq '[wsl2]') { $start = $i; break } }
if ($start -lt 0) { $lines.Add('[wsl2]'); $start = $lines.Count - 1 }
$end = $lines.Count
for ($i = $start + 1; $i -lt $lines.Count; $i++) { if ($lines[$i].Trim().StartsWith('[')) { $end = $i; break } }
$hasProc = $false; $hasMem = $false
for ($i = $start + 1; $i -lt $end; $i++) {
  if ($lines[$i] -match '^\s*processors\s*=') { $lines[$i] = 'processors=8'; $hasProc = $true }
  if ($lines[$i] -match '^\s*memory\s*=') { $hasMem = $true }
}
$add = @()
if (-not $hasProc) { $add += 'processors=8' }
if (-not $hasMem) { $add += 'memory=10GB' }
if ($add.Count -gt 0) { $lines.InsertRange($start + 1, [string[]]$add) }
Set-Content -Path $path -Value $lines -Encoding ASCII
if ($had) { $back = "Copy-Item '$backup' '$path' -Force; wsl --shutdown" } else { $back = "Remove-Item '$path'; wsl --shutdown" }
Write-Host "---- $path ----"; Get-Content $path
Write-Host "WAY BACK (then start WSL again): $back"
Read-Host 'Enter = restart WSL now (the desktop is off the network for about a minute); Ctrl+C = leave it for later'
$distro = ((wsl -l -q) -replace "`0", '' | Where-Object { $_.Trim() } | Select-Object -First 1).Trim()
wsl --shutdown
Start-Sleep -Seconds 8
Start-Process wsl.exe -ArgumentList '-d', $distro, '--exec', 'sleep', 'infinity' -WindowStyle Hidden
Start-Sleep -Seconds 10
wsl -d $distro --exec nproc
```

Right looks like: the printed file has `processors=8` under `[wsl2]` beside whatever was there, and the last
line prints `8`. Then, on the desktop, `nproc` says 8 and `mw status` shows the factory back.

**Way back**, if the desktop misbehaves or builds are too slow: the paste printed its own line
(`WAY BACK: ...`) with the dated backup's name; paste that line into a Windows PowerShell. It puts the
old file back, or removes the file if there was none, and shuts WSL down; start it as the paste did:

```powershell
$distro = ((wsl -l -q) -replace "`0", '' | Where-Object { $_.Trim() } | Select-Object -First 1).Trim()
Start-Process wsl.exe -ArgumentList '-d', $distro, '--exec', 'sleep', 'infinity' -WindowStyle Hidden
```

If the desktop does not come back by itself within a few minutes, run only that second block. If the
`millwright-wsl-keeper` task of [windows-hands.md](windows-hands.md) has been registered, it does it
within five minutes without being asked.

A consequence worth knowing: `mw dispatch` reads `runtime.NumCPU()` as the host's cores for its room
check (`room_load_per_core`), and the guest's CPU count is now 8, so the check tightens by itself:
no story starts at load 8 where it started at 16.

Not in the paste, and not verified: lowering the *Windows* priority of the `vmmemWSL` process
(`(Get-Process vmmemWSL).PriorityClass = 'BelowNormal'`) would be a nice that Windows can see; it is
lost at every VM restart, so it needs a task to redo it. `processors=` is the setting that holds.

## 2. Should test runners be capped by a per-host setting? Yes, one key

`processors=8` bounds the VM, but inside it a Builder's tests still start as many workers as the
guest has CPUs, and `cap` Builders do so at once: `go test -p`, vitest's pool, playwright's workers all
default to the CPU count. Four Builders times 16 workers is what held the load at 15. With the VM at 8
and `cap = 2`, two test runs of 8 workers each would still fill the VM. A per-host cap on the *workers*
is the missing half, and it must be per host: the Laptop and the VPS want their defaults.

**`builder_jobs`** (`MW_BUILDER_JOBS`; root table of `~/.config/mw/config.toml`). A whole number of 1 or
more; unset is no limit, so every other host is unchanged. When set, a Builder's session and every
rig's test command that `mw next`, `mw check` and `mw suite` run get:

| Variable | Read by |
|---|---|
| `JOBS` | millwright's `Makefile`, which turns it into `GOFLAGS=-p=$(JOBS)` and `GOMAXPROCS` (`JOBS ?= nproc`, so the environment wins) |
| `GOMAXPROCS` | go itself, for a rig whose Makefile does not read `JOBS` (`go test`'s `-p` defaults to it) |
| `VITEST_MAX_WORKERS` | vitest. Documented only in vitest v4's [improving performance](https://v4.vitest.dev/guide/improving-performance) guide; older vitest reads `VITEST_MAX_THREADS` / `VITEST_MAX_FORKS` instead, which this does not set |

It is **not** set for the commands that run after a landing (build, deploy). Playwright has no
environment variable for workers; a rig that wants one reads `JOBS` in its own `playwright.config`
(`workers: process.env.JOBS ? Number(process.env.JOBS) : undefined`). That is a story for that rig,
not mw's.

For the desktop, with `processors=8`: `cap = 2` (the Mayor's step, done 2026-10-13) and
`builder_jobs = 4`, so two Builders at four workers fill the VM exactly and no more:

Add the line `builder_jobs = 4` to the root of `~/.config/mw/config.toml` on the desktop, above the
first `[table]` header, beside `cap` (way back: delete the line). `mw config set builder_jobs 4`
cannot do it yet: this build refuses a key outside a table (`"builder_jobs" needs a table: say
<table>.<key>`), and so would refuse `mw config set cap 2`; the desktop's `cap = 2` line, with its
dated comment, got there some other way. Or, for one shell, `MW_BUILDER_JOBS=4`.

It applies to sessions started after the change and to every later gate; a story already running keeps the
environment it started with. If Grace is away, `mw config set cap 4` and `mw config set builder_jobs 8`
give the Builders the VM back; there is no clock in mw that does that by itself, and none was added.
