# millwright: give way to the person at this PC's screen (mw-2891blf.2, .15). The desktop
# is also another person's PC, and the factory runs inside its WSL. This script is the Windows
# half of `mw giveway`: four scheduled tasks of the account that owns the WSL distro run it,
# with the event as the argument.
#
#   -Event unlock   someone is at the screen (unlock, console or remote connect, logon):
#                   freeze mode      mw giveway on at once; then (not before) sync and drop
#                                    the page cache as root, so the guest hands memory back to
#                                    Windows; 10 s later one mw giveway mark with the free memory
#                   shutdown mode    mw giveway on, then wsl.exe --terminate
#                   WSL is not running (no vmmemWSL or vmmem process): nothing runs the factory,
#                   so nothing is frozen and wsl.exe is NOT called (it would start the distro).
#   -Event lock     nobody is (lock, console or remote disconnect):
#                   freeze mode      mw giveway off (not called when WSL is not running)
#                   shutdown mode    start WSL as the task 'Start WSL (millwright)' does, wait
#                                    until it answers, then mw giveway off (which retries the
#                                    sessions that died)
#   -Event logoff   a sign-out (event 4 of the User Profile Service, after the session ended):
#                   if a session of some user is still at the screen, nothing (the tick keeps
#                   the give way going); otherwise as lock, by windows-logoff.
#   -Event tick     every 5 minutes: freeze mode, a session of ANY user active at the screen
#                   (console or remote, its lock screen not up) and WSL running: mw giveway on (a
#                   renew). Otherwise nothing. Shutdown mode: nothing (a renew would start WSL).
#   -DryRun         prints the commands it would run, in order, and runs none; writes no state
#                   and no log. -AssumeWsl up|down (default up) says what the WSL check would
#                   find; -AssumeScreen occupied|empty says what the screen check would find
#                   (default: occupied for a tick, empty for a logoff).
#
# Files beside this script (the folder the install paste makes under ProgramData):
#   mode          one word, freeze (the default when the file is missing) or shutdown
#   state         one word, unlocked or locked: what the last unlock or lock said
#   giveway.log   one line per run: UTC time, event, mode, each command's exit code and ms
# `mw host giveway --install-line` prints the paste that installs this file and registers
# the four tasks; --uninstall-line the way back; --mode-line freeze|shutdown flips the mode.
# Windows PowerShell 5.1, no module beyond what Windows ships. No window is shown: the task's
# action is conhost.exe --headless powershell.exe ... (Windows 10 build 19041 or later).
# A failure of wsl.exe (WSL down, the distro missing) is logged; the script exits 0.

param(
    [Parameter(Mandatory = $true)][ValidateSet('unlock', 'lock', 'logoff', 'tick')][string]$Event,
    [switch]$DryRun,
    [ValidateSet('up', 'down')][string]$AssumeWsl = 'up',
    [ValidateSet('', 'occupied', 'empty')][string]$AssumeScreen = ''
)

$ErrorActionPreference = 'Continue'
$env:WSL_UTF8 = '1'
$distro    = 'Ubuntu-24.04'
$wslUser   = 'jwhite'
$mw        = '/home/jwhite/.local/bin/mw'
$startTask = 'Start WSL (millwright)'
$modeFile  = Join-Path $PSScriptRoot 'mode'
$stateFile = Join-Path $PSScriptRoot 'state'
$logFile   = Join-Path $PSScriptRoot 'giveway.log'
$began     = Get-Date
$steps     = New-Object System.Collections.Generic.List[string]

function Now { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss') + 'Z' }

# Milliseconds since this process started (PowerShell's own start-up counts).
function Elapsed-Ms {
    try { return [int]((Get-Date) - (Get-Process -Id $PID).StartTime).TotalMilliseconds }
    catch { return [int]((Get-Date) - $began).TotalMilliseconds }
}

# Windows' Available MBytes, or -1 when the counter cannot be read.
function Free-Mb {
    try { $c = New-Object System.Diagnostics.PerformanceCounter('Memory', 'Available MBytes'); return [int]$c.NextValue() }
    catch { return -1 }
}

function Read-Word($file, $default) {
    try { if (Test-Path -LiteralPath $file) { return ((Get-Content -LiteralPath $file -TotalCount 1) + '').Trim().ToLower() } } catch { }
    return $default
}

function Set-State($word) {
    if ($DryRun) { Write-Host "DRYRUN state: write '$word' to $stateFile"; return }
    try { Set-Content -LiteralPath $stateFile -Value $word -Encoding ASCII -ErrorAction Stop; $steps.Add("state=$word") }
    catch { $steps.Add("state=$word FAILED($($_.Exception.Message))") }
}

# Runs wsl.exe with the arguments, or only prints the line in a dry run. Returns the exit code.
function Wsl-Run([string]$label, [string[]]$wslArgs) {
    if ($DryRun) {
        $shown = $wslArgs | ForEach-Object { if ($_ -match '\s') { "'" + $_ + "'" } else { $_ } }
        Write-Host ("DRYRUN " + $label + ": wsl.exe " + ($shown -join ' '))
        return 0
    }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $code = -1
    try { $null = & wsl.exe @wslArgs 2>&1; $code = $LASTEXITCODE } catch { $code = -1 }
    $steps.Add("$label exit=$code ms=$($sw.ElapsedMilliseconds)")
    return $code
}

# mw giveway <verb> <more> run as the WSL user.
function Mw-Giveway([string]$label, [string[]]$more) {
    return (Wsl-Run $label (@('-d', $distro, '-u', $wslUser, '--exec', $mw) + $more))
}

# Someone is at the screen: a session of ANY user, on the console or remote, is Active and its
# lock screen (a LogonUI in that session) is not up.
function Screen-Occupied {
    if ($DryRun) {
        $yes = if ($AssumeScreen) { $AssumeScreen -eq 'occupied' } else { $Event -eq 'tick' }
        Write-Host ("DRYRUN check a session of some user is active at the screen: assumed " + $(if ($yes) { 'occupied' } else { 'empty' }))
        return $yes
    }
    try { $rows = @(& quser.exe 2>$null) } catch { return $false }
    $locked = @(Get-Process -Name LogonUI -ErrorAction SilentlyContinue | ForEach-Object { $_.SessionId })
    foreach ($r in $rows) {
        if ($r -match '\s(\d+)\s+Active\b' -and ($locked -notcontains [int]$Matches[1])) { return $true }
    }
    return $false
}

# WSL is running: the WSL 2 virtual machine has a process. This takes some tens of ms (asking
# wsl.exe for its running distros takes about 400), and unlike wsl.exe --exec it does not start the distro.
# When the check itself fails it says yes: better to call wsl.exe than to leave the host unfrozen.
function Wsl-Running {
    if ($DryRun) {
        Write-Host "DRYRUN check WSL is running: a vmmemWSL or vmmem process (assumed $AssumeWsl)"
        return ($AssumeWsl -eq 'up')
    }
    try {
        foreach ($n in 'vmmemWSL', 'vmmem') { if ([System.Diagnostics.Process]::GetProcessesByName($n).Count -gt 0) { return $true } }
        return $false
    } catch { return $true }
}

function Start-Wsl {
    if ($DryRun) { Write-Host "DRYRUN start WSL: Start-ScheduledTask -TaskName '$startTask'"; return }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        if (Get-ScheduledTask -TaskName $startTask -ErrorAction SilentlyContinue) { Start-ScheduledTask -TaskName $startTask }
        else { Start-Process wsl.exe -ArgumentList '-d', $distro, '--exec', 'sleep', 'infinity' -WindowStyle Hidden }
        $steps.Add("start-wsl ms=$($sw.ElapsedMilliseconds)")
    } catch { $steps.Add("start-wsl FAILED($($_.Exception.Message))") }
}

# Waits up to 2 minutes for the distro to answer.
function Wait-Wsl {
    if ($DryRun) { Write-Host "DRYRUN wait until WSL answers: wsl.exe -d $distro -u $wslUser --exec true (every 5 s, up to 120 s)"; return }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    for ($i = 0; $i -lt 24; $i++) {
        $code = -1
        try { $null = & wsl.exe -d $distro -u $wslUser --exec true 2>&1; $code = $LASTEXITCODE } catch { }
        if ($code -eq 0) { $steps.Add("wsl-answers ms=$($sw.ElapsedMilliseconds)"); return }
        Start-Sleep 5
    }
    $steps.Add("wsl-answers NO ms=$($sw.ElapsedMilliseconds)")
}

function Write-Log($mode) {
    if ($DryRun) { return }
    try {
        if ((Test-Path -LiteralPath $logFile) -and (Get-Item -LiteralPath $logFile).Length -gt 262144) {
            Move-Item -LiteralPath $logFile -Destination "$logFile.1" -Force
        }
        "$(Now) $Event $mode $($steps -join ' ') total_ms=$(Elapsed-Ms)" | Add-Content -LiteralPath $logFile
    } catch { }
}

$mode = Read-Word $modeFile 'freeze'
if ($mode -ne 'freeze' -and $mode -ne 'shutdown') { $steps.Add("mode '$mode' not known: freeze"); $mode = 'freeze' }
if ($DryRun) { Write-Host "DRYRUN event: $Event, mode: $mode" }

try {
    if ($Event -eq 'unlock') {
        if (-not (Wsl-Running)) {
            # Nothing runs the factory, so nothing is frozen; calling wsl.exe would start the distro.
            Set-State 'unlocked'
            if ($DryRun) { Write-Host 'DRYRUN WSL is not running: nothing frozen, wsl.exe not called' } else { $steps.Add('WSL is not running: nothing to freeze, not started') }
        } else {
            # The freeze comes first, before anything that can take time.
            $null = Mw-Giveway 'on' @('giveway', 'on', '--by', 'windows-unlock', '--note', "windows_free_mb=$(Free-Mb)", '--note', "windows_ms=$(Elapsed-Ms)")
            Set-State 'unlocked'
            if ($mode -eq 'shutdown') {
                $null = Wsl-Run 'terminate' @('--terminate', $distro)
            } else {
                # After the freeze: hand the guest's memory back to Windows.
                if ($DryRun -or (Read-Word $stateFile '') -eq 'unlocked') {
                    $null = Wsl-Run 'drop-caches' @('-d', $distro, '-u', 'root', '--exec', 'sh', '-c', 'sync; echo 3 > /proc/sys/vm/drop_caches')
                    if ($DryRun) { Write-Host 'DRYRUN wait 10 s' } else { Start-Sleep 10 }
                    if ($DryRun -or (Read-Word $stateFile '') -eq 'unlocked') {
                        $null = Mw-Giveway 'mark' @('giveway', 'mark', '--by', 'windows-unlock', '--note', "windows_free_mb=$(Free-Mb)")
                    }
                } else {
                    $steps.Add('locked again: no drop-caches')
                }
            }
        }
    }
    elseif ($Event -eq 'lock' -or $Event -eq 'logoff') {
        $by = if ($Event -eq 'logoff') { 'windows-logoff' } else { 'windows-lock' }
        if ($Event -eq 'logoff') {
            # The sign-out event comes as the session ends: give it a moment, then look. Someone
            # else may still be at the screen (another user's session); the tick keeps that going.
            if ($DryRun) { Write-Host 'DRYRUN wait 3 s' } else { Start-Sleep 3 }
        }
        if ($Event -eq 'logoff' -and (Screen-Occupied)) {
            if ($DryRun) { Write-Host 'DRYRUN nothing: someone else is still at the screen' } else { $steps.Add('someone else still at the screen: nothing') }
        } else {
            Set-State 'locked'
            if ($mode -eq 'shutdown') {
                # Starting WSL is the point of the lock in this mode.
                Start-Wsl
                Wait-Wsl
                $null = Mw-Giveway 'off' @('giveway', 'off', '--by', $by)
            } elseif (Wsl-Running) {
                $null = Mw-Giveway 'off' @('giveway', 'off', '--by', $by)
            } else {
                if ($DryRun) { Write-Host 'DRYRUN WSL is not running: nothing frozen to lift, wsl.exe not called' } else { $steps.Add('WSL is not running: nothing to lift, not started') }
            }
        }
    }
    else {
        if ($mode -eq 'shutdown') {
            if ($DryRun) { Write-Host 'DRYRUN nothing: shutdown mode keeps WSL down while unlocked, and a renew would start it' }
            else { $steps.Add('shutdown mode: nothing') }
        } else {
            if (-not (Screen-Occupied)) {
                if ($DryRun) { Write-Host 'DRYRUN nothing: nobody at the screen' } else { $steps.Add('nobody at the screen: nothing') }
            } elseif (-not (Wsl-Running)) {
                if ($DryRun) { Write-Host 'DRYRUN nothing: WSL is not running, a renew would start it' } else { $steps.Add('WSL is not running: nothing to renew, not started') }
            } else {
                $null = Mw-Giveway 'renew' @('giveway', 'on', '--by', 'windows-tick')
            }
        }
    }
} catch {
    $steps.Add("FAILED($($_.Exception.Message))")
}
Write-Log $mode
exit 0
