# millwright: restart or compact the desktop's WSL without a visit to the desk
# (mw-gq6.1039). WSL on the desktop is sealed from Windows (interop and /mnt/c
# off), so nothing inside it can start a Windows program. This runs OUTSIDE it,
# as a scheduled task of the Windows user that owns the distro (\\wsl.localhost
# is per user), every 2 minutes. It reads one flag file inside WSL and performs
# one of a FIXED set of actions: WSL can ask, never command.
#
#   wsl-flag content   action
#   restart            wsl --shutdown, then the task 'Start WSL (millwright)'
#   compact            fstrim -av inside, wsl --shutdown, diskpart compact of the
#                      vhdx (attach readonly / compact / detach), then the task
#   inventory          read only: write windows-inventory.txt beside the flag (the
#                      state lines, the sleeps, wakes and shutdowns of the last 7
#                      days from the System log in UTC, each physical disk, the
#                      last 40 lines of this task's log); nothing is changed
#   anything else      ignored and logged
#
# The flag is deleted BEFORE acting. After WSL is back the outcome and the time
# are written to wsl-flag.result beside it: '<UTC time> <action> ok|failed: ...'.
# `mw host wsl-flag restart|compact|inventory` writes the flag and waits for that
# file (the inventory: for windows-inventory.txt) and prints it;
# `mw host wsl-flag --install-line` prints the one admin-PowerShell line that
# installs this file and registers the task. Every tick, before it looks at the
# flag, it also writes two files beside it. windows-drive-free is one line,
# '<drive> <free bytes> <unix seconds>': the free space of the vhdx's drive,
# which nothing inside WSL can read; the wsl-drive-free check of `mw doctor`
# alarms under 5 GB (mw-gq6.1366). windows-state is one 'key value' line each of
# what is true of the Windows side (the time, this script's SHA-256, the boot
# time, a restart pending, the vhdx's drive and size, C: free, memory, the AC
# standby timeout, the WSL version, every 'millwright' task, each disk's health,
# .wslconfig); the windows-state check of `mw doctor` reads it and names what is
# wrong (mw-6ww.185.2). Log: %LOCALAPPDATA%\millwright\wsl-flag-watch.log
# Way back: Unregister-ScheduledTask -TaskName millwright-wsl-flag-watch -Confirm:$false;
#           Remove-Item "$env:ProgramData\millwright\wsl-flag-watch.ps1"   (no WSL change)
#
# -StateDryRun prints the state lines and does nothing else: no file is written
# and no directory made (the test runs it under Windows PowerShell 5.1).

param([switch]$StateDryRun)

$env:WSL_UTF8 = '1'
$distro  = 'Ubuntu-24.04'
$dir     = "\\wsl.localhost\$distro\home\jwhite\.local\state\mw"
$flag    = "$dir\wsl-flag"
$driveFree = "$dir\windows-drive-free"
$stateFile = "$dir\windows-state"
$inventory = "$dir\windows-inventory.txt"
$installed = "$env:ProgramData\millwright\wsl-flag-watch.ps1"
$result  = "$dir\wsl-flag.result"
$vhdx    = 'D:\wsl\Ubuntu-24.04\ext4.vhdx'
$startTask = 'Start WSL (millwright)'
$logDir  = "$env:LOCALAPPDATA\millwright"
$log     = "$logDir\wsl-flag-watch.log"
function Now { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss') + 'Z' }

# Run a script block with a time limit, so a frozen WSL cannot hold this task.
function Within($seconds, [scriptblock]$block, $arg) {
    $j = Start-Job -ScriptBlock $block -ArgumentList $arg
    $done = Wait-Job $j -Timeout $seconds
    $out = $null
    if ($done) { $out = Receive-Job $j -ErrorAction SilentlyContinue } else { Stop-Job $j }
    Remove-Job $j -Force
    return @{ Done = [bool]$done; Out = $out }
}

# One value of the state: what the block prints, or 'unknown' when it fails or prints nothing.
function Value($block) {
    try { $v = & $block; if ($null -eq $v -or "$v".Trim() -eq '') { return 'unknown' }; return "$v".Trim() } catch { return 'unknown' }
}

function Restart-Pending {
    foreach ($k in @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired',
                     'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')) {
        if (Test-Path -LiteralPath $k) { return $true }
    }
    return $false
}

# The state of the Windows side, one 'key value' line each. With $persist it keeps
# when a restart was first seen pending in a file of its own beside the log; with
# none (the dry run) it writes nothing.
function Get-WindowsStateLines($persist) {
    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("time $(Now)")
    $self = if ($PSCommandPath) { $PSCommandPath } else { $installed }
    $hash = Value { if (Test-Path -LiteralPath $self) { (Get-FileHash -Algorithm SHA256 -LiteralPath $self).Hash } else { 'none' } }
    $lines.Add("script_sha256 $hash")
    $boot = Value { (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss') + 'Z' }
    $lines.Add("boot_time $boot")
    $pending = $false
    try { $pending = Restart-Pending } catch { }
    $sinceFile = "$logDir\restart-pending-since.txt"
    $since = 'none'
    if ($pending) {
        $first = $null
        try { if (Test-Path -LiteralPath $sinceFile) { $first = ([IO.File]::ReadAllText($sinceFile)).Trim() } } catch { }
        # a file older than the last boot is from a restart that has since happened
        if (-not $first -or ($boot -ne 'unknown' -and $first -lt $boot)) {
            $first = Now
            if ($persist) { try { [IO.File]::WriteAllText($sinceFile, $first) } catch { } }
        }
        $since = $first
    } elseif ($persist) {
        try { [IO.File]::Delete($sinceFile) } catch { }
    }
    $lines.Add("restart_pending $(if ($pending) { 'yes' } else { 'no' })")
    $lines.Add("restart_pending_since $since")
    $letter = (Split-Path -Qualifier $vhdx).TrimEnd(':')
    $vd = Value { $d = Get-PSDrive $letter -ErrorAction Stop; $letter + ' ' + ($d.Used + $d.Free) + ' ' + $d.Free }
    $lines.Add("vhdx_drive $vd")
    $cfree = Value { (Get-PSDrive C -ErrorAction Stop).Free }
    $lines.Add("c_free $cfree")
    $vsize = Value { if (Test-Path -LiteralPath $vhdx) { (Get-Item -LiteralPath $vhdx).Length } else { 'missing' } }
    $lines.Add("vhdx_size $vsize")
    $mtotal = Value { [int64](Get-CimInstance Win32_OperatingSystem).TotalVisibleMemorySize * 1024 }
    $lines.Add("memory_total $mtotal")
    $mfree = Value { [int64](Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory * 1024 }
    $lines.Add("memory_free $mfree")
    # the last two 0x values of the query are the AC and DC setting, in seconds
    $standby = Value {
        $q = powercfg /q SCHEME_CURRENT SUB_SLEEP STANDBYIDLE | Out-String
        $hex = @([regex]::Matches($q, '0x[0-9a-fA-F]+') | ForEach-Object { $_.Value })
        [Convert]::ToInt64($hex[$hex.Count - 2].Substring(2), 16)
    }
    $lines.Add("ac_standby_timeout $standby")
    $wslv = Value {
        $r = Within 20 { wsl.exe --version 2>&1 | Select-Object -First 1 } $null
        $first = ("$($r.Out)" -replace "`0", '')
        if ($r.Done -and $first -match '(\d+(\.\d+)+)') { $Matches[1] }
    }
    $lines.Add("wsl_version $wslv")
    try {
        foreach ($t in @(Get-ScheduledTask -ErrorAction Stop | Where-Object { $_.TaskName -like '*millwright*' })) {
            $i = Get-ScheduledTaskInfo -TaskName $t.TaskName -TaskPath $t.TaskPath -ErrorAction SilentlyContinue
            $lines.Add("task $($t.TaskName)|$($t.State)|$($i.LastTaskResult)|$($t.Principal.LogonType)")
        }
    } catch { $lines.Add("task unknown|unknown|unknown|unknown") }
    try {
        foreach ($d in @(Get-PhysicalDisk -ErrorAction Stop)) { $lines.Add("disk $($d.FriendlyName)|$($d.MediaType)|$($d.HealthStatus)") }
    } catch { }
    $cfg = @{}
    try {
        $cfgPath = Join-Path $env:USERPROFILE '.wslconfig'
        if (Test-Path -LiteralPath $cfgPath) {
            foreach ($l in [IO.File]::ReadAllLines($cfgPath)) {
                if ($l -match '^\s*(memory|processors|swap)\s*=\s*(.+?)\s*(#.*)?$') { $cfg[$Matches[1].ToLower()] = $Matches[2] }
            }
        }
    } catch { }
    $lines.Add("wslconfig_memory $(if ($cfg.memory) { $cfg.memory } else { 'unset' })")
    $lines.Add("wslconfig_processors $(if ($cfg.processors) { $cfg.processors } else { 'unset' })")
    $lines.Add("wslconfig_swap $(if ($cfg.swap) { $cfg.swap } else { 'unset' })")
    return $lines.ToArray()
}

if ($StateDryRun) { Get-WindowsStateLines $false | ForEach-Object { $_ }; return }

New-Item -ItemType Directory -Force $logDir | Out-Null

function Log($m) { "$(Now) $m" | Add-Content $log }

# What kind of power event a System log event is, or $null when it is none of them.
function Event-Kind($e) {
    $p = "$($e.ProviderName)"
    if ($e.Id -eq 42 -and $p -like '*Kernel-Power*') { return 'sleep' }
    if ($e.Id -eq 1 -and $p -like '*Power-Troubleshooter*') { return 'wake' }
    if ($e.Id -eq 1074 -or $e.Id -eq 6006) { return 'shutdown' }
    if (($e.Id -eq 41 -and $p -like '*Kernel-Power*') -or $e.Id -eq 6008) { return 'unexpected-shutdown' }
    return $null
}

# The long, read-only form of the state: the state lines, the sleeps, wakes,
# shutdowns and unexpected shutdowns of the last 7 days in UTC, each physical
# disk, and the last 40 lines of this task's log.
function Get-Inventory {
    $o = New-Object System.Collections.Generic.List[string]
    $o.Add('== state ==')
    foreach ($l in @(Get-WindowsStateLines $true)) { $o.Add($l) }
    $o.Add('== sleeps, wakes, shutdowns and unexpected shutdowns of the last 7 days (UTC) ==')
    try {
        $events = @(Get-WinEvent -FilterHashtable @{ LogName = 'System'; Id = 1, 42, 41, 1074, 6006, 6008; StartTime = (Get-Date).AddDays(-7) } -ErrorAction Stop | Sort-Object TimeCreated)
        foreach ($e in $events) {
            $kind = Event-Kind $e
            if (-not $kind) { continue }
            $msg = ("$($e.Message)" -split "[\r\n]+")[0]
            if ($msg.Length -gt 120) { $msg = $msg.Substring(0, 120) }
            $o.Add("$($e.TimeCreated.ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss'))Z $kind id=$($e.Id) $msg")
        }
    } catch { $o.Add("none found, or the System log could not be read: $($_.Exception.Message)") }
    $o.Add('== physical disks ==')
    try {
        foreach ($d in @(Get-PhysicalDisk -ErrorAction Stop)) {
            $o.Add("$($d.FriendlyName) MediaType=$($d.MediaType) HealthStatus=$($d.HealthStatus) OperationalStatus=$($d.OperationalStatus -join ',') Size=$($d.Size)")
        }
    } catch { $o.Add("the disks could not be read: $($_.Exception.Message)") }
    $o.Add('== the last 40 lines of this task''s log ==')
    try { if (Test-Path -LiteralPath $log) { foreach ($l in @(Get-Content -LiteralPath $log -Tail 40)) { $o.Add("$l") } } } catch { }
    return $o.ToArray()
}

# Write text to a file inside WSL whole, so that a reader never sees half of it.
$writeWhole = { param($f, $t) $tmp = "$f.tmp"; [IO.File]::WriteAllText($tmp, $t, (New-Object Text.UTF8Encoding $false)); Move-Item -LiteralPath $tmp -Destination $f -Force }

function Wsl-Up {
    $r = Within 40 { wsl.exe -d $using:distro -e true; $LASTEXITCODE } $null
    return ($r.Done -and "$($r.Out)".Trim() -eq '0')
}

# Wait up to 5 minutes for WSL to answer, then write the result file into it.
function Finish($action, $outcome) {
    $line = "$(Now) $action $outcome"
    for ($i = 0; $i -lt 20; $i++) {
        if (Wsl-Up) {
            try { Set-Content -LiteralPath $result -Value $line -ErrorAction Stop; Log $line; return } catch { }
        }
        Start-Sleep 15
    }
    Log "$line (WSL did not come back in 5 minutes; result not written)"
}

function Start-Wsl {
    Start-ScheduledTask -TaskName $startTask
    Start-Sleep 20
}

# Report the vhdx drive's free space into WSL (a failure is logged, never fatal).
try {
    $letter = (Split-Path -Qualifier $vhdx).TrimEnd(':')
    $report = "$($letter): $((Get-PSDrive $letter -ErrorAction Stop).Free) $([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
    $w = Within 30 { param($f, $t) Set-Content -LiteralPath $f -Value $t -ErrorAction Stop } @($driveFree, $report)
    if (-not $w.Done) { Log 'the drive-free report did not finish in 30 seconds' }
} catch { Log "the drive-free report failed: $($_.Exception.Message)" }

# Report the Windows side's state into WSL (a failure is logged, never fatal).
try {
    $windowsState = (@(Get-WindowsStateLines $true) -join "`n") + "`n"
    $w = Within 30 $writeWhole @($stateFile, $windowsState)
    if (-not $w.Done) { Log 'the state report did not finish in 30 seconds' }
} catch { Log "the state report failed: $($_.Exception.Message)" }

$read = Within 60 { param($f) if (Test-Path -LiteralPath $f) { Get-Content -LiteralPath $f -Raw } } $flag
if (-not $read.Done -or $null -eq $read.Out) { exit 0 }
$want = ("$($read.Out)").Trim()

# Delete the flag first, so that one request is one action.
try { Remove-Item -LiteralPath $flag -Force -ErrorAction Stop }
catch { Log "could not delete the flag ($($_.Exception.Message)); not acting"; exit 1 }

if ($want -ne 'restart' -and $want -ne 'compact' -and $want -ne 'inventory') {
    $shown = if ($want.Length -gt 40) { $want.Substring(0, 40) } else { $want }
    Log "ignored flag content: '$shown'"
    try { Set-Content -LiteralPath $result -Value "$(Now) ignored: the flag held '$shown'" -ErrorAction Stop } catch { }
    exit 0
}

if ($want -eq 'inventory') {
    Log 'inventory: begin'
    try {
        $text = (@(Get-Inventory) -join "`n") + "`n"
        $w = Within 60 $writeWhole @($inventory, $text)
        if (-not $w.Done) { throw 'the write into WSL did not finish in 60 seconds' }
        Log 'inventory: written'
        try { Set-Content -LiteralPath $result -Value "$(Now) inventory ok" -ErrorAction Stop } catch { }
        exit 0
    } catch {
        Log "inventory failed: $($_.Exception.Message)"
        try { Set-Content -LiteralPath $result -Value "$(Now) inventory failed: $($_.Exception.Message)" -ErrorAction Stop } catch { }
        exit 1
    }
}

if (-not (Get-ScheduledTask -TaskName $startTask -ErrorAction SilentlyContinue)) {
    Log "$want refused: no '$startTask' task, nothing would start WSL again"
    try { Set-Content -LiteralPath $result -Value "$(Now) $want failed: no '$startTask' task to start WSL again; WSL left as it was" -ErrorAction Stop } catch { }
    exit 1
}

Log "${want}: begin"
if ($want -eq 'restart') {
    wsl.exe --shutdown
    Start-Sleep 15
    Start-Wsl
    Finish 'restart' 'ok'
    exit 0
}

# compact
$before = if (Test-Path -LiteralPath $vhdx) { (Get-Item -LiteralPath $vhdx).Length } else { 0 }
$trim = (wsl.exe -d $distro -u root -- fstrim -av 2>&1) -join ' '
$trimExit = $LASTEXITCODE
Log "fstrim exit $trimExit : $trim"
wsl.exe --shutdown
Start-Sleep 15
$failure = $null
if (-not (Test-Path -LiteralPath $vhdx)) {
    $failure = "$vhdx not found"
} else {
    $sc = Join-Path $env:TEMP 'mw-wsl-compact.txt'
    @('select vdisk file="' + $vhdx + '"', 'attach vdisk readonly', 'compact vdisk', 'detach vdisk', 'exit') | Set-Content -Encoding ASCII $sc
    $o = (diskpart.exe /s $sc 2>&1) -join ' '
    $code = $LASTEXITCODE
    Log "diskpart exit $code : $o"
    if ($code -ne 0 -or $o -match 'DiskPart has encountered an error') {
        $failure = "diskpart exit $code"
        # an error stops the script before detach: release the disk
        @('select vdisk file="' + $vhdx + '"', 'detach vdisk', 'exit') | Set-Content -Encoding ASCII $sc
        diskpart.exe /s $sc | Out-Null
    }
    Remove-Item $sc -ErrorAction SilentlyContinue
}
$after = if (Test-Path -LiteralPath $vhdx) { (Get-Item -LiteralPath $vhdx).Length } else { 0 }
Start-Wsl
$gb = { param($b) [math]::Round($b / 1GB, 1) }
$sizes = "vhdx $(& $gb $before) GB -> $(& $gb $after) GB, fstrim exit $trimExit"
if ($failure) { Finish 'compact' "failed: $failure ($sizes); WSL started again" } else { Finish 'compact' "ok: $sizes" }
