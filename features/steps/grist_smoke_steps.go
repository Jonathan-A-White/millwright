package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// smokeSent is one grist the fake backend was sent.
type smokeSent struct {
	app, kind string
	version   string
	photos    []string
}

// smokeSender is the backend and the mill behind it: it answers each grist
// with the next scripted answer, and remembers what it was sent.
type smokeSender struct {
	script []smokeAnswer
	sent   []smokeSent
}

type smokeAnswer struct {
	status, reason string
	answer         string
	silent         bool
	unreachable    bool
}

func (s *smokeSender) Run(_ context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	sent := smokeSent{app: req.App, kind: req.Kind, version: strings.TrimSpace(req.Version)}
	raw, err := os.ReadFile(req.RequestFile)
	if err != nil {
		return application.GristSendReport{}, err
	}
	// As the real send does: the version asked for, else the request's own schemaVersion.
	if sent.version == "" {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		_ = json.Unmarshal(fields["schemaVersion"], &sent.version)
	}
	if strings.TrimSpace(sent.version) == "" {
		return application.GristSendReport{}, fmt.Errorf("mw grist send: the request has no schemaVersion to say which version of the app's schema it is: give --schema-version")
	}
	for _, photo := range req.Photos {
		if _, err := os.Stat(photo); err != nil {
			return application.GristSendReport{}, err
		}
		sent.photos = append(sent.photos, filepath.Base(photo))
	}
	s.sent = append(s.sent, sent)
	if len(s.script) == 0 {
		return application.GristSendReport{}, fmt.Errorf("the scenario scripted no answer for the grist %d", len(s.sent))
	}
	next := s.script[0]
	s.script = s.script[1:]
	report := application.GristSendReport{Txid: "direct:smoke"}
	if next.unreachable {
		return application.GristSendReport{}, fmt.Errorf("reaching the postern backend at http://postern.test: %w", application.ErrPosternUnreachable)
	}
	if next.silent {
		return report, &application.GristUnanswered{Txid: report.Txid, Wait: req.Wait}
	}
	answer := &application.GristAnswer{Re: report.Txid, Status: next.status, Reason: next.reason}
	if next.status == application.GristAnswered {
		answer.Answer = json.RawMessage(next.answer)
	}
	report.Answer = answer
	if next.status != application.GristAnswered {
		return report, &application.GristUnanswered{Txid: report.Txid, Answer: answer, Wait: req.Wait}
	}
	return report, nil
}

// smokeTouches answers a landing's pathspecs from the files it changed.
type smokeTouches struct{ changed []string }

func (t *smokeTouches) Changed(_ context.Context, _, _, _, pathspec string) (bool, error) {
	for _, file := range t.changed {
		if file == pathspec || strings.HasPrefix(file, pathspec+"/") {
			return true, nil
		}
		if ok, _ := path.Match(pathspec, file); ok {
			return true, nil
		}
	}
	return false, nil
}

// smokeContext is the smoke of an app's grist: its rig's grinds, a backend with
// a scripted mill, the tracker's notes and the home's event log.
type smokeContext struct {
	grinds  *apptest.FakeGrinds
	sender  *smokeSender
	notes   *apptest.FakeTracker
	events  *apptest.FakeEventLog
	apps    map[string]string
	rigs    map[string]string
	client  map[string][]string
	report  application.GristSmokeReport
	err     error
	smoked  []string
	touches *smokeTouches
}

func (c *smokeContext) book() application.GristSmokeBook {
	return application.GristSmokeBook{Notes: c.notes, Events: c.events, Host: "laptop", Now: func() time.Time { return gristNow }}
}

func (c *smokeContext) smoke() application.GristSmoke {
	return application.GristSmoke{Grinds: c.grinds, Send: c.sender, Apps: c.apps, Wait: application.GristSmokeWait}
}

// InitializeGristSmokeScenario registers the steps of features/grist_smoke.feature.
func InitializeGristSmokeScenario(ctx *godog.ScenarioContext) {
	c := &smokeContext{}
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = smokeContext{
			grinds: apptest.NewFakeGrinds(), sender: &smokeSender{}, notes: apptest.NewFakeTracker(),
			events: &apptest.FakeEventLog{}, apps: map[string]string{},
			rigs: map[string]string{"millwright": "/rigs/millwright"}, client: map[string][]string{},
			touches: &smokeTouches{},
		}
		return ctx, nil
	})

	ctx.Given(`^the app "([^"]*)" has the grind "([^"]*)" whose answer must carry a price and a confidence$`, c.theAppHasTheGrind)
	ctx.Given(`^the app "([^"]*)" has the example "([^"]*)" containing:$`, c.theAppHasTheExample)
	ctx.Given(`^the example "([^"]*)" of "([^"]*)" has the photo "([^"]*)"$`, c.theExampleHasThePhoto)
	ctx.Given(`^the app "([^"]*)" has no grinds$`, c.theAppHasNoGrinds)
	ctx.Given(`^the mill answers the next grist with:$`, c.theMillAnswers)
	ctx.Given(`^the mill refuses the next grist because "([^"]*)"$`, c.theMillRefuses)
	ctx.Given(`^the mill does not answer the next grist$`, c.theMillIsSilent)
	ctx.Given(`^the postern backend cannot be reached for the next grist$`, c.theBackendIsUnreachable)
	ctx.Given(`^a rig "([^"]*)" checked out where the app "([^"]*)" is$`, c.aRigWhereTheAppIs)
	ctx.Given(`^the rig "([^"]*)" names the grist client path "([^"]*)"$`, c.theRigNamesAClientPath)
	ctx.Given(`^a landing in "([^"]*)" changes "([^"]*)"$`, c.aLandingChanges)

	ctx.When(`^mw grist smoke is run for "([^"]*)"$`, c.smokeApp)
	ctx.When(`^mw grist smoke is run for "([^"]*)" kind "([^"]*)"$`, c.smokeKind)
	ctx.When(`^a landing in "([^"]*)" changes "([^"]*)"$`, c.aLandingChanges)
	ctx.When(`^the hold of the smoke of "([^"]*)" is lifted$`, c.liftTheHold)

	ctx.Then(`^the smoke passed, saying "([^"]*)"$`, c.passed)
	ctx.Then(`^the smoke was not run, saying "([^"]*)"$`, c.notRun)
	ctx.Then(`^the smoke failed, saying "([^"]*)"$`, c.failed)
	ctx.Then(`^the smoke sent (\d+) grist as "([^"]*)" with the photo "([^"]*)"$`, c.sentWithPhoto)
	ctx.Then(`^the smoke sent (\d+) grist as "([^"]*)" with no photo$`, c.sentWithNoPhoto)
	ctx.Then(`^the smoke sent (\d+) grist$`, c.sentTotal)
	ctx.Then(`^the smoke sent the grist of "([^"]*)" with the version "([^"]*)"$`, c.sentWithVersion)
	ctx.Then(`^no alarm was posted$`, c.noAlarm)
	ctx.Then(`^an alarm event was posted saying "([^"]*)"$`, c.alarmPosted)
	ctx.Then(`^the landing smoked "([^"]*)"$`, c.landingSmoked)
	ctx.Then(`^the landing smoked nothing$`, c.landingSmokedNothing)
	ctx.Then(`^mw status shows "([^"]*)"$`, c.statusShows)
	ctx.Then(`^the open stories of "([^"]*)" are held, saying "([^"]*)"$`, c.held)
	ctx.Then(`^the open stories of "([^"]*)" are not held$`, c.notHeld)
}

func (c *smokeContext) checkout(app string) string {
	dir := "/rigs/" + app
	c.apps[app] = dir
	return dir
}

func (c *smokeContext) put(app, file, content string) {
	c.grinds.SetFile(c.checkout(app), "c1", file, []byte(content))
}

func (c *smokeContext) theAppHasTheGrind(app, kind string) error {
	c.put(app, "grinds/"+kind+".json", fmt.Sprintf(`{"grind": 1, "app": %q, "kind": %q, "versions": ["1"], "model": "sonnet", "effort": "low", `+
		`"instructions": "grinds/%s.md", "answerSchema": "schemas/%s.json"}`, app, kind, kind, kind))
	c.put(app, "grinds/"+kind+".md", "Read the price.")
	c.put(app, "schemas/"+kind+".json", `{"type": "object", "required": ["price", "confidence"], "properties": {`+
		`"price": {"type": ["number", "null"]}, "confidence": {"enum": ["low", "medium", "high"]}}}`)
	return nil
}

func (c *smokeContext) theAppHasTheExample(app, name string, doc *godog.DocString) error {
	c.put(app, "grinds/examples/"+name+".json", doc.Content)
	return nil
}

func (c *smokeContext) theExampleHasThePhoto(name, app, photo string) error {
	c.put(app, path.Join("grinds/examples", path.Dir(name), photo), "not really a jpeg")
	return nil
}

func (c *smokeContext) theAppHasNoGrinds(app string) error {
	c.put(app, "README.md", "an app that asks the mill for nothing")
	return nil
}

func (c *smokeContext) theMillAnswers(doc *godog.DocString) error {
	c.sender.script = append(c.sender.script, smokeAnswer{status: application.GristAnswered, answer: doc.Content})
	return nil
}

func (c *smokeContext) theMillRefuses(reason string) error {
	c.sender.script = append(c.sender.script, smokeAnswer{status: application.GristRefused, reason: reason})
	return nil
}

func (c *smokeContext) theMillIsSilent() error {
	c.sender.script = append(c.sender.script, smokeAnswer{silent: true})
	return nil
}

func (c *smokeContext) theBackendIsUnreachable() error {
	c.sender.script = append(c.sender.script, smokeAnswer{unreachable: true})
	return nil
}

func (c *smokeContext) aRigWhereTheAppIs(rig, app string) error {
	c.rigs[rig] = c.checkout(app)
	return nil
}

func (c *smokeContext) theRigNamesAClientPath(rig, spec string) error {
	c.client[rig] = append(c.client[rig], spec)
	return nil
}

func (c *smokeContext) smokeApp(app string) error { return c.smokeKind(app, "") }

// smokeKind is mw grist smoke: the smoke, and its record in the book, as the
// command makes them.
func (c *smokeContext) smokeKind(app, kind string) error {
	c.report, c.err = c.smoke().Run(context.Background(), app, kind)
	if c.err != nil {
		return nil
	}
	return c.book().Record(context.Background(), "", c.report)
}

func (c *smokeContext) aLandingChanges(rig, file string) error {
	c.touches.changed = append(c.touches.changed, file)
	after := application.GristSmokeAfter{
		Touches: c.touches, Smoke: c.smoke(), Book: c.book(), Apps: c.apps, Rigs: c.rigs, ClientPaths: c.client,
	}
	apps, notes := after.Wants(context.Background(), rig, c.rigs[rig], "origin/main", "mw/story")
	if len(notes) > 0 {
		return fmt.Errorf("the landing could not be read: %v", notes)
	}
	c.smoked = nil
	if len(apps) == 0 {
		return nil
	}
	lines, _ := after.Run(context.Background(), rig, apps)
	_ = lines
	c.smoked = apps
	return nil
}

func (c *smokeContext) passed(said string) error {
	if c.err != nil {
		return fmt.Errorf("the smoke could not be made: %v", c.err)
	}
	if c.report.Failed() {
		return fmt.Errorf("the smoke failed: %s", c.report.Line())
	}
	if !strings.Contains(c.report.Line(), said) {
		return fmt.Errorf("the smoke said %q, not %q", c.report.Line(), said)
	}
	return nil
}

func (c *smokeContext) notRun(said string) error {
	if c.err != nil {
		return fmt.Errorf("the smoke could not be made: %v", c.err)
	}
	if c.report.Failed() {
		return fmt.Errorf("the smoke failed: %s", c.report.Line())
	}
	if !strings.Contains(c.report.Line(), said) {
		return fmt.Errorf("the smoke said %q, not %q", c.report.Line(), said)
	}
	return nil
}

func (c *smokeContext) failed(said string) error {
	if c.err != nil {
		return fmt.Errorf("the smoke could not be made: %v", c.err)
	}
	if !c.report.Failed() {
		return fmt.Errorf("the smoke did not fail: %s", c.report.Line())
	}
	if !strings.Contains(c.report.Line(), said) {
		return fmt.Errorf("the smoke said %q, not %q", c.report.Line(), said)
	}
	return nil
}

func (c *smokeContext) sentAs(n int, target string, photos []string) error {
	app, kind, _ := strings.Cut(target, "/")
	count := 0
	for _, sent := range c.sender.sent {
		if sent.app == app && sent.kind == kind {
			count++
			if strings.Join(sent.photos, ",") != strings.Join(photos, ",") {
				return fmt.Errorf("%s was sent with the photos %v, not %v", target, sent.photos, photos)
			}
		}
	}
	if count != n {
		return fmt.Errorf("%d grist were sent as %s, not %d", count, target, n)
	}
	return nil
}

func (c *smokeContext) sentWithPhoto(n int, target, photo string) error {
	return c.sentAs(n, target, []string{photo})
}

func (c *smokeContext) sentWithNoPhoto(n int, target string) error {
	return c.sentAs(n, target, nil)
}

func (c *smokeContext) sentWithVersion(target, version string) error {
	app, kind, _ := strings.Cut(target, "/")
	for _, sent := range c.sender.sent {
		if sent.app == app && sent.kind == kind {
			if sent.version != version {
				return fmt.Errorf("%s was sent with the version %q, not %q", target, sent.version, version)
			}
			return nil
		}
	}
	return fmt.Errorf("no grist was sent as %s", target)
}

func (c *smokeContext) sentTotal(n int) error {
	if len(c.sender.sent) != n {
		return fmt.Errorf("%d grist were sent, not %d", len(c.sender.sent), n)
	}
	return nil
}

func (c *smokeContext) noAlarm() error {
	if len(c.events.All()) != 0 {
		return fmt.Errorf("an alarm was posted: %v", c.events.All())
	}
	return nil
}

func (c *smokeContext) alarmPosted(said string) error {
	for _, ev := range c.events.All() {
		if strings.Contains(ev.Detail, said) {
			return nil
		}
	}
	return fmt.Errorf("no alarm event said %q: %v", said, c.events.All())
}

func (c *smokeContext) landingSmoked(want string) error {
	apps := append([]string(nil), c.smoked...)
	sort.Strings(apps)
	if got := strings.Join(apps, ", "); got != want {
		return fmt.Errorf("the landing smoked %q, not %q", got, want)
	}
	return nil
}

func (c *smokeContext) landingSmokedNothing() error {
	if len(c.smoked) != 0 || len(c.sender.sent) != 0 {
		return fmt.Errorf("the landing smoked %v and sent %d grist", c.smoked, len(c.sender.sent))
	}
	return nil
}

func (c *smokeContext) statusShows(said string) error {
	records, err := c.book().Records(context.Background())
	if err != nil {
		return err
	}
	shown := application.StatusReport{GristSmoke: records}.String()
	if !strings.Contains(shown, said) {
		return fmt.Errorf("mw status did not show %q:\n%s", said, shown)
	}
	return nil
}

func (c *smokeContext) held(rig, said string) error {
	why, err := c.book().HeldBy(context.Background(), rig)
	if err != nil {
		return err
	}
	if !strings.Contains(why, said) {
		return fmt.Errorf("the stories of %s are held for %q, not %q", rig, why, said)
	}
	return nil
}

func (c *smokeContext) notHeld(rig string) error {
	why, err := c.book().HeldBy(context.Background(), rig)
	if err != nil {
		return err
	}
	if why != "" {
		return fmt.Errorf("the stories of %s are held: %s", rig, why)
	}
	return nil
}

func (c *smokeContext) liftTheHold(app string) error {
	lifted, err := c.book().Lift(context.Background(), app)
	if err != nil {
		return err
	}
	if !lifted {
		return fmt.Errorf("there was no hold on %s to lift", app)
	}
	return nil
}
