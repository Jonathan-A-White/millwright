package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Battery)(nil)

// BatteryName is what the check is called: in the log, and on the command
// line as `mw doctor battery`.
const BatteryName = "battery"

// DefaultBatteryDir is where Linux lists a host's power supplies, a battery
// among them as BAT0, BAT1 and so on.
const DefaultBatteryDir = "/sys/class/power_supply"

// The percent at which the check first alarms and the lower one at which it
// alarms again, when nothing says otherwise. config.DefaultDoctorBatteryLow
// and DefaultDoctorBatteryCritical are the same numbers.
const (
	DefaultBatteryLow      = 25
	DefaultBatteryCritical = 10
)

// The battery check's damper: a failed alarm is tried again a minute on, at
// the next run, and given up on, for a person, after three.
const (
	BatteryDamperWait = time.Minute
	BatteryDamperCap  = 3
)

// batterySeenStateName is where the check remembers, between runs, the
// threshold it last alarmed at, apart from the episode key Doctor keeps this
// check's cure state under.
const batterySeenStateName = "battery-seen"

// Battery is the check that warns before the home sleeps on a flat battery
// (mw-gq6.206: the laptop went down at 17:54Z on 2026-10-01 with nobody
// told). The battery is read from /sys/class/power_supply. It is faulty only
// while a battery is Discharging at or under Low percent, and then only once
// per fall below each threshold — Low, then Critical — so a battery sitting
// at 20% for an hour is one alarm, and one that falls on to 9% is a second.
// Charging, Full, anything above Low, and a host with no battery are ok.
// Its cure is the alarm itself: it changes nothing on the host.
type Battery struct {
	// Dir is the power_supply directory. Empty reads DefaultBatteryDir.
	Dir string
	// Low and Critical are the two thresholds, in percent. Zero reads the
	// defaults.
	Low, Critical int
	// State is where the check keeps the threshold it last alarmed at.
	State application.DoctorState
	// Alarm sends the Governor the alarm text and gives the seq of the
	// emergency event it was written as, 0 for none. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) (uint64, error)
	// Clear tells the Governor the battery has recovered, in the normal lane,
	// naming in clears the emergency of the last alarm. A nil Clear sends none.
	Clear func(ctx context.Context, text string, clears uint64) error
}

// NewBattery is the check over the power_supply directory dir, remembering
// what it has alarmed about in state.
func NewBattery(dir string, state application.DoctorState) *Battery {
	return &Battery{Dir: dir, State: state}
}

// Name implements application.DoctorCheck.
func (b *Battery) Name() string { return BatteryName }

// batteryReading is what one look at the battery found.
type batteryReading struct {
	found       bool
	discharging bool
	capacity    int
}

// Probe implements application.DoctorCheck: ok, with a reason said once, on a
// host with no battery; ok while the battery is not discharging or is above
// Low (forgetting what it alarmed about, so the next fall alarms afresh);
// ok too while it is under a threshold it has already alarmed at; faulty,
// naming the percent, on a fall to a threshold it has not.
func (b *Battery) Probe(ctx context.Context) (application.Verdict, string) {
	reading, err := b.read()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !reading.found {
		return application.DoctorOK, "n/a: no battery here"
	}
	if !reading.discharging || reading.capacity > b.low() {
		if err := b.recovered(ctx, reading); err != nil {
			return application.DoctorCannotTell, err.Error()
		}
		return application.DoctorOK, ""
	}

	seen, err := b.seen(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if seen != 0 && b.band(reading.capacity) >= seen {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, alarmText(reading.capacity)
}

// Cure implements application.DoctorCheck: send the alarm, then remember the
// threshold it was for. A battery that has been plugged in since Probe needs
// no alarm. The threshold is kept only once the alarm is sent, so one that
// could not be sent is tried again.
func (b *Battery) Cure(ctx context.Context) error {
	reading, err := b.read()
	if err != nil {
		return err
	}
	if !reading.found || !reading.discharging || reading.capacity > b.low() {
		return nil
	}
	if b.Alarm == nil {
		return fmt.Errorf("there is no way to send the battery alarm")
	}
	seq, err := b.Alarm(ctx, alarmText(reading.capacity))
	if err != nil {
		return fmt.Errorf("the battery alarm could not be sent: %w", err)
	}
	if b.State == nil {
		return nil
	}
	// A fall on from Low to Critical is a second emergency: the seqs of every
	// alarm of the fall are kept, and the recovery clears each.
	var earlier []string
	if prior, err := b.State.Load(ctx, batterySeenStateName); err == nil && len(prior.SeenPaths) > 1 {
		earlier = prior.SeenPaths[1:]
	}
	paths := append([]string{strconv.Itoa(b.band(reading.capacity))}, earlier...)
	err = b.State.Save(ctx, batterySeenStateName, application.DoctorEpisode{SeenPaths: withSeq(paths, seq)})
	if err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (b *Battery) Damper() (time.Duration, int) { return BatteryDamperWait, BatteryDamperCap }

// WayBack implements application.DoctorCheck.
func (b *Battery) WayBack() string {
	return "none needed: the alarm only tells the Governor; plug the laptop in"
}

func alarmText(capacity int) string {
	return fmt.Sprintf("Laptop battery %d%%, discharging: plug it in or it sleeps", capacity)
}

func recoveredText(reading batteryReading) string {
	if reading.discharging {
		return fmt.Sprintf("Laptop battery %d%%: recovered above the alarm", reading.capacity)
	}
	return fmt.Sprintf("Laptop battery %d%%, no longer discharging: plugged in", reading.capacity)
}

// band is the threshold a capacity has fallen to: Critical at or under it,
// otherwise Low. A smaller band is a worse one.
func (b *Battery) band(capacity int) int {
	if capacity <= b.critical() {
		return b.critical()
	}
	return b.low()
}

// seen is the band last alarmed at since the battery was last not
// discharging, 0 for none.
func (b *Battery) seen(ctx context.Context) (int, error) {
	if b.State == nil {
		return 0, nil
	}
	episode, err := b.State.Load(ctx, batterySeenStateName)
	if err != nil {
		return 0, fmt.Errorf("reading this check's own state: %w", err)
	}
	if len(episode.SeenPaths) < 1 {
		return 0, nil
	}
	band, err := strconv.Atoi(episode.SeenPaths[0])
	if err != nil {
		return 0, nil
	}
	return band, nil
}

// recovered ends a fall the battery alarmed about: it says so, once, naming
// the emergency of the last alarm, then forgets what it alarmed about. With
// no alarm recorded there is nothing to say, and in a dry run nothing is sent
// or forgotten. The state is forgotten only once the event is sent, so one that
// could not be sent is tried again.
func (b *Battery) recovered(ctx context.Context, reading batteryReading) error {
	if b.State == nil {
		return nil
	}
	if application.DoctorDryRun(ctx) {
		return nil
	}
	episode, err := b.State.Load(ctx, batterySeenStateName)
	if err != nil {
		return fmt.Errorf("reading this check's own state: %w", err)
	}
	if b.Clear != nil {
		// One event for each emergency of the fall, the oldest first. A send
		// that fails leaves every seq kept, so the next run sends them all
		// again: the app dedupes by seq, and a clear of a cleared seq is harmless.
		for i := 1; i < len(episode.SeenPaths); i++ {
			clears := seqAt(episode.SeenPaths, i)
			if clears == 0 {
				continue
			}
			if err := b.Clear(ctx, recoveredText(reading), clears); err != nil {
				return fmt.Errorf("the battery recovery could not be sent: %w", err)
			}
		}
	}
	return b.forget(ctx)
}

func (b *Battery) forget(ctx context.Context) error {
	if b.State == nil {
		return nil
	}
	if err := b.State.Reset(ctx, batterySeenStateName); err != nil {
		return fmt.Errorf("forgetting this check's own state: %w", err)
	}
	return nil
}

// read looks at the first battery (BAT* in name order) under Dir. A host with
// none, or no power_supply directory at all, reads found false; a battery
// whose status or capacity cannot be read or understood is an error.
func (b *Battery) read() (batteryReading, error) {
	dir := b.Dir
	if dir == "" {
		dir = DefaultBatteryDir
	}
	supplies, err := filepath.Glob(filepath.Join(dir, "BAT*"))
	if err != nil || len(supplies) == 0 {
		return batteryReading{}, nil
	}
	sort.Strings(supplies)
	supply := supplies[0]

	status, err := os.ReadFile(filepath.Join(supply, "status"))
	if err != nil {
		return batteryReading{}, fmt.Errorf("reading the battery's status: %w", err)
	}
	said, err := os.ReadFile(filepath.Join(supply, "capacity"))
	if err != nil {
		return batteryReading{}, fmt.Errorf("reading the battery's capacity: %w", err)
	}
	capacity, err := strconv.Atoi(strings.TrimSpace(string(said)))
	if err != nil {
		return batteryReading{}, fmt.Errorf("the battery's capacity is %q, not a whole percent", strings.TrimSpace(string(said)))
	}
	return batteryReading{found: true, discharging: strings.TrimSpace(string(status)) == "Discharging", capacity: capacity}, nil
}

func (b *Battery) low() int {
	if b.Low > 0 {
		return b.Low
	}
	return DefaultBatteryLow
}

func (b *Battery) critical() int {
	if b.Critical > 0 {
		return b.Critical
	}
	return DefaultBatteryCritical
}
