package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// parentFactory is a member's factory as the parent's note reads it: a
// tracker, a ledger, a fake backend and a clock in a zone five hours behind
// UTC, so that 07:00 local is 12:00 UTC.
type parentFactory struct {
	t       *testing.T
	tracker *apptest.FakeTracker
	files   *vault.Vault
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	sent    *apptest.FakeReportSentLog
	review  *reviewCounts
	zone    *time.Location
	out     bytes.Buffer
}

const parentGovernorKey = "parent-pubkey-hex"

func newParentFactory(t *testing.T) *parentFactory {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = sendMayorKey
	f := &parentFactory{
		t: t, tracker: apptest.NewFakeTracker(), files: vault.New(t.TempDir()),
		backend: apptest.NewFakePostern(), cipher: cipher, sent: &apptest.FakeReportSentLog{},
		review: &reviewCounts{}, zone: time.FixedZone("CDT", -5*3600),
	}
	f.tracker.AddEpic("lk-epic", domain.Path{})
	return f
}

// landed files a story of the epic, and a ledger row saying it landed on day.
func (f *parentFactory) landed(id, title, day string) {
	f.t.Helper()
	f.tracker.AddStory("lk-epic", domain.Story{ID: id, Title: title,
		Overrides: domain.Path{Rig: "game", Host: "laptop", Model: domain.Model("sonnet")}})
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.tracker.SetClosedAt(id, at.Add(10*time.Hour)); err != nil {
		f.t.Fatal(err)
	}
	if err := f.tracker.SetUpdated(id, at.Add(10*time.Hour)); err != nil {
		f.t.Fatal(err)
	}
	line := application.LedgerLine{When: at, StoryID: id, Title: title, Outcome: "landed on main",
		Path:   domain.Path{Model: domain.Model("sonnet"), Effort: domain.Effort("high")},
		Result: application.SessionResult{Fuel: application.Fuel{Input: 123456}}}.String()
	if err := f.files.AppendToLedger(context.Background(), "builder", line); err != nil {
		f.t.Fatal(err)
	}
}

// send is the note as the Mayor's loop runs it at the local clock now.
func (f *parentFactory) send(now time.Time, force bool) (string, error) {
	f.t.Helper()
	at := func() time.Time { return now }
	return application.ParentNoteSend{
		Report: application.FactoryReport{Tracker: f.tracker, Files: f.files, Seat: "builder", Review: f.review},
		Name:   "Luke",
		Hour:   7,
		Sent:   f.sent,
		Post: application.PosternSend{
			Postern: f.backend, Chain: apptest.NewFakeChain(f.backend, stubPosternKeys{pubKey: sendMayorKey}),
			Cipher: f.cipher, Keys: stubPosternKeys{pubKey: sendMayorKey},
			GovernorKey: parentGovernorKey, Channel: application.PosternChannelDirect, Now: at,
		},
		Force: force,
		Now:   at,
		Out:   &f.out,
	}.Run(context.Background())
}

// posted is the plaintext of the n-th message the backend was handed, and who
// it was addressed to.
func (f *parentFactory) posted(n int) (text, to string) {
	f.t.Helper()
	sent := f.backend.Delivered()
	if len(sent) <= n {
		f.t.Fatalf("expected at least %d posts, got %d", n+1, len(sent))
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(sent[n], &payload); err != nil {
		f.t.Fatal(err)
	}
	text, _, err := f.cipher.Decrypt("governor-private-key", payload.Ct)
	if err != nil {
		f.t.Fatal(err)
	}
	return text, payload.To
}

func (f *parentFactory) at(day string, hour, minute int) time.Time {
	f.t.Helper()
	d, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		f.t.Fatal(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, f.zone)
}

// One post at the configured local hour, to the parent's key, and none at any
// other hour of the day; the same hour again the same day posts nothing more.
func TestParentNotePostsOnceAtTheConfiguredLocalHourAndAtNoOther(t *testing.T) {
	f := newParentFactory(t)
	f.landed("lk-1", "Build the dragon menu", "2026-10-27")

	for hour := 0; hour < 24; hour++ {
		if hour == 7 {
			continue
		}
		if txid, err := f.send(f.at("2026-10-28", hour, 5), false); err != nil || txid != "" {
			t.Fatalf("at %02d:05 got txid %q, err %v; want nothing sent", hour, txid, err)
		}
	}
	if got := len(f.backend.Delivered()); got != 0 {
		t.Fatalf("%d posts outside the hour", got)
	}

	txid, err := f.send(f.at("2026-10-28", 7, 2), false)
	if err != nil || !strings.HasPrefix(txid, "direct:") {
		t.Fatalf("at 07:02 got txid %q, err %v; want a post", txid, err)
	}
	if text, to := f.posted(0); to != parentGovernorKey || text == "" {
		t.Fatalf("posted %q to %q; want text to the parent's key %q", text, to, parentGovernorKey)
	}
	if txid, err := f.send(f.at("2026-10-28", 7, 40), false); err != nil || txid != "" {
		t.Fatalf("a second run in the hour got %q, %v; want nothing", txid, err)
	}
	if got := len(f.backend.Delivered()); got != 1 {
		t.Fatalf("%d posts in the day, want 1", got)
	}
	// The next morning it posts again.
	if txid, err := f.send(f.at("2026-10-29", 7, 0), false); err != nil || txid == "" {
		t.Fatalf("the next 07:00 got %q, %v; want a post", txid, err)
	}
}

// The configured hour is the member's local one, not UTC's: 07:00 in a zone
// five hours behind is 12:00 UTC, and 07:00 UTC is a different local hour.
func TestParentNoteReadsTheHourInTheClocksOwnZone(t *testing.T) {
	f := newParentFactory(t)
	utcSeven := time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC).In(f.zone)
	if txid, err := f.send(utcSeven, false); err != nil || txid != "" {
		t.Fatalf("02:00 local got %q, %v; want nothing", txid, err)
	}
	localSeven := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC).In(f.zone)
	if txid, err := f.send(localSeven, false); err != nil || txid == "" {
		t.Fatalf("07:00 local got %q, %v; want a post", txid, err)
	}
}

var storyDetail = regexp.MustCompile(`(?i)\blk-\w|\bmw-\w|\btokens?\b|\$\d|\bsonnet\b|\bopus\b|\bhigh\b|123456|\bdragon\b|\bgame\b`)

// The note says in plain words what moved, and carries no bead id, no story
// title, no rig, model or fuel figure.
func TestParentNoteHasNoBeadIDNoStoryTitleAndNoFuel(t *testing.T) {
	f := newParentFactory(t)
	f.landed("lk-1", "Build the dragon menu", "2026-10-27")
	f.landed("lk-2", "Fix the score bug", "2026-10-27")
	f.landed("lk-3", "Add sounds", "2026-10-28")
	f.review.queue = 2

	if _, err := f.send(f.at("2026-10-28", 7, 0), false); err != nil {
		t.Fatal(err)
	}
	text, _ := f.posted(0)
	if !strings.Contains(text, "Luke") {
		t.Errorf("the note does not name the member: %q", text)
	}
	for _, bad := range []string{"lk-1", "lk-2", "lk-3", "lk-epic", "Build the dragon menu", "Fix the score bug", "Add sounds"} {
		if strings.Contains(text, bad) {
			t.Errorf("the note carries %q: %s", bad, text)
		}
	}
	if m := storyDetail.FindString(text); m != "" {
		t.Errorf("the note carries story or fuel detail %q: %s", m, text)
	}
	if !strings.Contains(text, "fair amount") {
		t.Errorf("three landings should read as a fair amount: %s", text)
	}
	if !strings.Contains(text, "1 area") {
		t.Errorf("one epic moved, the note should say so in plain words: %s", text)
	}
	if !strings.Contains(text, "2 things") || !strings.Contains(text, "waiting") {
		t.Errorf("the two cards waiting should be told: %s", text)
	}
}

// A day with nothing moved posts one line saying so.
func TestParentNoteOnADayNothingMovedIsOneLine(t *testing.T) {
	f := newParentFactory(t)
	// A landing a week ago is not this day's.
	f.landed("lk-1", "Build the dragon menu", "2026-10-20")

	if _, err := f.send(f.at("2026-10-28", 7, 0), false); err != nil {
		t.Fatal(err)
	}
	text, _ := f.posted(0)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 1 || !strings.Contains(text, "Nothing moved") || !strings.Contains(text, "Luke") {
		t.Errorf("want one line saying nothing moved in Luke's factory, got %d lines: %q", len(lines), text)
	}
	if got := len(f.backend.Delivered()); got != 1 {
		t.Errorf("%d posts, want 1", got)
	}
}

// --force posts at any hour, and the day sent is remembered apart from the
// factory report's own, which shares the log.
func TestParentNoteForceSendsOutOfHourAndTheDayIsItsOwn(t *testing.T) {
	f := newParentFactory(t)
	if txid, err := f.send(f.at("2026-10-28", 15, 0), true); err != nil || txid == "" {
		t.Fatalf("--force got %q, %v; want a post", txid, err)
	}
	days := f.sent.All()
	if len(days) != 1 || !strings.HasPrefix(days[0].Day, "parent-") {
		t.Errorf("the day remembered is %+v; want one starting parent-", days)
	}
	// The factory report's day of the same date does not stop the note.
	g := newParentFactory(t)
	g.sent.Record(context.Background(), application.ReportSent{Day: "2026-10-28"})
	if txid, err := g.send(g.at("2026-10-28", 7, 0), false); err != nil || txid == "" {
		t.Errorf("a factory report sent the same day stopped the note: %q, %v", txid, err)
	}
}
