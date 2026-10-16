package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BoostAsleep)(nil)

// BoostAsleepName is what the check is called: in the log, and on the command
// line as `mw doctor boost-asleep`.
const BoostAsleepName = "boost-asleep"

// The boost-asleep check's damper: there is no cure to retry, so one failed
// cure attempt is all an episode ever spends, as with postern-channel.
const (
	BoostAsleepDamperWait = 0 * time.Second
	BoostAsleepDamperCap  = 1
)

// boostAsleepAdvice is what Cure says after the reason: the check only
// reports.
const boostAsleepAdvice = "no cure: the check only reports; look at the Boost's screen, and run wsl --shutdown there if WSL has stalled (mw-6ww.60)"

// BoostAsleep is the check that tells a person when the home's Boost has gone
// quiet: its last recorded sync is further back than the host-silence
// threshold (host_silent_hours, application.DefaultHostSilence), the same
// line mw status draws to call a host ASLEEP. mw status says it only to whoever
// reads it; the desktop sat silent 49 hours, 2026-10-05 to 10-07, and nobody
// was told (mw-6ww.93).
//
// It runs only on the home host and looks only at the Boost, the other of
// desktop and laptop: the VPS runs no bd and is silent by design. A host that
// is not home is ok, n/a. A Boost with no recorded last sync, or one that is
// not a time, is cannot-tell. It has no cure and sends nothing itself: the
// verdict is the report, and its log line and note are how it reaches the
// Millhand.
type BoostAsleep struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Notes is where each host's last sync is recorded.
	Notes interface {
		Note(ctx context.Context, key string) (string, error)
	}
	// Work and Signs are where the Boost's other signs of life are read: its
	// claimed stories' claims and lease renewals, and what the vault holds of it
	// (mw-gq6.523). A Boost with a sign within Limit is not called asleep. Nil
	// ones, or ones that cannot be read, judge it by its last sync alone.
	Work interface {
		WorkInHand(ctx context.Context) (application.WorkInHand, error)
	}
	Signs application.HostSigns
	// Limit is how long the Boost may go without a sync. Zero reads
	// application.DefaultHostSilence.
	Limit time.Duration
	// LimitErr is why the threshold could not be read, for a config the host
	// cannot make sense of: the check then cannot tell, and the other checks
	// still run.
	LimitErr error
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time

	// faulty is the reason Probe last found the Boost silent, for Cure to
	// carry: a failed cure is what the doctor logs, in place of the probe's own
	// reason.
	faulty string
}

// NewBoostAsleep is the check for the host named host, against the given home
// file, reading last syncs from notes.
func NewBoostAsleep(home application.HomeFile, host string, notes interface {
	Note(ctx context.Context, key string) (string, error)
}) *BoostAsleep {
	return &BoostAsleep{Home: home, Host: host, Notes: notes}
}

// Name implements application.DoctorCheck.
func (b *BoostAsleep) Name() string { return BoostAsleepName }

// Probe implements application.DoctorCheck: cannot-tell when the home cannot be
// told, the threshold cannot be read, or the Boost has no readable last sync;
// ok, n/a, on a host that is not home; faulty when the Boost's last sync is
// older than the threshold, naming it and how long; ok otherwise.
func (b *BoostAsleep) Probe(ctx context.Context) (application.Verdict, string) {
	b.faulty = ""
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
	if b.LimitErr != nil {
		return application.DoctorCannotTell, b.LimitErr.Error()
	}
	limit := b.Limit
	if limit <= 0 {
		limit = application.DefaultHostSilence
	}

	boost := application.BoostOf(b.Host)
	said, err := b.Notes.Note(ctx, application.LastSyncKey(boost))
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading when %s last synced: %v", boost, err)
	}
	said = strings.TrimSpace(said)
	if said == "" {
		return application.DoctorCannotTell, fmt.Sprintf("%s has recorded no last sync", boost)
	}
	at, err := time.Parse(application.LastSyncFormat, said)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("%s's last sync is %q, which is not a time", boost, said)
	}

	silent := b.now().Sub(at)
	reason := fmt.Sprintf("%s silent %s (threshold %s)", boost, application.Clock(silent), thresholdFor(limit))
	if silent > limit {
		// A Boost syncs on its dispatch ticks, so its sync age alone is not a sign
		// it is asleep: it is called so only when nothing else of it is newer than
		// the threshold either (mw-gq6.523).
		if sign := b.newestSign(ctx, boost); sign.At.After(at) {
			quiet := b.now().Sub(sign.At)
			if quiet <= limit {
				return application.DoctorOK, fmt.Sprintf("%s synced %s ago, but %s %s ago (threshold %s)",
					boost, application.Clock(silent), withArticle(sign.What), application.Clock(quiet), thresholdFor(limit))
			}
			reason += fmt.Sprintf(", newest sign of life %s %s ago", withArticle(sign.What), application.Clock(quiet))
		}
		b.faulty = reason
		return application.DoctorFaulty, reason
	}
	return application.DoctorOK, reason
}

// newestSign is the newest sign of life of the Boost besides its sync note
// (application.NewestSign). Work that cannot be read leaves out the claims and
// leases, and signs that cannot be read the vault's: the Boost is judged by the
// rest.
func (b *BoostAsleep) newestSign(ctx context.Context, boost string) application.HostSign {
	var stories []application.StoryDetail
	if b.Work != nil {
		if work, err := b.Work.WorkInHand(ctx); err == nil {
			stories = work.PathedTo(boost)
		}
	}
	return application.NewestSign(ctx, b.Signs, boost, stories)
}

// withArticle puts "a" or "an" before a noun phrase.
func withArticle(what string) string {
	if what != "" && strings.ContainsRune("aeiou", rune(what[0])) {
		return "an " + what
	}
	return "a " + what
}

// Cure implements application.DoctorCheck: there is none. It fails, saying why
// the Boost is called asleep, so the doctor's log and note name the host and
// how long.
func (b *BoostAsleep) Cure(context.Context) error {
	if b.faulty == "" {
		return errors.New(boostAsleepAdvice)
	}
	return fmt.Errorf("%s: %s", b.faulty, boostAsleepAdvice)
}

// Damper implements application.DoctorCheck.
func (b *BoostAsleep) Damper() (time.Duration, int) {
	return BoostAsleepDamperWait, BoostAsleepDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes, so there
// is nothing to undo.
func (b *BoostAsleep) WayBack() string {
	return "none: no cure runs; look at the Boost's screen, and run wsl --shutdown there if WSL has stalled"
}

func (b *BoostAsleep) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

// thresholdFor is the threshold as the report says it: "2h" for whole hours,
// which host_silent_hours always is, and the status line's clock otherwise.
func thresholdFor(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return application.Clock(d)
}
