package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// hostHiddenTaskLike is the pattern of the task names the paste of `mw host
// hidden` reaches: every task whose name starts 'millwright', whoever wrote it
// (mw's own pastes, and the keeper, watchdog and wifi tasks written by hand in the
// vault's hosts/*.ps1).
const hostHiddenTaskLike = "millwright*"

// HostHiddenLine is the paste, for a NEW admin Windows PowerShell on the desktop,
// that makes every scheduled task named millwright* start its program under
// conhost.exe --headless (no console window at all; a -WindowStyle Hidden flag, or
// none, shows a dark window after a restart). For each task whose action is not
// already conhost.exe it prints the old exe and arguments and the line that puts
// them back, first and before any change, then replaces the action with
// conhost.exe --headless <old exe> <old arguments> and prints the task. A task
// already headless is left alone, so a second run changes nothing. It throws
// before reading any task on a Windows build older than giveWayMinBuild.
func HostHiddenLine() string {
	return windowsPasteOneLine("millwright host hidden (mw host hidden): every scheduled task named '"+hostHiddenTaskLike+"' whose action is not already conhost.exe starts under conhost.exe --headless instead (no window), keeping its old exe and arguments. Run it twice and the second run changes nothing. To undo it: the paste prints, before it changes anything, one line that restores each task.",
		windowsElevationCheck,
		fmt.Sprintf("if ([Environment]::OSVersion.Version.Build -lt %d) { throw 'needs Windows 10 build %d or later (conhost.exe --headless): nothing changed' }", giveWayMinBuild, giveWayMinBuild)+
			`$mwhd_q = { param($s) "'" + ("$s" -replace "'", "''") + "'" }`,
		`$mwhd_done = { param($a) (-not $a.Execute) -or ([IO.Path]::GetFileName("$($a.Execute)".Trim('"')) -ieq 'conhost.exe') }`,
		`$mwhd_hide = { param($a) $x = "$($a.Execute)"; if ($x -match '\s' -and $x -notmatch '^"') { $x = '"' + $x + '"' }; $arg = ("--headless $x $($a.Arguments)").TrimEnd(); if ($a.WorkingDirectory) { New-ScheduledTaskAction -Execute 'conhost.exe' -Argument $arg -WorkingDirectory $a.WorkingDirectory } else { New-ScheduledTaskAction -Execute 'conhost.exe' -Argument $arg } }`,
		`$mwhd_back = { param($a) $w = if ($a.WorkingDirectory) { " -WorkingDirectory " + (& $mwhd_q $a.WorkingDirectory) } else { '' }; "(New-ScheduledTaskAction -Execute " + (& $mwhd_q $a.Execute) + " -Argument " + (& $mwhd_q $a.Arguments) + $w + ")" }`,
		`$mwhd_todo = @(Get-ScheduledTask | Where-Object { $_.TaskName -like '`+hostHiddenTaskLike+`' } | Where-Object { @($_.Actions | Where-Object { -not (& $mwhd_done $_) }).Count -gt 0 })`,
		`if ($mwhd_todo.Count -eq 0) { Write-Host 'already headless: every millwright task, nothing changed'; return }`,
		`Write-Host "Way back (one line: the old exe and arguments of each task; paste it in an elevated PowerShell to restore them):"`,
		`Write-Host ((@($mwhd_todo | ForEach-Object { "Set-ScheduledTask -TaskName " + (& $mwhd_q $_.TaskName) + " -TaskPath " + (& $mwhd_q $_.TaskPath) + " -Action @(" + (($_.Actions | ForEach-Object { & $mwhd_back $_ }) -join ',') + ") | Out-Null" })) -join '; ')`,
		`foreach ($mwhd_t in $mwhd_todo) { try { $mwhd_new = @($mwhd_t.Actions | ForEach-Object { if (& $mwhd_done $_) { $_ } else { & $mwhd_hide $_ } }); Set-ScheduledTask -TaskName $mwhd_t.TaskName -TaskPath $mwhd_t.TaskPath -Action $mwhd_new | Out-Null; Write-Host ("changed: " + $mwhd_t.TaskName + ": " + (($mwhd_new | ForEach-Object { "$($_.Execute) $($_.Arguments)" }) -join ' ; ')) } catch { Write-Host ("NOT changed: " + $mwhd_t.TaskName + ": " + $_.Exception.Message) } }`,
		`Get-ScheduledTask | Where-Object { $_.TaskName -like '`+hostHiddenTaskLike+`' -and @($_.Actions | Where-Object { -not (& $mwhd_done $_) }).Count -eq 0 } | ForEach-Object { Write-Host ("already headless: " + $_.TaskName) }`)
}

// newHostHiddenCmd builds `mw host hidden`.
func newHostHiddenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hidden",
		Short: "Print the paste that makes every 'millwright' Windows task start with no window, the hand-written ones too",
		Long: "After a Windows restart the start-up tasks of the desktop (the one that keeps WSL up, its watchdog, the wsl-flag\n" +
			"watcher) can show dark console windows titled wsl.exe or powershell.exe. `mw host hidden` prints one paste for a NEW admin\n" +
			"Windows PowerShell on the host that finds every scheduled task whose name starts 'millwright', whoever registered it, and for each\n" +
			"whose action is not already conhost.exe replaces the action with `conhost.exe --headless <old exe> <old arguments>` (no console\n" +
			"at all, which -WindowStyle Hidden does not give). It prints, first and before any change, the old exe and arguments of each task and the\n" +
			"line that restores them; it prints each task it changed; a task already headless is left alone, so a second run changes\n" +
			"nothing. It throws before changing anything on a Windows older than build " + fmt.Sprint(giveWayMinBuild) + ". Nothing here runs on Windows by itself.\n" +
			"The tasks that mw writes itself (`mw host giveway`, `mw host wsl-flag --install-line`) start headless already.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), HostHiddenLine())
			return nil
		},
	}
}
