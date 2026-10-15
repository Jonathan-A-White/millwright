package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// CloudProvider makes and destroys the cloud boxes: contrib/vultr-boost, or a
// fake in a scenario. A box is named for the host it becomes (cloud1, cloud2).
type CloudProvider interface {
	// Begin starts the making of the box in a process of its own, outliving
	// the caller and its context, and returns once that has started: the
	// process runs CloudMake. An error is a make that did not start.
	Begin(ctx context.Context, name string) error
	// Up makes the box and returns once it is up; an error is a box that did
	// not come up, which may still have been partly made. It is what the
	// process Begin starts calls, never the check.
	Up(ctx context.Context, name string) error
	// Down destroys the box, or what there is of it; a box not there is no
	// error.
	Down(ctx context.Context, name string) error
	// Billed is what the provider's billing says the boxes named have cost so
	// far this month, and whether it could be asked at all.
	Billed(ctx context.Context, names []string) (usd float64, known bool, err error)
	// Instances are the boxes the provider holds, whatever the book says,
	// each by the name it was labelled with.
	Instances(ctx context.Context) ([]CloudInstance, error)
	// Reap destroys one instance by its id, for a box no state of ours
	// holds, so that Down could not find it.
	Reap(ctx context.Context, instance CloudInstance) error
	// Adopt takes a box the provider holds and the book names, whose make
	// was cut off before its state was kept, into the provider's state
	// (terraform import), so that Down finds it.
	Adopt(ctx context.Context, name string, instance CloudInstance) error
}

// CloudInstance is one box as the provider holds it: Status is Vultr's
// status (active, pending, ...) and Server its server_status (ok, locked,
// installingbooting, none), empty when not said.
type CloudInstance struct {
	ID, Label, Status, Server string
}

// Up reports whether the provider says the instance is made and ready: active
// and its server ok. One that does not say its server status is read by its
// status alone.
func (i CloudInstance) Up() bool {
	return i.Status == "active" && (i.Server == "" || i.Server == "ok")
}

// What the provider can refuse, each a reason a retry does not mend. The
// adapter wraps one of these around the provider's own words; the text is
// what the book and the cloud events say.
var (
	ErrCloudFeeLimit = errors.New("Vultr fee limit reached: raise it at my.vultr.com or wait for the month")
	ErrCloudQuota    = errors.New("Vultr instance limit reached: raise it at my.vultr.com, or destroy a box there")
	ErrCloudSoldOut  = errors.New("Vultr has none of the plan left in the region: choose another region or plan in the tfvars")
	ErrCloudBadToken = errors.New("Vultr refused the API token: put a new vultr_api_token (mw secrets put), and check the API's access control at my.vultr.com")
	// ErrCloudLocked is Vultr's 409: it will not delete an instance it has
	// locked, as one being restored from a snapshot is. It is no refusal of a
	// make; a delete is tried again.
	ErrCloudLocked = errors.New("Vultr would not delete (locked)")
	// ErrCloudUpDeadline is a make that ran past its deadline: what became of
	// the box is Vultr's to say, not the wrapper's exit status.
	ErrCloudUpDeadline = errors.New("the make ran past its deadline")
)

// CloudNotice tells the Governor what only he can mend: a card to him.
type CloudNotice interface {
	Tell(ctx context.Context, title, text string) error
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
	// FeeLimitAt is when Vultr last refused a box for its monthly fee limit,
	// zero while it has not or a box has come up since: no box is made for
	// CloudFeeLimitPause after it, and the Governor has one card for it.
	FeeLimitAt time.Time `json:"fee_limit_at,omitzero"`
	// OrphansSaid are the Vultr ids of the orphans a check could not destroy
	// and said so for, that the next check does not say it again.
	OrphansSaid []string `json:"orphans_said,omitempty"`
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
	// Making is set from the time the box is asked for until its make ends,
	// Up being when it was asked for: the box is billed from then, and no
	// check destroys it or counts it idle meanwhile.
	Making bool `json:"making,omitempty"`
	// Restoring is a box whose make ran past its deadline while Vultr still
	// held the instance, restoring from a snapshot: InstanceID is Vultr's id
	// for it and RestoringSince when the make gave up waiting. A check adopts
	// it when Vultr says it is up, and destroys it Plan.Up after that. Like a
	// box being made it is billed, and never idle.
	Restoring      bool      `json:"restoring,omitempty"`
	InstanceID     string    `json:"instance_id,omitempty"`
	RestoringSince time.Time `json:"restoring_since,omitzero"`
}

// CloudMove is one up, down or failure, as the cloud event said it.
type CloudMove struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// cloudMovesKept is how many moves the book keeps for mw status.
const cloudMovesKept = 5

// CloudMakeStaleMargin is how long past the make's own deadline a box may be
// made before a check gives it up for lost (CloudPlan.MakeStale): the process
// that was making it did not live to say how it ended.
const CloudMakeStaleMargin = 15 * time.Minute

// DefaultCloudUp is how long a make may run when the plan does not say: a
// Vultr snapshot restore took about 50 minutes on 2026-10-15.
const DefaultCloudUp = 60 * time.Minute

// CloudFailHold is how long no box is made after one failed to come up twice
// running: a fault at the provider is not tried every minute.
const CloudFailHold = time.Hour

// CloudFeeLimitPause is how long no box is tried after Vultr refused one for
// its monthly fee limit: not a failure to come up, so not CloudFailHold, but
// not a try every minute either; the Governor may raise the limit meanwhile.
const CloudFeeLimitPause = 10 * time.Minute

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
	// Up is how long a make may run before it reads the instance back from the
	// provider ([cloud] up_minutes); zero is DefaultCloudUp.
	Up time.Duration
}

// UpLimit is how long a make may run.
func (p CloudPlan) UpLimit() time.Duration {
	if p.Up <= 0 {
		return DefaultCloudUp
	}
	return p.Up
}

// MakeStale is how long a box may be made before a check gives it up for
// lost: the make's deadline and CloudMakeStaleMargin.
func (p CloudPlan) MakeStale() time.Duration { return p.UpLimit() + CloudMakeStaleMargin }

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
//   - asks the provider for every instance it holds, adopts each box the book
//     holds as restoring (its make ran past its deadline with Vultr still
//     restoring the instance) that Vultr now says is up, drops one Vultr no
//     longer holds, and destroys one still restoring up_minutes after the
//     make gave up waiting; then destroys each instance
//     named for a box (cloud1, cloud2, ...) that the book does not hold, an
//     orphan of a make that was killed, which is billed and which Down cannot
//     find, having no state; each is a move, and one that cannot be destroyed
//     yet (Vultr locks an instance being restored) is said once and tried
//     again at the next check;
//   - marks each box idle from the first check that finds no story claimed on
//     it, and busy again once one is;
//   - gives up a box begun the plan's MakeStale ago and not yet up (the process
//     making it died) and destroys it;
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
//
// The check never waits for a box: a make takes minutes (terraform apply) and
// must outlive a caller that is stopped, so the check writes the box into the
// book as being made, asks the provider to Begin it in a process of its own
// and returns. That process is CloudMake, which says how the make ended.
type CloudCheck struct {
	Work     CloudWork
	Provider CloudProvider
	Book     CloudBook
	Log      EventLog
	// Notice tells the Governor what only he can mend. Nil tells no one.
	Notice CloudNotice
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
	// Making are the boxes the check began, still being made.
	Making []string
}

// String is the report in one line.
func (r CloudReport) String() string {
	line := fmt.Sprintf("cloud: %d stories wait for any host, %d sessions free, %d boxes up, $%.2f spent this month", r.Waiting, r.Free, r.Boxes, r.SpentUSD)
	for _, m := range r.Moves {
		line += "\n  " + m
	}
	for _, name := range r.Making {
		line += "\n  making " + name + " in the background; the book says when it is up"
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
	p.reconcile(ctx)
	p.dropStale(ctx)
	p.markIdle(work)

	if n := len(p.state.Boxes); n > 0 && p.spent()+c.Plan.HourlyUSD*float64(n) > c.Plan.MonthlyCapUSD {
		p.capReached(ctx, "destroying every box")
		for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
			if !box.Making && !p.down(ctx, box.Name, "the month's cap") {
				break
			}
		}
	}
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Making && !box.IdleSince.IsZero() && now.Sub(box.IdleSince) >= c.Plan.Idle {
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

// reconcile reads the provider's instances back and sets the book right by
// them, before anything is believed of the book: a restoring box is adopted
// when Vultr says it is up, given up when it is gone or has restored too long,
// and each instance named for a box that the book does not hold is destroyed.
// An orphan is billed and counts against the account's limits, and no state is
// there for Down to find it by. A box the book holds, being made, restoring or
// up, is never one.
func (p *cloudPass) reconcile(ctx context.Context) {
	instances, err := p.check.Provider.Instances(ctx)
	if err != nil {
		p.check.say("the provider's instances could not be listed, so no orphan is looked for and no restoring box is read: %v", err)
		return
	}
	p.settleRestoring(ctx, instances)
	held := map[string]bool{}
	for _, box := range p.state.Boxes {
		held[box.Name] = true
	}
	present := map[string]bool{}
	for _, inst := range instances {
		present[inst.ID] = true
		if !p.boxName(inst.Label) || held[inst.Label] {
			continue
		}
		which := fmt.Sprintf("%s (Vultr instance %s, %s)", inst.Label, inst.ID, inst.Status)
		if !p.reapInstance(ctx, inst, "orphan "+which+", no box of the book's") {
			continue
		}
		p.move(ctx, fmt.Sprintf("destroyed orphan %s: a box no state held, billed and in no book", which))
		p.dirty = "destroyed orphan " + inst.Label
	}
	// What was said of an instance that Vultr no longer holds is no more to say.
	kept := p.state.OrphansSaid[:0:0]
	for _, id := range p.state.OrphansSaid {
		if present[id] {
			kept = append(kept, id)
		}
	}
	if !slices.Equal(kept, p.state.OrphansSaid) {
		p.state.OrphansSaid = kept
		p.dirty = "orphans said"
	}
}

// settleRestoring reads each box the book holds as restoring against what
// Vultr holds. One Vultr says is up is adopted into the provider's state and
// is a box up. One Vultr no longer holds is dropped. One still restoring is
// waited for, the make's deadline again, and then destroyed.
func (p *cloudPass) settleRestoring(ctx context.Context, instances []CloudInstance) {
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Restoring {
			continue
		}
		var inst CloudInstance
		found := false
		for _, i := range instances {
			if i.ID == box.InstanceID {
				inst, found = i, true
			}
		}
		since := Clock(p.now.Sub(box.RestoringSince))
		switch {
		case !found:
			p.state.Hours += billedHours(maxTime(box.Up, cloudMonthStart(p.now)), p.now)
			p.dropBox(box.Name)
			p.move(ctx, fmt.Sprintf("%s is gone from Vultr: the instance %s it was restoring is no longer held; the book drops it", box.Name, box.InstanceID))
			p.dirty = "gone " + box.Name
		case inst.Up() && p.adopt(ctx, box, inst):
			p.setBox(box.Name, func(b *CloudBox) {
				b.Restoring, b.InstanceID, b.RestoringSince, b.IdleSince = false, "", time.Time{}, p.now
			})
			p.state.FeeLimitAt = time.Time{}
			p.move(ctx, fmt.Sprintf("up %s: Vultr finished restoring it, %s after the make gave up waiting; adopted", box.Name, since))
			p.dirty = "up " + box.Name
		case p.now.Sub(box.RestoringSince) >= p.check.Plan.UpLimit():
			if p.down(ctx, box.Name, "it did not come up") {
				p.move(ctx, fmt.Sprintf("destroyed %s: Vultr was still restoring instance %s %s after the make gave up waiting", box.Name, box.InstanceID, since))
			}
		}
	}
}

// adopt has the provider take the instance of a restoring box into its state,
// and reports whether it did; a box it could not take is tried again at the
// next check, until the restore has taken too long.
func (p *cloudPass) adopt(ctx context.Context, box CloudBox, inst CloudInstance) bool {
	if err := p.check.Provider.Adopt(ctx, box.Name, inst); err != nil {
		p.check.say("Vultr says %s is up, but it could not be adopted: %v", box.Name, err)
		return false
	}
	return true
}

func (p *cloudPass) setBox(name string, change func(b *CloudBox)) {
	for i := range p.state.Boxes {
		if p.state.Boxes[i].Name == name {
			change(&p.state.Boxes[i])
		}
	}
}

func (p *cloudPass) dropBox(name string) {
	p.state.Boxes = slices.DeleteFunc(p.state.Boxes, func(b CloudBox) bool { return b.Name == name })
}

// held reads the provider back for the instance labelled name.
func (p *cloudPass) held(ctx context.Context, name string) (inst CloudInstance, found bool, err error) {
	instances, err := p.check.Provider.Instances(ctx)
	if err != nil {
		return CloudInstance{}, false, err
	}
	for _, i := range instances {
		if i.Label == name {
			return i, true, nil
		}
	}
	return CloudInstance{}, false, nil
}

// reapInstance destroys one instance Vultr holds, by its id, and reports
// whether it is gone: the provider answers well only to Vultr's 204 or 404.
// What it was refused is a move the first time, and said again, quietly, at
// each check after.
func (p *cloudPass) reapInstance(ctx context.Context, inst CloudInstance, what string) bool {
	err := p.check.Provider.Reap(ctx, inst)
	if err == nil {
		p.state.OrphansSaid = slices.DeleteFunc(p.state.OrphansSaid, func(id string) bool { return id == inst.ID })
		return true
	}
	text := fmt.Sprintf("could not destroy %s: %v; tried again at each check", what, oneLine(err.Error()))
	if slices.Contains(p.state.OrphansSaid, inst.ID) {
		p.check.say("%s", text)
		return false
	}
	p.state.OrphansSaid = append(p.state.OrphansSaid, inst.ID)
	p.dirty = "orphans said"
	p.move(ctx, text)
	return false
}

// boxName reports whether label is a name a box is given: the prefix and a
// number.
func (p *cloudPass) boxName(label string) bool {
	n, ok := strings.CutPrefix(label, p.check.Plan.prefix())
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// feeLimit notes that Vultr refused a box for its monthly fee limit. It is a
// money cap and no fault of a make: the first refusal is a move and one card
// to the Governor, who alone can raise the limit; the box is tried again after
// CloudFeeLimitPause, and a box that comes up ends it.
func (p *cloudPass) feeLimit(ctx context.Context) {
	first := p.state.FeeLimitAt.IsZero()
	p.state.FeeLimitAt = p.now
	p.dirty = "Vultr fee limit"
	if !first {
		p.check.say("%v", ErrCloudFeeLimit)
		return
	}
	p.move(ctx, ErrCloudFeeLimit.Error())
	if p.check.Notice == nil {
		return
	}
	if err := p.check.Notice.Tell(ctx, "Vultr will make no cloud box: its monthly fee limit is reached", ErrCloudFeeLimit.Error()); err != nil {
		p.check.say("the card to the Governor could not be sent: %v", err)
	}
}

// dropStale gives up each box begun the plan's MakeStale ago and still being made:
// destroys what there is of it, and says so.
func (p *cloudPass) dropStale(ctx context.Context) {
	for _, box := range append([]CloudBox(nil), p.state.Boxes...) {
		if !box.Making || p.now.Sub(box.Up) < p.check.Plan.MakeStale() {
			continue
		}
		if p.down(ctx, box.Name, "it did not come up") {
			p.move(ctx, fmt.Sprintf("failed %s: the make begun at %s never said how it ended; destroyed", box.Name, box.Up.Format("15:04Z")))
			p.keep(ctx, "failed "+box.Name)
		}
	}
}

func (p *cloudPass) markIdle(work WorkInHand) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring {
			continue
		}
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

// make begins one box: it is written into the book as being made, and the
// provider is asked to make it in a process of its own, which CloudMake is.
func (p *cloudPass) make(ctx context.Context) {
	plan := p.check.Plan
	if p.now.Before(p.state.HeldUntil) {
		p.check.say("%d stories wait, but no box is made before %s: one failed to come up twice", p.report.Waiting, p.state.HeldUntil.Format("15:04Z"))
		return
	}
	if !p.state.FeeLimitAt.IsZero() && p.now.Before(p.state.FeeLimitAt.Add(CloudFeeLimitPause)) {
		p.check.say("%d stories wait, but no box is made before %s: %v", p.report.Waiting, p.state.FeeLimitAt.Add(CloudFeeLimitPause).Format("15:04Z"), ErrCloudFeeLimit)
		return
	}
	name := p.nextName()
	if p.spent()+plan.HourlyUSD*float64(len(p.state.Boxes)+2) > plan.MonthlyCapUSD {
		p.capReached(ctx, "no box is made")
		return
	}
	p.state.Boxes = append(p.state.Boxes, CloudBox{Name: name, Up: p.now, IdleSince: p.now, Making: true})
	if !p.keep(ctx, "making "+name) {
		p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
		return
	}
	p.move(ctx, fmt.Sprintf("making %s: %d stories wait for any host, %d sessions free", name, p.report.Waiting, p.report.Free))
	p.dirty = "making " + name
	if err := p.check.Provider.Begin(ctx, name); err != nil {
		// Nothing is making it, so nothing is billed: it is not in the book.
		p.state.Boxes = p.state.Boxes[:len(p.state.Boxes)-1]
		p.dirty = "failed " + name
		p.fail(fmt.Errorf("beginning to make %s: %w", name, err))
		return
	}
	p.report.Making = append(p.report.Making, name)
}

// down destroys the box, counting its hours into the month's, and reports
// whether it is gone. It is gone when Vultr says it is, read back after the
// provider's Down: a wrapper that holds no state for a box (its make was
// killed, or its state lost) exits well and destroys nothing. A box still held
// is then destroyed by its id, and the book keeps it, saying what Vultr said,
// when Vultr will not. A box restoring has no state to find it by: it is
// destroyed by its id at once.
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
	box := p.state.Boxes[at]
	if box.Restoring && box.InstanceID != "" {
		if !p.reapInstance(ctx, CloudInstance{ID: box.InstanceID, Label: name}, fmt.Sprintf("%s (Vultr instance %s, restoring)", name, box.InstanceID)) {
			return false
		}
	} else {
		if err := p.check.Provider.Down(ctx, name); err != nil {
			p.move(ctx, fmt.Sprintf("failed to destroy %s (%s): %v", name, why, oneLine(err.Error())))
			p.fail(fmt.Errorf("destroying %s: %w", name, err))
			return false
		}
		inst, found, err := p.held(ctx, name)
		if err != nil {
			p.move(ctx, fmt.Sprintf("could not tell whether %s (%s) is destroyed: Vultr's instances could not be listed: %v", name, why, oneLine(err.Error())))
			p.fail(fmt.Errorf("reading back the destroying of %s: %w", name, err))
			return false
		}
		if found && !p.reapInstance(ctx, inst, fmt.Sprintf("%s (Vultr instance %s, %s), still held after the wrapper was done with it", name, inst.ID, inst.Status)) {
			return false
		}
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
	// FeeLimitAt is when Vultr last refused a box for its fee limit, zero when
	// it is not holding one back.
	FeeLimitAt time.Time
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

		FeeLimitAt: state.FeeLimitAt,
	}, nil
}

// write is the CLOUD section: the month's spend against the cap, each box up
// with its age, and the latest moves.
func (r CloudReading) write(b *strings.Builder) {
	clip(b, fmt.Sprintf("CLOUD: $%.2f spent of $%.2f this month, %d of %d boxes", r.SpentUSD, r.CapUSD, len(r.Boxes), r.MaxBoxes))
	for _, box := range r.Boxes {
		if box.Restoring {
			clip(b, fmt.Sprintf("  %s  restoring %s (Vultr instance %s), up %s", box.Name, Clock(r.Now.Sub(box.RestoringSince)), box.InstanceID, Clock(r.Now.Sub(box.Up))))
			continue
		}
		line := fmt.Sprintf("  %s  up %s", box.Name, Clock(r.Now.Sub(box.Up)))
		if !box.IdleSince.IsZero() {
			line += ", idle " + Clock(r.Now.Sub(box.IdleSince))
		} else {
			line += ", working"
		}
		clip(b, line)
	}
	if !r.FeeLimitAt.IsZero() {
		clip(b, "  Vultr fee limit reached since "+r.FeeLimitAt.UTC().Format("01-02 15:04Z"))
	}
	for _, m := range r.Moves {
		clip(b, "  "+m.At.UTC().Format("01-02 15:04Z")+" "+m.What)
	}
}
