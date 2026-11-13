package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	millwright "github.com/Jonathan-A-White/millwright"
)

// The Windows half of give way (mw-2891blf.2): contrib/windows/giveway.ps1 and
// the pastes that install it. The script is copied to a folder under ProgramData;
// four scheduled tasks of the account that owns WSL run it, one per event.

const (
	// giveWayScriptPath is the script's path in the repo.
	giveWayScriptPath = "contrib/windows/giveway.ps1"
	// giveWayDir is the folder the paste makes, as PowerShell names it. The
	// script, its mode file, its state file and its log live there.
	giveWayDir = `$env:ProgramData\millwright\giveway`
	// giveWayAccount is the Windows account that owns the WSL distro on the desktop.
	giveWayAccount = "grace"
	// giveWayMinBuild is the first Windows 10 build whose conhost.exe takes --headless.
	giveWayMinBuild = 19041
	// giveWayDistro and giveWayWSLUser are the distro and its user on the desktop;
	// giveWayMw is mw's path inside it. giveway.ps1 holds the same three.
	giveWayDistro  = "Ubuntu-24.04"
	giveWayWSLUser = "jwhite"
	giveWayMw      = "/home/jwhite/.local/bin/mw"
)

// giveWayTaskNames are the four tasks, by event.
var giveWayTaskNames = []string{"millwright give way (unlock)", "millwright give way (lock)", "millwright give way (logoff)", "millwright give way (tick)"}

// giveWayLogoffQuery is the event-log subscription of the logoff task. Task
// Scheduler has no logoff trigger, only this: the User Profile Service log's event 4,
// "Finished processing user logoff notification", which is written after the
// session has ended, for any user. (Security 4647 needs the Logoff audit policy
// on; this log is on by default.)
const giveWayLogoffQuery = `<QueryList><Query Id="0" Path="Microsoft-Windows-User Profile Service/Operational"><Select Path="Microsoft-Windows-User Profile Service/Operational">*[System[Provider[@Name="Microsoft-Windows-User Profiles Service"] and EventID=4]]</Select></Query></QueryList>`

// windowsElevationCheck is the first statement of every paste that registers a
// scheduled task (the lock and unlock tasks' session-state triggers, and a task
// at RunLevel Highest, are refused 'Access is denied' in a PowerShell that is
// not elevated, which leaves a half-installed set): it throws before anything is
// written.
const windowsElevationCheck = `if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'this PowerShell is not elevated: run as administrator: nothing installed' }`

var giveWayAccountRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// giveWayModes are the two modes of the script's mode file: freeze (the default)
// and shutdown, the fallback that ends WSL at unlock and starts it at lock.
var giveWayModes = []string{"freeze", "shutdown"}

// giveWayAction is the PowerShell for a task's action: powershell.exe under
// conhost.exe --headless, which creates no console window at all (a -WindowStyle
// Hidden flag still flashes one). It needs Windows 10 build 19041 or later.
func giveWayAction(event string) string {
	return "(New-ScheduledTaskAction -Execute conhost.exe -Argument \"--headless powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `\"$d\\giveway.ps1`\" -Event " + event + "\")"
}

// giveWayRegister is one Register-ScheduledTask statement.
func giveWayRegister(name, event string, triggers ...string) string {
	return "Register-ScheduledTask -TaskName '" + name + "' -Action " + giveWayAction(event) +
		" -Trigger @(" + strings.Join(triggers, ",") + ") -Principal $p -Settings $s -Force | Out-Null"
}

// giveWayRegisterAll is the statements that register the four tasks with the
// script already under $d. Unlock and lock fire for ANY user's session (no UserId
// on a trigger): StateChange 8 is workstation unlock, 7 lock, 1 console connect,
// 2 console disconnect, 3 remote connect (remote desktop), 4 remote disconnect.
// The logoff task fires on the event-log trigger $lo.
func giveWayRegisterAll() string {
	state := func(n int) string {
		return fmt.Sprintf("$t%d=New-CimInstance -CimClass $c -Property @{StateChange=%d; Enabled=$true} -ClientOnly", n, n)
	}
	logon := "(New-ScheduledTaskTrigger -AtLogOn)"
	tick := "(New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 5))"
	return strings.Join([]string{
		`$c=Get-CimClass -ClassName MSFT_TaskSessionStateChangeTrigger -Namespace Root/Microsoft/Windows/TaskScheduler`,
		state(8), state(7), state(1), state(2), state(3), state(4),
		`$lo=New-CimInstance -CimClass (Get-CimClass -ClassName MSFT_TaskEventTrigger -Namespace Root/Microsoft/Windows/TaskScheduler) -Property @{Subscription='` + giveWayLogoffQuery + `'; Enabled=$true} -ClientOnly`,
		`$p=New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited`,
		`$s=New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 10) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries`,
		giveWayRegister(giveWayTaskNames[0], "unlock", "$t8", "$t1", "$t3", logon),
		giveWayRegister(giveWayTaskNames[1], "lock", "$t7", "$t2", "$t4"),
		giveWayRegister(giveWayTaskNames[2], "logoff", "$lo"),
		giveWayRegister(giveWayTaskNames[3], "tick", tick),
		`Get-ScheduledTask -TaskName 'millwright give way*' | ForEach-Object { $i=Get-ScheduledTaskInfo -InputObject $_; "$($_.TaskName): $($_.State), next run $(if ($i.NextRunTime) { $i.NextRunTime } else { 'on its events' })" }`,
	}, "\n")
}

// GiveWayInstallLine is the paste, for a NEW admin Windows PowerShell of the
// account that owns WSL, that puts giveway.ps1 under ProgramData, writes the mode
// file (freeze), registers the four tasks and prints them. It refuses to run
// under any other account, and on a Windows too old for a window-less task. The
// script is downloaded from the public repo pinned at the commit this binary was
// built from and held to the SHA-256 of the embedded copy; inline carries it in
// the paste (base64) instead, for a host that cannot reach the repo.
func GiveWayInstallLine(account string, inline bool) (string, error) {
	if !giveWayAccountRe.MatchString(account) {
		return "", fmt.Errorf("%q is not a Windows account name mw will put in a paste", account)
	}
	head := "# millwright give way (mw host giveway --install-line): copies giveway.ps1 to " + giveWayDir +
		", writes its mode file (freeze) and registers the tasks '" + strings.Join(giveWayTaskNames, "', '") +
		"' under this account. Way back: mw host giveway --uninstall-line.\n" +
		windowsElevationCheck + "\n" +
		"if ($env:USERNAME -ne '" + account + "') { throw 'run this as " + account + ", the account that owns WSL: nothing installed' }\n" +
		fmt.Sprintf("if ([Environment]::OSVersion.Version.Build -lt %d) { throw 'needs Windows 10 build %d or later (a task without a window): nothing installed' }\n", giveWayMinBuild, giveWayMinBuild) +
		`$d="` + giveWayDir + `"; New-Item -ItemType Directory -Force $d | Out-Null` + "\n"
	var get string
	if inline {
		enc := base64.StdEncoding.EncodeToString([]byte(millwright.GiveWayScript))
		get = `[IO.File]::WriteAllBytes("$d\giveway.ps1",[Convert]::FromBase64String('` + enc + `'))` + "\n"
	} else {
		commit := wslFlagCommit()
		if len(commit) != 40 {
			return "", fmt.Errorf("this mw was built with no commit to pin the download at (rebuild from the checkout with make build); `--inline` prints the paste that carries the script in itself instead")
		}
		sum := sha256.Sum256([]byte(millwright.GiveWayScript))
		get = `[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; $t="$d\giveway.ps1.new"` + "\n" +
			`Invoke-WebRequest -UseBasicParsing -Uri '` + wslFlagRepoRaw + commit + "/" + giveWayScriptPath + `' -OutFile $t` + "\n" +
			`if ((Get-FileHash -Algorithm SHA256 $t).Hash -ne '` + strings.ToUpper(hex.EncodeToString(sum[:])) +
			`') { Remove-Item $t; throw 'giveway.ps1 does not match its expected SHA-256: nothing installed' }` + "\n" +
			`Move-Item -Force $t "$d\giveway.ps1"` + "\n"
	}
	return head + get + `Set-Content -Path "$d\mode" -Value freeze -Encoding ASCII` + "\n" + giveWayRegisterAll() + "\n", nil
}

// GiveWayUninstallLine is the way back: the four tasks and the folder removed,
// and the host told to go on again first, so that a host left giving way (or, in
// the shutdown mode, with WSL down) is not left so. The wsl-flag script's own
// folder above is untouched. It checks for an elevated session first: its
// removals are silent about errors, so in a PowerShell that is not elevated they
// would leave the tasks and the folder and still tell the host to go on.
func GiveWayUninstallLine() string {
	var names []string
	for _, n := range giveWayTaskNames {
		names = append(names, "'"+n+"'")
	}
	return "# millwright give way, taken away (mw host giveway --uninstall-line): removes the four tasks and the folder " + giveWayDir +
		", then tells the host to go on (mw giveway off). Way back: mw host giveway --install-line.\n" +
		windowsElevationCheck + "\n" +
		strings.Join(names, ",") + " | ForEach-Object { Unregister-ScheduledTask -TaskName $_ -Confirm:$false -ErrorAction SilentlyContinue }\n" +
		`Remove-Item -Recurse -Force "` + giveWayDir + `" -ErrorAction SilentlyContinue` + "\n" +
		"$env:WSL_UTF8='1'; wsl.exe -d " + giveWayDistro + " -u " + giveWayWSLUser + " --exec " + giveWayMw + " giveway off --by windows-uninstall\n"
}

// GiveWayModeLine is the paste that flips the mode file to mode.
func GiveWayModeLine(mode string) (string, error) {
	other := ""
	var what string
	switch mode {
	case "freeze":
		other, what = "shutdown", "at unlock the host's processes are frozen (SIGSTOP) and at lock they go on"
	case "shutdown":
		other, what = "freeze", "at unlock WSL is shut down and at lock it is started again"
	default:
		return "", fmt.Errorf("%q is not a mode: one of %s", mode, strings.Join(giveWayModes, ", "))
	}
	file := `"` + giveWayDir + `\mode"`
	return "# millwright give way, mode " + mode + " (mw host giveway --mode-line " + mode + "): " + what + ". Way back: mw host giveway --mode-line " + other + ".\n" +
		"Set-Content -Path " + file + " -Value " + mode + " -Encoding ASCII; Get-Content " + file + "\n", nil
}

// newHostGiveWayCmd builds `mw host giveway`.
func newHostGiveWayCmd() *cobra.Command {
	var installLine, uninstallLine, inline bool
	var modeLine, account string
	cmd := &cobra.Command{
		Use:   "giveway --install-line [--inline] [--account <name>] | --uninstall-line | --mode-line freeze|shutdown",
		Short: "Print the paste that sets up, takes away or switches the Windows half of give way on a host a person also sits at",
		Long: "The desktop is also another person's PC. `mw giveway on` freezes the factory while she is at the screen, `off` lets it go\n" +
			"on; the Windows half (contrib/windows/giveway.ps1) calls them: four scheduled tasks of the account that owns\n" +
			"the WSL distro run it at unlock (and console and remote connect, and logon), at lock (and console and remote\n" +
			"disconnect), at logoff, and every 5 minutes (a renew). No window is shown. docs/give-way.md has what each does.\n\n" +
			"  --install-line    one paste for a NEW admin Windows PowerShell as " + giveWayAccount + ": copies the script under ProgramData,\n" +
			"                    writes the mode file (freeze), registers the four tasks and prints them. The script is\n" +
			"                    downloaded pinned at this build's commit and held to its SHA-256; --inline carries it in the paste.\n" +
			"  --uninstall-line  the way back: the tasks and the folder removed, the host told to go on.\n" +
			"  --mode-line M     the paste that flips the fallback mode: freeze (the default) or shutdown (WSL is shut down at\n" +
			"                    unlock and started at lock).\n\n" +
			"Each paste begins with a comment saying what it does and its way back. Nothing here runs on Windows by itself.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			n := 0
			for _, on := range []bool{installLine, uninstallLine, modeLine != ""} {
				if on {
					n++
				}
			}
			if n != 1 {
				return fmt.Errorf("say which: exactly one of --install-line, --uninstall-line, --mode-line freeze|shutdown")
			}
			if inline && !installLine {
				return fmt.Errorf("--inline goes with --install-line")
			}
			var text string
			var err error
			switch {
			case installLine:
				text, err = GiveWayInstallLine(account, inline)
			case uninstallLine:
				text = GiveWayUninstallLine()
			default:
				text, err = GiveWayModeLine(modeLine)
			}
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
	cmd.Flags().BoolVar(&installLine, "install-line", false, "print the paste that installs the script and registers the four tasks")
	cmd.Flags().BoolVar(&uninstallLine, "uninstall-line", false, "print the paste that removes the four tasks and the folder")
	cmd.Flags().StringVar(&modeLine, "mode-line", "", "print the paste that sets the mode: freeze or shutdown")
	cmd.Flags().BoolVar(&inline, "inline", false, "with --install-line: carry the script in the paste (base64) instead of downloading it pinned at this build's commit")
	cmd.Flags().StringVar(&account, "account", giveWayAccount, "with --install-line: the Windows account that owns WSL, the only one the paste will run under")
	return cmd
}
