package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	millwright "github.com/Jonathan-A-White/millwright"
)

// The files beside which a WSL host's flag lives, under $HOME, and what the
// Windows task (contrib/windows/wsl-flag-watch.ps1) reads and writes there.
const (
	wslFlagDir    = ".local/state/mw"
	wslFlagFile   = "wsl-flag"
	wslFlagResult = "wsl-flag.result"
)

// wslFlagActions is the fixed set of things the Windows task will do; the
// task ignores any other content.
var wslFlagActions = []string{"restart", "compact"}

// wslFlagTask is the name the install line registers the task under.
const wslFlagTask = "millwright-wsl-flag-watch"

// wslFlagLimit is how long mw host wsl-flag waits for the result by default:
// the task polls every 2 minutes and a compaction takes a good while.
const wslFlagLimit = 45 * time.Minute

// wslFlagPoll is how often the result file is looked for. A test shortens it.
var wslFlagPoll = 10 * time.Second

// hostWSLFlagRun runs a shell script on a host and returns what it prints: on
// this host when ssh is empty, otherwise over that ssh prefix. A test swaps it.
var hostWSLFlagRun = func(ctx context.Context, host, ssh, script string) (string, error) {
	var cmd *exec.Cmd
	if ssh == "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", script)
	} else {
		words := strings.Fields(ssh)
		cmd = exec.CommandContext(ctx, words[0], append(words[1:], "sh -c '"+script+"'")...)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", host, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// wslFlagPath is a file of the flag directory as the shell on the host names it.
func wslFlagPath(name string) string { return `"$HOME/` + wslFlagDir + "/" + name + `"` }

// wslFlagWriteScript writes the flag for action, atomically, after clearing
// the last result so that a result seen is the answer to this request.
func wslFlagWriteScript(action string) string {
	return `mkdir -p "$HOME/` + wslFlagDir + `" && rm -f ` + wslFlagPath(wslFlagResult) + " " + wslFlagPath(wslFlagFile+".tmp") +
		` && printf %s ` + action + " > " + wslFlagPath(wslFlagFile+".tmp") +
		" && mv " + wslFlagPath(wslFlagFile+".tmp") + " " + wslFlagPath(wslFlagFile)
}

// wslFlagResultScript prints the result file, or nothing.
func wslFlagResultScript() string {
	return "cat " + wslFlagPath(wslFlagResult) + " 2>/dev/null; true"
}

// wslFlagPendingScript prints "pending" while the flag is still there.
func wslFlagPendingScript() string {
	return "[ -e " + wslFlagPath(wslFlagFile) + " ] && echo pending; true"
}

// WSLFlagInstallLine is the one line of admin PowerShell that writes the
// watcher script (carried in the line, base64) under ProgramData and registers
// the scheduled task that runs it every 2 minutes and at logon, as the Windows
// user who ran the line.
func WSLFlagInstallLine() string {
	enc := base64.StdEncoding.EncodeToString([]byte(millwright.WSLFlagWatch))
	return `$d="$env:ProgramData\millwright"; New-Item -ItemType Directory -Force $d | Out-Null; ` +
		`[IO.File]::WriteAllBytes("$d\wsl-flag-watch.ps1",[Convert]::FromBase64String('` + enc + `')); ` +
		`Register-ScheduledTask -TaskName '` + wslFlagTask + `' ` +
		"-Action (New-ScheduledTaskAction -Execute powershell.exe -Argument \"-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `\"$d\\wsl-flag-watch.ps1`\"\") " +
		`-Trigger @((New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2)),(New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME)) ` +
		`-Principal (New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Highest) ` +
		`-Settings (New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Hours 3) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries) -Force`
}

// WSLFlagWayBack is the one line that takes the task and its script away. It
// changes nothing inside WSL.
func WSLFlagWayBack() string {
	return `Unregister-ScheduledTask -TaskName ` + wslFlagTask + ` -Confirm:$false; Remove-Item "$env:ProgramData\millwright\wsl-flag-watch.ps1"`
}

// newHostWSLFlagCmd builds `mw host wsl-flag`.
func newHostWSLFlagCmd() *cobra.Command {
	var host string
	var limit time.Duration
	var installLine bool
	cmd := &cobra.Command{
		Use:   "wsl-flag restart|compact [--host <host>] [--limit <duration>] | --install-line",
		Short: "Ask a WSL host's Windows side to restart WSL, or to trim and compact its disk, with no visit to the desk",
		Long: "WSL on the desktop is sealed from Windows (interop and /mnt/c off), so nothing inside it can start a\n" +
			"Windows program. A Windows scheduled task (contrib/windows/wsl-flag-watch.ps1), run as the Windows user\n" +
			"that owns the distro, reads the flag file ~/" + wslFlagDir + "/" + wslFlagFile + " inside WSL every 2 minutes and\n" +
			"does one of a fixed set of things: `restart` shuts WSL down and starts it again; `compact` also trims\n" +
			"the disk (fstrim) and compacts the vhdx with diskpart. WSL can ask, never command: any other content is\n" +
			"ignored.\n\n" +
			"`mw host wsl-flag restart|compact` writes the flag on the host (this host, or --host, reached over\n" +
			"[hands_hosts]) and waits, up to --limit, for the task to write " + wslFlagResult + " beside it after WSL is\n" +
			"back, and prints it. The wait outlives the restart: a host that does not answer meanwhile is asked again.\n\n" +
			"`mw host wsl-flag --install-line` prints the one line of admin PowerShell, run once at the desk, that\n" +
			"installs the task, and the line that takes it away.",
		Args: func(cmd *cobra.Command, args []string) error {
			if installLine {
				return cobra.NoArgs(cmd, args)
			}
			if len(args) != 1 {
				return fmt.Errorf("say what to ask for: one of %s (or --install-line)", strings.Join(wslFlagActions, ", "))
			}
			for _, a := range wslFlagActions {
				if args[0] == a {
					return nil
				}
			}
			return fmt.Errorf("%q is not one of %s: the Windows side does only those", args[0], strings.Join(wslFlagActions, ", "))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if installLine {
				fmt.Fprintf(out, "Once, in an admin Windows PowerShell on the host, as the Windows user that owns the WSL distro:\n\n"+
					"```\nPS C:\\WINDOWS\\system32> %s\n```\n\n"+
					"Way back (changes nothing inside WSL):\n\n```\nPS C:\\WINDOWS\\system32> %s\n```\n",
					WSLFlagInstallLine(), WSLFlagWayBack())
				return nil
			}
			here, reach, err := hostTrimHostsFn()
			if err != nil {
				return err
			}
			ssh := ""
			if host == "" {
				host = here
			}
			if host != here {
				var ok bool
				if ssh, ok = reach[host]; !ok {
					return fmt.Errorf("host %q is neither this host (%s) nor in [hands_hosts], so it cannot be reached", host, here)
				}
			}
			return askWSLFlag(cmd, host, ssh, args[0], limit)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "the host whose WSL to ask about (default: this host)")
	cmd.Flags().DurationVar(&limit, "limit", wslFlagLimit, "how long to wait for the result")
	cmd.Flags().BoolVar(&installLine, "install-line", false, "print the one admin-PowerShell line that installs the Windows task, and its way back")
	return cmd
}

// askWSLFlag writes the flag for action on host and waits for the result.
func askWSLFlag(cmd *cobra.Command, host, ssh, action string, limit time.Duration) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), limit)
	defer cancel()
	out := cmd.OutOrStdout()
	if _, err := hostWSLFlagRun(ctx, host, ssh, wslFlagWriteScript(action)); err != nil {
		return fmt.Errorf("the flag was not written: %w", err)
	}
	fmt.Fprintf(out, "%s: asked for %s; waiting up to %s for the Windows task (it looks every 2 minutes)\n", host, action, limit)
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return wslFlagUnanswered(cmd.Context(), host, ssh, action, limit, lastErr)
		case <-time.After(wslFlagPoll):
		}
		got, err := hostWSLFlagRun(ctx, host, ssh, wslFlagResultScript())
		if err != nil {
			lastErr = err // WSL is down while it restarts: ask again
			continue
		}
		if got == "" {
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", host, got)
		if strings.Contains(got, " failed") || strings.Contains(got, " ignored") {
			return fmt.Errorf("the Windows side did not do %s: %s", action, got)
		}
		return nil
	}
}

// wslFlagUnanswered is the error for a wait that ended with no result, saying
// whether the flag is still there (the task never picked it up) or was taken.
func wslFlagUnanswered(ctx context.Context, host, ssh, action string, limit time.Duration, lastErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	state, err := hostWSLFlagRun(ctx, host, ssh, wslFlagPendingScript())
	switch {
	case err != nil:
		return fmt.Errorf("no result from %s within %s, and it cannot be asked now (%v): the flag may still be there; `mw host wsl-flag %s` again, or look at the desk", host, limit, err, action)
	case state == "pending":
		return fmt.Errorf("no result from %s within %s: the flag is still there, so the Windows task has not taken it. Is it installed and running? `mw host wsl-flag --install-line` prints the line that installs it", host, limit)
	}
	msg := fmt.Sprintf("no result from %s within %s: the Windows task took the flag but has not written %s yet; it may still be working (a compaction takes long), look at the result later", host, limit, wslFlagResult)
	if lastErr != nil {
		msg += fmt.Sprintf(" (last answer: %v)", lastErr)
	}
	return fmt.Errorf("%s", msg)
}
