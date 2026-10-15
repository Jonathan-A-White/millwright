package steps

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// poolContext is what features/grist_pool.feature adds to a mill: a warm pool
// whose sessions are a fake harness's, kept across the scenario's passes, and
// a clock of its own that only the steps move.
type poolContext struct {
	c      *gristContext
	warmer *apptest.FakeWarmGrinder
	pool   *application.GristPool
	quiet  time.Duration

	mu sync.Mutex
	at time.Time
}

func (p *poolContext) now() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.at
}

// port is the pool the mill is given: nil until a step asks for one, built
// the first time it is wanted, with as many slots as the mill has.
func (p *poolContext) port() *application.GristPool {
	if p.warmer == nil {
		return nil
	}
	if p.pool == nil {
		quiet := p.quiet
		if quiet == 0 {
			quiet = application.DefaultGristPoolQuiet
		}
		p.pool = &application.GristPool{
			Warmer: p.warmer, Slots: 1 + len(p.c.slots.extra),
			Quiet: quiet, MaxAge: application.DefaultGristPoolMaxAge,
			TempDir: p.c.home, Now: p.now,
		}
	}
	return p.pool
}

func registerGristPool(ctx *godog.ScenarioContext, c *gristContext) {
	p := &c.pool
	// After the reset of gristContext's own Before hook, which clears it.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		p.c, p.at = c, gristNow
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if p.pool != nil {
			p.pool.Close()
		}
		return ctx, nil
	})

	ctx.Given(`^the mill keeps a warm pool$`, p.keepsAPool)
	ctx.Given(`^the pool's quiet spell is (\d+) minutes$`, p.quietSpellIs)
	ctx.Given(`^the harness cannot start a warm session$`, p.cannotStart)
	ctx.Given(`^the mill grinds$`, c.theMillGrinds)
	ctx.Given(`^the warm sessions are held until (\d+) of them are answering at once$`, p.sessionsMeet)

	ctx.When(`^the phone sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", with (\d+) photos?$`, c.thePhoneSends)
	ctx.When(`^(\d+) minutes pass with no grist$`, p.minutesPass)
	ctx.When(`^the grind "([^"]*)"'s instructions change at commit "([^"]*)"$`, p.instructionsChange)
	ctx.When(`^the warm sessions fail when given a grist$`, p.sessionsFail)
	ctx.When(`^the warm session ends on its own$`, p.sessionEnds)

	ctx.Then(`^(\d+) cold sessions? and (\d+) warm sessions? (?:was|were) given grists$`, p.sessionsAnswered)
	ctx.Then(`^the pool holds (\d+) warm sessions? for "([^"]*)" "([^"]*)"$`, p.poolHolds)
	ctx.Then(`^the pool holds no session for "([^"]*)" "([^"]*)"$`, p.poolHoldsNone)
	ctx.Then(`^mw status's pool line reads "([^"]*)"$`, p.statusLineReads)
	ctx.Then(`^no warm session was given more than one grist$`, p.noSessionTwice)
	ctx.Then(`^no warm session is left running$`, p.noneRunning)
	ctx.Then(`^(\d+) warm sessions? (?:was|were) retired unused$`, p.retiredUnused)
	ctx.Then(`^the warm session was started for the grind's own system prompt, model, effort and schema$`, p.startedForTheGrind)
	ctx.Then(`^the warm session's message names the photo in its own directory, and a tag the system prompt does not hold$`, p.messageNamesThePhoto)
	ctx.Then(`^the warm session waiting was started for the changed instructions$`, p.waitingForChanged)
	ctx.Then(`^the report notes that the warm session could not answer, so the grist ran cold$`, p.reportNotesTheFallback)
}

func (p *poolContext) keepsAPool() error {
	p.warmer = &apptest.FakeWarmGrinder{ResultOf: func() application.SessionResult { return p.c.grinder.Result }}
	return nil
}

func (p *poolContext) sessionsMeet(n int) error {
	p.warmer.Together = n
	return nil
}

func (p *poolContext) quietSpellIs(minutes int) error {
	p.quiet = time.Duration(minutes) * time.Minute
	return nil
}

func (p *poolContext) cannotStart() error {
	p.warmer.StartErr = errors.New("claude: no such file or directory")
	return nil
}

func (p *poolContext) minutesPass(minutes int) error {
	p.mu.Lock()
	p.at = p.at.Add(time.Duration(minutes) * time.Minute)
	p.mu.Unlock()
	p.port().Tick(context.Background())
	return nil
}

// changedInstructions is what the grind's instructions say once a step has
// changed them.
const changedInstructions = "List every kind of thing kept at the Place, and nothing else."

func (p *poolContext) instructionsChange(kind, commit string) error {
	checkout := p.c.checkout("cairn")
	p.c.commit = commit
	p.c.grinds.SetFile(checkout, commit, "grinds/"+kind+".json", []byte(gristGrindFile))
	p.c.grinds.SetFile(checkout, commit, "grinds/sweep.md", []byte(changedInstructions))
	p.c.grinds.SetFile(checkout, commit, "contexts/inventory/schemas/sweep-result.schema.json", []byte(gristSchema))
	return nil
}

func (p *poolContext) sessionsFail() error {
	p.warmer.AnswerErr = errors.New("claude: the session's result is empty")
	return nil
}

func (p *poolContext) sessionEnds() error {
	waiting := p.waiting()
	if len(waiting) != 1 {
		return fmt.Errorf("expected 1 warm session waiting, found %d", len(waiting))
	}
	waiting[0].Exit()
	return nil
}

// waiting is every warm session still waiting for its grist.
func (p *poolContext) waiting() []*apptest.FakeWarmSession {
	var running []*apptest.FakeWarmSession
	for _, s := range p.warmer.Sessions() {
		if s.Running() {
			running = append(running, s)
		}
	}
	return running
}

// answeredWarm is every warm session given a grist, in the order started.
func (p *poolContext) answeredWarm() []*apptest.FakeWarmSession {
	var answered []*apptest.FakeWarmSession
	for _, s := range p.warmer.Sessions() {
		if len(s.Messages()) > 0 {
			answered = append(answered, s)
		}
	}
	return answered
}

func (p *poolContext) sessionsAnswered(cold, warm int) error {
	gotCold := len(p.c.grinder.Calls())
	gotWarm := 0
	for _, s := range p.warmer.Sessions() {
		gotWarm += len(s.Messages())
	}
	if gotCold != cold || gotWarm != warm {
		return fmt.Errorf("expected %d cold and %d warm sessions given grists, got %d cold and %d warm", cold, warm, gotCold, gotWarm)
	}
	return nil
}

// kind is the pool's view of one app and kind, and whether it holds any.
func (p *poolContext) kind(app, kind string) (application.GristPoolKind, bool) {
	for _, k := range p.port().View().Kinds {
		if k.App == app && k.Kind == kind {
			return k, true
		}
	}
	return application.GristPoolKind{}, false
}

func (p *poolContext) poolHolds(n int, app, kind string) error {
	k, _ := p.kind(app, kind)
	if k.Warm != n {
		return fmt.Errorf("expected the pool to hold %d warm sessions for %s %s, it holds %d (%+v)", n, app, kind, k.Warm, p.port().View())
	}
	if running := len(p.waiting()); running != n {
		return fmt.Errorf("expected %d warm sessions to be running, %d are", n, running)
	}
	return nil
}

func (p *poolContext) poolHoldsNone(app, kind string) error {
	if k, ok := p.kind(app, kind); ok && (k.Warm != 0 || k.Busy != 0) {
		return fmt.Errorf("expected the pool to hold nothing for %s %s, it holds %+v", app, kind, k)
	}
	return nil
}

func (p *poolContext) statusLineReads(want string) error {
	if got := p.port().View().Line(); got != want {
		return fmt.Errorf("expected mw status's pool line %q, got %q", want, got)
	}
	return nil
}

func (p *poolContext) noSessionTwice() error {
	for i, s := range p.warmer.Sessions() {
		if n := len(s.Messages()); n > 1 {
			return fmt.Errorf("warm session %d was given %d grists", i+1, n)
		}
	}
	return nil
}

func (p *poolContext) noneRunning() error {
	if running := len(p.waiting()); running != 0 {
		return fmt.Errorf("expected no warm session to be running, %d are", running)
	}
	return nil
}

func (p *poolContext) retiredUnused(n int) error {
	got := 0
	for _, s := range p.warmer.Sessions() {
		if s.Retired() && len(s.Messages()) == 0 {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected %d warm sessions retired unused, got %d", n, got)
	}
	return nil
}

// gristTagIn is the tag of the lines a message sets the app's request between.
var gristTagIn = regexp.MustCompile(`GRIST INPUT ([0-9a-f]+) BEGINS`)

func (p *poolContext) onlyAnsweredWarm() (*apptest.FakeWarmSession, error) {
	answered := p.answeredWarm()
	if len(answered) != 1 {
		return nil, fmt.Errorf("expected 1 warm session to have answered, %d did", len(answered))
	}
	return answered[0], nil
}

func (p *poolContext) startedForTheGrind() error {
	s, err := p.onlyAnsweredWarm()
	if err != nil {
		return err
	}
	cold := p.c.grinder.Calls()
	if len(cold) == 0 {
		return fmt.Errorf("no cold grind ran to compare the warm session with")
	}
	want, got := cold[0].Call, s.Call
	if got.Prompt != "" {
		return fmt.Errorf("the warm session was started with a prompt: %q", got.Prompt)
	}
	if got.Model != want.Model || got.Effort != want.Effort || got.Schema != want.Schema ||
		got.MaxTurns != want.MaxTurns || got.Timeout != want.Timeout {
		return fmt.Errorf("the warm session was started with %+v, the cold one with %+v", got, want)
	}
	// The system prompts are the same bytes: the tag is in each message.
	if got.System != want.System {
		return fmt.Errorf("the warm session's system prompt is not the cold one's:\n%s\n---\n%s", got.System, want.System)
	}
	if !strings.HasSuffix(got.System, gristInstructions) {
		return fmt.Errorf("the warm session's system prompt does not end with the grind's instructions:\n%s", got.System)
	}
	return nil
}

func (p *poolContext) messageNamesThePhoto() error {
	s, err := p.onlyAnsweredWarm()
	if err != nil {
		return err
	}
	seen := s.Messages()[0]
	if len(seen.Files) != 1 {
		return fmt.Errorf("expected the warm session's directory to hold the grist's photo, it held %d files", len(seen.Files))
	}
	for name := range seen.Files {
		if full := seen.Call.Dir + "/" + name; !strings.Contains(seen.Call.Prompt, full) {
			return fmt.Errorf("the message does not name the photo %s:\n%s", full, seen.Call.Prompt)
		}
	}
	if promptTag := gristTagIn.FindStringSubmatch(seen.Call.Prompt); promptTag == nil || strings.Contains(s.Call.System, promptTag[1]) {
		return fmt.Errorf("the message does not carry a tag of its own, apart from the system prompt:\n%s\n---\n%s", s.Call.System, seen.Call.Prompt)
	}
	return nil
}

func (p *poolContext) waitingForChanged() error {
	waiting := p.waiting()
	if len(waiting) != 1 {
		return fmt.Errorf("expected 1 warm session waiting, found %d", len(waiting))
	}
	if !strings.HasSuffix(waiting[0].Call.System, changedInstructions) {
		return fmt.Errorf("the warm session waiting was started for:\n%s", waiting[0].Call.System)
	}
	return nil
}

func (p *poolContext) reportNotesTheFallback() error {
	for _, note := range p.c.report.Notes {
		if strings.Contains(note, "warm session") && strings.Contains(note, "cold") {
			return nil
		}
	}
	return fmt.Errorf("expected a note that the warm session could not answer, got %q", p.c.report.Notes)
}
