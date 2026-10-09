package application

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// CloudProvider makes and destroys the cloud boxes: contrib/vultr-boost, or a
// fake in a scenario. A box is named for the host it becomes (cloud1, cloud2).
type CloudProvider interface {
	// Up makes the box and returns once it is up; an error is a box that did
	// not come up, which may still have been partly made.
	Up(ctx context.Context, name string) error
	// Down destroys the box, or what there is of it; a box not there is no
	// error.
	Down(ctx context.Context, name string) error
	// Billed is what the provider's billing says the boxes named have cost so
	// far this month, and whether it could be asked at all.
	Billed(ctx context.Context, names []string) (usd float64, known bool, err error)
}

// CloudBook is where the cloud's state is kept between checks: in the vault,
// so that the month's spend outlives any one host.
type CloudBook interface {
	// Read is the state as last written; a book never written is the zero
	// state.
	Read(ctx context.Context) (CloudState, error)
	// Write keeps the state, why saying what changed.
	Write(ctx context.Context, state CloudState, why string) error
}

// CloudWork is what the cloud check reads of the tracker: the stories ready
// and the stories claimed, across every host.
type CloudWork interface {
	WorkInHand(ctx context.Context) (WorkInHand, error)
}

// CloudState is what the book holds.
type CloudState struct {
	// Month is the UTC month ("2026-10") Hours counts, and Hours the box hours
	// billed that month for the boxes already destroyed, each begun hour a
	// whole one.
	Month string `json:"month,omitempty"`
	Hours int    `json:"hours,omitempty"`
	// Boxes are the boxes up, oldest first.
	Boxes []CloudBox `json:"boxes,omitempty"`
	// CapSaid is the month the cloud cap reached event was written for: it is
	// written once a month.
	CapSaid string `json:"cap_said,omitempty"`
	// HeldUntil is when a box may be made again after one failed to come up
	// twice running.
	HeldUntil time.Time `json:"held_until,omitzero"`
	// Moves are the latest ups and downs, oldest first, at most
	// cloudMovesKept of them, for mw status.
	Moves []CloudMove `json:"moves,omitempty"`
}

// CloudBox is one box up.
type CloudBox struct {
	Name string    `json:"name"`
	Up   time.Time `json:"up"`
	// IdleSince is when a check first found the box with no story claimed on
	// it, the time it was made until it has taken one; zero while it has work.
	IdleSince time.Time `json:"idle_since,omitzero"`
}

// CloudMove is one up, down or failure, as the cloud event said it.
type CloudMove struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// cloudMovesKept is how many moves the book keeps for mw status.
const cloudMovesKept = 5

// CloudFailHold is how long no box is made after one failed to come up twice
// running: a fault at the provider is not tried every minute.
const CloudFailHold = time.Hour

// CloudPlan is the [cloud] table, as the check reads it.
type CloudPlan struct {
	// MaxBoxes is the most boxes up at once; MonthlyCapUSD the most the boxes
	// may cost in a UTC month; HourlyUSD what one costs an hour.
	MaxBoxes      int
	MonthlyCapUSD float64
	HourlyUSD     float64
	// Idle is how long a box may go with no story claimed on it before it is
	// destroyed.
	Idle time.Duration
	// BoxCap is how many sessions a box runs at once, and HostCaps how many
	// each other host does: the free sessions are counted from them.
	BoxCap   int
	HostCaps map[string]int
	// Prefix names the boxes, Prefix1, Prefix2, ...; empty is "cloud".
	Prefix string
}

// DefaultCloudPrefix is what a box's name begins with.
const DefaultCloudPrefix = "cloud"

func (p CloudPlan) prefix() string {
	if p.Prefix == "" {
		return DefaultCloudPrefix
	}
	return p.Prefix
}

// DefaultCloudBoxCap is how many sessions a box runs at once when the plan
// does not say: boost-bootstrap.sh's own default.
const DefaultCloudBoxCap = 2

// CloudCheck is one look at the cloud (mw cloud check; the follower's cloud
// job each minute). It costs no tokens: it reads the tracker's work in hand,
// the book and the provider's billing, and calls the provider only to make or
// destroy a box. In order, it:
//
//   - marks each box idle from the first check that finds no story claimed on
//     it, and busy again once one is;
//   - destroys every box up when one more hour of them would pass the
//     month's cap;
//   - destroys each box idle for Plan.Idle;
//   - makes one box when more stories wait for any host than there are
//     sessions free across the hosts and the boxes, while there are fewer
//     than Plan.MaxBoxes, and the month can pay a first hour of it and one
//     more hour of every box with it.
//
// The month's spend is the box hours the book counts, each begun hour a
// whole one, or the provider's billing when it answers more. At the cap no
// box is made and one cloud event says so, once a month. A box that does not
// come up is destroyed and tried once more; when that fails too, no box is
// made for CloudFailHold. Every up, down and failure is a cloud event, actor
// cloud@Host, and a move the book keeps for mw status. The book is written
// before a box is asked for, so a box that costs is never one it does not
// know of.
type CloudCheck struct {
	Work     CloudWork
	Provider CloudProvider
	Book     CloudBook
	Log      EventLog
	// Host is the host the check runs on: its events' actor is cloud@Host.
	Host string
	Plan CloudPlan
	Now  func() time.Time
	// Out is where what the check did and could not do is said. Nil says
	// nothing.
	Out io.Writer
}

// CloudReport is what one check found and did.
type CloudReport struct {
	// Waiting are the stories that wait for any host, Free the sessions free
	// across the hosts and the boxes.
	Waiting, Free int
	Boxes         int
	SpentUSD      float64
	// Moves are the cloud events the check wrote.
	Moves []string
}

// String is the report in one line.
func (r CloudReport) String() string {
	line := fmt.Sprintf("cloud: %d stories wait for any host, %d sessions free, %d boxes up, $%.2f spent this month", r.Waiting, r.Free, r.Boxes, r.SpentUSD)
	for _, m := range r.Moves {
		line += "\n  " + m
	}
	return line
}

// Run does one check. A box the provider could not destroy, or a book that
// could not be written, is an error, once the rest of the check is done.
func (c CloudCheck) Run(ctx context.Context) (CloudReport, error) {
	switch {
	case c.Work == nil || c.Provider == nil || c.Book == nil:
		return CloudReport{}, fmt.Errorf("the cloud check needs the tracker, a provider and the book")
	case c.Plan.MaxBoxes < 0 || c.Plan.MonthlyCapUSD <= 0 || c.Plan.HourlyUSD <= 0 || c.Plan.Idle <= 0:
		return CloudReport{}, fmt.Errorf("the cloud check needs a cap, an hourly price and an idle time above zero")
	}
	now := c.now()
	state, err := c.Book.Read(ctx)
	if err != nil {
		return CloudReport{}, fmt.Errorf("reading the cloud's book: %w", err)
	}
	work, err := c.Work.WorkInHand(ctx)
	if err != nil {
		return CloudReport{}, fmt.Errorf("reading the work in hand: %w", err)
	}
	p := cloudPass{check: c, now: now, state: state}
	p.roll()
	p.billed = p.askBilled(ctx)
	p.markIdle(work)

	if n := len(p.state.Boxes); n > 0 && p.spent()+c.Plan.HourlyUSD*float64(n) > c.Plan.MonthlyCapUSD {
		p.capReached(ctx, "destroying every box")
		for len(p.state.Boxes) > 0 && p.down(ctx, p.state.Boxes[0].Name, "the month's cap") {
		}
	}
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.IdleSince.IsZero() && now.Sub(box.IdleSince) >= c.Plan.Idle {
			p.down(ctx, box.Name, "idle "+Clock(now.Sub(box.IdleSince)))
		}
	}
	p.report.Waiting, p.report.Free = p.demand(work)
	if p.report.Waiting > p.report.Free && len(p.state.Boxes) < c.Plan.MaxBoxes {
		p.make(ctx)
	}
	if p.dirty != "" {
		p.keep(ctx, p.dirty)
	}
	p.report.Boxes, p.report.SpentUSD = len(p.state.Boxes), p.spent()
	return p.report, p.err
}

func (c CloudCheck) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c CloudCheck) say(format string, args ...any) {
	if c.Out != nil {
		fmt.Fprintf(c.Out, "mw cloud: "+format+"\n", args...)
	}
}

// cloudPass is one check's working state.
type cloudPass struct {
	check  CloudCheck
	now    time.Time
	state  CloudState
	billed float64
	// dirty says why the state wants writing at the end, "" when it does not.
	dirty  string
	report CloudReport
	err    error
}

func (p *cloudPass) fail(err error) {
	p.check.say("%v", err)
	if p.err == nil {
		p.err = err
	}
}

// roll starts the month's count afresh when the book's month is past.
func (p *cloudPass) roll() {
	if month := cloudMonth(p.now); p.state.Month != month {
		p.state.Month, p.state.Hours = month, 0
		p.dirty = "a new month"
	}
}

// askBilled is what the provider's billing says of the boxes' names this
// month, 0 when it cannot say.
func (p *cloudPass) askBilled(ctx context.Context) float64 {
	var names []string
	for i := 1; i <= max(p.check.Plan.MaxBoxes, len(p.state.Boxes)); i++ {
		names = append(names, fmt.Sprintf("%s%d", p.check.Plan.prefix(), i))
	}
	usd, known, err := p.check.Provider.Billed(ctx, names)
	if err != nil || !known {
		if err != nil {
			p.check.say("the provider's billing could not be read, so the book's count stands: %v", err)
		}
		return 0
	}
	return usd
}

// spent is the month's spend: the book's count or the billing's, whichever is
// more.
func (p *cloudPass) spent() float64 {
	return max(float64(p.state.hours(p.now))*p.check.Plan.HourlyUSD, p.billed)
}

func (p *cloudPass) markIdle(work WorkInHand) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		busy := sessions(work, box.Name) > 0
		switch {
		case busy && !box.IdleSince.IsZero():
			box.IdleSince = time.Time{}
			p.dirty = box.Name + " is busy"
		case !busy && box.IdleSince.IsZero():
			box.IdleSince = p.now
			p.dirty = box.Name + " is idle"
		}
	}
}

// demand counts the stories that wait for any host, and the sessions free
// across the hosts the plan names and the boxes.
func (p *cloudPass) demand(work WorkInHand) (waiting, free int) {
	for _, d := range work.Ready {
		if d.Merged().Host == domain.HostAuto && !d.Hitl() {
			waiting++
		}
	}
	for host, limit := range p.check.Plan.HostCaps {
		free += max(0, limit-sessions(work, host))
	}
	boxCap := p.check.Plan.BoxCap
	if boxCap <= 0 {
		boxCap = DefaultCloudBoxCap
	}
	for _, box := range p.state.Boxes {
		free += max(0, boxCap-sessions(work, box.Name))
	}
	return waiting, free
}

// sessions counts the stories claimed on host that a session works.
func sessions(work WorkInHand, host string) int {
	n := 0
	for _, d := range work.RunningOn(host) {
		if !d.Hitl() {
			n++
		}
	}
	return n
}

// make makes one box, and tries once more when it does not come up.
func (p *cloudPass) make(ctx context.Context) {
	plan := p.check.Plan
	if p.now.Before(p.state.HeldUntil) {
		p.check.say("%d stories wait, but no box is made before %s: one failed to come up twice", p.report.Waiting, p.state.HeldUntil.Format("15:04Z"))
		return
	}
	name := p.nextName()
	for try := 1; try <= 2; try++ {
		if p.spent()+plan.HourlyUSD*float64(len(p.state.Boxes)+2) > plan.MonthlyCapUSD {
			p.capReached(ctx, "no box is made")
			return
		}
		p.state.Boxes = append(p.state.Boxes, CloudBox{Name: name, Up: p.now, IdleSince: p.now})
		if !p.keep(ctx, "making "+name) {
			p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
			return
		}
		err := p.check.Provider.Up(ctx, name)
		if err == nil {
			p.move(ctx, fmt.Sprintf("up %s: %d stories wait for any host, %d sessions free", name, p.report.Waiting, p.report.Free))
			p.keep(ctx, "up "+name)
			return
		}
		failed := fmt.Sprintf("failed %s: %v", name, oneLine(err.Error()))
		if !p.down(ctx, name, "it did not come up") {
			p.move(ctx, failed+"; it could not be destroyed either, so no box is made until it is")
			p.keep(ctx, failed)
			return
		}
		if try == 1 {
			p.move(ctx, failed+"; destroyed, and tried once more")
		} else {
			p.state.HeldUntil = p.now.Add(CloudFailHold)
			p.move(ctx, failed+fmt.Sprintf(" again; destroyed, and no box is made before %s", p.state.HeldUntil.Format("15:04Z")))
		}
		p.keep(ctx, failed)
	}
}

// down destroys the box, counting its hours into the month's, and reports
// whether it is gone.
func (p *cloudPass) down(ctx context.Context, name, why string) bool {
	at := -1
	for i, box := range p.state.Boxes {
		if box.Name == name {
			at = i
		}
	}
	if at < 0 {
		return true
	}
	if err := p.check.Provider.Down(ctx, name); err != nil {
		p.move(ctx, fmt.Sprintf("failed to destroy %s (%s): %v", name, why, oneLine(err.Error())))
		p.fail(fmt.Errorf("destroying %s: %w", name, err))
		return false
	}
	p.state.Hours += billedHours(maxTime(p.state.Boxes[at].Up, cloudMonthStart(p.now)), p.now)
	p.state.Boxes = append(p.state.Boxes[:at], p.state.Boxes[at+1:]...)
	if why != "it did not come up" {
		p.move(ctx, fmt.Sprintf("down %s: %s", name, why))
	}
	p.keep(ctx, "down "+name)
	return true
}

// capReached says the month's cap is reached, once a month.
func (p *cloudPass) capReached(ctx context.Context, what string) {
	line := fmt.Sprintf("cap reached: $%.2f spent of $%.2f this month; %s", p.spent(), p.check.Plan.MonthlyCapUSD, what)
	month := cloudMonth(p.now)
	if p.state.CapSaid == month {
		p.check.say("%s", line)
		return
	}
	p.state.CapSaid = month
	p.move(ctx, line)
	p.dirty = "cap reached"
}

// move is one cloud event, and a line the book keeps for mw status.
func (p *cloudPass) move(ctx context.Context, what string) {
	p.check.say("%s", what)
	p.report.Moves = append(p.report.Moves, what)
	p.state.Moves = append(p.state.Moves, CloudMove{At: p.now, What: what})
	if n := len(p.state.Moves); n > cloudMovesKept {
		p.state.Moves = append([]CloudMove(nil), p.state.Moves[n-cloudMovesKept:]...)
	}
	if p.check.Log == nil {
		return
	}
	_, err := EventEmit{
		Log:   p.check.Log,
		Now:   func() time.Time { return p.now },
		Event: events.Event{Kind: events.KindCloud, Actor: "cloud@" + p.check.Host, Detail: what},
	}.Run(ctx)
	if err != nil {
		p.check.say("the cloud event %q could not be written: %v", what, err)
	}
}

// keep writes the book, and reports whether it was written.
func (p *cloudPass) keep(ctx context.Context, why string) bool {
	if err := p.check.Book.Write(ctx, p.state, why); err != nil {
		p.fail(fmt.Errorf("writing the cloud's book (%s): %w", why, err))
		return false
	}
	p.dirty = ""
	return true
}

func (p *cloudPass) nextName() string {
	taken := map[string]bool{}
	for _, box := range p.state.Boxes {
		taken[box.Name] = true
	}
	for i := 1; ; i++ {
		if name := fmt.Sprintf("%s%d", p.check.Plan.prefix(), i); !taken[name] {
			return name
		}
	}
}

// hours are the box hours billed this month: the book's count of the boxes
// destroyed, and the hours begun of each box up.
func (s CloudState) hours(now time.Time) int {
	h := 0
	if s.Month == cloudMonth(now) {
		h = s.Hours
	}
	for _, box := range s.Boxes {
		h += billedHours(maxTime(box.Up, cloudMonthStart(now)), now)
	}
	return h
}

// billedHours are the hours begun from from to to, each a whole one, and at
// least one: a box is billed its first hour as soon as it is made.
func billedHours(from, to time.Time) int {
	return max(1, int(math.Ceil(to.Sub(from).Hours())))
}

func cloudMonth(t time.Time) string { return t.UTC().Format("2006-01") }

func cloudMonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// CloudReading is the cloud as mw status shows it.
type CloudReading struct {
	Boxes    []CloudBox
	Moves    []CloudMove
	SpentUSD float64
	CapUSD   float64
	MaxBoxes int
	Now      time.Time
}

// ReadCloud reads the book for mw status: the boxes, the latest moves and the
// month's spend counted from the box hours. It asks the provider nothing.
func ReadCloud(ctx context.Context, book CloudBook, plan CloudPlan, now time.Time) (*CloudReading, error) {
	state, err := book.Read(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudReading{
		Boxes:    state.Boxes,
		Moves:    state.Moves,
		SpentUSD: float64(state.hours(now)) * plan.HourlyUSD,
		CapUSD:   plan.MonthlyCapUSD,
		MaxBoxes: plan.MaxBoxes,
		Now:      now,
	}, nil
}

// write is the CLOUD section: the month's spend against the cap, each box up
// with its age, and the latest moves.
func (r CloudReading) write(b *strings.Builder) {
	clip(b, fmt.Sprintf("CLOUD: $%.2f spent of $%.2f this month, %d of %d boxes", r.SpentUSD, r.CapUSD, len(r.Boxes), r.MaxBoxes))
	for _, box := range r.Boxes {
		line := fmt.Sprintf("  %s  up %s", box.Name, Clock(r.Now.Sub(box.Up)))
		if !box.IdleSince.IsZero() {
			line += ", idle " + Clock(r.Now.Sub(box.IdleSince))
		} else {
			line += ", working"
		}
		clip(b, line)
	}
	for _, m := range r.Moves {
		clip(b, "  "+m.At.UTC().Format("01-02 15:04Z")+" "+m.What)
	}
}
