package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of features/tester.feature, the Tester trial (mw-it6qk5.5). They
// hang off nextContext: a Tester story is sprung by a real landing and closed
// out by the same mw next, against the same rig, origin and vault.

// testerFixture is the [tester] trial a scenario's home has, the Tester
// stories it set up itself, and what mw tester report printed.
type testerFixture struct {
	trial    application.TesterTrial
	fixtures map[string]bool
	report   application.TesterReportResult
	printed  bytes.Buffer
}

// registerTesterSteps registers the steps of features/tester.feature.
func registerTesterSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^a Tester trial on "([^"]*)" until "([^"]*)", on "([^"]*)" at "([^"]*)"$`, c.aTesterTrial)
	ctx.Given(`^the story "([^"]*)" carries the closing comment "([^"]*)"$`, c.theStoryCarriesTheClosingComment)
	ctx.Given(`^the story "([^"]*)" is labelled "([^"]*)" for the trial$`, c.theStoryIsLabelledForTheTrial)
	ctx.Given(`^the Tester story "([^"]*)" testing "([^"]*)" has been worked in its own worktree, committing nothing$`, c.aTesterStoryWorked)
	ctx.Given(`^the Tester of "([^"]*)" commented on "([^"]*)": "([^"]*)"$`, c.theTesterCommented)
	ctx.Given(`^the Tester of "([^"]*)" committed a script to its branch$`, c.theTesterCommittedAScript)
	ctx.Given(`^the trial saw these landings on "([^"]*)": (.+)$`, c.theTrialSawLandings)
	ctx.Given(`^the Tester story "([^"]*)" tested "([^"]*)" and found "([^"]*)"$`, c.aTesterStoryTested)
	ctx.Given(`^a bug story "([^"]*)" was filed naming "([^"]*)"$`, c.aBugStoryNamingAFinding)

	ctx.When(`^mw tester report runs since "([^"]*)"$`, c.mwTesterReportRuns)

	ctx.Then(`^one Tester story "([^"]*)" was filed under "([^"]*)", held and then let go$`, c.oneTesterStoryWasFiled)
	ctx.Then(`^that Tester story is labelled "([^"]*)" and pathed to "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", formula "([^"]*)"$`, c.thatTesterStoryIsPathed)
	ctx.Then(`^that Tester story's description names "([^"]*)" and quotes "([^"]*)"$`, c.thatTesterStorysDescription)
	ctx.Then(`^the close-out report names that Tester story$`, c.theReportNamesTheTesterStory)
	ctx.Given(`^the rig names "([^"]*)" as the label of an epic's last story$`, c.theRigNamesTheLastStoryLabel)
	ctx.Then(`^the story "([^"]*)" waits on that Tester story$`, c.theStoryWaitsOnTheTester)
	ctx.Then(`^no Tester story was filed$`, c.noTesterStoryWasFiled)
	ctx.Then(`^the tester report says for "([^"]*)":$`, c.theTesterReportSays)
	ctx.Then(`^the tester report gives "([^"]*)" its fuel "([^"]*)"$`, c.theTesterReportGivesFuel)
	ctx.Then(`^the check printed: (.+)$`, c.theCheckPrinted)
}

func (c *nextContext) theCheckPrinted(words string) error {
	if said := c.printed.String(); !strings.Contains(said, strings.TrimSpace(words)) {
		return fmt.Errorf("expected the check to print %q, got:\n%s", words, said)
	}
	return nil
}

// unescape turns the \n a step's quoted text writes into newlines.
func unescape(text string) string { return strings.ReplaceAll(text, `\n`, "\n") }

// testerTrial is the [tester] trial mw next is handed: none, unless the
// scenario set one.
func (c *nextContext) testerTrial() application.TesterTrial {
	if c.tester == nil {
		return application.TesterTrial{}
	}
	return c.tester.trial
}

// testers is the scenario's Tester fixture, made once.
func (c *nextContext) testers() *testerFixture {
	if c.tester == nil {
		c.tester = &testerFixture{fixtures: map[string]bool{}}
	}
	return c.tester
}

func (c *nextContext) aTesterTrial(rigs, until, model, effort string) error {
	at, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return err
	}
	c.testers().trial = application.TesterTrial{
		Rigs: strings.Split(rigs, ","), Until: at, Model: domain.Model(model), Effort: domain.Effort(effort),
	}
	return nil
}

func (c *nextContext) theStoryCarriesTheClosingComment(id, text string) error {
	return c.tracker.CommentOnStory(context.Background(), id, unescape(text))
}

func (c *nextContext) theStoryIsLabelledForTheTrial(id, label string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	return c.tracker.SetLabels(id, append(detail.Labels, label)...)
}

// addTester files a Tester story of landed under the scenario's plan, as
// springTester would, without a worktree.
func (c *nextContext) addTester(id, landed string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{
		ID: id, Title: application.TesterTitlePrefix + "The story " + landed,
		Overrides: domain.Path{Model: domain.ModelSonnet, Formula: application.TesterFormula},
	})
	if err := c.tracker.SetLabels(id, application.LabelTester); err != nil {
		return err
	}
	c.testers().fixtures[id] = true
	return c.tracker.SetDescription(id, application.TesterLandedLine+landed+" (landed on main)\n\nDrive it on a phone-sized screen.")
}

func (c *nextContext) aTesterStoryWorked(id, landed string) error {
	if err := c.addTester(id, landed); err != nil {
		return err
	}
	return c.claimAndCut(id)
}

func (c *nextContext) theTesterCommented(_, on, text string) error {
	return c.tracker.CommentOnStory(context.Background(), on, unescape(text))
}

func (c *nextContext) theTesterCommittedAScript(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, "drive.mjs"), []byte("// a Playwright script that belonged under /tmp\n"), 0o644); err != nil {
		return err
	}
	return commitIn(dir, "Add the Tester's script")
}

// testerClosed is when the report scenario's stories were closed: inside the
// trial it reads.
var testerClosed = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// closeForTheReport closes a story the way a landing left it.
func (c *nextContext) closeForTheReport(id string) error {
	if err := c.tracker.SetStatus(id, application.StatusClosed); err != nil {
		return err
	}
	return c.tracker.SetClosedAt(id, testerClosed)
}

var quotedID = regexp.MustCompile(`"([^"]+)"`)

func (c *nextContext) theTrialSawLandings(_ string, ids string) error {
	ctx := context.Background()
	files := vault.New(c.vault)
	for _, m := range quotedID.FindAllStringSubmatch(ids, -1) {
		id := m[1]
		if err := c.aStoryReadyHere(id); err != nil {
			return err
		}
		if err := c.closeForTheReport(id); err != nil {
			return err
		}
		line := application.LedgerLine{When: testerClosed, StoryID: id, Title: "The story " + id, Outcome: "landed on main"}.String()
		if err := files.AppendToLedger(ctx, nextSeat, line); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) aTesterStoryTested(id, landed, findings string) error {
	if err := c.addTester(id, landed); err != nil {
		return err
	}
	if err := c.closeForTheReport(id); err != nil {
		return err
	}
	if err := c.theSessionSucceeded(id); err != nil {
		return err
	}
	return c.tracker.CommentOnStory(context.Background(), landed, unescape(findings))
}

func (c *nextContext) aBugStoryNamingAFinding(id, description string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: domain.BugTitlePrefix + " " + description})
	return c.tracker.SetDescription(id, description)
}

func (c *nextContext) mwTesterReportRuns(since string) error {
	at, err := time.Parse("2006-01-02", since)
	if err != nil {
		return err
	}
	fixture := c.testers()
	fixture.report, err = application.TesterReport{
		Tracker: c.tracker,
		Files:   vault.New(c.vault),
		Trial:   fixture.trial,
		Seat:    nextSeat,
		Since:   at,
		Out:     &fixture.printed,
	}.Run(context.Background())
	return err
}

// filedTesters is every Tester story the scenario did not set up itself.
func (c *nextContext) filedTesters() ([]application.StoryDetail, error) {
	var filed []application.StoryDetail
	for _, id := range c.tracker.Stories() {
		if c.tester != nil && c.tester.fixtures[id] {
			continue
		}
		detail, err := c.tracker.ShowStory(context.Background(), id)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(detail.Story.Title, application.TesterTitlePrefix) || application.IsTesterStory(detail) {
			filed = append(filed, detail)
		}
	}
	return filed, nil
}

// theTester is the one Tester story the scenario's landing filed.
func (c *nextContext) theTester() (application.StoryDetail, error) {
	filed, err := c.filedTesters()
	if err != nil {
		return application.StoryDetail{}, err
	}
	if len(filed) != 1 {
		return application.StoryDetail{}, fmt.Errorf("expected one Tester story filed, got %d: %+v\n%s", len(filed), filed, c.printed.String())
	}
	return filed[0], nil
}

func (c *nextContext) oneTesterStoryWasFiled(title, epic string) error {
	tester, err := c.theTester()
	if err != nil {
		return err
	}
	if tester.Story.Title != title || tester.EpicID != epic {
		return fmt.Errorf("expected %q under %s, got %q under %s", title, epic, tester.Story.Title, tester.EpicID)
	}
	trail := c.tracker.Trail(tester.Story.ID)
	if len(trail) < 2 || trail[0] != "held" || trail[1] != "open" {
		return fmt.Errorf("expected %s to be filed held and then let go, its trail is %q", tester.Story.ID, trail)
	}
	return nil
}

func (c *nextContext) thatTesterStoryIsPathed(label, rigName, branch, host, model, effort, formula string) error {
	tester, err := c.theTester()
	if err != nil {
		return err
	}
	if len(tester.Labels) != 1 || tester.Labels[0] != label {
		return fmt.Errorf("expected the Tester story labelled only %q, got %q", label, tester.Labels)
	}
	path, err := tester.Path()
	if err != nil {
		return err
	}
	want := domain.Path{Rig: rigName, Branch: branch, Harness: domain.HarnessClaude, Model: domain.Model(model),
		Effort: domain.Effort(effort), Formula: formula, Host: host}
	if path != want {
		return fmt.Errorf("expected the Tester story pathed %+v, got %+v", want, path)
	}
	return nil
}

func (c *nextContext) thatTesterStorysDescription(landed, quote string) error {
	tester, err := c.theTester()
	if err != nil {
		return err
	}
	if got := application.TesterLanded(tester); got != landed {
		return fmt.Errorf("expected the description to name %s as the landed story, it names %q:\n%s", landed, got, tester.Description)
	}
	if !strings.Contains(tester.Description, quote) {
		return fmt.Errorf("expected the description to quote %q, got:\n%s", quote, tester.Description)
	}
	return nil
}

func (c *nextContext) theReportNamesTheTesterStory() error {
	tester, err := c.theTester()
	if err != nil {
		return err
	}
	if c.report.TesterFiled != tester.Story.ID || !strings.Contains(c.printed.String(), "tester  "+tester.Story.ID) {
		return fmt.Errorf("expected the report to name %s, got %q:\n%s", tester.Story.ID, c.report.TesterFiled, c.printed.String())
	}
	return nil
}

func (c *nextContext) noTesterStoryWasFiled() error {
	filed, err := c.filedTesters()
	if err != nil {
		return err
	}
	if len(filed) > 0 || c.report.TesterFiled != "" {
		return fmt.Errorf("expected no Tester story filed, got %+v (report: %q)", filed, c.report.TesterFiled)
	}
	return nil
}

func (c *nextContext) theTesterReportSays(rigName string, table *godog.Table) error {
	printed := c.testers().printed.String()
	_, after, found := strings.Cut(printed, "\n"+rigName+"\n")
	if !found {
		return fmt.Errorf("expected the report to have a section for %s, got:\n%s", rigName, printed)
	}
	for _, row := range table.Rows {
		label, want := row.Cells[0].Value, row.Cells[1].Value
		said := false
		for _, line := range strings.Split(after, "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), label); ok && strings.HasPrefix(strings.TrimSpace(rest), want) {
				said = true
				break
			}
		}
		if !said {
			return fmt.Errorf("expected the %s section to say %q %q, got:\n%s", rigName, label, want, printed)
		}
	}
	return nil
}

func (c *nextContext) theTesterReportGivesFuel(id, fuel string) error {
	printed := c.testers().printed.String()
	for _, line := range strings.Split(printed, "\n") {
		if strings.Contains(line, id+" ") && strings.Contains(line, fuel) {
			return nil
		}
	}
	return fmt.Errorf("expected a line giving %s its fuel %q, got:\n%s", id, fuel, printed)
}

// theRigNamesTheLastStoryLabel writes the rig's file in the vault with the label
// its epics' last story carries (epic_last_story_labels).
func (c *nextContext) theRigNamesTheLastStoryLabel(label string) error {
	dir := filepath.Join(c.vault, application.RigsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("epic_last_story_labels = [%q]\n", label)
	return os.WriteFile(filepath.Join(dir, c.rigKey()+vault.RigFileExt), []byte(line), 0o644)
}

func (c *nextContext) theStoryWaitsOnTheTester(id string) error {
	tester, err := c.theTester()
	if err != nil {
		return err
	}
	epic, err := c.tracker.ShowEpic(context.Background(), c.lastEpic)
	if err != nil {
		return err
	}
	for _, s := range epic.Stories {
		if s.Story.ID == id {
			if !slices.Contains(s.Needs, tester.Story.ID) {
				return fmt.Errorf("expected %s to wait on %s, it waits on %q", id, tester.Story.ID, s.Needs)
			}
			return nil
		}
	}
	return fmt.Errorf("no story %s in %s", id, c.lastEpic)
}
