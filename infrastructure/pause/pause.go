// Package pause holds the adapters of mw pause and mw resume: the paused set
// in the vault, the ssh that reaches the other hosts, the systemd timer that
// resumes, and mw retry.
package pause

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Book is application.PauseBook: plans/pause-<date>/paused.json in the vault,
// committed there, and the note that says which one is in force.
type Book struct {
	// Root is the vault's directory.
	Root  string
	Files application.BoostConfigVault
	Notes application.SweepNotes
}

var _ application.PauseBook = Book{}

// Current implements application.PauseBook.
func (b Book) Current(ctx context.Context) (string, bool, error) {
	dir, err := b.Notes.Note(ctx, application.PauseCurrentKey)
	dir = strings.TrimSpace(dir)
	return dir, dir != "", err
}

// SetCurrent implements application.PauseBook.
func (b Book) SetCurrent(ctx context.Context, dir string) error {
	return b.Notes.SetNote(ctx, application.PauseCurrentKey, dir)
}

// ClearCurrent implements application.PauseBook.
func (b Book) ClearCurrent(ctx context.Context) error {
	return b.Notes.ClearNote(ctx, application.PauseCurrentKey)
}

// Read implements application.PauseBook.
func (b Book) Read(_ context.Context, dir string) (application.PausedSet, error) {
	var set application.PausedSet
	data, err := os.ReadFile(filepath.Join(b.Root, dir, application.PausedFileName))
	if err != nil {
		return set, err
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return set, fmt.Errorf("%s/%s does not read: %w", dir, application.PausedFileName, err)
	}
	return set, nil
}

// Write implements application.PauseBook.
func (b Book) Write(ctx context.Context, dir string, set application.PausedSet) error {
	path := filepath.Join(b.Root, dir, application.PausedFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	_, err = b.Files.Commit(ctx, "pause "+set.Date+": the paused set (mw pause)", []string{dir + "/" + application.PausedFileName})
	return err
}

// Fleet is application.PauseFleet: the home's own host directly, every other
// by ssh running mw there.
type Fleet struct {
	Home  string
	Local application.FleetHostPause
	// Reach is how the home reaches each other host: an ssh prefix by host
	// name (`ssh desktop`), the last word of it the host itself — config
	// [hands_hosts], with a cloud box without one at `ssh <user>@<address>`,
	// its WireGuard address.
	Reach map[string]string
	// Gaps are the hosts left out of Reach, each with why (a cloud box whose
	// address could not be read): they are still hosts to pause, and the pause
	// of one says why it was not reached.
	Gaps map[string]string
	// Run runs a program and returns its standard output; empty runs for
	// real.
	Run func(ctx context.Context, argv []string) (string, error)
}

var _ application.PauseFleet = Fleet{}

// Hosts implements application.PauseFleet: the home, then the others by name.
func (f Fleet) Hosts(context.Context) ([]string, error) {
	hosts := []string{f.Home}
	var others []string
	for host := range f.Reach {
		if host != f.Home {
			others = append(others, host)
		}
	}
	for host := range f.Gaps {
		if _, reached := f.Reach[host]; host != f.Home && !reached {
			others = append(others, host)
		}
	}
	sort.Strings(others)
	return append(hosts, others...), nil
}

// Pause implements application.PauseFleet.
func (f Fleet) Pause(ctx context.Context, host string, order application.HostPauseOrder) (application.HostPaused, error) {
	if host == f.Home {
		return f.Local.Pause(ctx, order)
	}
	var got application.HostPaused
	err := f.remote(ctx, host, "pause", order, &got)
	return got, err
}

// Resume implements application.PauseFleet.
func (f Fleet) Resume(ctx context.Context, host string, order application.HostResumeOrder) (application.HostResumed, error) {
	if host == f.Home {
		return f.Local.Resume(ctx, order)
	}
	var got application.HostResumed
	err := f.remote(ctx, host, "resume", order, &got)
	return got, err
}

// remote runs `mw <verb> --here --order <json>` on host in a login shell, so
// that mw is on its PATH as it is for the person who logs in, and reads the
// JSON it prints.
func (f Fleet) remote(ctx context.Context, host, verb string, order, into any) error {
	prefix := strings.Fields(f.Reach[host])
	if len(prefix) < 2 {
		if why := f.Gaps[host]; why != "" {
			return fmt.Errorf("no way to reach %s: %s", host, why)
		}
		return fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", host, host)
	}
	text, err := json.Marshal(order)
	if err != nil {
		return err
	}
	argv := append(append(append([]string{}, prefix[:len(prefix)-1]...), "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", prefix[len(prefix)-1]),
		"bash -lc "+quote("mw "+verb+" --here --order "+quote(string(text))))
	run := f.Run
	if run == nil {
		run = execute
	}
	out, err := run(ctx, argv)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.Join(prefix, " "), err)
	}
	if err := json.Unmarshal([]byte(out), into); err != nil {
		return fmt.Errorf("%s printed what is not the report of mw %s --here: %q", host, verb, strings.TrimSpace(out))
	}
	return nil
}

// quote makes s one word for a shell.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// execute runs argv and returns its standard output; a failure carries what
// it said on standard error.
func execute(ctx context.Context, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// Timer is application.PauseTimer over systemd's user manager: a transient
// one-shot timer that runs `mw resume`.
type Timer struct {
	// Mw is the mw to run, a full path.
	Mw string
	// Path is the PATH the unit runs with: a user unit's own would not find bd.
	Path string
	Run  func(ctx context.Context, argv []string) (string, error)
}

var _ application.PauseTimer = Timer{}

// Schedule implements application.PauseTimer.
func (t Timer) Schedule(ctx context.Context, at time.Time, unit string) (string, error) {
	run := t.Run
	if run == nil {
		run = execute
	}
	argv := []string{"systemd-run", "--user", "--unit=" + unit,
		"--on-calendar=" + at.UTC().Format("2006-01-02 15:04:05") + " UTC",
		"--timer-property=Persistent=true", "--timer-property=AccuracySec=1s",
		"--description=mw resume (the end of a pause)"}
	if t.Path != "" {
		argv = append(argv, "--setenv=PATH="+t.Path)
	}
	// mw resume reads the pause note through bd, which reaches the home's served
	// beads only by BEADS_DOLT_* in beads.env, as the installed units' files say.
	// A transient unit takes no %h in EnvironmentFile (the path must be absolute),
	// so the home is read here; the "-" lets a host without the file go on.
	if home, err := os.UserHomeDir(); err == nil {
		argv = append(argv, "--property=EnvironmentFile=-"+filepath.Join(home, ".config", "mw", "beads.env"))
	}
	argv = append(argv, t.Mw, "resume")
	if _, err := run(ctx, argv); err != nil {
		return "", err
	}
	return unit + ".timer", nil
}

// Cancel implements application.PauseTimer. A timer already gone, as one
// that fired and is running this very resume is, is no error.
func (t Timer) Cancel(ctx context.Context, unit string) error {
	run := t.Run
	if run == nil {
		run = execute
	}
	if _, err := run(ctx, []string{"systemctl", "--user", "stop", unit}); err != nil && !strings.Contains(err.Error(), "not loaded") {
		return err
	}
	return nil
}

// Retry is application.PauseRetry over `mw retry <story>` run as this mw.
type Retry struct {
	Mw  string
	Run func(ctx context.Context, argv []string) (string, error)
}

var _ application.PauseRetry = Retry{}

// Retry implements application.PauseRetry.
func (r Retry) Retry(ctx context.Context, story string) (string, error) {
	run := r.Run
	if run == nil {
		run = execute
	}
	return run(ctx, []string{r.Mw, "retry", story})
}
