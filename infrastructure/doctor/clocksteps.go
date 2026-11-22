package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*ClockSteps)(nil)
	_ application.DoctorLongProbe       = (*ClockSteps)(nil)
	_ application.DoctorStandsAfterCure = (*ClockSteps)(nil)
)

// ClockStepsName is what the check is called: in the log, and on the command
// line as `mw doctor clock-steps`.
const ClockStepsName = "clock-steps"

// The sample: the wall clock is read against the monotonic clock every
// ClockStepsInterval for ClockStepsSample, and a step back of over
// ClockStepsLine in one interval is a fault. The desktop's WSL2 wall clock
// stepped back about 1.3 s roughly twice a minute (mw-3evcnk.7), which ordered
// tests read as a gap of -2815 ms (mw-xhtcup.20, mw-5r3p30.280).
const (
	ClockStepsInterval = 100 * time.Millisecond
	ClockStepsSample   = 90 * time.Second
	ClockStepsLine     = 100 * time.Millisecond
)

// ClockStepsEvery is how seldom the host samples: at most once an hour, so that
// the 90 s sample is not spent on every 5-minute run. A run inside the hour says
// again what the last sample found.
const ClockStepsEvery = time.Hour

// ClockStepsSampledStateName is where the check keeps, between runs, when it last
// sampled (FirstFaulty) and what it found (SeenPaths: empty is no step).
const ClockStepsSampledStateName = "clock-steps-sampled"

// The clock-steps check's damper: the cure is a note, so one a day is plenty,
// and it is never given up on.
const (
	ClockStepsDamperWait = 24 * time.Hour
	ClockStepsDamperCap  = 0
)

// ClockStepsCause is the cause on WSL2 and the one setting that ends it, said
// in the verdict and in the note. Windows pushes its time into a WSL2 guest
// every few seconds (Hyper-V implicit time sync) while the distro's own NTP
// client (systemd-timesyncd on Ubuntu 24.04 and earlier) sets the clock toward
// other servers, so the two disciplines fight and the wall clock steps; Ubuntu's
// WSL documentation names the conflict and the kernel option
// (https://documentation.ubuntu.com/wsl/stable/explanation/time-sync/).
const ClockStepsCause = "two clock disciplines on WSL2: Hyper-V's implicit time sync from Windows and the distro's own NTP client (systemd-timesyncd) both set the clock; " +
	"cure: `sudo systemctl disable --now systemd-timesyncd` (chrony on Ubuntu 25.10 and later), and if steps continue, add " +
	"kernelCommandLine = hv_utils.timesync_implicit=0 under [wsl2] in %USERPROFILE%\\.wslconfig, then `wsl --shutdown` " +
	"(WSL 2.5.4 and later may ignore that option: microsoft/WSL#12765)"

// ClockReading is the wall clock and the monotonic clock read together. Mono
// counts from any fixed start; only differences of it are read.
type ClockReading struct {
	Wall time.Time
	Mono time.Duration
}

// ClockSteps is the check that a WSL host's wall clock does not step back. It
// samples the wall clock against the monotonic clock and is faulty when the wall
// clock went back by over ClockStepsLine in one interval, saying how many steps
// and the largest; a forward jump is no fault. Inert on a host that is not WSL.
// Its cure is one note to the Mayor: the setting is the Governor's, on Windows.
type ClockSteps struct {
	// Read reads both clocks. Nil reads the real ones.
	Read func() ClockReading
	// Sleep waits d, ending early with ctx. Nil sleeps the real time.
	Sleep func(ctx context.Context, d time.Duration)
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// State keeps when the check last sampled and what it found. Nil samples
	// every run.
	State application.DoctorState
	// Now is the clock the hour is counted by. Nil reads the wall clock.
	Now func() time.Time
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error

	last string
	base time.Time
}

// NewClockSteps is the check over the real clocks.
func NewClockSteps() *ClockSteps { return &ClockSteps{} }

// Name implements application.DoctorCheck.
func (c *ClockSteps) Name() string { return ClockStepsName }

// ProbeBudget implements application.DoctorLongProbe: the sample is 90 s, over
// the run's one-minute budget for a check, with a margin for the state.
func (c *ClockSteps) ProbeBudget() time.Duration { return ClockStepsSample + 30*time.Second }

func (c *ClockSteps) now() time.Time {
	if c.Now == nil {
		//mw:realclock the default clock of the hour's throttle, when no Now is injected
		return time.Now()
	}
	return c.Now()
}

func (c *ClockSteps) read() ClockReading {
	if c.Read != nil {
		return c.Read()
	}
	if c.base.IsZero() {
		//mw:realclock the monotonic clock's start, which only differences of are read
		c.base = time.Now()
	}
	//mw:realclock the wall clock and the monotonic clock are what the check compares
	at := time.Now()
	// Round(0) strips the monotonic reading, so Wall is the wall clock alone.
	return ClockReading{Wall: at.Round(0), Mono: at.Sub(c.base)}
}

func (c *ClockSteps) sleep(ctx context.Context, d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(ctx, d)
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// Probe implements application.DoctorCheck.
func (c *ClockSteps) Probe(ctx context.Context) (application.Verdict, string) {
	c.last = ""
	virt, err := (&WSLTrim{DetectVirt: c.DetectVirt}).detectVirt(ctx)
	if err != nil {
		return application.DoctorOK, "n/a: systemd-detect-virt could not be run, so this host is not told to be WSL"
	}
	if virt != "wsl" {
		return application.DoctorOK, "n/a: not a WSL host (systemd-detect-virt says " + virt + ")"
	}
	if c.State != nil {
		kept, err := c.State.Load(ctx, ClockStepsSampledStateName)
		if err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
		}
		if age := c.now().Sub(kept.FirstFaulty); !kept.FirstFaulty.IsZero() && age >= 0 && age < ClockStepsEvery {
			return c.verdict(kept.SeenPaths)
		}
	}
	steps, ok := c.sample(ctx)
	if !ok {
		return application.DoctorCannotTell, "the 90 s sample of the wall clock was cut short"
	}
	var found []string
	if len(steps) > 0 {
		found = []string{c.said(steps)}
	}
	// A dry run changes nothing, the hour's clock included: the next real run
	// samples again.
	if c.State != nil && !application.DoctorDryRun(ctx) {
		if err := c.State.Save(ctx, ClockStepsSampledStateName, application.DoctorEpisode{FirstFaulty: c.now(), SeenPaths: found}); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("saving this check's own state: %v", err)
		}
	}
	return c.verdict(found)
}

func (c *ClockSteps) verdict(found []string) (application.Verdict, string) {
	if len(found) == 0 {
		return application.DoctorOK, "the wall clock went back by no more than 100 ms in 90 s"
	}
	c.last = found[0]
	return application.DoctorFaulty, c.last
}

// sample reads both clocks every ClockStepsInterval for ClockStepsSample and
// returns each step back of over ClockStepsLine, as how far back it went. It is
// not ok when ctx ended before the sample did.
func (c *ClockSteps) sample(ctx context.Context) (steps []time.Duration, ok bool) {
	prev := c.read()
	began := prev.Mono
	for prev.Mono-began < ClockStepsSample {
		c.sleep(ctx, ClockStepsInterval)
		if ctx.Err() != nil {
			return nil, false
		}
		next := c.read()
		// The wall clock's advance less the monotonic clock's: negative past the
		// line is a step back, positive a forward jump, which is no fault.
		if drift := next.Wall.Sub(prev.Wall) - (next.Mono - prev.Mono); drift < -ClockStepsLine {
			steps = append(steps, -drift)
		}
		prev = next
	}
	return steps, true
}

// said is the verdict's reason: how many steps, the largest, the cause and cure.
func (c *ClockSteps) said(steps []time.Duration) string {
	largest := steps[0]
	for _, s := range steps {
		largest = max(largest, s)
	}
	noun := "steps"
	if len(steps) == 1 {
		noun = "step"
	}
	return fmt.Sprintf("the wall clock stepped back %d %s of over 100 ms in 90 s, the largest %.1f s; likely cause: %s",
		len(steps), noun, largest.Seconds(), ClockStepsCause)
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. The setting is on Windows, which the Governor's hands reach.
func (c *ClockSteps) Cure(ctx context.Context) error {
	if c.Note == nil || c.last == "" {
		return nil
	}
	return c.Note(ctx, "clock-steps: "+c.last)
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the note changes
// nothing on the host.
func (c *ClockSteps) CureLeavesFault() bool { return true }

// Damper implements application.DoctorCheck.
func (c *ClockSteps) Damper() (time.Duration, int) { return ClockStepsDamperWait, ClockStepsDamperCap }

// WayBack implements application.DoctorCheck.
func (c *ClockSteps) WayBack() string {
	return "none needed: the check changes nothing on the host; the cure it names is the Governor's to make"
}
