package application_test

import (
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

// creditMill is aSmallMill billing through credits and a keyedGrinder.
func creditMill(t *testing.T, onKey application.SessionResult) (application.GristGrind, *apptest.FakePostern, *keyedGrinder, *application.GristCredits, *apptest.FakeCreditState, *apptest.FakeEventLog) {
	t.Helper()
	mill, backend, _ := aSmallMill(t)
	credits, state, log := creditsFor()
	grinder := &keyedGrinder{onKey: onKey}
	mill.Grinder, mill.Credits = grinder, credits
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

// With the switch off, or no usable key file, the line says subscription and
// the grind carries no key.
func TestAGristOnTheSubscriptionSaysSoAndCarriesNoKey(t *testing.T) {
	mill, _, grinder, credits, _, _ := creditMill(t, application.SessionResult{})
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

// A grist refused before any grind ran still says what it was billed to.
func TestARefusedGristSaysSubscription(t *testing.T) {
	mill, _, grinder, _, _, _ := creditMill(t, application.SessionResult{})
	mill.Ceilings.Models = []string{"opus"}
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(grinder.keys()) != 0 {
		t.Fatalf("expected no grind, got %q", grinder.keys())
	}
	if line := lineOf(t, mill); line.Status != application.GristRefused || line.Billing != application.BillingSubscription {
		t.Fatalf("expected a refused line billed to subscription, got %+v", line)
	}
}

// 'Credit balance is too low' writes the mark, posts one event, and runs the
// same grist again at once on the subscription: one answer, one line, one
// count against the day.
func TestACreditFailureRunsTheSameGristAgainOnTheSubscription(t *testing.T) {
	low := application.SessionResult{IsError: true, Subtype: "success", Said: "Credit balance is too low"}
	mill, backend, grinder, _, state, log := creditMill(t, low)
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
	mill, _, grinder, _, _, log := creditMill(t, bad)
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

// While the mark stands every grist uses the subscription.
func TestWhileSpentEveryGristUsesTheSubscription(t *testing.T) {
	mill, _, grinder, _, state, _ := creditMill(t, application.SessionResult{})
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
	mill, _, grinder, _, state, log := creditMill(t, low)
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
// subscription.
func TestWarmSessionsOnTheKeyAreRetiredWhenTheMarkIsWritten(t *testing.T) {
	ctx := context.Background()
	credits, state, _ := creditsFor()
	warmer := &apptest.FakeWarmGrinder{}
	now := creditsNow
	pool := &application.GristPool{Warmer: warmer, Slots: 2, TempDir: t.TempDir(), Now: func() time.Time { return now }, Credits: credits}

	mill, _, _, _, _, _ := creditMill(t, application.SessionResult{IsError: true, Said: "Credit balance is too low"})
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
