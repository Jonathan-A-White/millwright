package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

var creditsNow = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

const testAPIKey = "sk-ant-api03-secret-for-tests"

// creditsFor is the credits a test runs with: on, a key, no mark.
func creditsFor() (*application.GristCredits, *apptest.FakeCreditState, *apptest.FakeEventLog) {
	state, log := &apptest.FakeCreditState{}, &apptest.FakeEventLog{}
	return &application.GristCredits{
		On: true, Key: &apptest.FakeCreditKey{Key: testAPIKey}, State: state, Events: log, Host: "laptop",
		Now: func() time.Time { return creditsNow },
	}, state, log
}

// The key is used only with the switch on, a usable key file and no spent
// mark; a mark is waited out for a day, then the key is tried again.
func TestGristCreditsChooseWhatAGrindIsBilledTo(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		set     func(c *application.GristCredits, s *apptest.FakeCreditState)
		billing string
		retry   bool
		note    string
	}{
		"the switch off":            {func(c *application.GristCredits, _ *apptest.FakeCreditState) { c.On = false }, application.BillingSubscription, false, ""},
		"on with a key and no mark": {func(*application.GristCredits, *apptest.FakeCreditState) {}, application.BillingAPI, false, ""},
		"on but the key file refused": {func(c *application.GristCredits, _ *apptest.FakeCreditState) {
			c.Key = &apptest.FakeCreditKey{Refused: "the API key file /k has mode 0644, not 0600"}
		}, application.BillingSubscription, false, "0644"},
		"spent an hour ago": {func(_ *application.GristCredits, s *apptest.FakeCreditState) {
			s.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-time.Hour)})
		}, application.BillingSubscription, false, ""},
		"spent 25 hours ago": {func(_ *application.GristCredits, s *apptest.FakeCreditState) {
			s.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-25 * time.Hour)})
		}, application.BillingAPI, true, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, state, _ := creditsFor()
			tc.set(c, state)
			got := c.Choose(ctx)
			if got.Billing != tc.billing || got.Retry != tc.retry || !strings.Contains(got.Note, tc.note) {
				t.Fatalf("expected %s retry=%v note %q, got %+v", tc.billing, tc.retry, tc.note, got)
			}
			if (got.Billing == application.BillingAPI) != (got.Key() == testAPIKey) {
				t.Fatalf("expected the key exactly when billed to the API, got %q for %s", got.Key(), got.Billing)
			}
		})
	}
	var none *application.GristCredits
	if got := none.Choose(ctx); got.Billing != application.BillingSubscription || got.Key() != "" {
		t.Fatalf("expected no credits to bill the subscription, got %+v", got)
	}
}

// A billing never prints its key.
func TestAGristBillingNeverPrintsItsKey(t *testing.T) {
	c, _, _ := creditsFor()
	got := c.Choose(context.Background())
	for _, text := range []string{got.String(), strings.TrimSpace(fmtAll(got))} {
		if strings.Contains(text, testAPIKey) {
			t.Fatalf("expected the key out of %q", text)
		}
	}
}

var sprint = fmt.Sprintf

func fmtAll(b application.GristBilling) string {
	return strings.Join([]string{sprint("%v", b), sprint("%+v", b), sprint("%#v", b)}, " ")
}

// The CLI's credit refusals and its invalid-key refusals, wherever it puts
// them; nothing else is read as either.
func TestCreditFailureOfReadsTheCLIsWords(t *testing.T) {
	cases := []struct {
		name   string
		result application.SessionResult
		err    error
		want   string
	}{
		{"credit balance in result", application.SessionResult{IsError: true, Subtype: "success", Said: "Credit balance is too low"}, nil, application.CreditsSpent},
		{"credit balance, any case, in the API error", application.SessionResult{IsError: true, Said: `API Error: 400 {"error":{"message":"Your CREDIT BALANCE IS TOO LOW to access the Anthropic API"}}`}, nil, application.CreditsSpent},
		{"credit balance in errors", application.SessionResult{IsError: true, Subtype: "error_during_execution", ErrorsText: `["Credit balance is too low"]`}, nil, application.CreditsSpent},
		{"credit balance in the error", application.SessionResult{}, errors.New("claude: exit status 1: Credit balance is too low"), application.CreditsSpent},
		{"spend limit", application.SessionResult{IsError: true, Said: "You have reached your workspace's monthly spend limit"}, nil, application.CreditsSpent},
		{"invalid key", application.SessionResult{IsError: true, Said: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`}, nil, application.CreditsKeyRefused},
		{"authentication error in errors", application.SessionResult{IsError: true, ErrorsText: `[{"message":"Invalid API key · Please run /login"}]`}, nil, application.CreditsKeyRefused},
		{"some other failure", application.SessionResult{IsError: true, Said: "API Error: 529 overloaded_error"}, nil, ""},
		{"a finished answer that says it", application.SessionResult{Subtype: "success", Said: "the credit balance is too low, says the photo"}, nil, ""},
		{"a timeout", application.SessionResult{}, application.ErrGrindTimedOut, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := application.CreditFailureOf(tc.result, tc.err); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// Writing the mark posts one event for the Mayor, in the normal lane, saying
// why and never the key; a second grind failing the same way at once adds none.
func TestSpendWritesTheMarkAndPostsOneEvent(t *testing.T) {
	ctx := context.Background()
	c, state, log := creditsFor()
	c.Spend(ctx, application.CreditsSpent, "Credit balance is too low")
	c.Spend(ctx, application.CreditsSpent, "Credit balance is too low")

	if mark, found, _ := state.Mark(ctx); !found || !mark.SpentAt.Equal(creditsNow) {
		t.Fatalf("expected the mark written at %s, got %v %v", creditsNow, mark, found)
	}
	evs, _ := log.Since(ctx, 0)
	if len(evs) != 1 {
		t.Fatalf("expected one event, got %+v", evs)
	}
	ev := evs[0]
	if ev.Kind != events.KindJob || ev.Actor != "grist-credits-spent@laptop" || ev.Lane != events.LaneNormal || ev.To != events.JobFailed {
		t.Fatalf("expected a normal-lane job event of grist-credits-spent@laptop, got %+v", ev)
	}
	if !strings.Contains(ev.Detail, "credits spent") || strings.Contains(ev.Detail, testAPIKey) {
		t.Fatalf("expected the detail to say credits spent and hold no key, got %q", ev.Detail)
	}

	c2, _, log2 := creditsFor()
	c2.Spend(ctx, application.CreditsKeyRefused, "invalid x-api-key")
	evs, _ = log2.Since(ctx, 0)
	if len(evs) != 1 || !strings.Contains(evs[0].Detail, "key refused") || strings.Contains(evs[0].Detail, "credits spent") {
		t.Fatalf("expected an event saying key refused, got %+v", evs)
	}
}

// A grind that tried the key again and got through clears the mark.
func TestSucceededClearsTheMark(t *testing.T) {
	c, state, _ := creditsFor()
	state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-30 * time.Hour)})
	c.Succeeded(context.Background())
	if _, found, _ := state.Mark(context.Background()); found {
		t.Fatal("expected the mark cleared")
	}
}

// keyedGrinder is a grinder whose answer depends on what the call is billed
// to: onKey is what a call carrying the key gets, else the grind succeeds.
type keyedGrinder struct {
	mu    sync.Mutex
	onKey application.SessionResult
	errOn error
	calls []application.GrindCall
}

func (g *keyedGrinder) Grind(_ context.Context, call application.GrindCall) (application.SessionResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, call)
	if call.APIKey != "" {
		return g.onKey, g.errOn
	}
	return application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`), StopReason: "end_turn"}, nil
}

func (g *keyedGrinder) keys() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var keys []string
	for _, c := range g.calls {
		keys = append(keys, c.APIKey)
	}
	return keys
}

// creditMill is aSmallMill billing through credits and a keyedGrinder, its
// grist's sender a person marked API only (mw-gq6.1518).
func creditMill(t *testing.T, onKey application.SessionResult) (application.GristGrind, *apptest.FakePostern, *keyedGrinder, *application.GristCredits, *apptest.FakeCreditState, *apptest.FakeEventLog) {
	t.Helper()
	mill, backend, _ := aSmallMill(t)
	credits, state, log := creditsFor()
	grinder := &keyedGrinder{onKey: onKey}
	mill.Grinder, mill.Credits = grinder, credits
	mill.Persons = markedPersons(smallMillPhone, application.PersonBillingAPI)
	mill.Now = func() time.Time { return creditsNow }
	return mill, backend, grinder, credits, state, log
}

// markedPersons is a person lookup that knows key alone, as person p-phone's,
// with the billing mark billing.
func markedPersons(key, billing string) *application.PersonCap {
	return application.NewPersonCap(aRegister(func(_ context.Context, asked string) (application.Person, bool, error) {
		if asked != key {
			return application.Person{}, false, nil
		}
		return application.Person{ID: "p-phone", Keys: []string{key}, Billing: billing}, true, nil
	}), &bytes.Buffer{})
}

// factoryCreditMill is creditMill whose grist is sealed by the factory's own
// test key: the one sealer whose grist still falls back to the subscription.
func factoryCreditMill(t *testing.T, onKey application.SessionResult) (application.GristGrind, *apptest.FakePostern, *keyedGrinder, *application.GristCredits, *apptest.FakeCreditState, *apptest.FakeEventLog) {
	t.Helper()
	mill, backend, grinder, credits, state, log := creditMill(t, onKey)
	mill.TestKey = smallMillPhone
	return mill, backend, grinder, credits, state, log
}

func lineOf(t *testing.T, mill application.GristGrind) application.GrindLine {
	t.Helper()
	lines, err := mill.State.Lines(context.Background())
	if err != nil || len(lines) != 1 {
		t.Fatalf("expected one line in the record, got %d %v", len(lines), err)
	}
	return lines[0]
}

// A grist is ground with the key and its line says billing api, with the key
// nowhere in it.
func TestAGristBilledToTheKeyIsGroundWithItAndItsLineSaysApi(t *testing.T) {
	good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
	mill, _, grinder, _, _, log := creditMill(t, good)
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
		t.Fatalf("expected one grind with the key, got %q", keys)
	}
	line := lineOf(t, mill)
	if line.Billing != application.BillingAPI || line.Status != application.GristAnswered {
		t.Fatalf("expected an answered line billed to api, got %+v", line)
	}
	text, _ := json.Marshal(line)
	if strings.Contains(string(text), testAPIKey) || !strings.Contains(string(text), `"billing":"api"`) {
		t.Fatalf("expected billing api and no key in the line, got %s", text)
	}
	if evs, _ := log.Since(context.Background(), 0); len(evs) != 0 {
		t.Fatalf("expected no event for a grind that got through, got %+v", evs)
	}
}

// With the switch off, or no usable key file, the factory's own grist's line
// says subscription and the grind carries no key.
func TestAGristOnTheSubscriptionSaysSoAndCarriesNoKey(t *testing.T) {
	mill, _, grinder, credits, _, _ := factoryCreditMill(t, application.SessionResult{})
	credits.On = false
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != "" {
		t.Fatalf("expected one grind with no key, got %q", keys)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingSubscription {
		t.Fatalf("expected billing subscription, got %+v", line)
	}
}

// A grist refused before any grind ran still says what it was billed to:
// nothing, "none" (mw-gq6.1520; before it, "subscription").
func TestARefusedGristSaysNone(t *testing.T) {
	mill, _, grinder, _, _, _ := creditMill(t, application.SessionResult{})
	mill.Ceilings.Models = []string{"opus"}
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(grinder.keys()) != 0 {
		t.Fatalf("expected no grind, got %q", grinder.keys())
	}
	if line := lineOf(t, mill); line.Status != application.GristRefused || line.Billing != application.BillingNone {
		t.Fatalf("expected a refused line billed to none, got %+v", line)
	}
}

// 'Credit balance is too low' writes the mark, posts one event, and runs the
// factory's own grist again at once on the subscription: one answer, one line,
// one count against the day.
func TestACreditFailureRunsTheSameGristAgainOnTheSubscription(t *testing.T) {
	low := application.SessionResult{IsError: true, Subtype: "success", Said: "Credit balance is too low"}
	mill, backend, grinder, _, state, log := factoryCreditMill(t, low)
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 2 || keys[0] != testAPIKey || keys[1] != "" {
		t.Fatalf("expected the key tried, then the same grist without it, got %q", keys)
	}
	if grinder.calls[0].Prompt != grinder.calls[1].Prompt || grinder.calls[0].Dir != grinder.calls[1].Dir {
		t.Fatalf("expected the same grist both times, got %q in %s then %q in %s", grinder.calls[0].Prompt, grinder.calls[0].Dir, grinder.calls[1].Prompt, grinder.calls[1].Dir)
	}
	if report.Answered != 1 || report.Failed != 0 || len(backend.Delivered()) != 1 {
		t.Fatalf("expected one answer delivered, got %+v and %d delivered", report, len(backend.Delivered()))
	}
	if answer := openFakeAnswer(t, backend.Delivered()[0]); answer.Status != application.GristAnswered {
		t.Fatalf("expected the grist answered, got %+v", answer)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingSubscription || line.Status != application.GristAnswered {
		t.Fatalf("expected one answered line billed to subscription, got %+v", line)
	}
	if mark, found, _ := state.Mark(context.Background()); !found || !mark.SpentAt.Equal(creditsNow) {
		t.Fatalf("expected the mark written, got %v %v", mark, found)
	}
	evs, _ := log.Since(context.Background(), 0)
	if len(evs) != 1 || evs[0].Actor != "grist-credits-spent@laptop" || !strings.Contains(evs[0].Detail, "credits spent") || evs[0].Lane == events.LaneEmergency {
		t.Fatalf("expected one quiet grist-credits-spent event, got %+v", evs)
	}
}

// A key the API refuses is treated the same, its event saying so.
func TestAKeyTheAPIRefusesFallsBackAtOnceAndSaysKeyRefused(t *testing.T) {
	bad := application.SessionResult{IsError: true, Said: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`}
	mill, _, grinder, _, _, log := factoryCreditMill(t, bad)
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 2 || keys[1] != "" {
		t.Fatalf("expected the grist run again without the key, got %q", keys)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingSubscription || line.Status != application.GristAnswered {
		t.Fatalf("expected an answered line billed to subscription, got %+v", line)
	}
	evs, _ := log.Since(context.Background(), 0)
	if len(evs) != 1 || !strings.Contains(evs[0].Detail, "key refused") {
		t.Fatalf("expected one event saying key refused, got %+v", evs)
	}
}

// A failure that is not the key's is not retried on the subscription.
func TestAnotherFailureOfAGrindOnTheKeyIsNotRetried(t *testing.T) {
	mill, _, grinder, _, state, log := creditMill(t, application.SessionResult{IsError: true, Said: "API Error: 529 overloaded_error"})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(grinder.keys()) != 1 {
		t.Fatalf("expected one grind, got %q", grinder.keys())
	}
	if _, found, _ := state.Mark(context.Background()); found {
		t.Fatal("expected no mark")
	}
	if evs, _ := log.Since(context.Background(), 0); len(evs) != 0 {
		t.Fatalf("expected no event, got %+v", evs)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingAPI || line.Status != application.GristFailed {
		t.Fatalf("expected a failed line billed to api, got %+v", line)
	}
}

// While the mark stands every grist of the factory's own uses the subscription.
func TestWhileSpentEveryGristUsesTheSubscription(t *testing.T) {
	mill, _, grinder, _, state, _ := factoryCreditMill(t, application.SessionResult{})
	state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-2 * time.Hour)})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != "" {
		t.Fatalf("expected no key while spent, got %q", keys)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingSubscription {
		t.Fatalf("expected billing subscription, got %+v", line)
	}
}

// The first grist a day after the mark tries the key again; getting through
// clears the mark.
func TestADayAfterTheMarkTheKeyIsTriedAgainAndSuccessClearsIt(t *testing.T) {
	good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
	mill, _, grinder, _, state, _ := creditMill(t, good)
	state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-25 * time.Hour)})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
		t.Fatalf("expected the key tried again, got %q", keys)
	}
	if _, found, _ := state.Mark(context.Background()); found {
		t.Fatal("expected the mark cleared by the grind that got through")
	}
}

// ...and failing again writes a fresh mark and posts a fresh event.
func TestADayAfterTheMarkTheKeyFailingAgainWritesAFreshMark(t *testing.T) {
	low := application.SessionResult{IsError: true, Said: "Credit balance is too low"}
	mill, _, grinder, _, state, log := factoryCreditMill(t, low)
	state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-25 * time.Hour)})
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 2 || keys[0] != testAPIKey || keys[1] != "" {
		t.Fatalf("expected the key tried and then the subscription, got %q", keys)
	}
	if mark, found, _ := state.Mark(context.Background()); !found || !mark.SpentAt.Equal(creditsNow) {
		t.Fatalf("expected a fresh mark, got %v %v", mark, found)
	}
	if evs, _ := log.Since(context.Background(), 0); len(evs) != 1 {
		t.Fatalf("expected one event, got %+v", evs)
	}
}

// Warm sessions are started on the key when it is in use, and the ones started
// on it are retired when the mark is written; their replacements are on the
// subscription. (The grist is the factory's own, which falls back; another
// licence's retires them the same way.)
func TestWarmSessionsOnTheKeyAreRetiredWhenTheMarkIsWritten(t *testing.T) {
	ctx := context.Background()
	credits, state, _ := creditsFor()
	warmer := &apptest.FakeWarmGrinder{}
	now := creditsNow
	pool := &application.GristPool{Warmer: warmer, Slots: 2, TempDir: t.TempDir(), Now: func() time.Time { return now }, Credits: credits}

	mill, _, _, _, _, _ := factoryCreditMill(t, application.SessionResult{IsError: true, Said: "Credit balance is too low"})
	mill.Credits, mill.Pool = credits, pool
	if _, err := mill.Run(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := warmer.Sessions()
	if len(sessions) < 2 {
		t.Fatalf("expected a warm session started before the mark and one after, got %d", len(sessions))
	}
	if sessions[0].Call.APIKey != testAPIKey {
		t.Fatalf("expected the first warm session started on the key, got %+v", sessions[0].Call.APIKey)
	}
	if !sessions[0].Retired() {
		t.Fatal("expected the warm session started on the key retired when the mark was written")
	}
	last := sessions[len(sessions)-1]
	if last.Call.APIKey != "" || last.Retired() {
		t.Fatalf("expected the replacement waiting on the subscription, got key %q retired %v", last.Call.APIKey, last.Retired())
	}
	if _, found, _ := state.Mark(ctx); !found {
		t.Fatal("expected the mark written")
	}
	pool.Close()
}

// storeOf is a GristCreditKeyStore of a test's own: a file that is present or
// not, and the last key written to it.
type storeOf struct {
	apptest.FakeCreditKey
	present bool
	written string
}

func (s *storeOf) Present(context.Context) (bool, error) { return s.present, nil }
func (s *storeOf) Write(_ context.Context, key string) error {
	s.written = key
	return nil
}

func TestCreditsReportCountsTheUTCCalendarMonthOfLinesBilledToTheKey(t *testing.T) {
	record := &apptest.FakeGristState{}
	at := func(text string) time.Time {
		t.Helper()
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, line := range []application.GrindLine{
		{Time: at("2026-09-30T23:59:59Z"), Billing: application.BillingAPI, CostUSD: 7},
		{Time: at("2026-10-01T00:00:00Z"), Billing: application.BillingAPI, CostUSD: 1.25},
		{Time: at("2026-10-15T00:00:00Z"), Billing: application.BillingSubscription, CostUSD: 50},
		{Time: at("2026-10-19T22:00:00-05:00"), Billing: application.BillingAPI, CostUSD: 2},
		{Time: at("2026-11-01T00:00:00Z"), Billing: application.BillingAPI, CostUSD: 9},
		{Time: at("2026-10-10T00:00:00Z")},
	} {
		if err := record.Append(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	admin := application.GristCreditsAdmin{
		Keys: &storeOf{present: true}, State: &apptest.FakeCreditState{}, Record: record,
		Now: func() time.Time { return creditsNow },
	}
	report, err := admin.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Month != "2026-10" || report.Grists != 2 || report.CostUSD != 3.25 || report.BudgetUSD != 200 {
		t.Errorf("expected 2 grists, $3.25 of $200 in 2026-10, got %+v", report)
	}
	if report.LastAPI == nil || !report.LastAPI.Equal(at("2026-11-01T00:00:00Z")) {
		t.Errorf("expected the last api grist to be the latest of all time, got %v", report.LastAPI)
	}
}

func TestCreditsSetKeyChecksTheLineAfterTheHomeAndNeverRepeatsIt(t *testing.T) {
	home := &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop\n"}
	for line, want := range map[string]string{
		"":                      "the line was empty",
		"   ":                   "the line was empty",
		"sk-other-secret":       "does not start with sk-ant-",
		"sk-ant-two words here": "is not one key",
	} {
		store := &storeOf{}
		var out strings.Builder
		err := application.GristCreditsAdmin{Keys: store, State: &apptest.FakeCreditState{}, Home: home, Host: "laptop", Out: &out}.
			SetKey(context.Background(), func() (string, error) { return line + "\n", nil })
		if err == nil || !strings.Contains(err.Error(), want) || store.written != "" {
			t.Errorf("line %q: expected a refusal saying %q and nothing written, got %v (written %q)", line, want, err, store.written)
		}
		if strings.TrimSpace(line) != "" && strings.Contains(err.Error()+out.String(), strings.TrimSpace(line)) {
			t.Errorf("line %q was repeated: %v %s", line, err, out.String())
		}
	}
	store := &storeOf{}
	err := application.GristCreditsAdmin{Keys: store, State: &apptest.FakeCreditState{}, Home: home, Host: "cloud1"}.
		SetKey(context.Background(), func() (string, error) { t.Fatal("read a line away from home"); return "", nil })
	if err == nil || !strings.Contains(err.Error(), "is not home") {
		t.Errorf("expected a refusal away from home, got %v", err)
	}
}

// smallMillPhone is the key that seals aSmallMill's grist.
const smallMillPhone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"

// The Governor's own grist is billed to his subscription even with api
// credits on, a usable key and no spent mark (mw-gq6.1517).
func TestTheGovernorsGristIsBilledToTheSubscriptionWhateverTheCredits(t *testing.T) {
	good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
	mill, _, grinder, _, _, _ := creditMill(t, good)
	mill.GovernorKey = smallMillPhone
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != "" {
		t.Fatalf("expected the Governor's grist ground once with no key, got %q", keys)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingSubscription || line.Status != application.GristAnswered {
		t.Fatalf("expected an answered line billed to subscription, got %+v (%+v)", line, report)
	}
}

// A person marked API only's grist is not ground at all while the API key
// cannot be used: no key, a key file the mill refuses, no credits, or a
// standing mark the API's refusal of the key wrote. It is refused, resting,
// and never runs on the subscription (mw-gq6.1517, mw-gq6.1518).
func TestAnAPIOnlyPersonsGristRestsWithoutAUsableKey(t *testing.T) {
	cases := map[string]func(mill *application.GristGrind, c *application.GristCredits, s *apptest.FakeCreditState){
		"no key file": func(_ *application.GristGrind, c *application.GristCredits, _ *apptest.FakeCreditState) {
			c.Key = &apptest.FakeCreditKey{}
		},
		"a refused key": func(_ *application.GristGrind, c *application.GristCredits, _ *apptest.FakeCreditState) {
			c.Key = &apptest.FakeCreditKey{Refused: "the key file is readable by others"}
		},
		"no credits at all": func(m *application.GristGrind, _ *application.GristCredits, _ *apptest.FakeCreditState) {
			m.Credits = nil
		},
		"a standing mark of a refused key": func(_ *application.GristGrind, _ *application.GristCredits, s *apptest.FakeCreditState) {
			s.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-2 * time.Hour), Why: application.CreditsKeyRefused})
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder, credits, state, _ := creditMill(t, application.SessionResult{})
			mill.GovernorKey = gristStrangerKey
			set(&mill, credits, state)
			report, err := mill.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(grinder.calls) != 0 {
				t.Fatalf("expected no grind for an API-only person without a usable key, got %q", grinder.keys())
			}
			if report.Refused != 1 || len(backend.Delivered()) != 1 {
				t.Fatalf("expected one refusal delivered, got %+v", report)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonResting ||
				!strings.Contains(answer.Reason, "tutor is resting") || !strings.Contains(answer.Reason, "topped up") {
				t.Fatalf("expected a refusal saying the tutor is resting, got %+v", answer)
			}
			if line := lineOf(t, mill); line.Status != application.GristRefused || line.Reason != application.GristReasonResting {
				t.Fatalf("expected a refused resting line, got %+v", line)
			}
		})
	}
}

// A resting refusal is the mill's want of credit, not the sender's use of
// the day.
func TestARestingRefusalIsNoUseOfTheDay(t *testing.T) {
	mill, _, _, credits, _, _ := creditMill(t, application.SessionResult{})
	credits.Key = &apptest.FakeCreditKey{}
	mill.Ceilings.DailyLimit = 1
	for i := 0; i < 2; i++ {
		if err := mill.State.Append(context.Background(), application.GrindLine{
			Time: mill.Now(), Txid: fmt.Sprintf("direct:old%d", i), Sender: application.KeyFingerprint(smallMillPhone),
			Status: application.GristRefused, Reason: application.GristReasonResting,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := mill.State.Lines(context.Background())
	if last := lines[len(lines)-1]; last.Reason != application.GristReasonResting {
		t.Fatalf("expected the grist refused resting, not for the day's limit, got %+v", last)
	}
}

// An API-only person's grind billed to the key that finds the credit spent
// writes the mark and is answered out of credits until the renewal day; one
// that finds the key refused, resting. Neither is run again on the
// subscription (mw-gq6.1517, mw-gq6.1518).
func TestAnAPIOnlyPersonsGristSpentMidGrindIsRefusedAndNotRetried(t *testing.T) {
	for name, tc := range map[string]struct {
		onKey  application.SessionResult
		reason string
	}{
		"credit spent": {application.SessionResult{IsError: true, Subtype: "success", Said: "Credit balance is too low"},
			"The tutor is out of credits until Nov 17; send it again then."},
		"key refused": {application.SessionResult{IsError: true, Said: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`},
			application.GristReasonResting},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder, _, state, log := creditMill(t, tc.onKey)
			report, err := mill.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
				t.Fatalf("expected one grind on the key and none on the subscription, got %q", keys)
			}
			if _, found, _ := state.Mark(context.Background()); !found {
				t.Fatal("expected the spent mark written")
			}
			if evs, _ := log.Since(context.Background(), 0); len(evs) != 1 {
				t.Fatalf("expected one grist-credits-spent event, got %+v", evs)
			}
			if report.Refused != 1 || len(backend.Delivered()) != 1 {
				t.Fatalf("expected one refusal delivered, got %+v", report)
			}
			if answer := openFakeAnswer(t, backend.Delivered()[0]); answer.Status != application.GristRefused || answer.Reason != tc.reason {
				t.Fatalf("expected the grist refused %q, got %+v", tc.reason, answer)
			}
			if line := lineOf(t, mill); line.Billing != application.BillingAPI || line.Reason != tc.reason {
				t.Fatalf("expected a refused line billed to api, got %+v", line)
			}
		})
	}
}

// The factory's own test key keeps the mill's fallback: a spent key runs its
// grist again on the subscription.
func TestTheTestKeysGristStillFallsBackToTheSubscription(t *testing.T) {
	low := application.SessionResult{IsError: true, Subtype: "success", Said: "Credit balance is too low"}
	mill, _, grinder, _, _, _ := creditMill(t, low)
	mill.TestKey = smallMillPhone
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 2 || keys[0] != testAPIKey || keys[1] != "" {
		t.Fatalf("expected the key tried, then the subscription, got %q", keys)
	}
}

// creditTalks is a GristTalkStore in memory, its talk directory a test's own, which the
// grind makes its photos' directory in.
type creditTalks struct {
	dir   string
	mu    sync.Mutex
	talks map[application.GristTalkKey]application.GristTalk
}

func (m *creditTalks) Cwd(string) (string, error) { return m.dir, nil }
func (m *creditTalks) Load(_ context.Context, key application.GristTalkKey) (application.GristTalk, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	talk, found := m.talks[key]
	return talk, found, nil
}
func (m *creditTalks) Save(_ context.Context, talk application.GristTalk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.talks == nil {
		m.talks = map[application.GristTalkKey]application.GristTalk{}
	}
	m.talks[talk.GristTalkKey] = talk
	return nil
}
func (m *creditTalks) Remove(context.Context, application.GristTalkKey) error { return nil }
func (m *creditTalks) List(context.Context) ([]application.GristTalk, error)  { return nil, nil }
func (m *creditTalks) Transcript(string, string) (bool, error)                { return true, nil }
func (m *creditTalks) RemoveTranscript(string, string) error                  { return nil }

// talkCreditMill is creditMill whose one grist names a talk.
func talkCreditMill(t *testing.T, onKey application.SessionResult) (application.GristGrind, *apptest.FakePostern, *keyedGrinder, *apptest.FakeCreditState) {
	t.Helper()
	mill, _, _ := aSmallMill(t)
	millKey, _, _ := mill.Keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: smallMillPhone}
	photo, _ := sealer.EncryptBytes(millKey, []byte("a photo"))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, photo))
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1", Talk: "kitchen"}, Input: json.RawMessage(`{"place":"Top drawer"}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
	})
	ct, _ := sealer.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:t1", Class: application.GristClass, From: smallMillPhone, To: millKey,
		Signer: smallMillPhone, SignerApps: []string{"cairn"}, Ciphertext: ct})
	credits, state, _ := creditsFor()
	grinder := &keyedGrinder{onKey: onKey}
	mill.Postern, mill.Grinder, mill.Credits = backend, grinder, credits
	mill.Persons = markedPersons(smallMillPhone, application.PersonBillingAPI)
	mill.Now = func() time.Time { return creditsNow }
	mill.Talks = application.NewGristTalks(&creditTalks{dir: t.TempDir()})
	return mill, backend, grinder, state
}

// A talk's grist is billed by its sealer too: the Governor's to the
// subscription, an API-only person's to the key, and an API-only person's
// refused out of credits when the key's credit is spent, never on the
// subscription (mw-gq6.1517, mw-gq6.1518).
func TestATalksGristIsBilledBySealer(t *testing.T) {
	good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`), SessionID: "s1"}
	t.Run("the Governor's", func(t *testing.T) {
		mill, _, grinder, _ := talkCreditMill(t, good)
		mill.GovernorKey = smallMillPhone
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if keys := grinder.keys(); len(keys) != 1 || keys[0] != "" {
			t.Fatalf("expected the Governor's talk ground on the subscription, got %q", keys)
		}
		if line := lineOf(t, mill); line.Billing != application.BillingSubscription {
			t.Fatalf("expected billing subscription, got %+v", line)
		}
	})
	t.Run("another's", func(t *testing.T) {
		mill, _, grinder, _ := talkCreditMill(t, good)
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
			t.Fatalf("expected another's talk ground on the key, got %q", keys)
		}
		if line := lineOf(t, mill); line.Billing != application.BillingAPI {
			t.Fatalf("expected billing api, got %+v", line)
		}
	})
	t.Run("another's, spent", func(t *testing.T) {
		mill, backend, grinder, _ := talkCreditMill(t, application.SessionResult{IsError: true, Said: "Credit balance is too low"})
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
			t.Fatalf("expected one grind on the key and none on the subscription, got %q", keys)
		}
		if answer := openFakeAnswer(t, backend.Delivered()[0]); answer.Status != application.GristRefused || answer.Reason != outOfCreditsNov17 {
			t.Fatalf("expected the talk's grist answered out of credits, got %+v", answer)
		}
	})
	t.Run("another's, a standing mark", func(t *testing.T) {
		mill, _, grinder, state := talkCreditMill(t, good)
		state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-time.Hour)})
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(grinder.calls) != 0 {
			t.Fatalf("expected no grind, got %q", grinder.keys())
		}
		if line := lineOf(t, mill); line.Reason != outOfCreditsNov17 {
			t.Fatalf("expected an out-of-credits line, got %+v", line)
		}
	})
}

// outOfCreditsNov17 is the out-of-credits refusal at creditsNow (Oct 20), the
// credit renewing on the 17th.
const outOfCreditsNov17 = "The tutor is out of credits until Nov 17; send it again then."

// mw grist credits splits the month's grists and their list-price cost by
// what each was billed to, and by person: the Governor, a person by their id,
// a key with no person by its fingerprint (mw-gq6.1518).
func TestCreditsReportSplitsTheMonthByBillingAndByPerson(t *testing.T) {
	record := &apptest.FakeGristState{}
	governor, other := application.KeyFingerprint(smallMillPhone), application.KeyFingerprint(gristStrangerKey)
	for _, line := range []application.GrindLine{
		{Time: creditsNow.AddDate(0, -1, 0), Sender: other, Person: "p-ann", Billing: application.BillingAPI, CostUSD: 7},
		{Time: creditsNow, Sender: governor, Billing: application.BillingSubscription, CostUSD: 1.5},
		{Time: creditsNow, Sender: governor, Billing: application.BillingSubscription, CostUSD: 0.5},
		{Time: creditsNow, Sender: other, Person: "p-ann", Billing: application.BillingAPI, CostUSD: 2.25},
		{Time: creditsNow, Sender: other, Person: "p-ann", Billing: application.BillingNone, Status: application.GristRefused, Reason: outOfCreditsNov17},
		{Time: creditsNow, Sender: other, Billing: application.BillingSubscription, CostUSD: 0.75},
	} {
		if err := record.Append(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	admin := application.GristCreditsAdmin{
		Keys: &storeOf{present: true}, State: &apptest.FakeCreditState{}, Record: record,
		GovernorKey: smallMillPhone, Now: func() time.Time { return creditsNow },
	}
	report, err := admin.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	share := func(shares []application.GristCreditsShare, name string) application.GristCreditsShare {
		for _, s := range shares {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("expected a share named %q in %+v", name, shares)
		return application.GristCreditsShare{}
	}
	if api := share(report.ByBilling, "api"); api.Grists != 1 || api.CostUSD != 2.25 {
		t.Errorf("expected 1 grist at $2.25 billed to api, got %+v", api)
	}
	if sub := share(report.ByBilling, "subscription"); sub.Grists != 3 || sub.CostUSD != 2.75 {
		t.Errorf("expected 3 grists at $2.75 billed to the subscription, got %+v", sub)
	}
	if none := share(report.ByBilling, "none"); none.Resting != 1 {
		t.Errorf("expected 1 refused for want of credit billed to none, got %+v", none)
	}
	if gov := share(report.ByPerson, "the Governor"); gov.Grists != 2 || gov.CostUSD != 2 || gov.APIUSD != 0 {
		t.Errorf("expected the Governor's 2 grists at $2.00, none on the key, got %+v", gov)
	}
	if ann := share(report.ByPerson, "person p-ann"); ann.Grists != 1 || ann.APIUSD != 2.25 || ann.Resting != 1 {
		t.Errorf("expected p-ann's 1 grist at $2.25 on the key and 1 refused, got %+v", ann)
	}
	if key := share(report.ByPerson, "key "+other); key.Grists != 1 || key.CostUSD != 0.75 {
		t.Errorf("expected the unregistered key's 1 grist at $0.75, got %+v", key)
	}
	text := report.String()
	for _, want := range []string{"this month by billing:", "  api: 1 grist, $2.25", "  subscription: 3 grists, $2.75",
		"this month by person:", "  the Governor: 2 grists, $2.00 ($0.00 billed to the key)",
		"  person p-ann: 1 grist, $2.25 ($2.25 billed to the key), 1 refused for want of credit"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected the report to say %q, got:\n%s", want, text)
		}
	}
}

// A grist that ran no grind billed nothing, so its line says "none", not
// "subscription": a resting refusal of another licence's grist, whether its
// grist names a talk or not, and the key it was never ground on is not named
// either (mw-gq6.1520).
func TestARestingRefusalIsRecordedAsNotBilled(t *testing.T) {
	t.Run("a grist", func(t *testing.T) {
		mill, _, grinder, credits, _, _ := creditMill(t, application.SessionResult{})
		credits.Key = &apptest.FakeCreditKey{}
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(grinder.calls) != 0 {
			t.Fatalf("expected no grind, got %q", grinder.keys())
		}
		if line := lineOf(t, mill); line.Reason != application.GristReasonResting || line.Billing != application.BillingNone {
			t.Fatalf("expected a resting line billed to none, got billing %q in %+v", line.Billing, line)
		}
	})
	t.Run("a talk's grist", func(t *testing.T) {
		mill, _, grinder, state := talkCreditMill(t, application.SessionResult{})
		state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-time.Hour)})
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(grinder.calls) != 0 {
			t.Fatalf("expected no grind, got %q", grinder.keys())
		}
		if line := lineOf(t, mill); line.Reason != outOfCreditsNov17 || line.Billing != application.BillingNone {
			t.Fatalf("expected an out-of-credits line billed to none, got billing %q in %+v", line.Billing, line)
		}
	})
}
