package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SeatOther is the seat a session is counted under when its name is no folder
// under seats/.
const SeatOther = "other"

// SeatsOffHomeLine is what FUEL says, in place of the SEATS block, off the home.
const SeatsOffHomeLine = "seats: counted on the home"

// SeatSession is one main session run in the vault's directory: the name it was
// given (its custom-title line), when its transcript was last written, and the
// totalCostUSD of its last cost-state line. A session that has not written one
// (it is still running) has HasCost false: it is not free, only not counted.
type SeatSession struct {
	Name    string
	At      time.Time
	CostUSD float64
	HasCost bool
}

// SeatSessions is the port the seats' fuel is read through: the transcripts of
// the sessions run in the vault's directory, on this host only. SeatSessions
// lists the main sessions whose transcript was written at or after since; a
// subagent's file adds nothing. The real adapter is infrastructure/claude
// (SeatTranscripts).
type SeatSessions interface {
	SeatSessions(ctx context.Context, since time.Time) ([]SeatSession, error)
}

// SeatUse is what one seat's sessions cost in a window: the dollars of the
// sessions that reported one, and per landing of the whole factory.
type SeatUse struct {
	Seat          string  `json:"seat"`
	Sessions      int     `json:"sessions"`
	WithoutCost   int     `json:"sessions_without_cost"`
	USD           float64 `json:"usd"`
	USDPerLanding float64 `json:"usd_per_landing"`
}

// SeatFuel is the seats' share of FUEL, whole-factory only: each seat's dollars
// in the window and in the one before, and the stories' dollars per landing with
// the seats' added. It is flattened into the report's JSON.
type SeatFuel struct {
	// Counted says the block is the seats' figures; OffHome that this host is
	// not the home and the block is one line.
	Counted bool `json:"-"`
	OffHome bool `json:"seats_off_home,omitempty"`
	// Seats are the seats that ran a session in the window, dearest first.
	Seats         []SeatUse `json:"seats,omitempty"`
	SeatsPrevious []SeatUse `json:"seats_previous,omitempty"`
	// SessionsWithoutCost is how many sessions of the window gave no cost figure.
	SessionsWithoutCost int `json:"seat_sessions_without_cost,omitempty"`
	// USDPerLandingAll is the stories' $/landing plus the seats' dollars over
	// the window's landings; nil when nothing landed.
	USDPerLandingAll         *float64         `json:"usd_per_landing_all,omitempty"`
	USDPerLandingAllPrevious *float64         `json:"usd_per_landing_all_previous,omitempty"`
	SeatArrows               map[string]Trend `json:"seat_arrows,omitempty"`
}

// SeatOf is the seat a session name belongs to: its first word, lower-cased and
// cut at the first space or hyphen, if that is one of the known seats, else
// SeatOther.
func SeatOf(name string, known []string) string {
	word := strings.ToLower(strings.TrimSpace(name))
	if cut := strings.IndexAny(word, " -"); cut >= 0 {
		word = word[:cut]
	}
	for _, seat := range known {
		if word != "" && word == strings.ToLower(seat) {
			return word
		}
	}
	return SeatOther
}

// seatSums sums the sessions for which in is true, by seat.
func seatSums(sessions []SeatSession, known []string, in func(time.Time) bool) []SeatUse {
	bySeat := map[string]*SeatUse{}
	for _, s := range sessions {
		if !in(s.At) {
			continue
		}
		seat := SeatOf(s.Name, known)
		use := bySeat[seat]
		if use == nil {
			use = &SeatUse{Seat: seat}
			bySeat[seat] = use
		}
		use.Sessions++
		if s.HasCost {
			use.USD += s.CostUSD
		} else {
			use.WithoutCost++
		}
	}
	var uses []SeatUse
	for _, use := range bySeat {
		uses = append(uses, *use)
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].USD != uses[j].USD {
			return uses[i].USD > uses[j].USD
		}
		return uses[i].Seat < uses[j].Seat
	})
	return uses
}

// perLandingOf sets each seat's dollars per landing and rounds its dollars, and
// returns the seats' total and sessions without a cost figure.
func perLandingOf(uses []SeatUse, landed int) (total float64, without int) {
	for i := range uses {
		total += uses[i].USD
		without += uses[i].WithoutCost
		uses[i].USDPerLanding = twoPlaces(fraction(uses[i].USD, float64(landed)))
		uses[i].USD = twoPlaces(uses[i].USD)
	}
	return total, without
}

// seatsCounted says whether this report counts the seats: it was given the
// means, and is of the whole factory.
func (r FactoryReport) seatsCounted(result FactoryReportResult) bool {
	return r.SeatSessions != nil && r.Home != nil && r.Rig == "" && r.Host == "" && result.Landing == nil
}

// onTheHome says whether this report runs on the home. A home that cannot be
// told is not this host.
func (r FactoryReport) onTheHome(ctx context.Context) (bool, error) {
	home, err := IsHome(ctx, r.Home, r.Here)
	var unknown *HomeUnknown
	if errors.As(err, &unknown) {
		return false, nil
	}
	return home, err
}

// seatFuel reads the seats' fuel for the report's two windows, and returns the
// sessions it read so that the day's snapshot can sum them over its own week.
func (r FactoryReport) seatFuel(ctx context.Context, result FactoryReportResult, now time.Time) (SeatFuel, []SeatSession, error) {
	if !r.seatsCounted(result) {
		return SeatFuel{}, nil, nil
	}
	home, err := r.onTheHome(ctx)
	if err != nil {
		return SeatFuel{}, nil, fmt.Errorf("reading the seats' fuel: %w", err)
	}
	if !home {
		return SeatFuel{OffHome: true}, nil, nil
	}
	// The snapshot sums the seven days to today; read as far back as either needs.
	since := reportDay(now).AddDate(0, 0, -(ChangeWindowDays - 1))
	if result.PreviousWindow.Since.Before(since) {
		since = result.PreviousWindow.Since
	}
	sessions, err := r.SeatSessions.SeatSessions(ctx, since)
	if err != nil {
		return SeatFuel{}, nil, fmt.Errorf("reading the seats' fuel: the transcripts: %w", err)
	}
	fuel := SeatFuel{Counted: true, SeatArrows: map[string]Trend{}}
	var landed, landedBefore int
	var stories, storiesBefore FuelUse
	if whole := result.Whole; whole != nil {
		landed, landedBefore = whole.Quality.Landed, whole.Previous.Landed
		stories, storiesBefore = whole.Fuel, whole.FuelPrevious
	}
	fuel.Seats = seatSums(sessions, r.Seats, func(at time.Time) bool { previous, inside := result.windowOfTime(at); return inside && !previous })
	fuel.SeatsPrevious = seatSums(sessions, r.Seats, func(at time.Time) bool { previous, inside := result.windowOfTime(at); return inside && previous })
	total, without := perLandingOf(fuel.Seats, landed)
	totalBefore, _ := perLandingOf(fuel.SeatsPrevious, landedBefore)
	fuel.SessionsWithoutCost = without
	if landed > 0 {
		all := twoPlaces(stories.USDPerLanding + total/float64(landed))
		fuel.USDPerLandingAll = &all
	}
	if landedBefore > 0 && len(fuel.SeatsPrevious) > 0 {
		all := twoPlaces(storiesBefore.USDPerLanding + totalBefore/float64(landedBefore))
		fuel.USDPerLandingAllPrevious = &all
	}
	if fuel.USDPerLandingAll != nil && fuel.USDPerLandingAllPrevious != nil {
		fuel.SeatArrows["usd_per_landing_all"] = NewTrend(*fuel.USDPerLandingAll, *fuel.USDPerLandingAllPrevious, true)
	}
	for _, use := range fuel.Seats {
		for _, was := range fuel.SeatsPrevious {
			if was.Seat == use.Seat && landed > 0 && landedBefore > 0 {
				fuel.SeatArrows[use.Seat] = NewTrend(use.USDPerLanding, was.USDPerLanding, true)
			}
		}
	}
	return fuel, sessions, nil
}

// seatSnapshot sets the seat figures of a day's snapshot: the dollars of each
// seat over the week to today and the all-seats dollars per landing of it. It
// sets nothing when the report did not count the seats.
func seatSnapshot(s *MetricSnapshot, sessions []SeatSession, known []string, counted bool, week []reportEntry, now time.Time) {
	if !counted {
		return
	}
	today := reportDay(now)
	from, to := today.AddDate(0, 0, -(ChangeWindowDays-1)), today.AddDate(0, 0, 1)
	uses := seatSums(sessions, known, func(at time.Time) bool { return !at.Before(from) && at.Before(to) })
	total, _ := perLandingOf(uses, s.Landed)
	if len(uses) > 0 {
		s.SeatUSD = map[string]float64{}
		for _, use := range uses {
			s.SeatUSD[use.Seat] = use.USD
		}
	}
	if s.Landed > 0 {
		stories := 0.0
		if v, ok := changeMetrics["$/landing"].value(week); ok {
			stories = v
		}
		all := twoPlaces(stories + total/float64(s.Landed))
		s.USDPerLandingAll = &all
	}
}

// write prints the SEATS block that ends FUEL: a line a seat, the sessions
// that gave no cost figure, and the stories' $/landing with the seats' added.
func (f SeatFuel) write(b *strings.Builder) {
	if f.OffHome {
		fmt.Fprintf(b, "\nSEATS\n  %s\n", SeatsOffHomeLine)
		return
	}
	if !f.Counted {
		return
	}
	b.WriteString("\nSEATS\n")
	for _, use := range f.Seats {
		fmt.Fprintf(b, "  %s\n", fitLine(fmt.Sprintf("%s $%.2f (%.2f/landing%s)", use.Seat, use.USD, use.USDPerLanding, f.SeatArrows[use.Seat].Text()), ReportWidth-2))
	}
	switch f.SessionsWithoutCost {
	case 0:
	case 1:
		b.WriteString("  1 session without a cost figure\n")
	default:
		fmt.Fprintf(b, "  %d sessions without a cost figure\n", f.SessionsWithoutCost)
	}
	switch {
	case f.USDPerLandingAll != nil:
		fmt.Fprintf(b, "  all seats $/landing %.2f%s\n", *f.USDPerLandingAll, f.SeatArrows["usd_per_landing_all"].Text())
	case len(f.Seats) == 0:
		b.WriteString("  no seat ran a session in it\n")
	default:
		b.WriteString("  no landings to divide by\n")
	}
}
