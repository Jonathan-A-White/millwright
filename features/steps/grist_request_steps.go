package steps

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// requestContext is what features/grist_request.feature adds to a mill: the
// outcome of the last mw grist request, and how many records the backend held
// before it ran.
type requestContext struct {
	c        *gristContext
	err      error
	before   int
	store    *grist.Requests
	landings *apptest.FakeLandings
	rules    *apptest.FakeEpicRules
}

// requests is the mill's record of the requests it linked, under its state
// directory, made the first time a step needs it.
func (r *requestContext) requests() *grist.Requests {
	if r.store == nil {
		r.store = grist.NewRequests(filepath.Join(r.c.home, "state"))
	}
	return r.store
}

func (r *requestContext) landingsFake() *apptest.FakeLandings {
	if r.landings == nil {
		r.landings = apptest.NewFakeLandings()
	}
	return r.landings
}

func (r *requestContext) rulesFake() *apptest.FakeEpicRules {
	if r.rules == nil {
		r.rules = apptest.NewFakeEpicRules()
	}
	return r.rules
}

func registerGristRequest(ctx *godog.ScenarioContext, c *gristContext) {
	r := &requestContext{c: c}

	// After the reset of the grist feature's own Before, which clears c.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*r = requestContext{c: c}
		return ctx, nil
	})

	ctx.Given(`^the bead "([^"]*)" is filed$`, r.beadIsFiled)
	ctx.When(`^mw grist request links "([^"]*)" to that grist$`, r.linksThatGrist)
	ctx.When(`^mw grist request links "([^"]*)" to the txid "([^"]*)"$`, r.links)
	ctx.Then(`^the forward mail's last line is the command "([^"]*)" and the grist's txid$`, r.mailEndsWithCommand)
	ctx.Then(`^the run of that grist keeps the phone's key as its sender$`, r.runKeepsSender)
	ctx.Then(`^mw grist request left without an error$`, r.leftWell)
	ctx.Then(`^mw grist request was refused saying "([^"]*)"$`, r.refusedSaying)
	ctx.Then(`^the bead "([^"]*)" has the metadata request as that grist's txid and request_app "([^"]*)"$`, r.beadIsLinked)
	ctx.Then(`^the bead "([^"]*)" has no request metadata$`, r.beadIsNotLinked)
	ctx.Then(`^the mill sent one request status after its answer, a grist record from the mill key sealed to the phone$`, r.oneStatusSealedToThePhone)
	ctx.Then(`^that status matches the protocol's "([^"]*)" vector for that grist and "([^"]*)" at the mill's clock$`, r.statusMatchesVector)
	ctx.Then(`^the mill sent no request status$`, r.noStatus)

	// The state steps are given before a pass and happen between two of them.
	for _, step := range []func(string, any){func(re string, f any) { ctx.Given(re, f) }, func(re string, f any) { ctx.When(re, f) }} {
		step(`^the bead "([^"]*)" goes in progress$`, r.goesInProgress)
		step(`^the bead "([^"]*)" is closed$`, r.isClosed)
		step(`^the bead "([^"]*)" is closed after landing in version "([^"]*)" at the commit "([^"]*)"$`, r.isClosedAfterLanding)
		step(`^the rig "([^"]*)" names the public url "([^"]*)"$`, r.rigNamesPublicURL)
		step(`^the status pass runs$`, r.statusPassRuns)
	}
	ctx.When(`^mw grist request declines "([^"]*)" for the reason "([^"]*)"$`, r.declines)
	ctx.When(`^mw grist request declines "([^"]*)" for the reason "([^"]*)" with the note "([^"]*)"$`, r.declinesWithNote)
	ctx.When(`^mw grist request adopts "([^"]*)" as the setting "([^"]*)"$`, r.adopts)
	ctx.When(`^mw dispatch runs on "([^"]*)" with a cap of (\d+) and the request status pass$`, r.dispatchRuns)
	ctx.Then(`^the bead "([^"]*)" is not closed$`, r.isNotClosed)
	ctx.Then(`^the mill keeps a state file for "([^"]*)" under its requests directory$`, r.keepsStateFile)
	ctx.Then(`^the status pass sent no status$`, r.noStatus)
	ctx.Then(`^the status pass sent one "([^"]*)" status for that grist and "([^"]*)"$`, r.sentOne)
	ctx.Then(`^the status pass sent one "([^"]*)" status for that grist and "([^"]*)" carrying:$`, r.sentOneCarrying)
}

func (r *requestContext) goesInProgress(bead string) error {
	return r.c.tracker.SetStatus(bead, application.StatusInProgress)
}

func (r *requestContext) isClosed(bead string) error {
	return r.c.tracker.SetStatus(bead, application.StatusClosed)
}

func (r *requestContext) isNotClosed(bead string) error {
	detail, err := r.c.tracker.ShowStory(context.Background(), bead)
	if err != nil {
		return err
	}
	if detail.Closed() {
		return fmt.Errorf("expected %s not to be closed, it is", bead)
	}
	return nil
}

func (r *requestContext) isClosedAfterLanding(bead, version, commit string) error {
	r.landingsFake().Land(bead, version, commit)
	return r.isClosed(bead)
}

func (r *requestContext) rigNamesPublicURL(rig, url string) error {
	r.rulesFake().Require(rig, domain.EpicRequirements{PublicURL: url})
	return nil
}

// statusPassRuns runs mw grist status; the records the backend takes after
// this are the statuses it sent.
func (r *requestContext) statusPassRuns() error {
	r.before = len(r.c.backend.Delivered())
	r.c.out.Reset()
	_, r.err = r.pass().Run(context.Background())
	if r.err != nil {
		return fmt.Errorf("expected the status pass to go well, it said: %v", r.err)
	}
	return nil
}

// pass is the status pass, as the command line makes it.
func (r *requestContext) pass() application.GristStatus {
	return application.GristStatus{
		Postern: r.c.backend, Cipher: &apptest.FakeCipher{From: r.c.millKey}, Keys: r.c.keys,
		Requests: r.requests(), Tracker: r.c.tracker, Rules: r.rulesFake(), Landings: r.landingsFake(),
		Rigs: map[string]string{"cairn": "/rigs/cairn"}, Now: r.c.audio.now, Out: &r.c.out,
	}
}

// dispatchRuns is a dispatch tick that is given the status pass, as cmd/mw
// gives it to a tick where the mill is configured.
func (r *requestContext) dispatchRuns(host string, cap int) error {
	r.before = len(r.c.backend.Delivered())
	r.c.statuses = r.pass()
	return r.c.mwDispatchRuns(host, cap)
}

func (r *requestContext) declines(bead, reason string) error {
	return r.declinesWithNote(bead, reason, "")
}

func (r *requestContext) declinesWithNote(bead, reason, note string) error {
	r.err = application.GristRequestDecline{Requests: r.requests(), Out: &r.c.out}.Run(context.Background(), bead, reason, note)
	return nil
}

func (r *requestContext) adopts(bead, setting string) error {
	r.err = application.GristRequestAdopt{Requests: r.requests(), Out: &r.c.out}.Run(context.Background(), bead, setting)
	return nil
}

func (r *requestContext) keepsStateFile(bead string) error {
	path := filepath.Join(r.c.home, "state", "requests", bead+".json")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("expected a state file for %s: %w", bead, err)
	}
	return nil
}

func (r *requestContext) sentOne(status, bead string) error {
	return r.sentOneCarrying(status, bead, nil)
}

// sentOneCarrying checks the one status the pass sent: its kind, request,
// bead and status, no re, and exactly the fields the table names besides.
func (r *requestContext) sentOneCarrying(status, bead string, table *godog.Table) error {
	_, _, _, got, err := r.theStatus()
	if err != nil {
		return err
	}
	want := map[string]any{
		"kind": "request-status", "request": r.c.grist.Txid, "bead": bead, "status": status,
		"ts": float64(gristNow.UnixMilli()),
	}
	if table != nil {
		for _, row := range table.Rows[1:] {
			want[row.Cells[0].Value] = row.Cells[1].Value
		}
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("expected the status %v, got %v", want, got)
	}
	return nil
}

func (r *requestContext) beadIsFiled(bead string) error {
	var path domain.Path
	for field, value := range map[string]string{
		"rig": "cairn", "branch": "main", "harness": "claude", "model": "sonnet", "effort": "high", "host": "laptop",
	} {
		if err := path.Set(field, value); err != nil {
			return err
		}
	}
	r.c.tracker.AddEpic("mw-x", path)
	r.c.tracker.AddStory("mw-x", domain.Story{ID: bead, Title: "A request from the app"})
	return nil
}

func (r *requestContext) linksThatGrist(bead string) error {
	return r.links(bead, r.c.grist.Txid)
}

func (r *requestContext) links(bead, txid string) error {
	r.before = len(r.c.backend.Delivered())
	r.c.out.Reset()
	r.err = application.GristRequest{
		Postern: r.c.backend, Cipher: &apptest.FakeCipher{From: r.c.millKey}, Keys: r.c.keys,
		Runs: r.c.audio.runs(), Tracker: r.c.tracker, Requests: r.requests(), Now: r.c.audio.now, Out: &r.c.out,
	}.Run(context.Background(), bead, txid)
	return nil
}

func (r *requestContext) mailEndsWithCommand(command string) error {
	m, err := r.c.forward.sent()
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(m.Body, "\n"), "\n")
	want := command + " " + r.c.grist.Txid
	if last := lines[len(lines)-1]; last != want {
		return fmt.Errorf("expected the mail to end with %q, its last line is %q:\n%s", want, last, m.Body)
	}
	if want != application.GristRequestCommand("<bead>", r.c.grist.Txid) {
		return fmt.Errorf("the command %q is not the one mw grist request is run by", want)
	}
	return nil
}

func (r *requestContext) runKeepsSender() error {
	raw, err := os.ReadFile(filepath.Join(r.c.home, "state", "runs", application.GristRunName(r.c.grist.Txid), "sender.json"))
	if err != nil {
		return err
	}
	var kept struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &kept); err != nil {
		return err
	}
	if kept.Key != r.c.phoneKey {
		return fmt.Errorf("expected the run to keep the phone's key %s, it keeps %q", r.c.phoneKey, kept.Key)
	}
	return nil
}

func (r *requestContext) leftWell() error {
	if r.err != nil {
		return fmt.Errorf("expected mw grist request to succeed, it said: %v", r.err)
	}
	return nil
}

func (r *requestContext) refusedSaying(text string) error {
	if r.err == nil {
		return fmt.Errorf("expected mw grist request to be refused, it was not")
	}
	if !strings.Contains(r.err.Error(), text) {
		return fmt.Errorf("expected the refusal to say %q, it said: %v", text, r.err)
	}
	return nil
}

func (r *requestContext) beadIsLinked(bead, app string) error {
	got := r.c.tracker.Metadata(bead)
	if got[application.RequestField] != r.c.grist.Txid || got[application.RequestAppField] != app {
		return fmt.Errorf("expected %s to carry request %q and request_app %q, got %v", bead, r.c.grist.Txid, app, got)
	}
	return nil
}

func (r *requestContext) beadIsNotLinked(bead string) error {
	got := r.c.tracker.Metadata(bead)
	if _, ok := got[application.RequestField]; ok {
		return fmt.Errorf("expected %s to carry no request, got %v", bead, got)
	}
	if _, ok := got[application.RequestAppField]; ok {
		return fmt.Errorf("expected %s to carry no request_app, got %v", bead, got)
	}
	return nil
}

// statuses are the records the backend took after the request ran.
func (r *requestContext) statuses() [][]byte {
	return r.c.backend.Delivered()[r.before:]
}

func (r *requestContext) noStatus() error {
	if got := r.statuses(); len(got) != 0 {
		return fmt.Errorf("expected no request status sent, %d were", len(got))
	}
	return nil
}

// theStatus is the one status sent: its payload, who sealed it to whom, and
// its plaintext.
func (r *requestContext) theStatus() (application.PosternPayload, string, string, map[string]any, error) {
	var payload application.PosternPayload
	sent := r.statuses()
	if len(sent) != 1 {
		return payload, "", "", nil, fmt.Errorf("expected one request status sent, got %d (mw grist request said: %v)", len(sent), r.err)
	}
	if err := json.Unmarshal(sent[0], &payload); err != nil {
		return payload, "", "", nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(payload.Ct)
	if err != nil {
		return payload, "", "", nil, err
	}
	parts := strings.SplitN(string(plain), "\x00", 3)
	if len(parts) != 3 {
		return payload, "", "", nil, fmt.Errorf("the status's ct is not the fake cipher's")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(parts[2]), &body); err != nil {
		return payload, "", "", nil, fmt.Errorf("the status's plaintext is not JSON: %w", err)
	}
	return payload, parts[0], parts[1], body, nil
}

func (r *requestContext) oneStatusSealedToThePhone() error {
	if got := len(r.c.backend.Delivered()); got != 2 {
		return fmt.Errorf("expected the answer and one status delivered, got %d records", got)
	}
	payload, sealedTo, sealedBy, _, err := r.theStatus()
	if err != nil {
		return err
	}
	if payload.V != 1 || payload.Kind != "msg" || payload.Class != application.GristClass {
		return fmt.Errorf("expected a version 1 msg of class grist, got %+v", payload)
	}
	if payload.To != r.c.phoneKey || payload.From != r.c.millKey {
		return fmt.Errorf("expected the status to the phone from the mill key, got to %s from %s", payload.To, payload.From)
	}
	if sealedTo != r.c.phoneKey || sealedBy != r.c.millKey {
		return fmt.Errorf("expected the status sealed to the phone by the mill key, got to %s by %s", sealedTo, sealedBy)
	}
	return nil
}

func (r *requestContext) statusMatchesVector(name, bead string) error {
	_, _, _, got, err := r.theStatus()
	if err != nil {
		return err
	}
	var want map[string]any
	if err := json.Unmarshal(r.c.vectors.RequestStatus[name], &want); err != nil {
		return fmt.Errorf("the vectors have no requestStatus %q: %w", name, err)
	}
	// The vector is for a request of its own, and a moment of its own.
	want["request"] = r.c.grist.Txid
	want["ts"] = float64(gristNow.UnixMilli())
	if want["bead"] != bead {
		return fmt.Errorf("the vector %q is for the bead %v, not %s", name, want["bead"], bead)
	}
	if _, hasRe := got["re"]; hasRe {
		return fmt.Errorf("a request status must have no re, got %v", got)
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("expected the status to match the vector %v, got %v", want, got)
	}
	return nil
}
