package steps

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// pathSetContext holds the fake work tracker, the stories the scenario made
// and what came of setting or showing a path.
type pathSetContext struct {
	tracker *apptest.FakeTracker

	printed bytes.Buffer
	// warned is what the command wrote on stderr, and heavy the rigs the config
	// names under [heavy_net].
	warned bytes.Buffer
	heavy  map[string]bool
	err    error

	// writesBefore is how many writes the tracker had taken when the command ran.
	writesBefore int

	// rules is the rig files of the scenario, nil until a rig is given one, and
	// log the order in which the command wrote the dependencies, the path and the
	// release.
	rules *apptest.FakeEpicRules
	log   []string

	// after and hold are the --after and --hold of the next command.
	after []string
	hold  string
}

// orderRecorder is the work tracker that notes, in order, the three writes of
// a path set that has to keep to an order: a dependency is added before the
// path is written, and the story is released last.
type orderRecorder struct {
	application.WorkTracker
	log *[]string
}

func (o orderRecorder) AddBlocker(ctx context.Context, blocked, blocker string) error {
	*o.log = append(*o.log, "blocker "+blocker)
	return o.WorkTracker.AddBlocker(ctx, blocked, blocker)
}

func (o orderRecorder) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	*o.log = append(*o.log, "path")
	return o.WorkTracker.SetStoryMetadata(ctx, id, fields)
}

func (o orderRecorder) ReleaseStory(ctx context.Context, id string) error {
	*o.log = append(*o.log, "release")
	return o.WorkTracker.ReleaseStory(ctx, id)
}

// InitializePathSetScenario registers the steps of features/path_set.feature.
func InitializePathSetScenario(ctx *godog.ScenarioContext) {
	c := &pathSetContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = pathSetContext{tracker: apptest.NewFakeTracker()}
		c.tracker.AddFormula("tdd-feature")
		return ctx, nil
	})

	ctx.Given(`^the path-set story "([^"]*)" is open with the metadata:$`, c.aStoryIsOpenWithTheMetadata)
	ctx.Given(`^the path-set story "([^"]*)" is open with no metadata$`, c.aStoryIsOpenWithNoMetadata)
	ctx.Given(`^the path-set story "([^"]*)" is in progress$`, c.aStoryIsInProgress)
	ctx.Given(`^the path-set story "([^"]*)" is closed$`, c.aStoryIsClosed)
	ctx.Given(`^the path-set config names the rig "([^"]*)" as installing dependencies$`, c.theConfigNamesTheRigAsHeavy)
	ctx.Then(`^the path-set warned, saying: (.+)$`, c.theSetWarnedSaying)
	ctx.Then(`^the path-set did not warn$`, c.theSetDidNotWarn)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)"$`, c.thePathIsSetFrom)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)" with (.+)$`, c.thePathIsSetFromWith)
	ctx.When(`^the path of "([^"]*)" is set with no keys$`, c.thePathIsSetWithNoKeys)
	ctx.When(`^the path of "([^"]*)" is set with (\S.*=.*)$`, c.thePathIsSetWith)
	ctx.When(`^the path of "([^"]*)" is shown$`, c.thePathIsShown)
	ctx.Given(`^the path-set rig "([^"]*)" releases on path set( and expects a files key)?$`, c.theRigReleasesOnPathSet)
	ctx.Given(`^the path-set story "([^"]*)" is filed held$`, c.aStoryIsFiledHeld)
	ctx.Given(`^the path-set story "([^"]*)" is filed held as a bug after the sweep rule$`, c.aBugIsFiledHeld)
	ctx.Given(`^the path-set story "([^"]*)" carries the label "([^"]*)"$`, c.aStoryCarriesTheLabel)
	ctx.Given(`^the path-set story "([^"]*)" has the comment "([^"]*)"$`, c.aStoryHasTheComment)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)" after "([^"]*)"$`, c.thePathIsSetFromAfter)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)" after "([^"]*)" holding "([^"]*)"$`, c.thePathIsSetFromAfterHolding)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)" after "([^"]*)" with (.+)$`, c.thePathIsSetFromAfterWith)
	ctx.Then(`^the path-set story "([^"]*)" is open$`, c.theStoryIsOpen)
	ctx.Then(`^the path-set story "([^"]*)" is held$`, c.theStoryIsHeld)
	ctx.Then(`^the path-set story "([^"]*)" waits on "([^"]*)"$`, c.theStoryWaitsOn)
	ctx.Then(`^the path-set story "([^"]*)" has a comment opening "([^"]*)"$`, c.theStoryHasACommentOpening)
	ctx.Then(`^the path-set story "([^"]*)" has no comment opening "([^"]*)"$`, c.theStoryHasNoCommentOpening)
	ctx.Then(`^the path-set added the blockers before it wrote the path and released the story$`, c.theWritesCameInOrder)
	ctx.Then(`^the path-set printed exactly (\d+) line starting: (.+)$`, c.printedLinesStarting)
	ctx.Then(`^the path-set printed the line containing: (.+)$`, c.printedALineContaining)
	ctx.Then(`^setting the path succeeds$`, c.settingSucceeds)
	ctx.Then(`^setting the path is refused, saying: (.+)$`, c.settingIsRefusedSaying)
	ctx.Then(`^nothing was written$`, c.nothingWasWritten)
	ctx.Then(`^the path-set took exactly (\d+) write$`, c.tookWrites)
	ctx.Then(`^the story "([^"]*)" has exactly the metadata:$`, c.theStoryHasExactlyTheMetadata)
	ctx.Then(`^the story "([^"]*)" has the path key "([^"]*)" as "([^"]*)"$`, c.theStoryHasThePathKey)
	ctx.Then(`^the path-set printed the line: (.+)$`, c.printedTheLine)
	ctx.Then(`^the path shown has the line: (.+)$`, c.printedTheLine)
	ctx.Then(`^the path-set echoed what showing the path of "([^"]*)" prints$`, c.echoedWhatShowPrints)
}

func (c *pathSetContext) aStoryIsOpenWithNoMetadata(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return nil
}

func (c *pathSetContext) aStoryIsOpenWithTheMetadata(id string, table *godog.Table) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	fields := map[string]string{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("metadata rows need a key and a value, got %d cells", len(row.Cells))
		}
		fields[row.Cells[0].Value] = row.Cells[1].Value
	}
	return c.tracker.SetStoryMetadata(context.Background(), id, fields)
}

func (c *pathSetContext) aStoryIsInProgress(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return c.tracker.SetStatus(id, apptest.StatusInProgress)
}

func (c *pathSetContext) aStoryIsClosed(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return c.tracker.CloseStory(context.Background(), id, "done")
}

func (c *pathSetContext) set(id, from string, pairs []string) error {
	c.printed.Reset()
	c.warned.Reset()
	c.writesBefore = c.tracker.Writes()
	c.log = nil
	set := application.PathSet{Tracker: orderRecorder{WorkTracker: c.tracker, log: &c.log}, HeavyNet: c.heavy, Out: &c.printed, Err: &c.warned,
		After: c.after, Hold: c.hold}
	if c.rules != nil {
		set.Rules = c.rules
	}
	_, c.err = set.Run(context.Background(), id, from, pairs)
	c.after, c.hold = nil, ""
	return nil
}

func (c *pathSetContext) thePathIsSetFrom(id, from string) error {
	return c.set(id, from, nil)
}

func (c *pathSetContext) thePathIsSetFromWith(id, from, pairs string) error {
	return c.set(id, from, strings.Fields(pairs))
}

func (c *pathSetContext) thePathIsSetWith(id, pairs string) error {
	return c.set(id, "", strings.Fields(pairs))
}

func (c *pathSetContext) thePathIsSetWithNoKeys(id string) error {
	return c.set(id, "", nil)
}

func (c *pathSetContext) thePathIsShown(id string) error {
	c.printed.Reset()
	_, c.err = application.PathShow{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id)
	return c.err
}

func (c *pathSetContext) settingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("setting the path failed: %w", c.err)
	}
	return nil
}

func (c *pathSetContext) settingIsRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected setting the path to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *pathSetContext) nothingWasWritten() error {
	return c.tookWrites(0)
}

func (c *pathSetContext) tookWrites(want int) error {
	if got := c.tracker.Writes() - c.writesBefore; got != want {
		return fmt.Errorf("expected %d writes, the tracker took %d", want, got)
	}
	return nil
}

func (c *pathSetContext) theStoryHasExactlyTheMetadata(id string, table *godog.Table) error {
	want := map[string]string{}
	for _, row := range table.Rows {
		want[row.Cells[0].Value] = row.Cells[1].Value
	}
	got := c.tracker.Metadata(id)
	if len(got) != len(want) {
		return fmt.Errorf("expected the story %s to hold exactly %v, it holds %v", id, want, got)
	}
	for key, value := range want {
		if got[key] != value {
			return fmt.Errorf("expected the story %s to hold exactly %v, it holds %v", id, want, got)
		}
	}
	return nil
}

func (c *pathSetContext) theStoryHasThePathKey(id, key, want string) error {
	if got := c.tracker.Metadata(id)[key]; got != want {
		return fmt.Errorf("expected the story %s to have %s=%s, it has %q", id, key, want, got)
	}
	return nil
}

func (c *pathSetContext) printedTheLine(line string) error {
	for _, got := range strings.Split(c.printed.String(), "\n") {
		if got == line {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q, got:\n%s", line, c.printed.String())
}

// echoedWhatShowPrints holds the echo of the last set to the keys that `mw path
// show` prints for the story now: the same nine lines.
func (c *pathSetContext) echoedWhatShowPrints(id string) error {
	var shown bytes.Buffer
	if _, err := (application.PathShow{Tracker: c.tracker, Out: &shown}).Run(context.Background(), id); err != nil {
		return err
	}
	_, after, found := strings.Cut(c.printed.String(), ":\n")
	if !found || after != shown.String() {
		return fmt.Errorf("expected the echo to end with what showing the path prints:\n%s\ngot:\n%s", shown.String(), c.printed.String())
	}
	return nil
}

func (c *pathSetContext) theConfigNamesTheRigAsHeavy(rig string) error {
	if c.heavy == nil {
		c.heavy = map[string]bool{}
	}
	c.heavy[rig] = true
	return nil
}

func (c *pathSetContext) theSetWarnedSaying(want string) error {
	if !strings.Contains(c.warned.String(), "WARN") || !strings.Contains(c.warned.String(), want) {
		return fmt.Errorf("expected a WARN on stderr saying %q, got:\n%s", want, c.warned.String())
	}
	return nil
}

func (c *pathSetContext) theSetDidNotWarn() error {
	if c.warned.Len() != 0 {
		return fmt.Errorf("expected no warning, got:\n%s", c.warned.String())
	}
	return nil
}

func (c *pathSetContext) theRigReleasesOnPathSet(rig, expects string) error {
	if c.rules == nil {
		c.rules = apptest.NewFakeEpicRules()
	}
	c.rules.Require(rig, domain.EpicRequirements{ReleaseOnPath: true, FilesExpected: expects != ""})
	return nil
}

func (c *pathSetContext) aStoryIsFiledHeld(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return c.tracker.SetStatus(id, apptest.StatusDeferred)
}

// aBugIsFiledHeld is a held bug dated after the sweep rule began, with no
// Sweep: line in it.
func (c *pathSetContext) aBugIsFiledHeld(id string) error {
	if err := c.aStoryIsFiledHeld(id); err != nil {
		return err
	}
	if err := c.tracker.SetType(id, "bug"); err != nil {
		return err
	}
	return c.tracker.SetCreated(id, domain.SweepRuleFrom.Add(24*time.Hour))
}

func (c *pathSetContext) aStoryCarriesTheLabel(id, label string) error {
	return c.tracker.SetLabels(id, label)
}

func (c *pathSetContext) aStoryHasTheComment(id, text string) error {
	return c.tracker.CommentOnStory(context.Background(), id, text)
}

// afterIDs reads --after as the command line does: "none" is an empty list that
// is given, and a list of ids is comma-separated.
func afterIDs(after string) []string {
	if after == "none" {
		return []string{}
	}
	return strings.Split(after, ",")
}

func (c *pathSetContext) thePathIsSetFromAfter(id, from, after string) error {
	c.after = afterIDs(after)
	return c.set(id, from, nil)
}

func (c *pathSetContext) thePathIsSetFromAfterHolding(id, from, after, why string) error {
	c.after, c.hold = afterIDs(after), why
	return c.set(id, from, nil)
}

func (c *pathSetContext) thePathIsSetFromAfterWith(id, from, after, pairs string) error {
	c.after = afterIDs(after)
	return c.set(id, from, strings.Fields(pairs))
}

func (c *pathSetContext) theStoryIsOpen(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != apptest.StatusOpen {
		return fmt.Errorf("expected the story %s to be open, it is %s", id, detail.Status)
	}
	return nil
}

func (c *pathSetContext) theStoryIsHeld(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if !detail.Held() {
		return fmt.Errorf("expected the story %s to be held, it is %s", id, detail.Status)
	}
	return nil
}

func (c *pathSetContext) theStoryWaitsOn(id, blocker string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if !slices.Contains(detail.Needs, blocker) {
		return fmt.Errorf("expected the story %s to wait on %s, it waits on %v", id, blocker, detail.Needs)
	}
	return nil
}

func (c *pathSetContext) commentOpening(id, opening string) (bool, []string, error) {
	comments, err := c.tracker.StoryComments(context.Background(), id)
	if err != nil {
		return false, nil, err
	}
	var texts []string
	for _, comment := range comments {
		texts = append(texts, comment.Text)
		if strings.HasPrefix(strings.TrimSpace(comment.Text), opening) {
			return true, texts, nil
		}
	}
	return false, texts, nil
}

func (c *pathSetContext) theStoryHasACommentOpening(id, opening string) error {
	found, texts, err := c.commentOpening(id, opening)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("expected a comment on %s opening %q, it has %q", id, opening, texts)
	}
	return nil
}

func (c *pathSetContext) theStoryHasNoCommentOpening(id, opening string) error {
	found, _, err := c.commentOpening(id, opening)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("expected no comment on %s opening %q", id, opening)
	}
	return nil
}

// theWritesCameInOrder: every dependency, then the path, then the release.
func (c *pathSetContext) theWritesCameInOrder() error {
	n := len(c.log)
	if n < 3 || c.log[n-1] != "release" || c.log[n-2] != "path" {
		return fmt.Errorf("expected the dependencies, then the path, then the release, the writes were %v", c.log)
	}
	for _, write := range c.log[:n-2] {
		if !strings.HasPrefix(write, "blocker ") {
			return fmt.Errorf("expected only dependencies before the path, the writes were %v", c.log)
		}
	}
	return nil
}

func (c *pathSetContext) printedLinesStarting(n int, prefix string) error {
	var got int
	for _, line := range strings.Split(c.printed.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected %d lines starting %q, got %d in:\n%s", n, prefix, got, c.printed.String())
	}
	return nil
}

func (c *pathSetContext) printedALineContaining(text string) error {
	for _, line := range strings.Split(c.printed.String(), "\n") {
		if strings.HasPrefix(line, "held: ") && strings.Contains(line, text) {
			return nil
		}
	}
	return fmt.Errorf("expected a held: line containing %q, got:\n%s", text, c.printed.String())
}
