package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*BoostFit)(nil)
	_ application.DoctorStandsAfterCure = (*BoostFit)(nil)
)

// BoostFitName is what the check is called: in the log, and on the command line
// as `mw doctor boost-fit`.
const BoostFitName = "boost-fit"

// BoostFitEvery is how seldom the home asks a Boost: at most once an hour. A run
// inside the hour says again what the last one found.
const BoostFitEvery = time.Hour

// BoostFitSeenStateName is where the check keeps, between runs, when it last
// asked (FirstFaulty) and one line a host that was not fit (SeenPaths). It is a
// key of its own, apart from the episode key Doctor keeps this check's cure
// state under.
const BoostFitSeenStateName = "boost-fit-asked"

// The boost-fit check's damper: the cure is a note, so one a day is plenty, and
// it is never given up on.
const (
	BoostFitDamperWait = 24 * time.Hour
	BoostFitDamperCap  = 0
)

// boostFitCommand is what the Boost is asked, in one ssh call.
const boostFitCommand = "mw host fit --json"

// boostFitSSHTimeout bounds one whole ask: a margin over ssh's own
// ConnectTimeout and a git read or two on the far end.
const boostFitSSHTimeout = 40 * time.Second

// boostFitSSHFailed is the exit status ssh itself ends with when it could not
// reach the host: that is boost-reach's to say.
const boostFitSSHFailed = 255

// BoostFit is the check, run by the home, that each Boost is fit: it asks every
// host of [hands_hosts] that is not retired, not a cloud box and not this one
// for `mw host fit --json` over ssh, at most once an hour, and is faulty naming
// each host's gaps, each with its cure (mw-6ww.185.9). It adds no fix of its
// own: it is the list, checked every hour. A host that does not answer ssh is
// boost-reach's to say, and is left out. A host whose mw has no `host fit` is
// itself a gap: mw there is behind main. On a Boost the check is ok, asked by
// the home, and so it is where there is no Boost to ask.
type BoostFit struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Reach is [hands_hosts]: how this host reaches each other host by ssh.
	Reach map[string]string
	// Retired are the hosts the Governor retired (retired_hosts).
	Retired []string
	// Cloud names the cloud boxes, which are asked nothing: the cloud check owns
	// them. Nil says there are none.
	Cloud func(ctx context.Context) ([]string, error)
	// State keeps when the check last asked and what it found.
	State application.DoctorState
	// Ask runs one ssh command line and returns what it printed and its exit
	// status; a non-nil error is a failure to run it at all. Nil runs the real ssh.
	Ask func(ctx context.Context, argv []string) (out []byte, exit int, err error)
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error

	last string
}

// NewBoostFit is the check over the vault's home file, run on host, asking the
// hosts reach names, remembering what it asked in state.
func NewBoostFit(home application.HomeFile, host string, reach map[string]string, state application.DoctorState) *BoostFit {
	return &BoostFit{Home: home, Host: host, Reach: reach, State: state}
}

// Name implements application.DoctorCheck.
func (b *BoostFit) Name() string { return BoostFitName }

func (b *BoostFit) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

// Probe implements application.DoctorCheck.
func (b *BoostFit) Probe(ctx context.Context) (application.Verdict, string) {
	b.last = ""
	if b.Home == nil {
		return application.DoctorOK, "n/a: no home file to say whether this host is home"
	}
	home, err := application.IsHome(ctx, b.Home, b.Host)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, "asked by the home"
	}
	hosts, err := b.targets(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if len(hosts) == 0 {
		return application.DoctorOK, "n/a: there is no Boost to ask"
	}

	kept, err := b.State.Load(ctx, BoostFitSeenStateName)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
	}
	var lines []string
	if !kept.FirstFaulty.IsZero() && b.now().Sub(kept.FirstFaulty) >= 0 && b.now().Sub(kept.FirstFaulty) < BoostFitEvery {
		lines = kept.SeenPaths
	} else {
		lines = b.askAll(ctx, hosts)
		if err := b.State.Save(ctx, BoostFitSeenStateName, application.DoctorEpisode{FirstFaulty: b.now(), SeenPaths: lines}); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("saving this check's own state: %v", err)
		}
	}
	if len(lines) == 0 {
		return application.DoctorOK, ""
	}
	b.last = strings.Join(lines, " | ")
	return application.DoctorFaulty, b.last
}

// targets are the hosts to ask, in name order: those of [hands_hosts] that are
// not this host, retired or a cloud box and have an ssh prefix.
func (b *BoostFit) targets(ctx context.Context) ([]string, error) {
	var cloud []string
	if b.Cloud != nil {
		var err error
		if cloud, err = b.Cloud(ctx); err != nil {
			return nil, fmt.Errorf("reading the cloud boxes: %w", err)
		}
	}
	var hosts []string
	for name, prefix := range b.Reach {
		switch {
		case name == b.Host, application.IsRetired(b.Retired, name), application.IsRetired(cloud, name), len(strings.Fields(prefix)) < 2:
			continue
		}
		hosts = append(hosts, name)
	}
	sort.Strings(hosts)
	return hosts, nil
}

// askAll asks every host together and returns one line for each that is not
// fit, in name order: "<host>: <gap> (cure: <cure>); <gap> (cure: ...)".
func (b *BoostFit) askAll(ctx context.Context, hosts []string) []string {
	said := make([]string, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			said[i] = b.askOne(ctx, host)
		}()
	}
	wg.Wait()
	var lines []string
	for _, line := range said {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// askOne is the one line for a host, "" when it is fit or does not answer.
func (b *BoostFit) askOne(ctx context.Context, host string) string {
	argv := append(append([]string{}, strings.Fields(b.Reach[host])...), "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", boostFitCommand)
	out, exit, err := b.ask(ctx, argv)
	if err != nil || exit == boostFitSSHFailed {
		return ""
	}
	if exit != 0 {
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
		return fmt.Sprintf("%s: `%s` did not run there (exit %d: %s) (cure: %s)", host, boostFitCommand, exit, fitOneLine(first), application.HostFitCureUpdate)
	}
	var report application.HostFitReport
	if err := json.Unmarshal(out, &report); err != nil {
		return fmt.Sprintf("%s: `%s` printed what could not be read (%v) (cure: %s)", host, boostFitCommand, err, application.HostFitCureUpdate)
	}
	gaps := report.Gaps()
	if len(gaps) == 0 {
		return ""
	}
	parts := make([]string, len(gaps))
	for i, gap := range gaps {
		parts[i] = fitOneLine(gap.Name+": "+gap.Detail) + " (cure: " + fitOneLine(gap.Cure) + ")"
	}
	return fmt.Sprintf("%s is not fit, %d gaps: %s", host, len(gaps), strings.Join(parts, "; "))
}

func fitOneLine(s string) string {
	return strings.NewReplacer("\t", " ", "|", "/").Replace(strings.Join(strings.Fields(s), " "))
}

func (b *BoostFit) ask(ctx context.Context, argv []string) ([]byte, int, error) {
	if b.Ask != nil {
		return b.Ask(ctx, argv)
	}
	ctx, cancel := context.WithTimeout(ctx, boostFitSSHTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.Bytes(), exit.ExitCode(), nil
	}
	return out.Bytes(), 0, err
}

// Cure implements application.DoctorCheck: the check adds no fix, so its cure is
// one note to the Mayor naming each host's gaps with their cures.
func (b *BoostFit) Cure(ctx context.Context) error {
	if b.Note == nil || b.last == "" {
		return nil
	}
	return b.Note(ctx, "boost-fit: "+b.last)
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the note
// changes nothing on the Boost.
func (b *BoostFit) CureLeavesFault() bool { return true }

// Damper implements application.DoctorCheck.
func (b *BoostFit) Damper() (time.Duration, int) { return BoostFitDamperWait, BoostFitDamperCap }

// WayBack implements application.DoctorCheck.
func (b *BoostFit) WayBack() string {
	return "none needed: the check changes nothing on a Boost; it names each gap with the command that closes it"
}

// SystemdTime is the time of a systemd timestamp property ('@1730000000', or
// 'Tue 2026-11-17 14:12:19 UTC' as systemd 255 prints it in UTC), and whether
// there is one: empty, n/a and @0 are a unit that never ran.
func SystemdTime(value string) (time.Time, bool) { return lastTrigger(value) }
