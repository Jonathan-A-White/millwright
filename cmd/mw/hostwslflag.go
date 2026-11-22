package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime/debug"
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
	// wslFlagInventory is the long, read-only report the task writes beside the
	// flag when it is asked for `inventory` (mw-6ww.185.2).
	wslFlagInventory = "windows-inventory.txt"
)

// wslFlagActions is the fixed set of things the Windows task will do; the
// task ignores any other content. inventory changes nothing: the task writes
// windows-inventory.txt and the wait is for that file, not for wsl-flag.result.
// down is written as 'down <seconds>' and is not waited for: WSL is down.
var wslFlagActions = []string{"restart", "compact", "inventory", "down"}

// The lengths `down --for` takes: the task ignores any other (mw-6ww.185.3).
const (
	wslFlagDownMin = 5 * time.Minute
	wslFlagDownMax = 24 * time.Hour
)

// wslFlagInventoryLimit is how long mw host wsl-flag inventory waits when --limit
// is not given: the task looks every 2 minutes and the read takes seconds.
const wslFlagInventoryLimit = 10 * time.Minute

// wslFlagTask is the name the install line registers the task under.
const wslFlagTask = "millwright-wsl-flag-watch"

// wslFlagLimit is how long mw host wsl-flag waits for the result by default:
// the task polls every 2 minutes and a compaction takes a good while.
const wslFlagLimit = 45 * time.Minute

// wslFlagWriteLimit bounds the write of the flag, apart from --limit, which
// bounds the wait for the result.
const wslFlagWriteLimit = 2 * time.Minute

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
// the last result (for inventory, the last inventory too) so that a result seen
// is the answer to this request.
func wslFlagWriteScript(action string) string {
	clear := wslFlagPath(wslFlagResult)
	if action == "inventory" {
		clear += " " + wslFlagPath(wslFlagInventory)
	}
	return `mkdir -p "$HOME/` + wslFlagDir + `" && rm -f ` + clear + " " + wslFlagPath(wslFlagFile+".tmp") +
		` && printf %s ` + wslFlagShellWord(action) + " > " + wslFlagPath(wslFlagFile+".tmp") +
		" && mv " + wslFlagPath(wslFlagFile+".tmp") + " " + wslFlagPath(wslFlagFile)
}

// wslFlagShellWord is an action as one shell word: quoted when it holds a space
// ('down 600'). It is never single-quoted, which the script may not hold over ssh.
func wslFlagShellWord(action string) string {
	if strings.Contains(action, " ") {
		return `"` + action + `"`
	}
	return action
}

// wslFlagResultScript prints the result file, or nothing.
func wslFlagResultScript() string {
	return "cat " + wslFlagPath(wslFlagResult) + " 2>/dev/null; true"
}

// wslFlagInventoryScript prints the inventory file, or nothing.
func wslFlagInventoryScript() string {
	return "cat " + wslFlagPath(wslFlagInventory) + " 2>/dev/null; true"
}

// wslFlagPendingScript prints "pending" while the flag is still there.
func wslFlagPendingScript() string {
	return "[ -e " + wslFlagPath(wslFlagFile) + " ] && echo pending; true"
}

// wslFlagRepoRaw is where the public millwright repo serves a file at a commit.
const wslFlagRepoRaw = "https://raw.githubusercontent.com/Jonathan-A-White/millwright/"

// wslFlagScriptPath is the watcher script's path in the repo.
const wslFlagScriptPath = "contrib/windows/wsl-flag-watch.ps1"

// wslFlagCommit is the full commit this binary was built from, "" when the
// toolchain embedded none. A test swaps it.
var wslFlagCommit = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

// wslFlagRegister is the part of the install line that registers the task
// with the script already under $mwwf_d, runs it once, and, when it has had a
// good tick, takes away the tasks it replaces. The task runs at startup and every
// 2 minutes whether or not anyone is signed in (logon type S4U: no password is
// stored), as the Windows user who ran the line, so that a Windows restart for an
// update does not leave WSL down until someone signs in. With no session there is
// no window to hide. It waits up to 3 minutes for last-good-tick, which the script
// writes after a tick that reached WSL (mw-6ww.185.3); without it the keeper and
// the watchdog stay and the line says so.
func wslFlagRegister() string {
	return "$mwwf_f=\"-NoProfile -ExecutionPolicy Bypass -File `\"$mwwf_d\\wsl-flag-watch.ps1`\"\"; " +
		`Register-ScheduledTask -TaskName '` + wslFlagTask + `' ` +
		"-Action (New-ScheduledTaskAction -Execute powershell.exe -Argument $mwwf_f) " +
		`-Trigger @((New-ScheduledTaskTrigger -AtStartup),(New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 2))) ` +
		`-Principal (New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType S4U -RunLevel Highest) ` +
		`-Settings (New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Hours 3) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries) -Force; ` +
		`$mwwf_g="$mwwf_d\last-good-tick"; Remove-Item $mwwf_g -EA 0; Start-ScheduledTask ` + wslFlagTask + `; ` +
		`for ($mwwf_i=0; $mwwf_i -lt 36 -and !(Test-Path $mwwf_g); $mwwf_i++) { sleep 5 }; ` +
		`if (Test-Path $mwwf_g) { Unregister-ScheduledTask -TaskName millwright-wsl-keeper,millwright-wsl-watchdog -Confirm:$false -EA 0 } ` +
		`else { Write-Warning 'no good tick in 3 min: keeper and watchdog left' }`
}

// wslFlagScriptSHA256 is the upper-case SHA-256 of the watcher script this
// binary carries: what the install line holds the download to, and what the
// windows-state check compares the installed script's hash with.
func wslFlagScriptSHA256() string {
	sum := sha256.Sum256([]byte(millwright.WSLFlagWatch))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// WSLFlagInstallLine is the one line of admin PowerShell that downloads the
// watcher script from the public repo, pinned at the commit this binary was
// built from, refuses it unless its SHA-256 is that of the script this binary
// carries, puts it under ProgramData and registers the scheduled task that
// runs it at startup and every 2 minutes with nobody signed in, as the Windows
// user who ran the line, and, after the task's first good tick, removes the
// keeper and watchdog tasks it replaces. It is short enough (well under 2000 bytes) to be sent to a phone.
func WSLFlagInstallLine() (string, error) {
	commit := wslFlagCommit()
	if len(commit) != 40 {
		return "", fmt.Errorf("this mw was built with no commit to pin the download at (rebuild from the checkout with make build)")
	}
	return windowsElevationCheck + `; $mwwf_d="$env:ProgramData\millwright"; mkdir -Force $mwwf_d | Out-Null; ` +
		`[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; $mwwf_t="$mwwf_d\wsl-flag-watch.ps1.new"; ` +
		`Invoke-WebRequest -UseBasicParsing -Uri '` + wslFlagRepoRaw + commit + "/" + wslFlagScriptPath + `' -OutFile $mwwf_t; ` +
		`if ((Get-FileHash -Algorithm SHA256 $mwwf_t).Hash -ne '` + wslFlagScriptSHA256() + `') { Remove-Item $mwwf_t; throw 'wsl-flag-watch.ps1 does not match its expected SHA-256: nothing installed' }; ` +
		`Move-Item -Force $mwwf_t "$mwwf_d\wsl-flag-watch.ps1"; ` + wslFlagRegister(), nil
}

// WSLFlagWayBack is the one line that takes the task and its script away, and
// only those: the keeper and the watchdog it removed are not put back. It
// changes nothing inside WSL. It checks for an elevated session first, as the
// install lines do: the task is at RunLevel Highest and its script is under
// ProgramData.
func WSLFlagWayBack() string {
	return windowsElevationCheckRemoval + `; Unregister-ScheduledTask -TaskName ` + wslFlagTask + ` -Confirm:$false; Remove-Item "$env:ProgramData\millwright\wsl-flag-watch.ps1"`
}

// newHostWSLFlagCmd builds `mw host wsl-flag`.
func newHostWSLFlagCmd() *cobra.Command {
	var host string
	var limit time.Duration
	var installLine, dryRun bool
	var downFor time.Duration
	cmd := &cobra.Command{
		Use:   "wsl-flag restart|compact|inventory [--host <host>] [--limit <duration>] | down --for <5m..24h> [--host <host>] [--dry-run] | --install-line",
		Short: "Ask a WSL host's Windows side to restart WSL, or to trim and compact its disk, with no visit to the desk",
		Long: "WSL on the desktop is sealed from Windows (interop and /mnt/c off), so nothing inside it can start a\n" +
			"Windows program. A Windows scheduled task (contrib/windows/wsl-flag-watch.ps1), run as the Windows user\n" +
			"that owns the distro, reads the flag file ~/" + wslFlagDir + "/" + wslFlagFile + " inside WSL every 2 minutes and\n" +
			"does one of a fixed set of things: `restart` shuts WSL down and starts it again; `compact` also trims\n" +
			"the disk (fstrim) and compacts the vhdx with diskpart; `inventory` changes nothing and has the task write\n" +
			"a long report of the Windows side (windows-inventory.txt beside the flag). WSL can ask, never command:\n" +
			"any other content is ignored.\n\n" +
			"`mw host wsl-flag down --for D` (D from 5 minutes to 24 hours, any other length is refused here and ignored\n" +
			"by the task) has the task shut WSL down, start nothing until D has passed ('down until T' in its log), and\n" +
			"start WSL again, writing the act 'down: started again' to windows-acts. It writes the flag and returns: WSL\n" +
			"is down meanwhile, so nothing here could wait. --dry-run prints the three moments and writes nothing.\n\n" +
			"`mw host wsl-flag restart|compact|inventory` writes the flag on the host (this host, or --host, reached over\n" +
			"[hands_hosts]) and waits, up to --limit, for the task to write " + wslFlagResult + " beside it after WSL is\n" +
			"back, and prints it (for inventory, it waits for " + wslFlagInventory + " and prints that, 10 minutes by default). The wait outlives the restart: a host that does not answer meanwhile is asked again.\n\n" +
			"`mw host wsl-flag --install-line` prints the one line of admin PowerShell, run once at the desk, that\n" +
			"installs the task (it runs at startup and every 2 minutes with nobody signed in, no password stored, and\n" +
			"after its first good tick removes the tasks millwright-wsl-keeper and millwright-wsl-watchdog), and the\n" +
			"line that takes it away (only the task and its script). The line downloads the script from the public repo\n" +
			"pinned at the commit this mw was built from and installs it only if its SHA-256 is the one mw carries;\n" +
			"it is short enough to send to a phone. There is no form that carries the script in the line: it would have to be encoded.",
		Args: func(cmd *cobra.Command, args []string) error {
			if installLine {
				return cobra.NoArgs(cmd, args)
			}
			if len(args) != 1 {
				return fmt.Errorf("say what to ask for: one of %s (or --install-line)", strings.Join(wslFlagActions, ", "))
			}
			for _, a := range wslFlagActions {
				if args[0] == a {
					return wslFlagCheckFor(cmd, a, downFor)
				}
			}
			return fmt.Errorf("%q is not one of %s: the Windows side does only those", args[0], strings.Join(wslFlagActions, ", "))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if installLine {
				line, err := WSLFlagInstallLine()
				if err != nil {
					return err
				}
				// The window and its prompt are named in the prose: a fenced block
				// is copied whole, so it holds only the command.
				fmt.Fprintf(out, "Once, in a NEW admin Windows PowerShell (its prompt reads `PS C:\\WINDOWS\\system32>`) on the host, as the Windows user that owns the WSL distro:\n\n"+
					"```\n%s\n```\n\n"+
					"Way back, in the same kind of window (changes nothing inside WSL):\n\n```\n%s\n```\n",
					line, WSLFlagWayBack())
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
			if args[0] == "down" {
				return askWSLDown(cmd, host, ssh, downFor, dryRun)
			}
			if dryRun {
				fmt.Fprintf(out, "%s: would write the flag %q into ~/%s/%s (nothing written)\n", host, args[0], wslFlagDir, wslFlagFile)
				return nil
			}
			if args[0] == "inventory" && !cmd.Flags().Changed("limit") {
				limit = wslFlagInventoryLimit
			}
			return askWSLFlag(cmd, host, ssh, args[0], limit)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "the host whose WSL to ask about (default: this host)")
	cmd.Flags().DurationVar(&limit, "limit", wslFlagLimit, "how long to wait for the result")
	cmd.Flags().DurationVar(&downFor, "for", 0, "down only: how long WSL stays down, from 5m to 24h in whole seconds")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be written and when, and write nothing")
	cmd.Flags().BoolVar(&installLine, "install-line", false, "print the one admin-PowerShell line that installs the Windows task, and its way back")
	return cmd
}

// askWSLFlag writes the flag for action on host and waits for the result.
func askWSLFlag(cmd *cobra.Command, host, ssh, action string, limit time.Duration) error {
	out := cmd.OutOrStdout()
	// --limit bounds the wait for the result, not the write of the flag: the
	// write has its own bound, and the wait's clock starts when it is done, so
	// a host slow to take the write is not told the flag was not written.
	wctx, wcancel := context.WithTimeout(cmd.Context(), wslFlagWriteLimit)
	_, err := hostWSLFlagRun(wctx, host, ssh, wslFlagWriteScript(action))
	wcancel()
	if err != nil {
		return fmt.Errorf("the flag was not written: %w", err)
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), limit)
	defer cancel()
	fmt.Fprintf(out, "%s: asked for %s; waiting up to %s for the Windows task (it looks every 2 minutes)\n", host, action, limit)
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return wslFlagUnanswered(cmd.Context(), host, ssh, action, limit, lastErr)
		case <-time.After(wslFlagPoll):
		}
		if action == "inventory" {
			got, err := hostWSLFlagRun(ctx, host, ssh, wslFlagInventoryScript())
			if err != nil {
				lastErr = err
				continue
			}
			if got != "" {
				fmt.Fprintf(out, "%s: inventory\n%s\n", host, got)
				return nil
			}
			// no inventory yet: the task says in the result file when it could not write one
			if res, rerr := hostWSLFlagRun(ctx, host, ssh, wslFlagResultScript()); rerr == nil && strings.Contains(res, " failed") {
				fmt.Fprintf(out, "%s: %s\n", host, res)
				return fmt.Errorf("the Windows side did not take the inventory: %s", res)
			}
			continue
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
	written := wslFlagResult
	if action == "inventory" {
		written = wslFlagInventory
	}
	msg := fmt.Sprintf("no result from %s within %s: the Windows task took the flag but has not written %s yet; it may still be working (a compaction takes long), look at the result later", host, limit, written)
	if lastErr != nil {
		msg += fmt.Sprintf(" (last answer: %v)", lastErr)
	}
	return fmt.Errorf("%s", msg)
}

// wslFlagCheckFor refuses --for on anything but down, down with no --for, and a
// length the task would ignore: before the flag is written.
func wslFlagCheckFor(cmd *cobra.Command, action string, d time.Duration) error {
	if action != "down" {
		if cmd.Flags().Changed("for") {
			return fmt.Errorf("--for belongs to down only, not %s", action)
		}
		return nil
	}
	if !cmd.Flags().Changed("for") {
		return fmt.Errorf("say how long: mw host wsl-flag down --for <%s to %s>", wslFlagDownMin, wslFlagDownMax)
	}
	_, err := wslFlagDownSeconds(d)
	return err
}

// wslFlagDownSeconds is d in whole seconds when it is a length the task takes:
// 5 minutes to 24 hours, with no fraction of a second.
func wslFlagDownSeconds(d time.Duration) (int, error) {
	if d < wslFlagDownMin || d > wslFlagDownMax || d%time.Second != 0 {
		return 0, fmt.Errorf("--for %s is not from %s to %s in whole seconds: the Windows side takes only those and nothing was written", d, wslFlagDownMin, wslFlagDownMax)
	}
	return int(d / time.Second), nil
}

// askWSLDown writes the flag 'down <seconds>' and returns: WSL goes down under
// whoever asks, so there is no result to wait for. With dryRun it prints the
// three moments and writes nothing.
func askWSLDown(cmd *cobra.Command, host, ssh string, d time.Duration, dryRun bool) error {
	out := cmd.OutOrStdout()
	secs, err := wslFlagDownSeconds(d)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(out, "%s: down for %s (nothing written; the flag would be 'down %d'):\n"+
			"  taken: within 2 minutes the Windows task deletes the flag, shuts WSL down and logs 'down until <now + %s>'\n"+
			"  still down: each tick until then starts nothing and asks WSL nothing\n"+
			"  started again: the first tick after that starts WSL and writes the act 'down: started again' to windows-acts\n", host, d, secs, d)
		return nil
	}
	wctx, wcancel := context.WithTimeout(cmd.Context(), wslFlagWriteLimit)
	defer wcancel()
	if _, err := hostWSLFlagRun(wctx, host, ssh, wslFlagWriteScript(fmt.Sprintf("down %d", secs))); err != nil {
		return fmt.Errorf("the flag was not written: %w", err)
	}
	fmt.Fprintf(out, "%s: asked for WSL down for %s. The Windows task looks every 2 minutes: it shuts WSL down, starts nothing for %s from then, and starts WSL again, writing the act 'down: started again' to windows-acts. WSL answers nothing meanwhile.\n", host, d, d)
	return nil
}
