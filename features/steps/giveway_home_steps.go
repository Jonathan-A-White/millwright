package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// giveWayHomeContext is one scenario of features/giveway_home.feature: a fake
// tracker holding the home's view of a host that gives way, and what each alarm
// said of it.
type giveWayHomeContext struct {
	tracker *apptest.FakeTracker
	now     time.Time
	stale   time.Duration

	clauses []application.NudgeClause
	sweep   application.SweepReport
	status  application.StatusReport
	printed string
	starved []application.DamperAlarm
	verdict application.Verdict
	said    string
}

// InitializeGiveWayHomeScenario registers the steps of features/giveway_home.feature.
func InitializeGiveWayHomeScenario(ctx *godog.ScenarioContext) {
	c := &giveWayHomeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = giveWayHomeContext{}
		return ctx, nil
	})

	ctx.Given(`^the home "([^"]*)" and the host "([^"]*)", at 17:00Z with giveway_stale of (\d+) minutes$`, c.theHomeAndTheHost)
	ctx.Given(`^the story "([^"]*)" pathed to "([^"]*)" and claimed by "([^"]*)" (\d+) minutes ago, its lease lapsed (\d+) minutes ago$`, c.aClaimedStory)
	ctx.Given(`^the story "([^"]*)" pathed to no host and claimed by "([^"]*)" (\d+) minutes ago, its lease lapsed (\d+) minutes ago$`, c.aClaimedStoryOfNoHost)
	ctx.Given(`^the host "([^"]*)" last synced (\d+) minutes ago$`, c.lastSynced)
	ctx.Given(`^the desktop's give-way note is (standing|missing|stale)$`, c.theNoteIs)
	ctx.Given(`^the desktop noted the rig "([^"]*)" held back for (\d+) hours with a story ready$`, c.theDesktopNotedARig)

	ctx.When(`^the home reads the quiet alarm$`, c.homeReadsTheQuietAlarm)
	ctx.When(`^the desktop reads its own quiet alarm$`, c.desktopReadsItsQuietAlarm)
	ctx.When(`^mw sweep reads the desktop$`, c.sweepReadsTheDesktop)
	ctx.When(`^mw status runs on the home$`, c.statusOnTheHome)
	ctx.When(`^mw status runs on the desktop$`, c.statusOnTheDesktop)
	ctx.When(`^the home judges the starved rigs$`, c.homeJudgesTheStarvedRigs)
	ctx.When(`^mw doctor checks the claims with no session$`, c.doctorChecksTheClaims)

	ctx.Then(`^the quiet alarm has no clause$`, c.quietHasNoClause)
	ctx.Then(`^the quiet alarm says: (.+)$`, c.quietSays)
	ctx.Then(`^the story "([^"]*)" gets no comment, is not run=stuck and is named as frozen$`, c.storyIsFrozen)
	ctx.Then(`^the story "([^"]*)" gets the stuck comment and is run=stuck$`, c.storyIsStuck)
	ctx.Then(`^the desktop's block says "([^"]*)" and has no STRANDED line$`, c.blockSaysAndNoStranded)
	ctx.Then(`^the desktop's block has a STRANDED line and no gives-way line$`, c.blockStrandedNoGivesWay)
	ctx.Then(`^the desktop's block says "([^"]*)" and has a STRANDED line$`, c.blockSaysAndStranded)
	ctx.Then(`^its own story is not STRANDED$`, c.ownNotStranded)
	ctx.Then(`^its own story is STRANDED$`, c.ownStranded)
	ctx.Then(`^no rig is starved$`, c.noRigStarved)
	ctx.Then(`^the rig "([^"]*)" is starved$`, c.rigStarved)
	ctx.Then(`^the stuck-claims check finds nothing$`, c.checkFindsNothing)
	ctx.Then(`^the stuck-claims check names "([^"]*)"$`, c.checkNames)
}

func (c *giveWayHomeContext) theHomeAndTheHost(home, host string, stale int) error {
	c.now = time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)
	c.stale = time.Duration(stale) * time.Minute
	c.tracker = apptest.NewFakeTracker()
	c.tracker.Clock = func() time.Time { return c.now }
	for _, epic := range []struct{ id, rig, host string }{{"mw-g", "millwright", host}, {"mw-l", "lampas", home}} {
		defaults := domain.Path{}
		for field, value := range map[string]string{
			"rig": epic.rig, "branch": "main", "harness": "claude", "model": "sonnet",
			"effort": "high", "formula": "tdd-feature", "host": epic.host,
		} {
			if err := defaults.Set(field, value); err != nil {
				return err
			}
		}
		c.tracker.AddEpic(epic.id, defaults)
	}
	return nil
}

func (c *giveWayHomeContext) claim(id, holder string, claimedAgo, lapsedAgo int) error {
	if err := c.tracker.ClaimAs(id, holder, c.now.Add(-time.Duration(lapsedAgo)*time.Minute)); err != nil {
		return err
	}
	return c.tracker.SetStarted(id, c.now.Add(-time.Duration(claimedAgo)*time.Minute))
}

func (c *giveWayHomeContext) aClaimedStory(id, host, holder string, claimedAgo, lapsedAgo int) error {
	story := domain.Story{ID: id, Title: "A story the desktop works"}
	if err := story.Overrides.Set("host", host); err != nil {
		return err
	}
	c.tracker.AddStory("mw-g", story)
	return c.claim(id, holder, claimedAgo, lapsedAgo)
}

func (c *giveWayHomeContext) aClaimedStoryOfNoHost(id, holder string, claimedAgo, lapsedAgo int) error {
	story := domain.Story{ID: id, Title: "A story a dispatch claimed and never placed"}
	if err := story.Overrides.Set("host", domain.HostAuto); err != nil {
		return err
	}
	c.tracker.AddStory("mw-g", story)
	return c.claim(id, holder, claimedAgo, lapsedAgo)
}

func (c *giveWayHomeContext) lastSynced(host string, ago int) error {
	at := c.now.Add(-time.Duration(ago) * time.Minute).UTC().Format(application.LastSyncFormat)
	return c.tracker.SetNote(context.Background(), application.LastSyncKey(host), at)
}

// theNoteIs leaves the desktop's note as a host writes it: standing is one written
// 5 minutes ago for a freeze begun 40 minutes ago; stale is one nobody has
// rewritten for 45 minutes (over twice giveway_stale).
func (c *giveWayHomeContext) theNoteIs(state string) error {
	var note application.GiveWayNote
	switch state {
	case "missing":
		return nil
	case "standing":
		note = application.GiveWayNote{Since: c.now.Add(-40 * time.Minute), By: "windows-unlock", Seen: c.now.Add(-5 * time.Minute)}
	case "stale":
		note = application.GiveWayNote{Since: c.now.Add(-3 * time.Hour), By: "windows-unlock", Seen: c.now.Add(-45 * time.Minute)}
	}
	return c.tracker.SetNote(context.Background(), application.GiveWayKey("desktop"), note.String())
}

func (c *giveWayHomeContext) theDesktopNotedARig(rig string, hours int) error {
	story := domain.Story{ID: "mw-l.1", Title: "A story that waits"}
	c.tracker.AddStory("mw-l", story)
	held := []application.StarvedRig{{Rig: rig, Since: c.now.Add(-time.Duration(hours) * time.Hour), Hold: application.StarvedHolds["cap"], Reason: "load 28.2"}}
	return c.tracker.SetNote(context.Background(), application.StarvedKey("desktop"), application.FormatStarved(held))
}

func (c *giveWayHomeContext) readQuiet(host string) error {
	clauses, err := application.Nudge{
		Tracker: c.tracker, Notes: c.tracker, Host: host, GiveWayStale: c.stale,
		Now: func() time.Time { return c.now },
	}.Run(context.Background())
	c.clauses = clauses
	return err
}

func (c *giveWayHomeContext) homeReadsTheQuietAlarm() error { return c.readQuiet("laptop") }

func (c *giveWayHomeContext) desktopReadsItsQuietAlarm() error { return c.readQuiet("desktop") }

func (c *giveWayHomeContext) sweepReadsTheDesktop() error {
	var err error
	c.sweep, err = application.Sweep{
		Tracker: c.tracker, Host: "desktop", Notes: c.tracker, GiveWayStale: c.stale,
		Now: func() time.Time { return c.now },
	}.Run(context.Background())
	return err
}

func (c *giveWayHomeContext) runStatus(host string) error {
	var err error
	c.status, err = application.Status{
		Tracker: c.tracker, Notes: c.tracker, Host: host, Seat: "builder", HostSilence: 2 * time.Hour,
		GiveWayStale: c.stale, Now: func() time.Time { return c.now },
	}.Run(context.Background())
	c.printed = c.status.String()
	return err
}

func (c *giveWayHomeContext) statusOnTheHome() error { return c.runStatus("laptop") }

func (c *giveWayHomeContext) statusOnTheDesktop() error { return c.runStatus("desktop") }

func (c *giveWayHomeContext) homeJudgesTheStarvedRigs() error {
	var err error
	c.starved, err = application.DamperSurvey{
		Tracker: c.tracker, Notes: c.tracker, GiveWayStale: c.stale,
	}.Starved(context.Background(), c.now)
	return err
}

func (c *giveWayHomeContext) doctorChecksTheClaims() error {
	check := &application.NoSessionClaimsCheck{
		Claims: c.tracker.StaleClaims, Now: func() time.Time { return c.now }, Host: "laptop",
		GiveWay: application.GiveWayReader{Notes: c.tracker, Stale: c.stale},
	}
	c.verdict, c.said = check.Probe(context.Background())
	return nil
}

func (c *giveWayHomeContext) quietHasNoClause() error {
	if len(c.clauses) != 0 {
		return fmt.Errorf("the quiet alarm spoke: %+v", c.clauses)
	}
	return nil
}

func (c *giveWayHomeContext) quietSays(want string) error {
	for _, clause := range c.clauses {
		if strings.Contains(clause.Text, want) {
			return nil
		}
	}
	return fmt.Errorf("no clause says %q: %+v", want, c.clauses)
}

func (c *giveWayHomeContext) storyIsFrozen(id string) error {
	if got := c.tracker.State(id, application.RunState); got == application.RunStuck {
		return fmt.Errorf("%s was recorded run=stuck", id)
	}
	if comments := c.tracker.Comments(id); len(comments) != 0 {
		return fmt.Errorf("%s was commented on: %v", id, comments)
	}
	if len(c.sweep.Stuck) != 0 {
		return fmt.Errorf("the sweep found %d stuck", len(c.sweep.Stuck))
	}
	if !strings.Contains(c.sweep.String(), "this host gives way, not stuck: "+id) {
		return fmt.Errorf("the sweep report does not name %s as frozen:\n%s", id, c.sweep.String())
	}
	return nil
}

func (c *giveWayHomeContext) storyIsStuck(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunStuck {
		return fmt.Errorf("%s run state = %q, want %s", id, got, application.RunStuck)
	}
	if len(c.tracker.Comments(id)) == 0 {
		return fmt.Errorf("%s got no comment", id)
	}
	return nil
}

// desktopBlock is the desktop's block of OTHER HOSTS as printed.
func (c *giveWayHomeContext) desktopBlock() (string, error) {
	_, rest, found := strings.Cut(c.printed, "\n  desktop · ")
	if !found {
		return "", fmt.Errorf("the report has no block for the desktop:\n%s", c.printed)
	}
	block, _, _ := strings.Cut(rest, "\n\n")
	// A line may wrap: the words are compared with whitespace normalised.
	return strings.Join(strings.Fields(block), " "), nil
}

func (c *giveWayHomeContext) blockSaysAndNoStranded(line string) error {
	block, err := c.desktopBlock()
	if err != nil {
		return err
	}
	if strings.Count(block, line) != 1 {
		return fmt.Errorf("the desktop's block must say %q once:\n%s", line, block)
	}
	if strings.Contains(block, "STRANDED") {
		return fmt.Errorf("the desktop's block has a STRANDED line:\n%s", block)
	}
	return nil
}

func (c *giveWayHomeContext) blockStrandedNoGivesWay() error {
	block, err := c.desktopBlock()
	if err != nil {
		return err
	}
	if !strings.Contains(block, "STRANDED") || strings.Contains(block, "gives way") || strings.Contains(block, "gives-way") {
		return fmt.Errorf("want a STRANDED line and no gives-way line:\n%s", block)
	}
	return nil
}

func (c *giveWayHomeContext) blockSaysAndStranded(line string) error {
	block, err := c.desktopBlock()
	if err != nil {
		return err
	}
	if !strings.Contains(block, line) || !strings.Contains(block, "STRANDED") {
		return fmt.Errorf("want %q and a STRANDED line:\n%s", line, block)
	}
	return nil
}

func (c *giveWayHomeContext) ownNotStranded() error {
	if len(c.status.Running) != 1 {
		return fmt.Errorf("want its one running story, got %d", len(c.status.Running))
	}
	if strings.Contains(c.printed, "STRANDED") || !c.status.Running[0].Stranded.IsZero() {
		return fmt.Errorf("the story is stranded:\n%s", c.printed)
	}
	if !strings.Contains(strings.Join(strings.Fields(c.printed), " "), "desktop gives way since 11-13 16:20Z (1 story frozen)") {
		return fmt.Errorf("the report does not say the host gives way:\n%s", c.printed)
	}
	return nil
}

func (c *giveWayHomeContext) ownStranded() error {
	if len(c.status.Running) != 1 || c.status.Running[0].Stranded.IsZero() || !strings.Contains(c.printed, "STRANDED") {
		return fmt.Errorf("the story is not stranded:\n%s", c.printed)
	}
	return nil
}

func (c *giveWayHomeContext) noRigStarved() error {
	if len(c.starved) != 0 {
		return fmt.Errorf("starved: %+v", c.starved)
	}
	return nil
}

func (c *giveWayHomeContext) rigStarved(rig string) error {
	for _, alarm := range c.starved {
		if strings.HasPrefix(alarm.Text, rig+":") {
			return nil
		}
	}
	return fmt.Errorf("the rig %s is not starved: %+v", rig, c.starved)
}

func (c *giveWayHomeContext) checkFindsNothing() error {
	if c.verdict != application.DoctorOK {
		return fmt.Errorf("the check said %v: %s", c.verdict, c.said)
	}
	return nil
}

func (c *giveWayHomeContext) checkNames(id string) error {
	if c.verdict == application.DoctorOK || !strings.Contains(c.said, id) {
		return fmt.Errorf("the check said %v: %q, want it to name %s", c.verdict, c.said, id)
	}
	return nil
}
