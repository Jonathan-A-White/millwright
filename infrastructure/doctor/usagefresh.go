package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*UsageFresh)(nil)

// UsageFreshName is what the check is called: in the log, and on the command
// line as `mw doctor usage-fresh`.
const UsageFreshName = "usage-fresh"

// The usage-fresh check's damper (PROVISIONAL, the Governor to confirm: a
// fault pages his phone): the alarm is sent again three hours on, up to eight
// times, so that a reading nobody takes does not go unheard for a night.
const (
	UsageFreshDamperWait = 3 * time.Hour
	UsageFreshDamperCap  = 8
)

// When a stale reading pages him (mw-gq6.883; PROVISIONAL, the Governor to
// confirm: it is his phone). A reading past the age of application.UsageStaleAfter
// is always shown as STALE in mw status, but pages only when it could hide a
// trip of the pause: it was taken before a reset that has passed, it is over
// UsageFreshPageAge old, or its percent is within UsageFreshPageMargin points of
// where the pause trips. No reading at all pages too. On 2026-10-29 a reading
// stood at 95% for 11 hours while the week reached 100%; on 2026-11-02 a 41%
// reading 72 minutes old, with 53 points of room, paged as an Emergency.
const (
	UsageFreshPageAge    = 6 * time.Hour
	UsageFreshPageMargin = 20.0
)

// usageFreshAlarmStateName is where the check remembers, between runs, the seqs
// of the alarms it has sent, so a fresh reading can clear each.
const usageFreshAlarmStateName = "usage-fresh-alarm"

// usageTimerUnit is the user timer that reads the usage.
const usageTimerUnit = "mw-usage-read.timer"

// UsageFresh is the check that notices a quota reading nobody has taken
// lately (mw-gq6.855). On 2026-10-29 the newest reading stood 95% for 11 hours
// while the week reached 100%, because the timer that reads the usage had never
// been installed and nothing said the reading was old. It is faulty when the
// newest reading is stale (application.UsageReading.Stale: older than
// application.UsageStaleAfter, or taken before a reset that has passed) and
// that is worth his phone (see UsageFreshPageAge), or when there is none, and
// ok otherwise; a stale reading with room to spare is ok, said in the reason
// and in mw status. Only the home reads the usage, so a host that is not home
// says ok. Its cure is the alarm to the Governor, ended by a Clear when the
// reading is fresh again.
type UsageFresh struct {
	// Readings are the readings kept, in a note of the tracker.
	Readings application.UsageReadings
	// Home and Host are the vault's home file and this host's name: a host
	// that is not home takes no readings. A nil Home asks on every host.
	Home application.HomeFile
	Host string
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time
	// Alarm sends the Governor the text and gives the seq of the emergency
	// event it was written as, 0 for none. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) (uint64, error)
	// Clear tells the Governor the reading is fresh again, in the normal lane,
	// naming in clears the emergency of an alarm. A nil Clear sends none.
	Clear func(ctx context.Context, text string, clears uint64) error
	// State is where the check keeps the seqs of the alarms it has sent.
	State application.DoctorState
	// Floors is the floor under the pause's reserve, which moves its trip.
	Floors application.UsageFloors
	// Timer says how the mw-usage-read timer unit stands on this host. A nil
	// Timer cannot ask, and the alarm says so.
	Timer func(ctx context.Context) (UsageTimerState, error)
}

// UsageTimerState is how systemd knows the mw-usage-read timer.
type UsageTimerState struct {
	// Installed is whether a unit file for the timer is on this host.
	Installed bool
	// Active is whether the timer is turned on and waiting for its next run.
	Active bool
}

// NewUsageFresh is the check over the readings.
func NewUsageFresh(readings application.UsageReadings) *UsageFresh {
	return &UsageFresh{Readings: readings}
}

// Name implements application.DoctorCheck.
func (u *UsageFresh) Name() string { return UsageFreshName }

func (u *UsageFresh) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// usageFreshFix is the cure the alarm names, a person's to do, said from how
// the timer unit stands on this host, read rather than assumed.
func (u *UsageFresh) usageFreshFix(ctx context.Context) string {
	if u.Timer == nil {
		return "the mw-usage-read timer could not be asked about: systemctl --user list-timers | grep usage"
	}
	state, err := u.Timer(ctx)
	switch {
	case err != nil:
		return "the mw-usage-read timer could not be asked about (" + err.Error() + "): systemctl --user list-timers | grep usage"
	case !state.Installed:
		return "the mw-usage-read timer is not installed here: sh scripts/install-units.sh --enable mw-usage-read"
	case !state.Active:
		return "the mw-usage-read timer is installed but not turned on: systemctl --user enable --now " + usageTimerUnit
	}
	return "the mw-usage-read timer is on, but its reads are not taking: journalctl --user -u mw-usage-read.service"
}

// finding is what one look at the newest reading found.
type finding struct {
	// why is the reading's trouble, empty when it is fresh.
	why string
	// page is whether it is worth the Governor's phone.
	page bool
	// note says, for a stale reading that does not page, why it does not.
	note string
}

// look judges the newest reading. A host that is not home, or a reading that is
// fresh, finds nothing.
func (u *UsageFresh) look(ctx context.Context) (finding, application.UsageReading, error) {
	if u.Home != nil {
		if home, err := application.IsHome(ctx, u.Home, u.Host); err == nil && !home {
			return finding{}, application.UsageReading{}, nil
		}
	}
	all, err := u.Readings.All(ctx)
	if err != nil {
		return finding{}, application.UsageReading{}, err
	}
	if len(all) == 0 {
		return finding{why: "no usage reading was ever kept", page: true}, application.UsageReading{}, nil
	}
	last := all[len(all)-1]
	now := u.now().UTC()
	stale := last.Stale(now)
	if stale == "" {
		return finding{}, last, nil
	}
	if last.BeforeReset(now) {
		return finding{why: "STALE: " + stale, page: true}, last, nil
	}
	if age := now.Sub(last.At); age > UsageFreshPageAge {
		return finding{why: "STALE: " + stale, page: true}, last, nil
	}
	var floor application.UsageFloor
	if floor, err = u.Floors.Get(ctx); err != nil {
		// The trip cannot be worked out, so the room cannot be vouched for.
		return finding{why: "STALE: " + stale + "; the pause's floor could not be read: " + err.Error(), page: true}, last, nil
	}
	reserve, _ := floor.Reserve(last.Resets, now)
	trip := 100 - reserve
	where := fmt.Sprintf("%s, at %.1f%% with the pause tripping at %.2f%%", stale, last.Percent, trip)
	if last.Percent >= trip-UsageFreshPageMargin {
		return finding{why: "STALE: " + where, page: true}, last, nil
	}
	return finding{
		why:  "STALE: " + where,
		note: fmt.Sprintf("STALE, no page: %s; room to spare, and it pages past %s old or within %.0f points of the trip; mw status shows STALE", where, UsageFreshPageAge, UsageFreshPageMargin),
	}, last, nil
}

// Probe implements application.DoctorCheck: faulty only for a reading that
// pages; ok, saying so, for a stale one with room to spare. A reading that no
// longer pages ends the alarm sent for an earlier one.
func (u *UsageFresh) Probe(ctx context.Context) (application.Verdict, string) {
	found, last, err := u.look(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if found.page {
		return application.DoctorFaulty, found.why + "; " + u.usageFreshFix(ctx)
	}
	if err := u.ended(ctx, last); err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	return application.DoctorOK, found.note
}

// Cure implements application.DoctorCheck: send the alarm, then remember its
// seq. The seq is kept only once the alarm is sent, so one that could not be
// sent is tried again.
func (u *UsageFresh) Cure(ctx context.Context) error {
	found, _, err := u.look(ctx)
	if err != nil || !found.page {
		return err
	}
	if u.Alarm == nil {
		return fmt.Errorf("there is no way to send the usage-fresh alarm")
	}
	seq, err := u.Alarm(ctx, "Usage reading "+found.why+"; "+u.usageFreshFix(ctx))
	if err != nil {
		return fmt.Errorf("the usage-fresh alarm could not be sent: %w", err)
	}
	if u.State == nil || seq == 0 {
		return nil
	}
	// The damper sends the alarm again while the reading stays old: the seqs
	// of every alarm are kept, and the fresh reading clears each.
	var seqs []string
	if prior, err := u.State.Load(ctx, usageFreshAlarmStateName); err == nil {
		seqs = prior.SeenPaths
	}
	if err := u.State.Save(ctx, usageFreshAlarmStateName, application.DoctorEpisode{SeenPaths: withSeq(seqs, seq)}); err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// ended tells the Governor, once, that the reading no longer needs him, naming
// each alarm's emergency, then forgets them. With no alarm recorded there is
// nothing to say; in a dry run nothing is sent or forgotten. The seqs are
// forgotten only once every event is sent, so one that could not be sent is
// tried again (a clear of a cleared seq is harmless).
func (u *UsageFresh) ended(ctx context.Context, last application.UsageReading) error {
	if u.State == nil || application.DoctorDryRun(ctx) {
		return nil
	}
	episode, err := u.State.Load(ctx, usageFreshAlarmStateName)
	if err != nil {
		return fmt.Errorf("reading this check's own state: %w", err)
	}
	if len(episode.SeenPaths) == 0 {
		return nil
	}
	if u.Clear != nil {
		text := fmt.Sprintf("Usage reading no longer stale: %.1f%% read %s", last.Percent, last.At.UTC().Format("01-02 15:04Z"))
		for i := range episode.SeenPaths {
			clears := seqAt(episode.SeenPaths, i)
			if clears == 0 {
				continue
			}
			if err := u.Clear(ctx, text, clears); err != nil {
				return fmt.Errorf("the usage-fresh recovery could not be sent: %w", err)
			}
		}
	}
	if err := u.State.Reset(ctx, usageFreshAlarmStateName); err != nil {
		return fmt.Errorf("forgetting this check's own state: %w", err)
	}
	return nil
}

// SystemctlUsageTimer asks `systemctl --user show` how the mw-usage-read timer
// stands on this host.
func SystemctlUsageTimer(ctx context.Context) (UsageTimerState, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", usageTimerUnit, "-p", "LoadState", "-p", "ActiveState").Output()
	if err != nil {
		return UsageTimerState{}, err
	}
	var state UsageTimerState
	for _, line := range strings.Split(string(out), "\n") {
		switch strings.TrimSpace(line) {
		case "LoadState=loaded":
			state.Installed = true
		case "ActiveState=active":
			state.Active = true
		}
	}
	return state, nil
}

// Damper implements application.DoctorCheck.
func (u *UsageFresh) Damper() (time.Duration, int) { return UsageFreshDamperWait, UsageFreshDamperCap }

// WayBack implements application.DoctorCheck.
func (u *UsageFresh) WayBack() string {
	return "none needed: the alarm only tells the Governor; install or start the mw-usage-read timer"
}
