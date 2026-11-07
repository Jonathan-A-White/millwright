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
#   anything else      ignored and logged
#
# The flag is deleted BEFORE acting. After WSL is back the outcome and the time
# are written to wsl-flag.result beside it: '<UTC time> <action> ok|failed: ...'.
# `mw host wsl-flag restart|compact` writes the flag and waits for that file;
# `mw host wsl-flag --install-line` prints the one admin-PowerShell line that
# installs this file and registers the task. Log: %LOCALAPPDATA%\millwright\wsl-flag-watch.log
# Way back: Unregister-ScheduledTask -TaskName millwright-wsl-flag-watch -Confirm:$false;
#           Remove-Item "$env:ProgramData\millwright\wsl-flag-watch.ps1"   (no WSL change)

$env:WSL_UTF8 = '1'
$distro  = 'Ubuntu-24.04'
$dir     = "\\wsl.localhost\$distro\home\jwhite\.local\state\mw"
$flag    = "$dir\wsl-flag"
$result  = "$dir\wsl-flag.result"
$vhdx    = 'D:\wsl\Ubuntu-24.04\ext4.vhdx'
$startTask = 'Start WSL (millwright)'
$logDir  = "$env:LOCALAPPDATA\millwright"
$log     = "$logDir\wsl-flag-watch.log"
New-Item -ItemType Directory -Force $logDir | Out-Null

function Now { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ss') + 'Z' }
function Log($m) { "$(Now) $m" | Add-Content $log }

# Run a script block with a time limit, so a frozen WSL cannot hold this task.
function Within($seconds, [scriptblock]$block, $arg) {
    $j = Start-Job -ScriptBlock $block -ArgumentList $arg
    $done = Wait-Job $j -Timeout $seconds
    $out = $null
    if ($done) { $out = Receive-Job $j -ErrorAction SilentlyContinue } else { Stop-Job $j }
    Remove-Job $j -Force
    return @{ Done = [bool]$done; Out = $out }
}

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

$read = Within 60 { param($f) if (Test-Path -LiteralPath $f) { Get-Content -LiteralPath $f -Raw } } $flag
if (-not $read.Done -or $null -eq $read.Out) { exit 0 }
$want = ("$($read.Out)").Trim()

# Delete the flag first, so that one request is one action.
try { Remove-Item -LiteralPath $flag -Force -ErrorAction Stop }
catch { Log "could not delete the flag ($($_.Exception.Message)); not acting"; exit 1 }

if ($want -ne 'restart' -and $want -ne 'compact') {
    $shown = if ($want.Length -gt 40) { $want.Substring(0, 40) } else { $want }
    Log "ignored flag content: '$shown'"
    try { Set-Content -LiteralPath $result -Value "$(Now) ignored: the flag held '$shown'" -ErrorAction Stop } catch { }
    exit 0
}

if (-not (Get-ScheduledTask -TaskName $startTask -ErrorAction SilentlyContinue)) {
    Log "$want refused: no '$startTask' task, nothing would start WSL again"
    try { Set-Content -LiteralPath $result -Value "$(Now) $want failed: no '$startTask' task to start WSL again; WSL left as it was" -ErrorAction Stop } catch { }
    exit 1
}

Log "$want: begin"
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
