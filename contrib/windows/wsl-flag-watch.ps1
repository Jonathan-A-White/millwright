# millwright: keep the desktop's WSL up by itself, and restart or compact it
# without a visit to the desk (mw-gq6.1039, mw-6ww.185.3). WSL on the desktop is
# sealed from Windows (interop and /mnt/c off), so nothing inside it can start a
# Windows program. This runs OUTSIDE it, as ONE scheduled task of the Windows user
# that owns the distro, at startup and every 2 minutes, WITH NOBODY SIGNED IN (a
# task of logon type S4U: no password stored). It replaces the tasks
# millwright-wsl-keeper, millwright-wsl-watchdog and the logon task that started WSL.
# Everything of WSL's it reads or writes goes through wsl.exe, never the per-user WSL share
# (a task with no session has no per-user share).
#
# Each tick, BEFORE it looks at the flag, it keeps WSL up, and does at most ONE act:
#   down until T      (see 'down' below) nothing is asked of WSL, nothing started;
#                     once T has passed WSL is started again: 'down: started again'
#   not running       a distro 'wsl.exe -l --running' does not list is started:
#                     'not running: started'
#   hung              a distro that fails, or does not answer 'wsl.exe -d <distro>
#                     --exec true' in 40 seconds, on two ticks in a row is shut down
#                     and started again: 'hung: restarted'
# To start the distro it runs 'wsl.exe -d <distro> --exec sleep infinity' as a
# process of its own (Win32_Process.Create, so that the task's job object does not
# end it with the tick): that process holds the distro up.
# Every act is one line, '<UTC time> <what>: <outcome>', written into the file
# windows-acts beside the flag once WSL answers (until then it waits in a file
# beside the log); `mw doctor windows-state` names the newest act of the last 24
# hours. After a good tick the task writes last-good-tick beside the log: the
# install line waits for it, then removes the keeper and the watchdog.
#
#   wsl-flag content   action
#   restart            wsl --shutdown, then start the distro as above
#   compact            fstrim -av inside, wsl --shutdown, diskpart compact of the
#                      vhdx (attach readonly / compact / detach), then start it
#   inventory          read only: write windows-inventory.txt beside the flag (the
#                      state lines, the sleeps, wakes and shutdowns of the last 7
#                      days from the System log in UTC, each physical disk, the
#                      last 40 lines of this task's log); nothing is changed
#   down <seconds>     300 to 86400 only: shut WSL down, start nothing until that
#                      long has passed ('down until T' in the log), then start it
#                      again and write the act; the end is kept in a file beside
#                      the log, so a restart of Windows does not forget it
#   anything else      ignored and logged
#
# The flag is deleted BEFORE acting. After WSL is back the outcome and the time
# are written to wsl-flag.result beside it: '<UTC time> <action> ok|failed: ...'.
# `mw host wsl-flag restart|compact|inventory|down --for D` writes the flag (the
# first three then wait for that file, the inventory for windows-inventory.txt) and
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
# wrong (mw-6ww.185.2). Log and the files of its own: %ProgramData%\millwright\
# Way back: Unregister-ScheduledTask -TaskName millwright-wsl-flag-watch -Confirm:$false;
#           Remove-Item "$env:ProgramData\millwright\wsl-flag-watch.ps1"   (no WSL change)
#
# -StateDryRun prints the state lines and does nothing else: no file is written
# and no directory made. -DryRun runs -Ticks ticks (default 1) against a stand-in
# for wsl.exe and prints what each would do, writing nothing: -Assume running (WSL
# answers), stopped (not running), hung (listed, never answers) or down (the flag
# holds 'down' -DownFor seconds on the first tick). The tests run both under
# Windows PowerShell 5.1.

param([switch]$StateDryRun,
      [switch]$DryRun,
      [ValidateSet('running', 'stopped', 'hung', 'down')][string]$Assume = 'running',
      [ValidateRange(1, 20)][int]$Ticks = 1,
      [int]$DownFor = 600)
$env:WSL_UTF8 = '1'
$distro    = 'Ubuntu-24.04'
$wslUser   = 'jwhite'
$wslDir    = '/home/jwhite/.local/state/mw'
$flag      = "$wslDir/wsl-flag"
$driveFree = "$wslDir/windows-drive-free"
$stateFile = "$wslDir/windows-state"
$inventory = "$wslDir/windows-inventory.txt"
$result    = "$wslDir/wsl-flag.result"
$actsFile  = "$wslDir/windows-acts"
$installed = "$env:ProgramData\millwright\wsl-flag-watch.ps1"
$vhdx      = 'D:\wsl\Ubuntu-24.04\ext4.vhdx'
$logDir    = "$env:ProgramData\millwright"
$log       = "$logDir\wsl-flag-watch.log"
$script:skew = 0
function Now-Utc { (Get-Date).ToUniversalTime().AddSeconds($script:skew) }
function Now { (Now-Utc).ToString('yyyy-MM-ddTHH:mm:ss') + 'Z' }

# Run a native command (wsl.exe) with a time limit, so a frozen WSL cannot hold this task:
# $cmd is the program and its arguments as one command line, $stdin its input when given.
# A command still running at the limit is killed. Returns Done (it ended in time), Code
# (its exit code) and Out (what it printed, with no NUL: wsl.exe may print UTF-16).
function Run-Native([string]$cmd, [int]$seconds, [string]$stdin) {
    if ($DryRun) { return (Wsl-StandIn $cmd) }
    $exe, $arguments = $cmd -split ' ', 2
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $exe
    $psi.Arguments = "$arguments"
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.StandardOutputEncoding = [Text.Encoding]::UTF8
    $p = $null
    try { $p = [Diagnostics.Process]::Start($psi) } catch { return @{ Done = $true; Code = -1; Out = "$($_.Exception.Message)" } }
    $out = $p.StandardOutput.ReadToEndAsync()
    $err = $p.StandardError.ReadToEndAsync()
    $ms = $seconds * 1000
    try {
        if ($stdin) {
            $bytes = (New-Object Text.UTF8Encoding $false).GetBytes($stdin)
            $w = $p.StandardInput.BaseStream.WriteAsync($bytes, 0, $bytes.Length)
            if (-not $w.Wait($ms)) { throw 'the input was not taken in time' }
        }
        $p.StandardInput.Close()
    } catch { try { $p.Kill() } catch { }; return @{ Done = $false; Code = -1; Out = '' } }
    if (-not $p.WaitForExit($ms)) { try { $p.Kill() } catch { }; return @{ Done = $false; Code = -1; Out = '' } }
    $p.WaitForExit()
    return @{ Done = $true; Code = $p.ExitCode; Out = ("$($out.Result)" -replace "`0", '') }
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
        $r = Run-Native 'wsl.exe --version' 20 $null
        $first = ("$($r.Out)" -split "[\r\n]+" | Where-Object { $_.Trim() } | Select-Object -First 1)
        if ($r.Done -and "$first" -match '(\d+(\.\d+)+)') { $Matches[1] }
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
        $prof = if ($env:USERPROFILE) { $env:USERPROFILE } else { "$env:SystemDrive\Users\$env:USERNAME" }
        $cfgPath = Join-Path $prof '.wslconfig'
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

if ($DryRun) { $script:dryUp = ($Assume -ne 'stopped'); $script:dryCured = $false; $script:tick = 0 }
$script:mem = @{}

function Say([string]$m) { if ($DryRun) { Write-Host $m } }
function Pause([int]$seconds) { if (-not $DryRun) { Start-Sleep $seconds } }

function Log($m) {
    if ($DryRun) { Write-Host "log $m"; return }
    try { "$(Now) $m" | Add-Content -LiteralPath $log } catch { }
}

# The stand-in for wsl.exe of a dry run: what a distro that is stopped, answering or hung
# would reply to the commands the tick sends; it prints each command it is sent.
function Wsl-StandIn([string]$cmd) {
    Write-Host "DRYRUN $cmd"
    if ($cmd -match '--shutdown') { $script:dryUp = $false; $script:dryCured = $true; return @{ Done = $true; Code = 0; Out = '' } }
    if ($cmd -match '-l --running') { return @{ Done = $true; Code = 0; Out = $(if ($script:dryUp) { $distro } else { '' }) } }
    if ($cmd -match '--exec true') {
        $ok = $script:dryUp -and ($Assume -ne 'hung' -or $script:dryCured)
        return @{ Done = $true; Code = $(if ($ok) { 0 } else { 1 }); Out = '' }
    }
    return @{ Done = $true; Code = 0; Out = '' }
}

# The files of this task's own, beside its log (on Windows): what it keeps between ticks.
function Store-Get([string]$name) {
    if ($DryRun) { return $script:mem[$name] }
    try { $f = Join-Path $logDir $name; if (Test-Path -LiteralPath $f) { return [IO.File]::ReadAllText($f) } } catch { }
    return $null
}
function Store-Set([string]$name, [string]$text) {
    if ($DryRun) { $script:mem[$name] = $text; return }
    try { [IO.File]::WriteAllText((Join-Path $logDir $name), $text) } catch { }
}
function Store-Del([string]$name) {
    if ($DryRun) { $script:mem.Remove($name); return }
    try { [IO.File]::Delete((Join-Path $logDir $name)) } catch { }
}

# A UTC time of the form the files hold, or $null.
function ConvertTo-Utc($text) {
    try { return [DateTime]::ParseExact("$text".Trim(), "yyyy-MM-dd'T'HH:mm:ss'Z'", [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal -bor [Globalization.DateTimeStyles]::AdjustToUniversal) } catch { return $null }
}

# Files inside WSL, through wsl.exe: the script runs in 'sh -s' as the WSL user and the text
# goes in as base64, so no quoting can break it. A write is whole (a temporary file, then mv).
function B64([string]$text) { return [Convert]::ToBase64String((New-Object Text.UTF8Encoding $false).GetBytes($text)) }
function Wsl-Sh([string]$body, [int]$seconds) { return (Run-Native "wsl.exe -d $distro -u $wslUser --exec sh -s" $seconds $body) }
function Wsl-Write([string]$path, [string]$text) {
    if ($DryRun) { Write-Host "DRYRUN write $path"; return $true }
    $r = Wsl-Sh "set -e`nmkdir -p '$wslDir'`nbase64 -d > '$path.tmp' <<'MW_EOF'`n$(B64 $text)`nMW_EOF`nmv -f '$path.tmp' '$path'`n" 60
    return ($r.Done -and $r.Code -eq 0)
}
function Wsl-Append([string]$path, [string]$text) {
    if ($DryRun) { Write-Host "DRYRUN append $path"; return $true }
    $r = Wsl-Sh "set -e`nmkdir -p '$wslDir'`nbase64 -d >> '$path' <<'MW_EOF'`n$(B64 $text)`nMW_EOF`ntail -n 500 '$path' > '$path.tmp'`nmv -f '$path.tmp' '$path'`n" 60
    return ($r.Done -and $r.Code -eq 0)
}
# The text of a file inside WSL, or $null when it is not there or WSL did not answer.
function Wsl-Read([string]$path) {
    if ($DryRun) {
        if ($path -eq $flag -and $Assume -eq 'down' -and $script:tick -eq 1) { return "down $DownFor" }
        return $null
    }
    $r = Wsl-Sh "if [ -f '$path' ]; then cat '$path'; fi`n" 60
    if (-not $r.Done -or $r.Code -ne 0 -or "$($r.Out)" -eq '') { return $null }
    return "$($r.Out)"
}
function Wsl-Delete([string]$path) {
    if ($DryRun) { Write-Host "DRYRUN delete $path"; return $true }
    $r = Wsl-Sh "rm -f '$path'`n" 30
    return ($r.Done -and $r.Code -eq 0)
}

# One act: a line '<UTC time> <what>: <outcome>'. WSL may be down, so it waits in a file of
# its own until Flush-Acts can write it into windows-acts.
function Act([string]$what, [string]$outcome) {
    $line = "$(Now) ${what}: $outcome"
    if ($DryRun) { Write-Host "act $line"; return }
    Log "act: $line"
    Store-Set 'windows-acts-pending' ("$(Store-Get 'windows-acts-pending')$line`n")
}
function Flush-Acts {
    if ($DryRun) { return }
    $pending = Store-Get 'windows-acts-pending'
    if (-not $pending) { return }
    if (Wsl-Append $actsFile $pending) { Store-Del 'windows-acts-pending' } else { Log 'the acts could not be written into WSL yet; kept for the next tick' }
}

# Does the distro answer 'wsl.exe -d <distro> --exec true' within 40 seconds?
function Wsl-Answers {
    $r = Run-Native "wsl.exe -d $distro --exec true" 40 $null
    return ($r.Done -and $r.Code -eq 0)
}
function Wait-WslAnswers([int]$seconds) {
    $until = (Get-Date).AddSeconds($seconds)
    while ($true) {
        if (Wsl-Answers) { return $true }
        if ((Get-Date) -ge $until) { return $false }
        Pause 5
    }
}
# $true when 'wsl.exe -l --running' lists the distro, $false when it does not, $null when it did not say.
function Wsl-Listed {
    $r = Run-Native 'wsl.exe -l --running -q' 20 $null
    if (-not $r.Done) { return $null }
    foreach ($l in ("$($r.Out)" -split "[\r\n]+")) { if ($l.Trim() -eq $distro) { return $true } }
    return $false
}

# Start the distro and leave a process of it running that holds it up: 'sleep infinity' inside.
# Win32_Process.Create makes it a process of WMI's, so the task's job object does not end it
# when the tick ends; -Plain starts it as a child of this script instead.
function Start-Wsl([switch]$Plain) {
    if ($DryRun) { Write-Host "DRYRUN wsl.exe -d $distro --exec sleep infinity"; $script:dryUp = $true; return }
    if (-not $Plain) {
        try {
            $r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = "`"$env:SystemRoot\System32\wsl.exe`" -d $distro --exec sleep infinity" } -ErrorAction Stop
            if ($r.ReturnValue -eq 0) { return }
            Log "Win32_Process.Create returned $($r.ReturnValue)"
        } catch { Log "Win32_Process.Create failed: $($_.Exception.Message)" }
    }
    try { Start-Process -FilePath wsl.exe -ArgumentList '-d', $distro, '--exec', 'sleep', 'infinity' -WindowStyle Hidden } catch { Log "starting WSL failed: $($_.Exception.Message)" }
}
# Start it and wait for it to answer; if it does not, start it the other way once more.
function Bring-Up {
    Start-Wsl
    if (Wait-WslAnswers 45) { return $true }
    Log 'WSL did not answer 45 seconds after it was started; starting it again as a child of this task'
    Start-Wsl -Plain
    return (Wait-WslAnswers 45)
}

# Keep WSL up: at most ONE act a tick, before the flag is looked at. Returns $true when WSL
# answers and the rest of the tick may use it.
function Keep-WslUp {
    $acted = $false
    $untilText = "$(Store-Get 'down-until')".Trim()
    if ($untilText) {
        $until = ConvertTo-Utc $untilText
        if ($until -and (Now-Utc) -lt $until) { Say "down: still down until $untilText; nothing is started and WSL is not asked"; return $false }
        Store-Del 'down-until'
        Log "down: over (until $untilText); starting WSL again"
        Say 'down: started again'
        Start-Wsl
        Act 'down' "started again (was down until $untilText)"
        $acted = $true
    }
    if (-not $acted) {
        $listed = Wsl-Listed
        if ($listed -eq $false) {
            Store-Del 'hung-since'
            Start-Wsl
            Act 'not running' 'started'
            $acted = $true
        } elseif (Wsl-Answers) {
            Store-Del 'hung-since'
            return $true
        } else {
            $first = ConvertTo-Utc (Store-Get 'hung-since')
            if ($first -and ((Now-Utc) - $first).TotalMinutes -lt 10) {
                Store-Del 'hung-since'
                Log 'hung: no answer on two ticks in a row; wsl.exe --shutdown, then started again'
                [void](Run-Native 'wsl.exe --shutdown' 60 $null)
                Pause 15
                Start-Wsl
                Act 'hung' 'restarted'
                $acted = $true
            } else {
                Store-Set 'hung-since' (Now)
                Log 'hung: WSL did not answer in 40 seconds; looking again next tick'
                return $false
            }
        }
    }
    if (Wait-WslAnswers 45) { return $true }
    Log 'WSL did not answer 45 seconds after it was started; starting it again as a child of this task'
    Start-Wsl -Plain
    return (Wait-WslAnswers 45)
}

# Wait up to 5 minutes for WSL to answer, then write the act and the result file into it.
function Finish($action, $outcome) {
    $line = "$(Now) $action $outcome"
    Act $action $outcome
    if (-not (Wait-WslAnswers 300)) { Log "$line (WSL did not come back in 5 minutes; result not written)"; return }
    Flush-Acts
    if (Wsl-Write $result "$line`n") { Log $line } else { Log "$line (the result could not be written)" }
}

function Invoke-Tick {
    if ($DryRun) { Write-Host "tick $($script:tick)" } else { New-Item -ItemType Directory -Force $logDir | Out-Null }
    $kept = Keep-WslUp
    if (-not $kept) { return 0 }
    Flush-Acts

    # Report the vhdx drive's free space into WSL (a failure is logged, never fatal).
    if ($DryRun) { Write-Host 'DRYRUN report windows-drive-free and windows-state into WSL' }
    else {
        try {
            $letter = (Split-Path -Qualifier $vhdx).TrimEnd(':')
            $report = "$($letter): $((Get-PSDrive $letter -ErrorAction Stop).Free) $([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
            if (-not (Wsl-Write $driveFree "$report`n")) { Log 'the drive-free report did not get written' }
        } catch { Log "the drive-free report failed: $($_.Exception.Message)" }

        # Report the Windows side's state into WSL (a failure is logged, never fatal).
        try {
            $windowsState = (@(Get-WindowsStateLines $true) -join "`n") + "`n"
            if (Wsl-Write $stateFile $windowsState) { Store-Set 'last-good-tick' (Now) } else { Log 'the state report did not get written' }
        } catch { Log "the state report failed: $($_.Exception.Message)" }
    }

    $want = Wsl-Read $flag
    if ($null -eq $want) { return 0 }
    $want = $want.Trim()

    # Delete the flag first, so that one request is one action.
    if (-not (Wsl-Delete $flag)) { Log 'could not delete the flag; not acting'; return 1 }

    $downSeconds = 0
    if ($want -match '^down ([0-9]{1,6})$') { $downSeconds = [int]$Matches[1] }
    $isDown = ($downSeconds -ge 300 -and $downSeconds -le 86400)
    if (-not $isDown -and $want -ne 'restart' -and $want -ne 'compact' -and $want -ne 'inventory') {
        $shown = if ($want.Length -gt 40) { $want.Substring(0, 40) } else { $want }
        Log "ignored flag content: '$shown'"
        [void](Wsl-Write $result "$(Now) ignored: the flag held '$shown'`n")
        return 0
    }

    if ($want -eq 'inventory') {
        Log 'inventory: begin'
        try {
            $text = (@(Get-Inventory) -join "`n") + "`n"
            if (-not (Wsl-Write $inventory $text)) { throw 'the write into WSL did not finish' }
            Log 'inventory: written'
            [void](Wsl-Write $result "$(Now) inventory ok`n")
            return 0
        } catch {
            Log "inventory failed: $($_.Exception.Message)"
            [void](Wsl-Write $result "$(Now) inventory failed: $($_.Exception.Message)`n")
            return 1
        }
    }

    if ($isDown) {
        # shut WSL down and start nothing until the end, which a file of this task's own keeps
        $untilText = (Now-Utc).AddSeconds($downSeconds).ToString('yyyy-MM-ddTHH:mm:ss') + 'Z'
        Store-Set 'down-until' $untilText
        Log "down: taken, down until $untilText"
        [void](Run-Native 'wsl.exe --shutdown' 60 $null)
        return 0
    }

    Log "${want}: begin"
    if ($want -eq 'restart') {
        [void](Run-Native 'wsl.exe --shutdown' 60 $null)
        Pause 15
        [void](Bring-Up)
        Finish 'restart' 'ok'
        return 0
    }

    # compact
    $before = if (Test-Path -LiteralPath $vhdx) { (Get-Item -LiteralPath $vhdx).Length } else { 0 }
    $t = Run-Native "wsl.exe -d $distro -u root --exec fstrim -av" 600 $null
    $trim = if ($t.Done) { ("$($t.Out)" -split "[\r\n]+" | Where-Object { $_.Trim() }) -join ' ' } else { 'did not finish in 10 minutes' }
    $trimExit = $t.Code
    Log "fstrim exit $trimExit : $trim"
    [void](Run-Native 'wsl.exe --shutdown' 60 $null)
    Pause 15
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
    [void](Bring-Up)
    $gb = { param($b) [math]::Round($b / 1GB, 1) }
    $sizes = "vhdx $(& $gb $before) GB -> $(& $gb $after) GB, fstrim exit $trimExit"
    if ($failure) { Finish 'compact' "failed: $failure ($sizes); WSL started again" } else { Finish 'compact' "ok: $sizes" }
    return 0
}

if ($DryRun) {
    for ($i = 1; $i -le $Ticks; $i++) {
        # the clock of a dry run moves 2 minutes a tick, and past the down's end for the third
        if ($i -gt 1) { $script:skew += 120 }
        if ($i -eq 3 -and $Assume -eq 'down') { $script:skew += $DownFor }
        $script:tick = $i
        [void](Invoke-Tick)
    }
    return
}
# the last thing the tick returns is its exit code
$rc = @(Invoke-Tick)
exit [int]$rc[-1]
