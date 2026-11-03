package application_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// memRequests is a GristRequestStore in memory.
type memRequests struct {
	records map[string]application.GristRequestRecord
}

func newMemRequests(records ...application.GristRequestRecord) *memRequests {
	m := &memRequests{records: map[string]application.GristRequestRecord{}}
	for _, r := range records {
		m.records[r.Bead] = r
	}
	return m
}

func (m *memRequests) Keep(_ context.Context, r application.GristRequestRecord) error {
	m.records[r.Bead] = r
	return nil
}

func (m *memRequests) Get(_ context.Context, bead string) (application.GristRequestRecord, bool, error) {
	r, ok := m.records[bead]
	return r, ok, nil
}

func (m *memRequests) List(context.Context) ([]application.GristRequestRecord, error) {
	var out []application.GristRequestRecord
	for _, r := range m.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bead < out[j].Bead })
	return out, nil
}

// statusPass is a mw grist status over a tracker with one epic whose stories
// are in the rig lampas on main, and a "plans" epic whose beads have no rig.
type statusPass struct {
	tracker  *apptest.FakeTracker
	postern  *apptest.FakePostern
	landings *apptest.FakeLandings
	requests *memRequests
}

func newStatusPass(records ...application.GristRequestRecord) *statusPass {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-app", domain.Path{Rig: "lampas", Branch: "main"})
	tracker.AddEpic("mw-plans", domain.Path{})
	return &statusPass{
		tracker: tracker, postern: apptest.NewFakePostern(),
		landings: apptest.NewFakeLandings(), requests: newMemRequests(records...),
	}
}

func (s *statusPass) closed(epic, id string) {
	s.tracker.AddStory(epic, domain.Story{ID: id, Title: id})
	_ = s.tracker.SetStatus(id, application.StatusClosed)
}

func (s *statusPass) run(t *testing.T) application.GristStatusReport {
	t.Helper()
	report, err := application.GristStatus{
		Postern: s.postern, Cipher: apptest.NewFakeCipher(), Keys: &fakeMillKey{},
		Requests: s.requests, Tracker: s.tracker, Landings: s.landings,
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// filedRecord is the record of a request linked to bead and told filed at ts.
func filedRecord(bead, request string, ts int64) application.GristRequestRecord {
	return application.GristRequestRecord{
		Bead: bead, Request: request, App: "lampas", Sender: "02ab",
		Last: &application.GristRequestStatus{Kind: application.GristRequestStatusKind, Request: request, Bead: bead, Status: application.GristRequestFiled, Ts: ts},
	}
}

// A closed bead of a rig that has no landing is still named, each pass: the
// request is told nothing and nothing else says why.
func TestAClosedBeadWithARigAndNoLandingIsStillNamed(t *testing.T) {
	s := newStatusPass(filedRecord("mw-story", "direct:aa", 1))
	s.closed("mw-app", "mw-story")

	report := s.run(t)

	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "mw-story: closed, but no landing of it was found in lampas on main") {
		t.Fatalf("expected one note naming mw-story, got %v", report.Notes)
	}
}

// A closed bead with no rig can never land: it is not named, and never as
// 'in  on '.
func TestAClosedBeadWithNoRigIsNotNamedAsMissingItsLanding(t *testing.T) {
	s := newStatusPass(filedRecord("mw-plan", "direct:aa", 1))
	s.closed("mw-plans", "mw-plan")

	report := s.run(t)

	if len(report.Notes) != 0 || len(report.Sent) != 0 {
		t.Fatalf("expected nothing noted or sent for a closed bead with no rig, got notes %v, sent %v", report.Notes, report.Sent)
	}
}

// A request whose link moved to another bead is told that bead's landing, and
// the bead it left is no longer named, though nothing marked its record moved.
func TestARequestWhoseLinkMovedReportsTheNewBeadsLandingAndLeavesTheOldBeadAlone(t *testing.T) {
	const request = "direct:aa"
	s := newStatusPass(filedRecord("mw-old", request, 1), filedRecord("mw-new", request, 2))
	s.closed("mw-app", "mw-old")
	s.closed("mw-app", "mw-new")
	s.landings.Land("mw-new", "0.5.0", "8dabab4")

	report := s.run(t)

	if len(report.Notes) != 0 {
		t.Fatalf("expected no note, got %v", report.Notes)
	}
	if len(report.Sent) != 1 || !strings.HasPrefix(report.Sent[0], "mw-new: landed (request direct:aa of lampas)") {
		t.Fatalf("expected mw-new told landed, got %v", report.Sent)
	}
	if got := s.requests.records["mw-old"].MovedTo; got != "mw-new" {
		t.Errorf("expected mw-old's record marked moved to mw-new, it says %q", got)
	}
	if again := s.run(t); len(again.Notes) != 0 || len(again.Sent) != 0 {
		t.Errorf("expected a second pass to say nothing, got notes %v, sent %v", again.Notes, again.Sent)
	}
}

// The bead a request moved from is let go; the one it moved to, closed with no
// landing yet, is named.
func TestARequestWhoseNewBeadHasNoLandingYetIsNamedOnTheNewBeadOnly(t *testing.T) {
	const request = "direct:aa"
	s := newStatusPass(filedRecord("mw-old", request, 1), filedRecord("mw-new", request, 2))
	s.closed("mw-app", "mw-old")
	s.closed("mw-app", "mw-new")

	report := s.run(t)

	if len(report.Notes) != 1 || !strings.HasPrefix(report.Notes[0], "mw-new: closed, but no landing") {
		t.Fatalf("expected one note, on mw-new, got %v", report.Notes)
	}
}

// A record moved on by hand (no MovedTo, a later record of the same request on
// a newer bead) is not told anything for the old bead, whatever state that
// bead is in: open and marked built differently, in progress, or closed with a
// landing. The request is followed on the newer bead.
func TestARecordMovedOnByHandIsToldNothingForTheOldBead(t *testing.T) {
	const request = "direct:aa"
	cases := []struct {
		name  string
		setup func(s *statusPass)
	}{
		{"open and marked built differently", func(s *statusPass) {
			s.tracker.AddStory("mw-app", domain.Story{ID: "mw-old", Title: "mw-old"})
			rec := s.requests.records["mw-old"]
			rec.Built = &application.GristBuilt{Note: "done another way"}
			s.requests.records["mw-old"] = rec
		}},
		{"in progress", func(s *statusPass) {
			s.tracker.AddStory("mw-app", domain.Story{ID: "mw-old", Title: "mw-old"})
			_ = s.tracker.SetStatus("mw-old", application.StatusInProgress)
		}},
		{"closed with a landing", func(s *statusPass) {
			s.closed("mw-app", "mw-old")
			s.landings.Land("mw-old", "0.4.0", "1111111")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStatusPass(filedRecord("mw-old", request, 1), filedRecord("mw-new", request, 2))
			tc.setup(s)
			s.tracker.AddStory("mw-app", domain.Story{ID: "mw-new", Title: "mw-new"})

			report := s.run(t)

			if len(report.Sent) != 0 || len(report.Notes) != 0 {
				t.Fatalf("expected nothing sent or noted for the old bead, got sent %v, notes %v", report.Sent, report.Notes)
			}
			if got := s.requests.records["mw-old"].MovedTo; got != "mw-new" {
				t.Errorf("expected mw-old's record marked moved to mw-new, it says %q", got)
			}
		})
	}
}

// A record that still follows its request (no later record of it elsewhere) is
// told its statuses as before.
func TestARecordThatStillFollowsItsRequestIsToldItsStatus(t *testing.T) {
	s := newStatusPass(filedRecord("mw-story", "direct:aa", 1))
	s.tracker.AddStory("mw-app", domain.Story{ID: "mw-story", Title: "mw-story"})
	_ = s.tracker.SetStatus("mw-story", application.StatusInProgress)

	report := s.run(t)

	if len(report.Sent) != 1 || !strings.HasPrefix(report.Sent[0], "mw-story: building (request direct:aa of lampas)") {
		t.Fatalf("expected mw-story told building, got sent %v, notes %v", report.Sent, report.Notes)
	}
}
