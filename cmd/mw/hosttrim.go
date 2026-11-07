package main

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// hostTrimStepPrefix starts the id of the hands step trim-setup files; a
// step id is used once per bead, so the host's name ends it.
const hostTrimStepPrefix = "wsl-fstrim-"

// hostTrimProbeTimeout is how long one host may take to answer its probe.
const hostTrimProbeTimeout = 30 * time.Second

// hostTrimHostsFn is this host's name and how it reaches each other host
// ([hands_hosts]: host -> ssh prefix). A test swaps it.
var hostTrimHostsFn = func() (here string, reach map[string]string, err error) {
	here, err = config.Host()
	if err != nil {
		return "", nil, err
	}
	reach, err = config.HandsHosts()
	return here, reach, err
}

// hostTrimProbe runs doctor.WSLTrimProbeScript on a host and returns what it
// prints: on this host when ssh is empty, otherwise over that ssh prefix. A
// test swaps it.
var hostTrimProbe = func(ctx context.Context, host, ssh string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, hostTrimProbeTimeout)
	defer cancel()
	script := doctor.WSLTrimProbeScript()
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

// newHostCmd builds `mw host`: upkeep of the machine a host runs on.
func newHostCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "host",
		Short: "Upkeep of the machine a host runs on",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newHostTrimSetupCmd())
	root.AddCommand(newHostWSLFlagCmd())
	return root
}

// newHostTrimSetupCmd builds `mw host trim-setup`.
func newHostTrimSetupCmd() *cobra.Command {
	var bead string
	var only []string
	cmd := &cobra.Command{
		Use:   "trim-setup [--bead <bead>] [--host <host>]...",
		Short: "Make a WSL host trim its disk: print the one PowerShell line, and file the root step on each WSL host that lacks it",
		Long: "On WSL, systemd skips fstrim.timer and fstrim.service (ConditionVirtualization=!container), so the\n" +
			"weekly trim never runs and the vhdx only grows until the Windows drive fills (the desktop's D:,\n" +
			"mw-6ww.156). trim-setup prints the one Windows PowerShell line that, pasted once per WSL host,\n" +
			"writes the two drop-ins that reset the conditions, reloads systemd and restarts fstrim.timer.\n\n" +
			"It then asks this host, and each host of [hands_hosts], whether it is WSL and has the drop-ins\n" +
			"(--host limits it to the hosts named). With --bead it files, for each WSL host that lacks them,\n" +
			"one `mw hands add` step as root on that bead (id " + hostTrimStepPrefix + "<host>), which the\n" +
			"Governor approves in Postern. A host that cannot be asked is said, not failed. The wsl-trim check\n" +
			"of mw doctor names this command when fstrim.timer has not run in 8 days.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			here, reach, err := hostTrimHostsFn()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Once per WSL host, in Windows PowerShell:\n\n%s\n\n", doctor.WSLTrimPowerShellLine())

			names := []string{here}
			var others []string
			for name := range reach {
				if name != here {
					others = append(others, name)
				}
			}
			sort.Strings(others)
			names = append(names, others...)

			var failed []string
			for _, name := range names {
				if len(only) > 0 && !slices.Contains(only, name) {
					continue
				}
				ssh := ""
				if name != here {
					ssh = reach[name]
				}
				state, err := hostTrimProbe(cmd.Context(), name, ssh)
				if err != nil {
					fmt.Fprintf(out, "%s: could not be asked (%v), skipped\n", name, err)
					continue
				}
				switch state {
				case doctor.WSLTrimNotWSL:
					fmt.Fprintf(out, "%s: not WSL, nothing to do\n", name)
				case doctor.WSLTrimHasDropIn:
					fmt.Fprintf(out, "%s: already has the drop-in\n", name)
				case doctor.WSLTrimLacksDropIn:
					if bead == "" {
						fmt.Fprintf(out, "%s: WSL, lacks the drop-in; run again with --bead <bead> to file its step\n", name)
						continue
					}
					fmt.Fprintf(out, "%s: WSL, lacks the drop-in; filing step %s%s on %s\n", name, hostTrimStepPrefix, name, bead)
					if err := fileHostTrimStep(cmd, bead, name); err != nil {
						fmt.Fprintf(out, "%s: the step was not filed: %v\n", name, err)
						failed = append(failed, name)
					}
				default:
					fmt.Fprintf(out, "%s: answered %q, not understood, skipped\n", name, state)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("the step was not filed for %s", strings.Join(failed, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bead, "bead", "", "file the root step for each WSL host that lacks the drop-in on this bead")
	cmd.Flags().StringArrayVar(&only, "host", nil, "ask only this host (repeatable)")
	return cmd
}

// fileHostTrimStep is `mw hands add <bead> --id wsl-fstrim-<host> --host <host>
// --as root -- <script>`, run in this process so it keeps everything hands add
// does: the comment, the hitl label, the push and the view.
func fileHostTrimStep(cmd *cobra.Command, bead, host string) error {
	add := newHandsAddCmd()
	add.SetOut(cmd.OutOrStdout())
	add.SetErr(cmd.ErrOrStderr())
	add.SetArgs([]string{bead, "--id", hostTrimStepPrefix + host, "--host", host, "--as", "root",
		"--way-back", doctor.WSLTrimWayBack(), "--", doctor.WSLTrimRootScript()})
	return add.ExecuteContext(cmd.Context())
}
