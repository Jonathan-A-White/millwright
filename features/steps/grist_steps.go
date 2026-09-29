package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// gristVectorsPath is postern's docs/fixtures/grist-vectors.json, copied: the
// keys and shapes the backend's tests and the mill's share.
const gristVectorsPath = "../application/testdata/grist-vectors.json"

// gristNow is when every grist scenario runs.
var gristNow = time.Date(2026, time.September, 29, 10, 0, 0, 0, time.UTC)

// The grind a scenario's app holds: Cairn's sweep, as its rig writes it.
const (
	gristGrindFile = `{
  "grind": 1, "app": "cairn", "kind": "sweep", "versions": ["1.1"],
  "model": "sonnet", "effort": "low",
  "instructions": "grinds/sweep.md",
  "answerSchema": "contexts/inventory/schemas/sweep-result.schema.json",
  "attachments": { "min": 1, "max": 4, "mime": ["image/jpeg", "image/png", "image/webp"], "maxBytes": 4194304 }
}`
	gristInstructions = "List the kinds of things you can see kept at the Place."
	gristSchema       = `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "required": ["schemaVersion", "responseType", "placeName", "items"]}`
)

// gristVectors is the part of the vectors a scenario reads.
type gristVectors struct {
	Keys map[string]struct {
		PrivateKeyHex string `json:"privateKeyHex"`
		PublicKeyHex  string `json:"publicKeyHex"`
	} `json:"keys"`
	Grist   application.GristPlaintext         `json:"grist"`
	Answers map[string]application.GristAnswer `json:"answers"`
}

// gristContext is one mill: a throwaway mill key (the vectors' own), a fake
// backend, a reversible cipher, fake state, grinds, grinder, tracker and
// locks. Nothing here reaches a network, a real harness or a real bd.
type gristContext struct {
	vectors gristVectors
	home    string

	keys      *postern.KeyFile
	millKey   string
	backend   *apptest.FakePostern
	state     *apptest.FakeGristState
	grinds    *apptest.FakeGrinds
	grinder   *apptest.FakeGrinder
	tracker   *apptest.FakeTracker
	pass      *apptest.FakeGristLock
	grinding  *apptest.FakeGristLock
	host      string
	cap       int
	commit    string
	ceilings  application.GristCeilings
	governor  string
	phoneKey  string
	phoneApps []string

	grist  application.PosternRecord
	photos map[string][]byte // blob hash to the photo it seals

	report     application.GristReport
	homeIs     string // the host the vault's home file names; empty is no home file
	configured bool   // a [grist] table is in the config file
	err        error
	out        bytes.Buffer
	dispatch   application.DispatchReport
}

// InitializeGristScenario registers the steps of features/grist.feature.
func InitializeGristScenario(ctx *godog.ScenarioContext) {
	c := &gristContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = gristContext{}
		raw, err := os.ReadFile(gristVectorsPath)
		if err != nil {
			return ctx, err
		}
		if err := json.Unmarshal(raw, &c.vectors); err != nil {
			return ctx, fmt.Errorf("reading %s: %w", gristVectorsPath, err)
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a mill on the host "([^"]*)" with a cap of (\d+)$`, c.aMill)
	ctx.Given(`^the app "([^"]*)" is checked out here, its main at commit "([^"]*)" with the grind "([^"]*)"$`, c.theAppIsCheckedOut)
	ctx.Given(`^a phone whose licence opens "([^"]*)"$`, c.aPhoneWhoseLicenceOpens)
	ctx.Given(`^the phone sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", with (\d+) photos?$`, c.thePhoneSends)
	ctx.Given(`^the Governor's key sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", with (\d+) photos?$`, c.theGovernorSends)
	ctx.Given(`^the grind answers with a sweep result$`, c.theGrindAnswers)
	ctx.Given(`^the grind's session ends in an error$`, c.theGrindEndsInAnError)
	ctx.Given(`^the grind finishes without a structured answer$`, c.theGrindGivesNoAnswer)
	ctx.Given(`^the factory allows at most (\d+) photos a grist$`, c.theFactoryAllowsPhotos)
	ctx.Given(`^the factory allows only the models "([^"]*)"$`, c.theFactoryAllowsModels)
	ctx.Given(`^the factory allows (\d+) grist a day from one key$`, c.theFactoryAllowsADay)
	ctx.Given(`^the phone has already sent (\d+) grist today$`, c.thePhoneHasSent)
	ctx.Given(`^a message to the mill key$`, c.aMessageToTheMill)
	ctx.Given(`^a grist from the phone to another key$`, c.aGristToAnotherKey)
	ctx.Given(`^a story is already running on "([^"]*)"$`, c.aStoryIsRunningOn)
	ctx.Given(`^the backend will not take a record$`, c.theBackendWillNotTake)
	ctx.Given(`^another pass of the mill is running$`, c.anotherPassIsRunning)
	ctx.Given(`^a grind is running on "([^"]*)"$`, c.aGrindIsRunningOn)
	ctx.Given(`^a story is ready on "([^"]*)"$`, c.aStoryIsReadyOn)
	ctx.Given(`^"([^"]*)" is home$`, c.hostIsHome)
	ctx.Given(`^\[grist\] is configured$`, c.gristIsConfigured)

	ctx.When(`^the mill grinds$`, c.theMillGrinds)
	ctx.When(`^the mill grinds again$`, c.theMillGrinds)
	ctx.When(`^the mill grinds again, reading from the start$`, c.theMillGrindsFromTheStart)
	ctx.When(`^the running story finishes$`, c.theRunningStoryFinishes)
	ctx.When(`^the backend takes records again$`, c.theBackendTakesRecordsAgain)
	ctx.When(`^mw dispatch runs on "([^"]*)" with a cap of (\d+)$`, c.mwDispatchRuns)

	ctx.Then(`^the mill answered (\d+), refused (\d+), failed (\d+), and left (\d+) waiting$`, c.theMillCounted)
	ctx.Then(`^the answer is a grist record from the mill key, sealed to the phone, re the grist$`, c.theAnswerIsSealedToThePhone)
	ctx.Then(`^the answer says "([^"]*)" with the grind's answer and the commit "([^"]*)"$`, c.theAnswerSaysAnswered)
	ctx.Then(`^the answer says "([^"]*)" because "([^"]*)"$`, c.theAnswerSaysBecause)
	ctx.Then(`^the grind ran on "([^"]*)" at "([^"]*)" effort, given the photo by its full path and the request between its markers$`, c.theGrindRan)
	ctx.Then(`^the grind's private directory is gone$`, c.thePrivateDirectoryIsGone)
	ctx.Then(`^the grist's photos were deleted from the backend$`, c.thePhotosWereDeleted)
	ctx.Then(`^the grist's photos are still on the backend$`, c.thePhotosAreStillThere)
	ctx.Then(`^the mill's record holds one line for the grist, "([^"]*)", with its Fuel and nothing of its content$`, c.theRecordHoldsOneLine)
	ctx.Then(`^no grind was run$`, c.noGrindWasRun)
	ctx.Then(`^(\d+) grinds? (?:was|were) run$`, c.grindsWereRun)
	ctx.Then(`^(\d+) answers? (?:was|were) delivered$`, c.answersWereDelivered)
	ctx.Then(`^no answer was delivered$`, c.noAnswerWasDelivered)
	ctx.Then(`^the mill's cursor is past them$`, c.theCursorIsPastThem)
	ctx.Then(`^the mill's cursor is before the grist$`, c.theCursorIsBeforeTheGrist)
	ctx.Then(`^the mill says it is waiting because "([^"]*)"$`, c.theMillIsWaitingBecause)
	ctx.Then(`^the mill says another pass is running$`, c.theMillSaysAnotherPass)
	ctx.Then(`^dispatch started nothing, since "([^"]*)"$`, c.dispatchStartedNothing)
	ctx.Then(`^dispatch says a grist grind holds one of its sessions$`, c.dispatchSaysGrinding)
	ctx.Then(`^the dispatch's grist pass answered (\d+), refused (\d+), failed (\d+), and left (\d+) waiting$`, c.dispatchGristPassCounted)
	ctx.Then(`^the dispatch ran no grist pass$`, c.dispatchRanNoGristPass)
}

// vectorKey is the vectors' key by name: its public hex, and its private
// key as the WIF a key file holds.
func (c *gristContext) vectorKey(name string) (pub, wif string, err error) {
	key, ok := c.vectors.Keys[name]
	if !ok {
		return "", "", fmt.Errorf("the grist vectors have no key %q", name)
	}
	raw, err := hex.DecodeString(key.PrivateKeyHex)
	if err != nil {
		return "", "", err
	}
	priv, _ := ec.PrivateKeyFromBytes(raw)
	return key.PublicKeyHex, priv.WifPrefix(byte(ec.TestNet)), nil
}

func (c *gristContext) aMill(host string, cap int) error {
	home, err := os.MkdirTemp("", "mw-grist-")
	if err != nil {
		return err
	}
	c.home, c.host, c.cap = home, host, cap
	pub, wif, err := c.vectorKey("mill")
	if err != nil {
		return err
	}
	path := filepath.Join(home, "mill.key")
	if err := os.WriteFile(path, []byte(wif+"\n"), 0o600); err != nil {
		return err
	}
	c.keys = postern.New(path)
	if c.millKey, _, err = c.keys.PublicKey(); err != nil {
		return err
	}
	if c.millKey != pub {
		return fmt.Errorf("the mill key file's public key is %s, not the vectors' %s", c.millKey, pub)
	}
	if c.governor, _, err = c.vectorKey("governor"); err != nil {
		return err
	}
	c.backend = apptest.NewFakePostern()
	c.state = apptest.NewFakeGristState()
	c.grinds = apptest.NewFakeGrinds()
	c.grinder = &apptest.FakeGrinder{}
	c.tracker = apptest.NewFakeTracker()
	c.pass, c.grinding = &apptest.FakeGristLock{}, &apptest.FakeGristLock{}
	c.photos = map[string][]byte{}
	return nil
}

func (c *gristContext) checkout(app string) string { return "/rigs/" + app }

func (c *gristContext) theAppIsCheckedOut(app, commit, kind string) error {
	c.commit = commit
	c.grinds.SetFile(c.checkout(app), commit, "grinds/"+kind+".json", []byte(gristGrindFile))
	c.grinds.SetFile(c.checkout(app), commit, "grinds/sweep.md", []byte(gristInstructions))
	c.grinds.SetFile(c.checkout(app), commit, "contexts/inventory/schemas/sweep-result.schema.json", []byte(gristSchema))
	return nil
}

func (c *gristContext) aPhoneWhoseLicenceOpens(app string) error {
	pub, _, err := c.vectorKey("cairnPhone")
	c.phoneKey, c.phoneApps = pub, []string{app}
	return err
}

func (c *gristContext) thePhoneSends(app, kind, v string, photos int) error {
	return c.send(c.phoneKey, c.phoneApps, app, kind, v, photos)
}

func (c *gristContext) theGovernorSends(app, kind, v string, photos int) error {
	return c.send(c.governor, nil, app, kind, v, photos)
}

// send seals a grist from key to the mill, uploading its photos first, each
// sealed to the mill as section 18 says, and adds it to the backend's index
// stamped with the apps key's licences open.
func (c *gristContext) send(key string, apps []string, app, kind, v string, photos int) error {
	sealer := &apptest.FakeCipher{From: key}
	plain := c.vectors.Grist
	plain.Grist = application.GristName{App: app, Kind: kind, V: v}
	plain.Attachments = nil
	for i := 1; i <= photos; i++ {
		photo := []byte(fmt.Sprintf("jpeg bytes of photo %d", i))
		sealed, err := sealer.EncryptBytes(c.millKey, photo)
		if err != nil {
			return err
		}
		raw, _ := base64.StdEncoding.DecodeString(sealed)
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		c.backend.SetBlob(hash, raw)
		c.photos[hash] = photo
		plain.Attachments = append(plain.Attachments, application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: "image/jpeg"})
	}
	text, err := json.Marshal(plain)
	if err != nil {
		return err
	}
	ct, err := sealer.Encrypt(c.millKey, string(text))
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(ct))
	c.grist = c.backend.AddRecord(application.PosternRecord{
		Txid: "direct:" + hex.EncodeToString(sum[:]), Class: application.GristClass,
		From: key, To: c.millKey, Signer: key, SignerApps: apps, Ts: gristNow, Ciphertext: ct,
	})
	return nil
}

func (c *gristContext) theGrindAnswers() error {
	answer, err := json.Marshal(c.vectors.Answers["answered"].Answer)
	if err != nil {
		return err
	}
	c.grinder.Result = application.SessionResult{
		Subtype: "success", Turns: 4, CostUSD: 0.027, Answer: answer,
		Fuel: application.Fuel{Input: 12, Output: 340, CacheRead: 9000, CacheWrite: 1200},
	}
	return nil
}

func (c *gristContext) theGrindEndsInAnError() error {
	c.grinder.Result = application.SessionResult{
		Subtype: "error_during_execution", IsError: true, CostUSD: 0.004,
		Fuel: application.Fuel{Input: 10, Output: 5},
	}
	return nil
}

func (c *gristContext) theGrindGivesNoAnswer() error {
	c.grinder.Result = application.SessionResult{Subtype: "success", Said: "Here are the items.", Fuel: application.Fuel{Input: 1}}
	return nil
}

func (c *gristContext) theFactoryAllowsPhotos(n int) error {
	c.ceilings.MaxAttachments = n
	return nil
}

func (c *gristContext) theFactoryAllowsModels(models string) error {
	c.ceilings.Models = strings.Split(models, ",")
	return nil
}

func (c *gristContext) theFactoryAllowsADay(n int) error {
	c.ceilings.DailyLimit = n
	return nil
}

func (c *gristContext) thePhoneHasSent(n int) error {
	for i := 0; i < n; i++ {
		if err := c.state.Append(context.Background(), application.GrindLine{
			Time: gristNow.Add(-time.Duration(i+1) * time.Minute), Txid: fmt.Sprintf("direct:earlier-%d", i),
			Sender: application.KeyFingerprint(c.phoneKey), Status: application.GristAnswered,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *gristContext) aMessageToTheMill() error {
	c.backend.AddRecord(application.PosternRecord{Txid: "direct:message", Class: "message", From: c.governor, To: c.millKey, Ciphertext: "x"})
	return nil
}

func (c *gristContext) aGristToAnotherKey() error {
	c.backend.AddRecord(application.PosternRecord{Txid: "direct:elsewhere", Class: application.GristClass, From: c.phoneKey, To: c.governor, Ciphertext: "x"})
	return nil
}

// aStoryIsRunningOn claims a story planned for host, as a session working it
// would hold it.
func (c *gristContext) aStoryIsRunningOn(host string) error {
	if err := c.aStoryIsReadyOn(host); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), "mw-9.1")
}

func (c *gristContext) aStoryIsReadyOn(host string) error {
	var path domain.Path
	for field, value := range map[string]string{
		"rig": "millwright", "branch": "main", "harness": "claude", "model": "opus", "effort": "high", "host": host,
	} {
		if err := path.Set(field, value); err != nil {
			return err
		}
	}
	c.tracker.AddEpic("mw-9", path)
	c.tracker.AddStory("mw-9", domain.Story{ID: "mw-9.1", Title: "A story"})
	return nil
}

func (c *gristContext) theBackendWillNotTake() error {
	c.backend.DeliverErr = errors.New("the postern backend said 503")
	return nil
}

func (c *gristContext) theBackendTakesRecordsAgain() error {
	c.backend.DeliverErr = nil
	return nil
}

func (c *gristContext) anotherPassIsRunning() error {
	c.pass.Hold()
	return nil
}

func (c *gristContext) aGrindIsRunningOn(host string) error {
	if host != c.host {
		return fmt.Errorf("the mill is on %s, not %s", c.host, host)
	}
	c.grinding.Hold()
	return nil
}

func (c *gristContext) mill() application.GristGrind {
	return application.GristGrind{
		Postern: c.backend, Cipher: &apptest.FakeCipher{From: c.millKey}, Keys: c.keys,
		State: c.state, Grinds: c.grinds, Grinder: c.grinder, Tracker: c.tracker,
		Pass: c.pass, Grinding: c.grinding, Host: c.host, Cap: c.cap,
		Apps:        map[string]string{"cairn": c.checkout("cairn")},
		Ceilings:    c.ceilings,
		GovernorKey: c.governor,
		TempDir:     c.home,
		Now:         func() time.Time { return gristNow },
		Out:         &c.out,
	}
}

func (c *gristContext) theMillGrinds() error {
	c.out.Reset()
	c.report, c.err = c.mill().Run(context.Background())
	return c.err
}

func (c *gristContext) theMillGrindsFromTheStart() error {
	if err := c.state.SetCursor(context.Background(), 0); err != nil {
		return err
	}
	return c.theMillGrinds()
}

func (c *gristContext) theRunningStoryFinishes() error {
	return c.tracker.CloseStory(context.Background(), "mw-9.1", "done")
}

// noWorktrees is a Worktrees that is never reached: a dispatch at its cap
// cuts nothing.
type noWorktrees struct{}

func (noWorktrees) Fetch(context.Context, string) error                          { return nil }
func (noWorktrees) Add(context.Context, string, string, string, string) error    { return nil }
func (noWorktrees) Exists(context.Context, string, string, string) (bool, error) { return false, nil }
func (noWorktrees) Remove(context.Context, string, string, string) error         { return nil }
func (noWorktrees) RemoveWithoutForce(context.Context, string, string) error     { return nil }
func (noWorktrees) DeleteBranch(context.Context, string, string) error           { return nil }

func (c *gristContext) hostIsHome(host string) error {
	c.homeIs = host
	return nil
}

func (c *gristContext) gristIsConfigured() error {
	c.configured = true
	return nil
}

// mwDispatchRuns is one dispatch tick, as cmd/mw wires it: the mill is given
// only where a [grist] table is configured, and the vault's home file says
// which host is home.
func (c *gristContext) mwDispatchRuns(host string, cap int) error {
	c.out.Reset()
	tick := application.Dispatch{
		Tracker: c.tracker, Worktrees: noWorktrees{}, Runner: apptest.NewFakeRunner(),
		Host: host, Cap: cap, Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Grinding: c.grinding, Out: &c.out,
	}
	if c.homeIs != "" {
		tick.Home = &apptest.FakeHomeFile{Text: c.homeIs + " 2026-09-29T09:00:00Z mw@" + c.homeIs + "\n"}
	}
	if c.configured {
		tick.Mill = c.mill()
	}
	c.dispatch, c.err = tick.Run(context.Background())
	return c.err
}

func (c *gristContext) dispatchGristPassCounted(answered, refused, failed, waiting int) error {
	r := c.dispatch.Grist
	if r == nil {
		return fmt.Errorf("expected the dispatch to run a grist pass, it said:\n%s", c.out.String())
	}
	if r.Answered != answered || r.Refused != refused || r.Failed != failed || r.Waiting != waiting {
		return fmt.Errorf("the dispatch's grist pass counted %d answered, %d refused, %d failed, %d waiting; expected %d, %d, %d, %d",
			r.Answered, r.Refused, r.Failed, r.Waiting, answered, refused, failed, waiting)
	}
	return nil
}

func (c *gristContext) dispatchRanNoGristPass() error {
	if c.dispatch.Grist != nil {
		return fmt.Errorf("expected no grist pass, the dispatch ran one: %+v", *c.dispatch.Grist)
	}
	return nil
}

// delivered opens every answer the mill delivered: its envelope, who the
// reversible cipher sealed it to and from, and its plaintext.
func (c *gristContext) delivered() ([]application.PosternPayload, []string, []application.GristAnswer, error) {
	var payloads []application.PosternPayload
	var sealedTo []string
	var answers []application.GristAnswer
	for _, raw := range c.backend.Delivered() {
		var p application.PosternPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, nil, nil, err
		}
		plain, err := base64.StdEncoding.DecodeString(p.Ct)
		if err != nil {
			return nil, nil, nil, err
		}
		parts := strings.SplitN(string(plain), "\x00", 3)
		if len(parts) != 3 {
			return nil, nil, nil, fmt.Errorf("the answer's ct is not the fake cipher's")
		}
		var answer application.GristAnswer
		if err := json.Unmarshal([]byte(parts[2]), &answer); err != nil {
			return nil, nil, nil, fmt.Errorf("the answer's plaintext is not an answer: %w", err)
		}
		payloads, sealedTo, answers = append(payloads, p), append(sealedTo, parts[0]+"\x00"+parts[1]), append(answers, answer)
	}
	return payloads, sealedTo, answers, nil
}

// onlyAnswer is the one answer delivered.
func (c *gristContext) onlyAnswer() (application.PosternPayload, string, application.GristAnswer, error) {
	payloads, sealed, answers, err := c.delivered()
	if err != nil {
		return application.PosternPayload{}, "", application.GristAnswer{}, err
	}
	if len(answers) != 1 {
		return application.PosternPayload{}, "", application.GristAnswer{}, fmt.Errorf("expected one answer delivered, got %d:\n%s", len(answers), c.out.String())
	}
	return payloads[0], sealed[0], answers[0], nil
}

func (c *gristContext) theMillCounted(answered, refused, failed, waiting int) error {
	r := c.report
	if r.Answered != answered || r.Refused != refused || r.Failed != failed || r.Waiting != waiting {
		return fmt.Errorf("expected %d answered, %d refused, %d failed, %d waiting; the mill said:\n%s", answered, refused, failed, waiting, c.out.String())
	}
	return nil
}

func (c *gristContext) theAnswerIsSealedToThePhone() error {
	payload, sealed, answer, err := c.onlyAnswer()
	if err != nil {
		return err
	}
	if payload.V != 1 || payload.Kind != "msg" || payload.Class != application.GristClass {
		return fmt.Errorf("expected a version 1 msg of class grist, got %+v", payload)
	}
	if payload.To != c.phoneKey || payload.From != c.millKey {
		return fmt.Errorf("expected the answer to the phone from the mill key, got to %s from %s", payload.To, payload.From)
	}
	if sealed != c.phoneKey+"\x00"+c.millKey {
		return fmt.Errorf("expected the answer sealed from the mill key to the phone, got %q", sealed)
	}
	if answer.Re != c.grist.Txid {
		return fmt.Errorf("expected the answer re %s, got %q", c.grist.Txid, answer.Re)
	}
	if payload.Ts != gristNow.Unix() {
		return fmt.Errorf("expected the answer stamped %d, got %d", gristNow.Unix(), payload.Ts)
	}
	return nil
}

func (c *gristContext) theAnswerSaysAnswered(status, commit string) error {
	_, _, answer, err := c.onlyAnswer()
	if err != nil {
		return err
	}
	want := c.vectors.Answers["answered"]
	if answer.Status != status || answer.Reason != "" {
		return fmt.Errorf("expected %s with no reason, got %s %q", status, answer.Status, answer.Reason)
	}
	var got, expected any
	if err := json.Unmarshal(answer.Answer, &got); err != nil {
		return err
	}
	if err := json.Unmarshal(want.Answer, &expected); err != nil {
		return err
	}
	if fmt.Sprint(got) != fmt.Sprint(expected) {
		return fmt.Errorf("expected the grind's answer %s, got %s", want.Answer, answer.Answer)
	}
	if grind := (application.GristAnswerGrind{App: "cairn", Kind: "sweep", V: "1.1", Commit: commit}); answer.Grind != grind {
		return fmt.Errorf("expected the grind %+v, got %+v", grind, answer.Grind)
	}
	return nil
}

func (c *gristContext) theAnswerSaysBecause(status, reason string) error {
	_, _, answer, err := c.onlyAnswer()
	if err != nil {
		return err
	}
	if answer.Status != status || answer.Reason != reason || len(answer.Answer) != 0 {
		return fmt.Errorf("expected %s because %q and no answer, got %s because %q (%s)", status, reason, answer.Status, answer.Reason, answer.Answer)
	}
	if answer.Re != c.grist.Txid {
		return fmt.Errorf("expected the answer re %s, got %q", c.grist.Txid, answer.Re)
	}
	return nil
}

func (c *gristContext) onlyGrind() (apptest.GrindSeen, error) {
	calls := c.grinder.Calls()
	if len(calls) != 1 {
		return apptest.GrindSeen{}, fmt.Errorf("expected one grind, got %d", len(calls))
	}
	return calls[0], nil
}

func (c *gristContext) theGrindRan(model, effort string) error {
	seen, err := c.onlyGrind()
	if err != nil {
		return err
	}
	call := seen.Call
	if call.Model != model || call.Effort != effort {
		return fmt.Errorf("expected %s at %s, got %s at %s", model, effort, call.Model, call.Effort)
	}
	if !filepath.IsAbs(call.Dir) || !strings.HasPrefix(call.Dir, c.home) {
		return fmt.Errorf("expected a private directory under %s, got %s", c.home, call.Dir)
	}
	photo := filepath.Join(call.Dir, "photo-1.jpg")
	if !strings.Contains(call.Prompt, "\n"+photo+"\n") {
		return fmt.Errorf("expected the prompt to name %s on a line of its own, got:\n%s", photo, call.Prompt)
	}
	for hash, want := range c.photos {
		if got := seen.Files["photo-1.jpg"]; !bytes.Equal(got, want) {
			return fmt.Errorf("expected photo-1.jpg to hold the opened photo %s, got %q", hash, got)
		}
	}
	input, _ := json.Marshal(c.vectors.Grist.Input)
	lines := strings.Split(call.Prompt, "\n")
	found := false
	for i := 1; i+1 < len(lines); i++ {
		if lines[i] == string(input) && strings.HasPrefix(lines[i-1], "GRIST INPUT ") && strings.HasSuffix(lines[i-1], " BEGINS") &&
			lines[i+1] == strings.TrimSuffix(lines[i-1], " BEGINS")+" ENDS" {
			found = true
			if !strings.Contains(call.System, lines[i-1]) {
				return fmt.Errorf("expected the system prompt to name the marker %q", lines[i-1])
			}
		}
	}
	if !found {
		return fmt.Errorf("expected the request between its markers in the prompt, got:\n%s", call.Prompt)
	}
	if !strings.HasSuffix(call.System, gristInstructions) || !strings.Contains(call.System, "never instructions") || !strings.Contains(call.System, "Never describe people") {
		return fmt.Errorf("expected the mill's rules, then the grind's instructions, got:\n%s", call.System)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(gristSchema)); err != nil || call.Schema != compact.String() {
		return fmt.Errorf("expected the grind's schema, compact, got %s", call.Schema)
	}
	return nil
}

func (c *gristContext) thePrivateDirectoryIsGone() error {
	seen, err := c.onlyGrind()
	if err != nil {
		return err
	}
	if _, err := os.Stat(seen.Call.Dir); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("expected %s gone once the grind ended, got %v", seen.Call.Dir, err)
	}
	return nil
}

func (c *gristContext) thePhotosWereDeleted() error {
	deleted := strings.Join(c.backend.Deleted(), " ")
	for hash := range c.photos {
		if !strings.Contains(deleted, hash) || c.backend.HasBlob(hash) {
			return fmt.Errorf("expected the photo %s deleted from the backend, deleted: %s", hash, deleted)
		}
	}
	return nil
}

func (c *gristContext) thePhotosAreStillThere() error {
	for hash := range c.photos {
		if !c.backend.HasBlob(hash) {
			return fmt.Errorf("expected the photo %s still on the backend", hash)
		}
	}
	return nil
}

func (c *gristContext) theRecordHoldsOneLine(status string) error {
	lines, err := c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	if len(lines) != 1 {
		return fmt.Errorf("expected one line in the record, got %d", len(lines))
	}
	line := lines[0]
	if line.Txid != c.grist.Txid || line.Status != status || line.App != "cairn" || line.Kind != "sweep" || line.V != "1.1" || line.Model != "sonnet" {
		return fmt.Errorf("expected a %s line for %s, got %+v", status, c.grist.Txid, line)
	}
	if line.Sender != application.KeyFingerprint(c.phoneKey) || line.Commit != c.commit || !line.Time.Equal(gristNow) {
		return fmt.Errorf("expected the phone's fingerprint, the commit and the time, got %+v", line)
	}
	if line.Tokens == 0 || line.Fuel == nil || line.CostUSD == 0 {
		return fmt.Errorf("expected the grind's Fuel recorded, got %+v", line)
	}
	encoded, _ := json.Marshal(line)
	for _, content := range []string{c.phoneKey, "Top drawer", "scissors", "jpeg bytes"} {
		if strings.Contains(string(encoded), content) {
			return fmt.Errorf("the record holds %q: %s", content, encoded)
		}
	}
	return nil
}

func (c *gristContext) noGrindWasRun() error {
	if calls := c.grinder.Calls(); len(calls) != 0 {
		return fmt.Errorf("expected no grind, got %d", len(calls))
	}
	return nil
}

func (c *gristContext) grindsWereRun(n int) error {
	if calls := c.grinder.Calls(); len(calls) != n {
		return fmt.Errorf("expected %d grinds, got %d", n, len(calls))
	}
	return nil
}

func (c *gristContext) answersWereDelivered(n int) error {
	if got := len(c.backend.Delivered()); got != n {
		return fmt.Errorf("expected %d answers delivered, got %d:\n%s", n, got, c.out.String())
	}
	return nil
}

func (c *gristContext) noAnswerWasDelivered() error { return c.answersWereDelivered(0) }

func (c *gristContext) theCursorIsPastThem() error {
	cursor, _ := c.state.Cursor(context.Background())
	records, _ := c.backend.Messages(context.Background(), 0)
	if last := records[len(records)-1].Seq; cursor != last {
		return fmt.Errorf("expected the cursor at %d, got %d", last, cursor)
	}
	return nil
}

func (c *gristContext) theCursorIsBeforeTheGrist() error {
	if cursor, _ := c.state.Cursor(context.Background()); cursor >= c.grist.Seq {
		return fmt.Errorf("expected the cursor before the grist's %d, got %d", c.grist.Seq, cursor)
	}
	return nil
}

func (c *gristContext) theMillIsWaitingBecause(why string) error {
	if c.report.WaitingWhy != why || !strings.Contains(c.out.String(), "waiting: "+why) {
		return fmt.Errorf("expected the mill waiting because %q, it said:\n%s", why, c.out.String())
	}
	return nil
}

func (c *gristContext) theMillSaysAnotherPass() error {
	if !c.report.Busy || !strings.Contains(c.out.String(), "another mw grist grind is running") {
		return fmt.Errorf("expected the mill to leave the grist to the other pass, it said:\n%s", c.out.String())
	}
	return nil
}

func (c *gristContext) dispatchStartedNothing(why string) error {
	if len(c.dispatch.Started) != 0 {
		return fmt.Errorf("expected nothing started, got %+v", c.dispatch.Started)
	}
	for _, passed := range c.dispatch.Passed {
		if strings.Contains(passed.Why, why) {
			return nil
		}
	}
	return fmt.Errorf("expected a story passed because %q, dispatch said:\n%s", why, c.out.String())
}

func (c *gristContext) dispatchSaysGrinding() error {
	if !c.dispatch.Grinding || c.dispatch.Running != 1 || !strings.Contains(c.out.String(), "a grist grind holds one of those sessions") {
		return fmt.Errorf("expected dispatch to count the grind as a running session, it said:\n%s", c.out.String())
	}
	return nil
}
