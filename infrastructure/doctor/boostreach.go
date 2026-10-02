package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BoostReach)(nil)

// BoostReachName is what the check is called: in the log, and on the command
// line as `mw doctor boost-reach`.
const BoostReachName = "boost-reach"

// BoostReachAfter is how long the Boost must have failed to answer ssh,
// straight, before the check alarms.
const BoostReachAfter = 30 * time.Minute

// The boost-reach check's damper: a failed alarm is tried again a minute on,
// at the next run, and given up on, for a person, after three.
const (
	BoostReachDamperWait = time.Minute
	BoostReachDamperCap  = 3
)

// BoostReachSeenStateName is where the check keeps, between runs, when the
// Boost first failed to answer (FirstFaulty) and, once the Governor has been
// told, a latch (SeenPaths holding BoostReachAlarmed) so that the alarm is one
// and the next that answers is the "answers again" one. It is a key of its own,
// apart from the episode key Doctor keeps this check's cure state under.
const BoostReachSeenStateName = "boost-reach-since"

// BoostReachAlarmed is the SeenPaths value that says the Governor has been
// told the Boost is down.
const BoostReachAlarmed = "alarmed"

// boostReachSSHTimeout bounds one whole probe, a margin over ssh's own
// ConnectTimeout, so a connection that opens and then stalls is a failure too.
const boostReachSSHTimeout = 30 * time.Second

// BoostReach is the check that tells the Governor when the home's Boost has
// not answered ssh for BoostReachAfter (mw-6ww.60: the desktop's WSL stalled
// for 24 hours and nobody knew), and again when it does. It runs only on the
// home host, and probes the Boost with the ssh prefix [hands_hosts] gives it:
// `<prefix> -o BatchMode=yes -o ConnectTimeout=10 true`. Its cure is the
// alarm itself: it changes nothing on either host.
//
// While the home's own wg hub check is faulty the home is offline, not the
// Boost: the check then records nothing and alarms nothing, and forgets an
// outage it had not yet alarmed about. A host that is not home, or whose
// [hands_hosts] has no entry for the Boost, is ok, n/a.
type BoostReach struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Reach is [hands_hosts]: how this host reaches each other host by ssh.
	Reach map[string]string
	// State is where the check keeps when the Boost first failed to answer.
	State application.DoctorState
	// WgFaulty reports whether the home's wg hub check is faulty. Nil says it
	// is not.
	WgFaulty func(ctx context.Context) bool
	// Ssh runs one ssh command line and reports whether it succeeded. Nil
	// runs the real ssh.
	Ssh func(ctx context.Context, argv []string) error
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time
	// Alarm sends the Governor the alarm text. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) error

	// due is the alarm Probe found owing, for Cure to send.
	due boostReachDue
}

// boostReachDue is an alarm Probe found owing: its text, and whether it is the
// "answers again" one, which clears the state once sent.
type boostReachDue struct {
	text    string
	back    bool
	first   time.Time
	pending bool
}

// NewBoostReach is the check over the vault's home file, run on host, reaching
// the Boost by reach, remembering what it has seen in state.
func NewBoostReach(home application.HomeFile, host string, reach map[string]string, state application.DoctorState) *BoostReach {
	return &BoostReach{Home: home, Host: host, Reach: reach, State: state}
}

// Name implements application.DoctorCheck.
func (b *BoostReach) Name() string { return BoostReachName }

// Probe implements application.DoctorCheck: ok, n/a, on a host that is not
// home or with no way to reach the Boost; ok, saying why, while the wg hub
// check is faulty; otherwise ssh to the Boost. Failing, the first time is
// recorded; faulty once it has failed BoostReachAfter and the Governor has not
// been told; ok while it fails and has been told. Answering after the Governor
// was told is faulty, for the "answers again" alarm; answering otherwise
// forgets the outage.
func (b *BoostReach) Probe(ctx context.Context) (application.Verdict, string) {
	b.due = boostReachDue{}
	if b.Home == nil {
		return application.DoctorOK, "n/a: no home file to say whether this host is home"
	}
	home, err := application.IsHome(ctx, b.Home, b.Host)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, "n/a: this host is not home"
	}
	boost := application.BoostOf(b.Host)
	prefix := strings.Fields(b.Reach[boost])
	if len(prefix) < 2 {
		return application.DoctorOK, fmt.Sprintf("n/a: no [hands_hosts] entry for %s", boost)
	}

	episode, err := b.State.Load(ctx, BoostReachSeenStateName)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
	}
	alarmed := len(episode.SeenPaths) == 1 && episode.SeenPaths[0] == BoostReachAlarmed

	if b.WgFaulty != nil && b.WgFaulty(ctx) {
		if !alarmed && !episode.FirstFaulty.IsZero() {
			if err := b.State.Reset(ctx, BoostReachSeenStateName); err != nil {
				return application.DoctorCannotTell, fmt.Sprintf("forgetting this check's own state: %v", err)
			}
		}
		return application.DoctorOK, "n/a: the home's own wg hub check is faulty, so the home is offline, not the Boost"
	}

	argv := append(append([]string{}, prefix...), "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "true")
	if b.ssh(ctx, argv) == nil {
		switch {
		case alarmed:
			b.due = boostReachDue{text: fmt.Sprintf("The %s (Boost) answers again (down %s to %s UTC)", boost, stamp(episode.FirstFaulty), stamp(b.now())), back: true, pending: true}
			return application.DoctorFaulty, b.due.text
		case !episode.FirstFaulty.IsZero():
			if err := b.State.Reset(ctx, BoostReachSeenStateName); err != nil {
				return application.DoctorCannotTell, fmt.Sprintf("forgetting this check's own state: %v", err)
			}
		}
		return application.DoctorOK, ""
	}

	if episode.FirstFaulty.IsZero() {
		if err := b.State.Save(ctx, BoostReachSeenStateName, application.DoctorEpisode{FirstFaulty: b.now()}); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("saving this check's own state: %v", err)
		}
		return application.DoctorOK, ""
	}
	if alarmed || b.now().Sub(episode.FirstFaulty) < BoostReachAfter {
		return application.DoctorOK, ""
	}
	b.due = boostReachDue{
		text:    fmt.Sprintf("The %s (Boost) has not answered ssh since %s UTC: look at its screen; if WSL has stalled, run wsl --shutdown (mw-6ww.60).", boost, stamp(episode.FirstFaulty)),
		first:   episode.FirstFaulty,
		pending: true,
	}
	return application.DoctorFaulty, b.due.text
}

// Cure implements application.DoctorCheck: send the alarm Probe found owing,
// then remember it: the latch for the "down" alarm, nothing at all for the
// "answers again" one. The state is changed only once the alarm is sent, so
// one that could not be sent is tried again.
func (b *BoostReach) Cure(ctx context.Context) error {
	if !b.due.pending {
		return nil
	}
	if b.Alarm == nil {
		return fmt.Errorf("there is no way to send the boost-reach alarm")
	}
	if err := b.Alarm(ctx, b.due.text); err != nil {
		return fmt.Errorf("the boost-reach alarm could not be sent: %w", err)
	}
	due := b.due
	b.due = boostReachDue{}
	if due.back {
		if err := b.State.Reset(ctx, BoostReachSeenStateName); err != nil {
			return fmt.Errorf("forgetting this check's own state: %w", err)
		}
		return nil
	}
	err := b.State.Save(ctx, BoostReachSeenStateName, application.DoctorEpisode{FirstFaulty: due.first, SeenPaths: []string{BoostReachAlarmed}})
	if err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (b *BoostReach) Damper() (time.Duration, int) { return BoostReachDamperWait, BoostReachDamperCap }

// WayBack implements application.DoctorCheck.
func (b *BoostReach) WayBack() string {
	return "none needed: the alarm only tells the Governor; look at the Boost's screen, and run wsl --shutdown there if WSL has stalled"
}

func (b *BoostReach) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

func (b *BoostReach) ssh(ctx context.Context, argv []string) error {
	if b.Ssh != nil {
		return b.Ssh(ctx, argv)
	}
	ctx, cancel := context.WithTimeout(ctx, boostReachSSHTimeout)
	defer cancel()
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

// stamp is a time as the alarm says it: UTC, to the minute.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") }
