package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/giveway"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// emptyBlob is the object name of a file with nothing in it: a tracked file with
// this blob is meant to be empty.
const emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"

// newHostFitCmd builds `mw host fit`.
func newHostFitCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "fit [--json]",
		Short: "Say what a fit Boost has, and which of it this host lacks",
		Long: "The one statement of what a fit Boost has (mw-6ww.185.9), one line an item, 'ok' or 'GAP' with the cure:\n\n" +
			"  mw-commit        this mw's commit against origin/main of its own checkout (behind by N)\n" +
			"  unit <name>      each unit of the Boost set (mw-dispatch, mw-doctor, mw-host-sweep, mw-host-tidy): its timer\n" +
			"                   enabled or not, and its last run\n" +
			"  doctor-verdict   the age of the newest doctor verdict: a gap over 1 hour\n" +
			"  zero-byte-files  tracked files of the rig checkout and the vault that are empty on disk though the commit holds content\n" +
			"  beads-env        the beads env file is there (its name only: its contents are never read)\n" +
			"  on WSL           windows-state's age and faults, fstrim's last run (a gap over 8 days), wsl-disk-fits where this build has it\n" +
			"  give-way-renew   on a host that gives way (its marker stands): the Windows half's last renew, a gap over 10 minutes\n" +
			"  limits           the cap, builder_jobs, memory and swap totals\n\n" +
			"It changes nothing and asks nobody: the home's boost-fit check of mw doctor runs it on each Boost over ssh, once an\n" +
			"hour, with --json, and names each gap with its cure. Exits 0 with gaps: the report is the answer.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			probes, err := newHostFitProbes()
			if err != nil {
				return err
			}
			report := application.HostFit{Probes: probes}.Run(cmd.Context())
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			report.WriteText(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON: the commit and each item's name, ok, detail and cure")
	return cmd
}

// hostFitProbes reads the real host.
type hostFitProbes struct {
	home      string
	rigDir    string
	vaultDir  string
	doctorDir string
	giveWay   string
}

var _ application.HostFitProbes = hostFitProbes{}

func newHostFitProbes() (hostFitProbes, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return hostFitProbes{}, err
	}
	p := hostFitProbes{home: home}
	if rigs, err := config.Rigs(); err == nil {
		p.rigDir = rigs["millwright"]
	}
	p.vaultDir, _ = config.Vault()
	p.doctorDir, _ = config.DoctorStateDir()
	p.giveWay, _ = config.GiveWayDir()
	return p, nil
}

func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
}

// MwBehind implements application.HostFitProbes: the commits origin/main of the
// rig's own checkout has that this mw was not built from. It fetches nothing.
func (p hostFitProbes) MwBehind(ctx context.Context) (string, int, error) {
	commit := builtCommit()
	if commit == "" {
		return "", 0, fmt.Errorf("this mw was built with no commit in it")
	}
	if p.rigDir == "" {
		return commit, 0, fmt.Errorf("the config names no rig millwright, so there is no checkout of mw's own to compare with")
	}
	out, err := gitOut(ctx, p.rigDir, "rev-list", "--count", commit+"..origin/main")
	if err != nil {
		return commit, 0, fmt.Errorf("git rev-list %s..origin/main in %s: %w", commit, p.rigDir, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return commit, n, err
}

// Unit implements application.HostFitProbes: the user timer of the unit.
func (p hostFitProbes) Unit(ctx context.Context, unit string) (application.HostFitUnitState, error) {
	timer := unit + ".timer"
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", timer).Output()
	state := strings.TrimSpace(string(out))
	if err != nil && state == "" {
		return application.HostFitUnitState{}, fmt.Errorf("systemctl --user is-enabled %s: %w", timer, err)
	}
	shown, _ := exec.CommandContext(ctx, "systemctl", "--user", "show", timer, "-p", "LastTriggerUSec", "--value", "--timestamp=unix").Output()
	last, _ := doctor.SystemdTime(strings.TrimSpace(string(shown)))
	return application.HostFitUnitState{Enabled: state == "enabled", LastRun: last}, nil
}

// DoctorVerdictAt implements application.HostFitProbes: the doctor appends a
// line to its log for every check it runs, so the log's own time is the newest.
func (p hostFitProbes) DoctorVerdictAt(context.Context) (time.Time, error) {
	if p.doctorDir == "" {
		return time.Time{}, fmt.Errorf("no doctor state directory")
	}
	info, err := os.Stat(filepath.Join(p.doctorDir, doctor.LogFile))
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// ZeroBytes implements application.HostFitProbes.
func (p hostFitProbes) ZeroBytes(ctx context.Context) ([]application.HostFitZeroBytes, error) {
	var found []application.HostFitZeroBytes
	for _, where := range []struct{ name, dir string }{{"the rig checkout", p.rigDir}, {"the vault", p.vaultDir}} {
		if where.dir == "" {
			continue
		}
		paths, err := emptyTracked(ctx, where.dir)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", where.name, where.dir, err)
		}
		found = append(found, application.HostFitZeroBytes{Where: where.name, Dir: where.dir, Paths: paths})
	}
	return found, nil
}

// emptyTracked are the tracked files of the checkout in dir that are empty on
// disk while the index holds content for them. A file that is meant to be empty
// has the empty blob and is left alone; a file deleted from disk is not this.
func emptyTracked(ctx context.Context, dir string) ([]string, error) {
	out, err := gitOut(ctx, dir, "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	var empty []string
	for _, entry := range bytes.Split(out, []byte{0}) {
		meta, path, ok := strings.Cut(string(entry), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[2] != "0" || fields[1] == emptyBlob || (fields[0] != "100644" && fields[0] != "100755") {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, path)); err == nil && info.Mode().IsRegular() && info.Size() == 0 {
			empty = append(empty, path)
		}
	}
	return empty, nil
}

// BeadsEnv implements application.HostFitProbes: its existence only.
func (p hostFitProbes) BeadsEnv(context.Context) (string, bool, error) {
	path := filepath.Join(p.home, ".config", "mw", "beads.env")
	name := "~/.config/mw/beads.env"
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return name, false, nil
	}
	return name, err == nil, err
}

// WSL implements application.HostFitProbes.
func (p hostFitProbes) WSL(context.Context) (bool, error) {
	raw, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false, nil
	}
	return strings.Contains(strings.ToLower(string(raw)), "microsoft"), nil
}

// WindowsState implements application.HostFitProbes: the windows-state check's
// own probe.
func (p hostFitProbes) WindowsState(ctx context.Context) (bool, string, error) {
	check := doctor.NewWindowsState()
	check.ScriptSHA256 = wslFlagScriptSHA256()
	verdict, text := check.Probe(ctx)
	return verdict == application.DoctorOK, text, nil
}

// FstrimLastRun implements application.HostFitProbes.
func (p hostFitProbes) FstrimLastRun(ctx context.Context) (time.Time, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "show", "fstrim.timer", "-p", "LastTriggerUSec", "--value", "--timestamp=unix")
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("systemctl show fstrim.timer: %w", err)
	}
	at, _ := doctor.SystemdTime(strings.TrimSpace(string(out)))
	return at, nil
}

// GiveWay implements application.HostFitProbes: a host gives way while its
// marker stands, and the Windows half renews the marker's seen time.
func (p hostFitProbes) GiveWay(ctx context.Context) (bool, time.Time, error) {
	if p.giveWay == "" {
		return false, time.Time{}, nil
	}
	marker, stands, err := giveway.Store{Dir: p.giveWay}.Read(ctx)
	if err != nil || !stands {
		return false, time.Time{}, err
	}
	return true, marker.Seen, nil
}

// Limits implements application.HostFitProbes.
func (p hostFitProbes) Limits(context.Context) (application.HostFitLimits, error) {
	var limits application.HostFitLimits
	var err error
	if limits.Cap, err = config.Cap(); err != nil {
		return limits, err
	}
	if limits.BuilderJobs, err = config.BuilderJobs(); err != nil {
		return limits, err
	}
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return limits, err
	}
	defer file.Close()
	return meminfoLimits(file, limits)
}

// meminfoLimits reads MemTotal and SwapTotal, kilobytes, into limits in megabytes.
func meminfoLimits(r io.Reader, limits application.HostFitLimits) (application.HostFitLimits, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		fields := strings.Fields(value)
		if !ok || len(fields) == 0 {
			continue
		}
		kb, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			limits.MemTotalMB = kb / 1024
		case "SwapTotal":
			limits.SwapTotalMB = kb / 1024
		}
	}
	return limits, scanner.Err()
}

// cloudBoxNames are the hosts the cloud owns: those [cloud.caps] names and the
// boxes the cloud's book holds. The boost-fit check asks none of them.
func cloudBoxNames(dir, host string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		settings, ok, err := config.Cloud()
		if err != nil || !ok {
			return nil, err
		}
		var names []string
		for name := range settings.HostCaps {
			names = append(names, name)
		}
		state, err := vault.CloudBook{Vault: mwVault(dir, host)}.Read(ctx)
		if err != nil {
			return names, err
		}
		for _, box := range state.Boxes {
			names = append(names, box.Name)
		}
		return names, nil
	}
}
