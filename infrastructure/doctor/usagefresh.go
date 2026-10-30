package doctor

import (
	"context"
	"fmt"
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

// usageFreshFix is the cure the alarm names, a person's to do.
const usageFreshFix = "the mw-usage-read timer has not run: systemctl --user list-timers | grep usage"

// UsageFresh is the check that notices a quota reading nobody has taken
// lately (mw-gq6.855). On 2026-10-29 the newest reading stood 95% for 11 hours
// while the week reached 100%, because the timer that reads the usage had never
// been installed and nothing said the reading was old. It is faulty when the
// newest reading is stale (application.UsageReading.Stale: older than
// application.UsageStaleAfter, or taken before a reset that has passed) or when
// there is none, and ok otherwise. Only the home reads the usage, so a host
// that is not home says ok. Its cure is the alarm to the Governor.
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

// problem is why the newest reading cannot be trusted, empty when it can.
func (u *UsageFresh) problem(ctx context.Context) (string, error) {
	if u.Home != nil {
		if home, err := application.IsHome(ctx, u.Home, u.Host); err == nil && !home {
			return "", nil
		}
	}
	all, err := u.Readings.All(ctx)
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "no usage reading was ever kept; " + usageFreshFix, nil
	}
	if why := all[len(all)-1].Stale(u.now().UTC()); why != "" {
		return "STALE: " + why + "; " + usageFreshFix, nil
	}
	return "", nil
}

// Probe implements application.DoctorCheck.
func (u *UsageFresh) Probe(ctx context.Context) (application.Verdict, string) {
	found, err := u.problem(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if found != "" {
		return application.DoctorFaulty, found
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: send the alarm.
func (u *UsageFresh) Cure(ctx context.Context) error {
	found, err := u.problem(ctx)
	if err != nil || found == "" {
		return err
	}
	if u.Alarm == nil {
		return fmt.Errorf("there is no way to send the usage-fresh alarm")
	}
	if _, err := u.Alarm(ctx, "Usage reading "+found); err != nil {
		return fmt.Errorf("the usage-fresh alarm could not be sent: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (u *UsageFresh) Damper() (time.Duration, int) { return UsageFreshDamperWait, UsageFreshDamperCap }

// WayBack implements application.DoctorCheck.
func (u *UsageFresh) WayBack() string {
	return "none needed: the alarm only tells the Governor; install or start the mw-usage-read timer"
}
