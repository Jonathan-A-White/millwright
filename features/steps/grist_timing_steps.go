package steps

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// timingContext is what features/grist_timing.feature adds to a mill: a clock
// the steps move (the mill's own looks at it move it not at all), the follower's
// notices, and wrappers of the backend and the harness that move it by the
// seconds a photo's fetch, a harness run and a delivery take.
type timingContext struct {
	c       *gristContext
	clock   *timingClock
	notices *application.GristNotices
	// noticedAfter is how long after the grist was sent the follower noticed
	// it; negative is that it did not.
	noticedAfter time.Duration
	noticed      bool

	blob, harness, deliver time.Duration
	harnessOwn             time.Duration

	line   application.GrindLine
	out    bytes.Buffer
	runErr error
	groups []application.GristTimingGroup
}

// timingClock is a clock that reads one moment until it is moved.
type timingClock struct {
	mu sync.Mutex
	at time.Time
}

func (t *timingClock) now() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.at
}

func (t *timingClock) move(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.at = t.at.Add(d)
}

// seconds is text such as "3.2" as a duration.
func seconds(text string) time.Duration {
	f, _ := strconv.ParseFloat(text, 64)
	return time.Duration(math.Round(f * float64(time.Second)))
}

func (t *timingContext) ensureClock() {
	if t.clock == nil {
		t.clock = &timingClock{at: gristNow}
	}
}

// nowOr is the timing clock when a step made one, else the clock given.
func (t *timingContext) nowOr(other func() time.Time) func() time.Time {
	if t.clock == nil {
		return other
	}
	return t.clock.now
}

// postern is the backend, moving the clock when a photo is fetched or an
// answer delivered, when a step said they take time.
func (t *timingContext) postern(inner application.Postern) application.Postern {
	if t.clock == nil {
		return inner
	}
	return timedPostern{Postern: inner, t: t}
}

type timedPostern struct {
	application.Postern
	t *timingContext
}

func (p timedPostern) Blob(ctx context.Context, hash string) ([]byte, error) {
	p.t.clock.move(p.t.blob)
	return p.Postern.Blob(ctx, hash)
}

func (p timedPostern) Deliver(ctx context.Context, payload []byte) (string, error) {
	p.t.clock.move(p.t.deliver)
	return p.Postern.Deliver(ctx, payload)
}

// grinder is the harness, taking the seconds a step said and reporting its own
// run as the harness's duration_ms.
func (t *timingContext) grinder(inner application.Grinder) application.Grinder {
	if t.clock == nil {
		return inner
	}
	return timedGrinder{Grinder: inner, t: t}
}

type timedGrinder struct {
	application.Grinder
	t *timingContext
}

func (g timedGrinder) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	result, err := g.Grinder.Grind(ctx, call)
	g.t.clock.move(g.t.harness)
	result.Duration = g.t.harnessOwn
	return result, err
}

// noticeGrists tells the notices when the follower noticed each grist, once
// they have all been sent.
func (t *timingContext) noticeGrists() error {
	if !t.noticed {
		return nil
	}
	records, err := t.c.backend.Messages(context.Background(), 0)
	if err != nil {
		return err
	}
	t.notices = application.NewGristNotices()
	for _, r := range records {
		t.notices.Note(r.Txid, gristNow.Add(t.noticedAfter))
	}
	return nil
}

func registerGristTiming(ctx *godog.ScenarioContext, c *gristContext) {
	t := &c.timing
	// After the reset of gristContext's own Before hook, which clears it.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		t.c = c
		return ctx, nil
	})

	ctx.Given(`^the mill's clock reads ([0-9.]+) seconds after the grist was sent$`, t.clockReads)
	ctx.Given(`^the follower noticed the grist ([0-9.]+) seconds after it was sent$`, t.followerNoticed)
	ctx.Given(`^fetching a photo takes ([0-9.]+) seconds$`, t.photoTakes)
	ctx.Given(`^the harness takes ([0-9.]+) seconds, ([0-9.]+) of them its own run$`, t.harnessTakes)
	ctx.Given(`^delivering an answer takes ([0-9.]+) seconds$`, t.deliveryTakes)
	ctx.Given(`^the record holds an old line of "([^"]*)" "([^"]*)" that took ([0-9.]+) seconds$`, t.oldLine)
	ctx.Given(`^the record holds a grind of "([^"]*)" "([^"]*)" that took ([0-9.]+) seconds, a day and a half later$`, t.laterLine)
	ctx.Given(`^the record holds a grind of "([^"]*)" "([^"]*)" that took ([0-9.]+) seconds$`, t.plainLine)
	ctx.Given(`^the record holds these grinds of "([^"]*)" "([^"]*)":$`, t.grinds)

	ctx.When(`^mw grist timing is run for the app "([^"]*)" and the kind "([^"]*)"$`, t.run)
	ctx.When(`^mw grist timing is run for the app "([^"]*)" with --since 24h$`, t.runSince)

	ctx.Then(`^the record holds one line for the grist$`, t.oneLine)
	ctx.Then(`^that line says wake ([0-9.]+), pickup ([0-9.]+), slot ([0-9.]+), deliver ([0-9.]+) and total ([0-9.]+) seconds$`, t.sayPhases)
	ctx.Then(`^that line says wake ([0-9.]+) and pickup ([0-9.]+) seconds$`, t.sayWakePickup)
	ctx.Then(`^that line says startup ([0-9.]+) and model ([0-9.]+) seconds, which add up to ([0-9.]+)$`, t.sayStartupModel)
	ctx.Then(`^that line says it took ([0-9.]+) seconds$`, t.sayTook)
	ctx.Then(`^that line has no slot, startup or model phase$`, t.noHarnessPhases)
	ctx.Then(`^that line has no deliver or total phase$`, t.noDeliverPhases)
	ctx.Then(`^the first two grists waited ([0-9.]+) seconds for a slot$`, t.firstTwoWaited)
	ctx.Then(`^the third grist waited ([0-9.]+) seconds for a slot$`, t.thirdWaited)
	ctx.Then(`^mw grist timing succeeded$`, t.succeeded)
	ctx.Then(`^it printed for "([^"]*)" (\d+) grinds? and these phases:$`, t.printed)
	ctx.Then(`^it printed nothing for "([^"]*)" or "([^"]*)"$`, t.printedNothing)
}

func (t *timingContext) clockReads(after string) error {
	t.ensureClock()
	t.clock.at = gristNow.Add(seconds(after))
	return nil
}

func (t *timingContext) followerNoticed(after string) error {
	t.noticed, t.noticedAfter = true, seconds(after)
	return nil
}

func (t *timingContext) photoTakes(s string) error {
	t.ensureClock()
	t.blob = seconds(s)
	return nil
}

func (t *timingContext) harnessTakes(all, own string) error {
	t.ensureClock()
	t.harness, t.harnessOwn = seconds(all), seconds(own)
	return nil
}

func (t *timingContext) deliveryTakes(s string) error {
	t.ensureClock()
	t.deliver = seconds(s)
	return nil
}

func (t *timingContext) append(line application.GrindLine) error {
	return t.c.state.Append(context.Background(), line)
}

func (t *timingContext) oldLine(app, kind, took string) error {
	return t.append(application.GrindLine{Time: gristNow.Add(-48 * time.Hour), App: app, Kind: kind, Status: application.GristAnswered, Seconds: seconds(took).Seconds(), Delivered: true})
}

func (t *timingContext) laterLine(app, kind, took string) error {
	return t.append(application.GrindLine{Time: gristNow.Add(-12 * time.Hour), App: app, Kind: kind, Status: application.GristAnswered, Seconds: seconds(took).Seconds(), Delivered: true})
}

func (t *timingContext) plainLine(app, kind, took string) error {
	return t.append(application.GrindLine{Time: gristNow.Add(-time.Hour), App: app, Kind: kind, Status: application.GristAnswered, Seconds: seconds(took).Seconds(), Delivered: true})
}

func (t *timingContext) grinds(app, kind string, table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("the table needs a header and a row")
	}
	header := table.Rows[0].Cells
	for _, row := range table.Rows[1:] {
		line := application.GrindLine{Time: gristNow.Add(-time.Hour), App: app, Kind: kind, Status: application.GristAnswered, Delivered: true}
		for i, cell := range row.Cells {
			v, err := strconv.ParseFloat(cell.Value, 64)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", header[i].Value, cell.Value)
			}
			switch header[i].Value {
			case "seconds":
				line.Seconds = v
			case "wake_s":
				line.WakeSeconds = &v
			case "pickup_s":
				line.PickupSeconds = &v
			case "slot_s":
				line.SlotSeconds = &v
			case "startup_s":
				line.StartupSeconds = &v
			case "model_s":
				line.ModelSeconds = &v
			case "deliver_s":
				line.DeliverSeconds = &v
			case "total_s":
				line.TotalSeconds = &v
			default:
				return fmt.Errorf("the column %q is no phase", header[i].Value)
			}
		}
		if err := t.append(line); err != nil {
			return err
		}
	}
	return nil
}

func (t *timingContext) timing(since time.Time, app, kind string) error {
	t.out.Reset()
	t.groups, t.runErr = application.GristTiming{
		State: t.c.state, Out: &t.out,
	}.Run(context.Background(), since, app, kind)
	return nil
}

func (t *timingContext) run(app, kind string) error { return t.timing(time.Time{}, app, kind) }

func (t *timingContext) runSince(app string) error {
	return t.timing(gristNow.Add(-24*time.Hour), app, "")
}

func (t *timingContext) succeeded() error {
	if t.runErr != nil {
		return fmt.Errorf("mw grist timing failed: %v", t.runErr)
	}
	return nil
}

func (t *timingContext) oneLine() error {
	lines, err := t.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	if len(lines) != 1 {
		return fmt.Errorf("expected one line in the record, got %d", len(lines))
	}
	t.line = lines[0]
	return nil
}

// near says a phase of the line is the seconds given, to the tenth.
func near(name string, got *float64, want string) error {
	if got == nil {
		return fmt.Errorf("expected %s_s %s, the line leaves it out", name, want)
	}
	if w, _ := strconv.ParseFloat(want, 64); math.Abs(*got-w) > 0.001 {
		return fmt.Errorf("expected %s_s %s, got %v", name, want, *got)
	}
	return nil
}

func (t *timingContext) sayPhases(wake, pickup, slot, deliver, total string) error {
	l := t.line
	for _, check := range []error{
		near("wake", l.WakeSeconds, wake), near("pickup", l.PickupSeconds, pickup), near("slot", l.SlotSeconds, slot),
		near("deliver", l.DeliverSeconds, deliver), near("total", l.TotalSeconds, total),
	} {
		if check != nil {
			return check
		}
	}
	return nil
}

func (t *timingContext) sayWakePickup(wake, pickup string) error {
	if err := near("wake", t.line.WakeSeconds, wake); err != nil {
		return err
	}
	return near("pickup", t.line.PickupSeconds, pickup)
}

func (t *timingContext) sayStartupModel(startup, model, sum string) error {
	l := t.line
	if err := near("startup", l.StartupSeconds, startup); err != nil {
		return err
	}
	if err := near("model", l.ModelSeconds, model); err != nil {
		return err
	}
	if got := *l.StartupSeconds + *l.ModelSeconds; math.Abs(got-seconds(sum).Seconds()) > 0.001 {
		return fmt.Errorf("expected startup and model to add up to %s, got %v", sum, got)
	}
	return nil
}

func (t *timingContext) sayTook(took string) error {
	if got, want := t.line.Seconds, seconds(took).Seconds(); math.Abs(got-want) > 0.001 {
		return fmt.Errorf("expected seconds %v, got %v", want, got)
	}
	return nil
}

func (t *timingContext) noHarnessPhases() error {
	l := t.line
	if l.SlotSeconds != nil || l.StartupSeconds != nil || l.ModelSeconds != nil {
		return fmt.Errorf("expected no slot, startup or model phase, got %+v", l)
	}
	return nil
}

func (t *timingContext) noDeliverPhases() error {
	l := t.line
	if l.Delivered || l.DeliverSeconds != nil || l.TotalSeconds != nil {
		return fmt.Errorf("expected an undelivered line with no deliver or total phase, got %+v", l)
	}
	return nil
}

// slotOf is the slot_s of the line of the i-th grist sent.
func (t *timingContext) slotOf(i int) (*float64, error) {
	records, err := t.c.backend.Messages(context.Background(), 0)
	if err != nil {
		return nil, err
	}
	lines, err := t.c.state.Lines(context.Background())
	if err != nil {
		return nil, err
	}
	if len(records) <= i {
		return nil, fmt.Errorf("the backend holds %d grists, not %d", len(records), i+1)
	}
	for _, l := range lines {
		if l.Txid == records[i].Txid {
			return l.SlotSeconds, nil
		}
	}
	return nil, fmt.Errorf("the record holds no line for the grist %d", i+1)
}

func (t *timingContext) firstTwoWaited(want string) error {
	for i := 0; i < 2; i++ {
		slot, err := t.slotOf(i)
		if err != nil {
			return err
		}
		if err := near(fmt.Sprintf("slot of grist %d", i+1), slot, want); err != nil {
			return err
		}
	}
	return nil
}

func (t *timingContext) thirdWaited(want string) error {
	slot, err := t.slotOf(2)
	if err != nil {
		return err
	}
	return near("slot of grist 3", slot, want)
}

// block is the part of the printed table that belongs to name, or "".
func (t *timingContext) block(name string) string {
	text := t.out.String()
	start := strings.Index(text, name+": ")
	if start < 0 {
		return ""
	}
	rest := text[start:]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func (t *timingContext) printed(name string, grinds int, table *godog.Table) error {
	block := t.block(name)
	if block == "" {
		return fmt.Errorf("nothing was printed for %q:\n%s", name, t.out.String())
	}
	noun := "grinds"
	if grinds == 1 {
		noun = "grind"
	}
	if head := fmt.Sprintf("%s: %d %s\n", name, grinds, noun); !strings.HasPrefix(block, head) {
		return fmt.Errorf("expected the heading %q:\n%s", strings.TrimSpace(head), block)
	}
	got := map[string][]string{}
	for _, row := range strings.Split(block, "\n")[2:] {
		if fields := strings.Fields(row); len(fields) == 4 {
			got[fields[0]] = fields[1:]
		}
	}
	wantRows := table.Rows[1:]
	if len(got) != len(wantRows) {
		return fmt.Errorf("expected %d phases, printed %d:\n%s", len(wantRows), len(got), block)
	}
	for _, row := range wantRows {
		phase, count, median, p90 := row.Cells[0].Value, row.Cells[1].Value, row.Cells[2].Value, row.Cells[3].Value
		fields, ok := got[phase]
		if !ok {
			return fmt.Errorf("the phase %s was not printed:\n%s", phase, block)
		}
		if fields[0] != count || fields[1] != median+"s" || fields[2] != p90+"s" {
			return fmt.Errorf("expected %s %s %ss %ss, printed %v", phase, count, median, p90, fields)
		}
	}
	return nil
}

func (t *timingContext) printedNothing(a, b string) error {
	for _, name := range []string{a, b} {
		if t.block(name) != "" {
			return fmt.Errorf("%q was printed:\n%s", name, t.out.String())
		}
	}
	return nil
}
