package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// doctorContext holds a table of fake checks (or, for the daemon-reload
// scenario, the real adapter over a fake systemctl) and a fake state and log.
// Nothing here reaches this host's own doctor state directory or a real
// systemctl.
type doctorContext struct {
	fakes []*apptest.FakeDoctorCheck
	order []string
	real  application.DoctorCheck

	state *apptest.FakeDoctorState
	log   *apptest.FakeDoctorLog
	notes *apptest.FakeDoctorNotes
	now   time.Time

	systemctlCalls string
	netshCalls     string
	wifi           *doctor.Wifi
	vaultDirtyDir  string

	mayorGoneVault string
	mayorUpCalls   string

	out    bytes.Buffer
	report application.DoctorReport
	err    error
}

// InitializeDoctorScenario registers the steps of features/doctor.feature.
func InitializeDoctorScenario(ctx *godog.ScenarioContext) {
	c := &doctorContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = doctorContext{
			state: apptest.NewFakeDoctorState(),
			log:   &apptest.FakeDoctorLog{},
			notes: apptest.NewFakeDoctorNotes(),
			now:   time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})

	ctx.Given(`^a doctor check "([^"]*)" whose probe says ok$`, c.aCheckWhoseProbeSaysOK)
	ctx.Given(`^a doctor check "([^"]*)" whose probe says faulty "([^"]*)"$`, c.aCheckWhoseProbeSaysFaulty)
	ctx.Given(`^a doctor check "([^"]*)" whose probe cannot tell "([^"]*)"$`, c.aCheckWhoseProbeCannotTell)
	ctx.Given(`^the notes port fails, saying "([^"]*)"$`, c.theNotesPortFails)
	ctx.Given(`^the check "([^"]*)"'s way back is "([^"]*)"$`, c.theChecksWayBackIs)
	ctx.Given(`^the check "([^"]*)"'s damper is (\d+) minutes?, cap (\d+)$`, c.theChecksDamperIs)
	ctx.Given(`^the check "([^"]*)"'s cure fails, saying "([^"]*)"$`, c.theChecksCureFails)
	ctx.Given(`^a fake systemctl that says "([^"]*)" needs a reload$`, c.aFakeSystemctlThatSaysNeedsAReload)
	ctx.Given(`^a fake netsh reporting the network "([^"]*)"$`, c.aFakeNetshReportingTheNetwork)
	ctx.Given(`^a fake powershell that exists$`, c.aFakePowershellThatExists)
	ctx.Given(`^the internet is unreachable$`, c.theInternetIsUnreachable)
	ctx.Given(`^a vault with a modified tracked file "([^"]*)"$`, c.aVaultWithAModifiedTrackedFile)
	ctx.Given(`^a fake systemctl reporting the timer "([^"]*)" enabled and inactive$`, c.aFakeSystemctlReportingTheTimerEnabledAndInactive)
	ctx.Given(`^a vault whose \.beads is (\d+) bytes, past a (\d+) byte budget$`, c.aVaultWhoseBeadsIsBytesPastABudget)
	ctx.Given(`^a vault with no \.mayor-acting$`, c.aVaultWithNoMayorActing)
	ctx.Given(`^a vault whose \.mayor-acting names the window "([^"]*)"$`, c.aVaultWhoseMayorActingNamesTheWindow)
	ctx.Given(`^a stand-in tmux listing that window with a live claude process$`, c.aStandInTmuxListingThatWindowWithALiveProcess)
	ctx.Given(`^a stand-in tmux with no window open$`, c.aStandInTmuxWithNoWindowOpen)
	ctx.Given(`^a stand-in bin/mayor-up in that vault that starts a Mayor in window "([^"]*)"$`, c.aStandInMayorUpThatStartsAMayorInWindow)
	ctx.Given(`^a stand-in bin/mayor-up in that vault that always exits 4, saying "([^"]*)"$`, c.aStandInMayorUpThatAlwaysExits4Saying)

	ctx.When(`^the check "([^"]*)"'s probe says ok$`, c.theChecksProbeSaysOK)
	ctx.When(`^the check "([^"]*)"'s probe says faulty "([^"]*)" again$`, c.theChecksProbeSaysFaultyAgain)
	ctx.When(`^mw doctor runs$`, c.mwDoctorRuns)
	ctx.When(`^mw doctor runs dry$`, c.mwDoctorRunsDry)
	ctx.When(`^mw doctor's daemon-reload check runs for real$`, c.mwDoctorsDaemonReloadCheckRunsForReal)
	ctx.When(`^mw doctor's wifi check runs$`, c.mwDoctorsWifiCheckRuns)
	ctx.When(`^mw doctor's vault-dirty check runs for real$`, c.mwDoctorsVaultDirtyCheckRunsForReal)
	ctx.When(`^mw doctor's timers check runs for real$`, c.mwDoctorsTimersCheckRunsForReal)
	ctx.When(`^mw doctor's beads-size check runs for real$`, c.mwDoctorsBeadsSizeCheckRunsForReal)
	ctx.When(`^mw doctor's mayor-gone check runs for real$`, c.mwDoctorsMayorGoneCheckRunsForReal)
	ctx.When(`^(\d+) minutes? go(?:es)? by$`, c.minutesPass)
	ctx.When(`^(\d+) hours? go(?:es)? by$`, c.hoursPass)

	ctx.Then(`^mw doctor leaves with the status (\d+)$`, c.mwDoctorLeavesWith)
	ctx.Then(`^the doctor log holds "([^"]*)"$`, c.theDoctorLogHolds)
	ctx.Then(`^the doctor log is empty$`, c.theDoctorLogIsEmpty)
	ctx.Then(`^the check "([^"]*)" was not cured$`, c.theCheckWasNotCured)
	ctx.Then(`^the check "([^"]*)" was cured (\d+) times?$`, c.theCheckWasCuredNTimes)
	ctx.Then(`^mw doctor printed "([^"]*)"$`, c.mwDoctorPrinted)
	ctx.Then(`^systemctl was run with "([^"]*)"$`, c.systemctlWasRunWith)
	ctx.Then(`^netsh was not run$`, c.netshWasNotRun)
	ctx.Then(`^netsh was run with "([^"]*)" (\d+) times?$`, c.netshWasRunWithNTimes)
	ctx.Then(`^the doctor log holds a way back naming the commit it made$`, c.theDoctorLogHoldsAWayBackNamingTheCommitItMade)
	ctx.Then(`^the note "([^"]*)" holds "([^"]*)"$`, c.theNoteHolds)
	ctx.Then(`^the note "([^"]*)" does not exist$`, c.theNoteDoesNotExist)
	ctx.Then(`^mayor-up was not run$`, c.mayorUpWasNotRun)
	ctx.Then(`^mayor-up was run (\d+) times?$`, c.mayorUpWasRunNTimes)
}

func (c *doctorContext) fake(name string) *apptest.FakeDoctorCheck {
	for _, fake := range c.fakes {
		if fake.CheckName == name {
			return fake
		}
	}
	return nil
}

func (c *doctorContext) addFake(name string) *apptest.FakeDoctorCheck {
	if existing := c.fake(name); existing != nil {
		return existing
	}
	fake := &apptest.FakeDoctorCheck{CheckName: name, Wait: 10 * time.Minute, Cap: 3}
	c.fakes = append(c.fakes, fake)
	c.order = append(c.order, name)
	return fake
}

func (c *doctorContext) aCheckWhoseProbeSaysOK(name string) error {
	c.addFake(name).Verdict = application.DoctorOK
	return nil
}

func (c *doctorContext) aCheckWhoseProbeSaysFaulty(name, reason string) error {
	fake := c.addFake(name)
	fake.Verdict = application.DoctorFaulty
	fake.Reason = reason
	return nil
}

func (c *doctorContext) aCheckWhoseProbeCannotTell(name, reason string) error {
	fake := c.addFake(name)
	fake.Verdict = application.DoctorCannotTell
	fake.Reason = reason
	return nil
}

func (c *doctorContext) theNotesPortFails(said string) error {
	c.notes.Err = errors.New(said)
	return nil
}

func (c *doctorContext) theChecksWayBackIs(name, way string) error {
	c.addFake(name).Way = way
	return nil
}

func (c *doctorContext) theChecksDamperIs(name, waitText, capText string) error {
	minutes, err := strconv.Atoi(waitText)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", waitText, err)
	}
	capPerEpisode, err := strconv.Atoi(capText)
	if err != nil {
		return fmt.Errorf("parsing %q as a cap: %w", capText, err)
	}
	fake := c.addFake(name)
	fake.Wait = time.Duration(minutes) * time.Minute
	fake.Cap = capPerEpisode
	return nil
}

func (c *doctorContext) theChecksCureFails(name, saying string) error {
	c.addFake(name).CureErr = errors.New(saying)
	return nil
}

func (c *doctorContext) theChecksProbeSaysOK(name string) error {
	return c.aCheckWhoseProbeSaysOK(name)
}

func (c *doctorContext) theChecksProbeSaysFaultyAgain(name, reason string) error {
	return c.aCheckWhoseProbeSaysFaulty(name, reason)
}

func (c *doctorContext) checks() application.DoctorChecks {
	if c.real != nil {
		return application.DoctorChecks{c.real}
	}
	checks := make(application.DoctorChecks, len(c.fakes))
	for i, fake := range c.fakes {
		checks[i] = fake
	}
	return checks
}

func (c *doctorContext) run(dryRun bool) error {
	c.out.Reset()
	c.report, c.err = application.Doctor{
		Checks: c.checks(),
		State:  c.state,
		Log:    c.log,
		Notes:  c.notes,
		Now:    func() time.Time { return c.now },
		Out:    &c.out,
	}.Run(context.Background(), "", dryRun)
	return nil
}

func (c *doctorContext) mwDoctorRuns() error { return c.run(false) }

func (c *doctorContext) mwDoctorRunsDry() error { return c.run(true) }

func (c *doctorContext) mwDoctorsDaemonReloadCheckRunsForReal() error { return c.run(false) }

func (c *doctorContext) minutesPass(text string) error {
	minutes, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", text, err)
	}
	c.now = c.now.Add(time.Duration(minutes) * time.Minute)
	return nil
}

func (c *doctorContext) hoursPass(text string) error {
	hours, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("parsing %q as hours: %w", text, err)
	}
	c.now = c.now.Add(time.Duration(hours) * time.Hour)
	return nil
}

func (c *doctorContext) mwDoctorLeavesWith(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected mw doctor to leave with %d, it leaves with %d (%v)", status, got, c.err)
	}
	return nil
}

func (c *doctorContext) theDoctorLogHolds(substr string) error {
	for _, line := range c.log.Lines() {
		if strings.Contains(line, substr) {
			return nil
		}
	}
	return fmt.Errorf("expected the doctor log to hold %q, got %v", substr, c.log.Lines())
}

func (c *doctorContext) theDoctorLogIsEmpty() error {
	if lines := c.log.Lines(); len(lines) != 0 {
		return fmt.Errorf("expected the doctor log empty, got %v", lines)
	}
	return nil
}

func (c *doctorContext) theCheckWasNotCured(name string) error {
	return c.theCheckWasCuredNTimes(name, "0")
}

func (c *doctorContext) theCheckWasCuredNTimes(name, wantText string) error {
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of cures: %w", wantText, err)
	}
	fake := c.fake(name)
	if fake == nil {
		return fmt.Errorf("no such doctor check in this scenario: %s", name)
	}
	if got := fake.Cures(); got != want {
		return fmt.Errorf("expected %s cured %d time(s), got %d", name, want, got)
	}
	return nil
}

func (c *doctorContext) mwDoctorPrinted(substr string) error {
	if !strings.Contains(c.out.String(), substr) {
		return fmt.Errorf("expected mw doctor to print %q, it printed:\n%s", substr, c.out.String())
	}
	return nil
}

// aFakeSystemctlThatSaysNeedsAReload writes a stand-in systemctl that answers
// `--user show <unit> -p NeedDaemonReload` yes for the named unit and no for
// any other, logs every call to a file this scenario reads back, and wires up
// the real infrastructure/doctor.DaemonReload check to run it.
func (c *doctorContext) aFakeSystemctlThatSaysNeedsAReload(unit string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the stand-in for systemctl is a shell script; this scenario does not run on windows")
	}
	dir, err := os.MkdirTemp("", "mw-doctor-systemctl")
	if err != nil {
		return err
	}
	program := filepath.Join(dir, "systemctl-stand-in")
	c.systemctlCalls = filepath.Join(dir, "calls")

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = daemon-reload ]; then
  exit 0
fi
if [ "$1" = --user ] && [ "$2" = show ]; then
  if [ "$3" = %q ]; then
    echo NeedDaemonReload=yes
  else
    echo NeedDaemonReload=no
  fi
  exit 0
fi
exit 1
`, c.systemctlCalls, unit)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return fmt.Errorf("writing the systemctl stand-in: %w", err)
	}

	c.real = doctor.NewDaemonReload([]string{unit})
	c.real.(*doctor.DaemonReload).Program = program
	return nil
}

// aFakeSystemctlReportingTheTimerEnabledAndInactive writes a stand-in
// systemctl that answers `--user is-enabled <timer>` enabled and `--user
// is-active <timer>` inactive for the named timer, succeeds `--user start
// <timer>`, logs every call to a file this scenario reads back, and wires up
// the real infrastructure/doctor.Timers check to run it, over the one unit
// that timer belongs to.
func (c *doctorContext) aFakeSystemctlReportingTheTimerEnabledAndInactive(timer string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the stand-in for systemctl is a shell script; this scenario does not run on windows")
	}
	dir, err := os.MkdirTemp("", "mw-doctor-timers-systemctl")
	if err != nil {
		return err
	}
	program := filepath.Join(dir, "systemctl-stand-in")
	c.systemctlCalls = filepath.Join(dir, "calls")

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = is-enabled ] && [ "$3" = %q ]; then
  echo enabled
  exit 0
fi
if [ "$1" = --user ] && [ "$2" = is-active ] && [ "$3" = %q ]; then
  echo inactive
  exit 3
fi
if [ "$1" = --user ] && [ "$2" = start ]; then
  exit 0
fi
exit 1
`, c.systemctlCalls, timer, timer)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return fmt.Errorf("writing the systemctl stand-in: %w", err)
	}

	unit := strings.TrimSuffix(timer, ".timer") + ".service"
	c.real = &doctor.Timers{Units: []string{unit}, Program: program}
	return nil
}

func (c *doctorContext) mwDoctorsTimersCheckRunsForReal() error { return c.run(false) }

// aVaultWhoseBeadsIsBytesPastABudget makes a temp dir standing in for a
// vault, with a .beads directory holding one file of exactly size bytes, and
// wires up the real infrastructure/doctor.BeadsSize check against it with
// the named budget — nothing here reads a real vault.
func (c *doctorContext) aVaultWhoseBeadsIsBytesPastABudget(sizeText, budgetText string) error {
	size, err := strconv.Atoi(sizeText)
	if err != nil {
		return fmt.Errorf("parsing %q as a byte count: %w", sizeText, err)
	}
	budget, err := strconv.Atoi(budgetText)
	if err != nil {
		return fmt.Errorf("parsing %q as a byte count: %w", budgetText, err)
	}

	dir, err := os.MkdirTemp("", "mw-doctor-beads-size")
	if err != nil {
		return err
	}
	beads := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", beads, err)
	}
	if err := os.WriteFile(filepath.Join(beads, "data"), make([]byte, size), 0o644); err != nil {
		return fmt.Errorf("writing the .beads fixture: %w", err)
	}

	c.real = &doctor.BeadsSize{Dir: dir, Budget: int64(budget)}
	return nil
}

func (c *doctorContext) mwDoctorsBeadsSizeCheckRunsForReal() error { return c.run(false) }

func (c *doctorContext) systemctlWasRunWith(args string) error {
	if c.systemctlCalls == "" {
		return fmt.Errorf("no fake systemctl was set up in this scenario")
	}
	data, err := os.ReadFile(c.systemctlCalls)
	if err != nil {
		return fmt.Errorf("reading the systemctl call log: %w", err)
	}
	if !strings.Contains(string(data), args) {
		return fmt.Errorf("expected systemctl to have been called with %q, it was called with:\n%s", args, data)
	}
	return nil
}

// aFakeNetshReportingTheNetwork writes a stand-in netsh.exe that answers
// `wlan show interfaces` with a fixture naming ssid (a BSSID line ahead of
// it, as real netsh output has), and `wlan disconnect` / `wlan connect`
// with success, logging every call to a file this scenario reads back, and
// wires up the real infrastructure/doctor.Wifi check to run it, its own
// clock tied to this scenario's.
func (c *doctorContext) aFakeNetshReportingTheNetwork(ssid string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the stand-in for netsh is a shell script; this scenario does not run on windows")
	}
	dir, err := os.MkdirTemp("", "mw-doctor-netsh")
	if err != nil {
		return err
	}
	program := filepath.Join(dir, "netsh-stand-in")
	c.netshCalls = filepath.Join(dir, "calls")

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = wlan ] && [ "$2" = show ] && [ "$3" = interfaces ]; then
  echo "    BSSID : 00:11:22:33:44:55"
  echo "    SSID : %s"
  exit 0
fi
if [ "$1" = wlan ] && [ "$2" = disconnect ]; then
  exit 0
fi
if [ "$1" = wlan ] && [ "$2" = connect ]; then
  exit 0
fi
exit 1
`, c.netshCalls, ssid)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return fmt.Errorf("writing the netsh stand-in: %w", err)
	}

	c.wifi = &doctor.Wifi{
		Netsh: program,
		State: c.state,
		Now:   func() time.Time { return c.now },
		// Sleep advances this scenario's own mocked clock rather than
		// really waiting, so Cure's post-reconnect poll (bounded by that
		// same clock) settles in no real time instead of spinning forever
		// against a clock nothing else is moving.
		Sleep: func(d time.Duration) { c.now = c.now.Add(d) },
	}
	c.real = c.wifi
	return nil
}

// aFakePowershellThatExists gives the scenario's wifi check a powershell.exe
// path that is present, so its probe is not inert.
func (c *doctorContext) aFakePowershellThatExists() error {
	if c.wifi == nil {
		return fmt.Errorf("no fake netsh was set up in this scenario")
	}
	dir, err := os.MkdirTemp("", "mw-doctor-powershell")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		return fmt.Errorf("writing a stand-in powershell.exe: %w", err)
	}
	c.wifi.Powershell = path
	return nil
}

// theInternetIsUnreachable points the scenario's wifi check at a closed
// local port, refusing every connection at once, standing in for the
// internet being down without a real network call.
func (c *doctorContext) theInternetIsUnreachable() error {
	if c.wifi == nil {
		return fmt.Errorf("no fake netsh was set up in this scenario")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("finding a closed port: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c.wifi.Reach = []string{addr}
	return nil
}

func (c *doctorContext) mwDoctorsWifiCheckRuns() error { return c.run(false) }

func (c *doctorContext) netshWasNotRun() error {
	lines, err := c.netshCallLines()
	if err != nil {
		return err
	}
	if len(lines) != 0 {
		return fmt.Errorf("expected netsh not to have run, it was called with:\n%v", lines)
	}
	return nil
}

func (c *doctorContext) netshWasRunWithNTimes(args, wantText string) error {
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of times: %w", wantText, err)
	}
	lines, err := c.netshCallLines()
	if err != nil {
		return err
	}
	got := 0
	for _, line := range lines {
		if line == args {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("expected netsh to have been called with %q %d time(s), got %d (all calls: %v)", args, want, got, lines)
	}
	return nil
}

// aVaultWithAModifiedTrackedFile makes a real git vault — a bare repository
// standing in for the remote both hosts share, and a clone of it — with path
// already committed, then rewrites it without committing: the fault a
// re-dispatch leaves behind by truncating its own runs/<id>/result.json or
// rewriting its boot.md. It wires the real infrastructure/doctor.VaultDirty
// check to run against that clone.
func (c *doctorContext) aVaultWithAModifiedTrackedFile(path string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is not on PATH; this scenario needs a real git clone")
	}
	root, err := os.MkdirTemp("", "mw-doctor-vault-dirty")
	if err != nil {
		return err
	}
	remote := filepath.Join(root, "origin.git")
	if _, err := runGit(root, "init", "--bare", "-q", "-b", "main", remote); err != nil {
		return err
	}
	dir := filepath.Join(root, "vault")
	if _, err := runGit(root, "clone", "-q", remote, dir); err != nil {
		return err
	}
	if _, err := runGit(dir, "config", "user.name", "millwright test"); err != nil {
		return err
	}
	if _, err := runGit(dir, "config", "user.email", "test@millwright.invalid"); err != nil {
		return err
	}

	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte("{}\n"), 0o644); err != nil {
		return err
	}
	if _, err := runGit(dir, "add", "-A"); err != nil {
		return err
	}
	if _, err := runGit(dir, "commit", "-qm", "the vault opens"); err != nil {
		return err
	}
	if _, err := runGit(dir, "push", "-q", "-u", "origin", "main"); err != nil {
		return err
	}

	// A re-dispatch truncating its own run file, not committing it.
	if err := os.WriteFile(full, []byte(""), 0o644); err != nil {
		return err
	}

	c.vaultDirtyDir = dir
	c.real = doctor.NewVaultDirty(dir, "laptop")
	return nil
}

func (c *doctorContext) mwDoctorsVaultDirtyCheckRunsForReal() error { return c.run(false) }

func (c *doctorContext) theDoctorLogHoldsAWayBackNamingTheCommitItMade() error {
	if c.vaultDirtyDir == "" {
		return fmt.Errorf("no vault-dirty scenario was set up")
	}
	hash, err := runGit(c.vaultDirtyDir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	for _, line := range c.log.Lines() {
		if strings.Contains(line, "revert") && strings.Contains(line, hash) {
			return nil
		}
	}
	return fmt.Errorf("expected the doctor log to hold a way back naming commit %s, got %v", hash, c.log.Lines())
}

func (c *doctorContext) theNoteHolds(key, substr string) error {
	value, ok := c.notes.Get(key)
	if !ok {
		return fmt.Errorf("expected the note %q to exist, it does not", key)
	}
	if !strings.Contains(value, substr) {
		return fmt.Errorf("expected the note %q to hold %q, it holds %q", key, substr, value)
	}
	return nil
}

func (c *doctorContext) theNoteDoesNotExist(key string) error {
	if value, ok := c.notes.Get(key); ok {
		return fmt.Errorf("expected the note %q not to exist, it holds %q", key, value)
	}
	return nil
}

func (c *doctorContext) netshCallLines() ([]string, error) {
	if c.netshCalls == "" {
		return nil, fmt.Errorf("no fake netsh was set up in this scenario")
	}
	data, err := os.ReadFile(c.netshCalls)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the netsh call log: %w", err)
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

// ensureMayorGoneVault makes, once per scenario, a temp dir standing in for a
// vault, and wires up the real infrastructure/doctor.MayorGone check against
// it. Nothing here reads a real vault or touches a real tmux server.
func (c *doctorContext) ensureMayorGoneVault() (string, error) {
	if c.mayorGoneVault != "" {
		return c.mayorGoneVault, nil
	}
	dir, err := os.MkdirTemp("", "mw-doctor-mayor-gone")
	if err != nil {
		return "", err
	}
	c.mayorGoneVault = dir
	c.real = doctor.NewMayorGone(dir)
	return dir, nil
}

func (c *doctorContext) mayorGoneCheck() (*doctor.MayorGone, error) {
	if _, err := c.ensureMayorGoneVault(); err != nil {
		return nil, err
	}
	check, ok := c.real.(*doctor.MayorGone)
	if !ok {
		return nil, fmt.Errorf("no mayor-gone check is set up in this scenario")
	}
	return check, nil
}

func (c *doctorContext) aVaultWithNoMayorActing() error {
	_, err := c.ensureMayorGoneVault()
	return err
}

func (c *doctorContext) aVaultWhoseMayorActingNamesTheWindow(name string) error {
	dir, err := c.ensureMayorGoneVault()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, application.ActingFileName("mayor")), []byte("acting in window "+name+"\n"), 0o644)
}

// writeMayorGoneTmux writes a stand-in tmux whose body answers list-windows
// and list-panes, the only two subcommands the real check ever runs, and
// wires the check to run it instead of a real tmux.
func (c *doctorContext) writeMayorGoneTmux(body string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the stand-in for tmux is a shell script; this scenario does not run on windows")
	}
	check, err := c.mayorGoneCheck()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "mw-doctor-mayor-gone-tmux")
	if err != nil {
		return err
	}
	program := filepath.Join(dir, "tmux-stand-in")
	script := "#!/bin/sh\n" + body + "exit 0\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return fmt.Errorf("writing the tmux stand-in: %w", err)
	}
	check.Tmux = program
	return nil
}

func (c *doctorContext) aStandInTmuxListingThatWindowWithALiveProcess() error {
	return c.writeMayorGoneTmux(`case "$1" in
list-windows) printf '@7|mayor-2026-09-23-39\n' ;;
list-panes) printf '0 claude\n' ;;
esac
`)
}

func (c *doctorContext) aStandInTmuxWithNoWindowOpen() error {
	return c.writeMayorGoneTmux(`case "$1" in
list-windows) : ;;
list-panes) printf '0 bash\n' ;;
esac
`)
}

// aStandInMayorUpThatStartsAMayorInWindow writes a stand-in bin/mayor-up
// under the scenario's vault that logs one line per call (for counting) and
// prints the window id last, the way the real script does, so WayBack reads
// it.
func (c *doctorContext) aStandInMayorUpThatStartsAMayorInWindow(windowID string) error {
	dir, err := c.ensureMayorGoneVault()
	if err != nil {
		return err
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	calls := filepath.Join(dir, "mayor-up-calls")
	c.mayorUpCalls = calls
	script := fmt.Sprintf(`#!/bin/sh
echo call >>%q
echo "started a Mayor in window %s from handoff N; the way back: tmux kill-window -t '%s'"
echo %s
exit 0
`, calls, windowID, windowID, windowID)
	return os.WriteFile(filepath.Join(binDir, "mayor-up"), []byte(script), 0o755)
}

// aStandInMayorUpThatAlwaysExits4Saying writes a stand-in bin/mayor-up that
// never starts a Mayor, the way the real script exits 4 when it cannot.
func (c *doctorContext) aStandInMayorUpThatAlwaysExits4Saying(msg string) error {
	dir, err := c.ensureMayorGoneVault()
	if err != nil {
		return err
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	calls := filepath.Join(dir, "mayor-up-calls")
	c.mayorUpCalls = calls
	script := fmt.Sprintf(`#!/bin/sh
echo call >>%q
echo %q
exit 4
`, calls, msg)
	return os.WriteFile(filepath.Join(binDir, "mayor-up"), []byte(script), 0o755)
}

func (c *doctorContext) mwDoctorsMayorGoneCheckRunsForReal() error { return c.run(false) }

func (c *doctorContext) mayorUpWasNotRun() error {
	return c.mayorUpCallCount(0)
}

func (c *doctorContext) mayorUpWasRunNTimes(wantText string) error {
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of calls: %w", wantText, err)
	}
	return c.mayorUpCallCount(want)
}

func (c *doctorContext) mayorUpCallCount(want int) error {
	if c.mayorUpCalls == "" {
		if want == 0 {
			return nil
		}
		return fmt.Errorf("no stand-in bin/mayor-up was set up in this scenario")
	}
	data, err := os.ReadFile(c.mayorUpCalls)
	if os.IsNotExist(err) {
		data = nil
	} else if err != nil {
		return fmt.Errorf("reading the mayor-up call log: %w", err)
	}
	text := strings.TrimRight(string(data), "\n")
	var got int
	if text != "" {
		got = len(strings.Split(text, "\n"))
	}
	if got != want {
		return fmt.Errorf("expected bin/mayor-up to have been run %d time(s), got %d:\n%s", want, got, data)
	}
	return nil
}
