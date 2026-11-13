package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// hlCheck is the check on the laptop, which the home file says is home, at
// Claude Code home, over the hosts desktop and cloud1 (a box), each with the
// version in versions as its last sync left it (a host not in versions left
// none).
func hlCheck(t *testing.T, home string, versions map[string]string) *doctor.HarnessLevel {
	t.Helper()
	notes := apptest.NewFakeDoctorNotes()
	for host, version := range versions {
		_ = notes.SetNote(context.Background(), application.HarnessKey(host), version)
	}
	check := doctor.NewHarnessLevel(pcThisHost, "laptop", notes)
	check.Version = func(context.Context) (string, error) { return home, nil }
	check.Hosts = func(context.Context) ([]doctor.HarnessHost, error) {
		return []doctor.HarnessHost{{Name: "desktop"}, {Name: "cloud1", Box: true}}, nil
	}
	return check
}

func TestTheHarnessLevelProbeIsFaultyForAHostBehindTheHomeNamingBothVersionsAndTheCure(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322", "cloud1": "2.1.300"})
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"cloud1", "2.1.300", "2.1.322", "vultr-boost set-harness --name cloud1 --version 2.1.322"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to hold %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "desktop") {
		t.Errorf("expected the level desktop left out, got %q", reason)
	}
}

func TestTheHarnessLevelCureForAHostWithTheNativeInstallIsClaudeInstall(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.310", "cloud1": "2.1.322"})
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if want := "desktop"; !strings.Contains(reason, want) || !strings.Contains(reason, "claude install 2.1.322") || !strings.Contains(reason, "2.1.310") {
		t.Errorf("expected the host, its version and `claude install 2.1.322`, got %q", reason)
	}
}

func TestTheHarnessLevelProbeIsOkWhenEveryHostIsLevelOrNewer(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322", "cloud1": "2.1.330"})
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelProbeComparesAsNumbers(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.1000", "cloud1": "2.1.99"})
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "cloud1") || strings.Contains(reason, "desktop") {
		t.Fatalf("expected only cloud1 faulty, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelProbeSaysAHostItCannotReadIsUnreadAndNeverGuessesIt(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322"})
	verdict, reason := check.Probe(context.Background())
	if verdict == application.DoctorFaulty || verdict == application.DoctorOK {
		t.Fatalf("expected cannot-tell for a host left unread, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "cloud1") || !strings.Contains(reason, "unread") {
		t.Errorf("expected cloud1 said unread, got %q", reason)
	}
	// A note that is no version is unread too.
	check = hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322", "cloud1": "claude: not installed"})
	if verdict, reason = check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "cloud1 unread") {
		t.Errorf("expected cloud1 unread, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelProbeSaysBothAHostBehindAndAHostUnread(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.300"})
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "desktop") || !strings.Contains(reason, "cloud1 unread") {
		t.Fatalf("expected the faulty host and the unread one, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelProbeCannotTellWhenTheHomeCannotSayItsVersion(t *testing.T) {
	check := hlCheck(t, "", map[string]string{"desktop": "2.1.300"})
	check.Version = func(context.Context) (string, error) { return "", errors.New("claude: not installed") }
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "not installed") {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelProbeIsNotApplicableOnAHostThatIsNotHome(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.300"})
	check.Host = "desktop"
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || !strings.Contains(reason, "n/a") {
		t.Fatalf("expected ok, n/a, got %s (%s)", verdict, reason)
	}
}

func TestTheHarnessLevelCureOnlyReports(t *testing.T) {
	check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322", "cloud1": "2.1.300"})
	check.Probe(context.Background())
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "cloud1") {
		t.Errorf("expected the cure to fail saying what is behind, got %v", err)
	}
}

// mw-gq6.1209: a harness note a box that is gone left, or one written before the
// box now holding the name was made, is none of that box's: the check reads it
// as no version, never as the old box's Claude Code.
func TestHarnessLevelLeavesOutTheNoteOfAnOlderBox(t *testing.T) {
	ctx := context.Background()
	made := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	plan := application.CloudPlan{Prefix: "cloud"}
	for _, c := range []struct {
		name     string
		synced   time.Time // when cloud1 last synced; zero leaves no last-sync note
		boxes    []application.CloudBox
		wantWord string
		verdict  application.Verdict
	}{
		{"a note from before the box was made", made.Add(-time.Hour), []application.CloudBox{{Name: "cloud1", Up: made}}, "unread", application.DoctorCannotTell},
		{"a note with no last sync beside it", time.Time{}, []application.CloudBox{{Name: "cloud1", Up: made}}, "unread", application.DoctorCannotTell},
		{"a note of a box the book no longer holds", made.Add(-time.Hour), nil, "unread", application.DoctorCannotTell},
		{"a note from after the box was made", made.Add(time.Hour), []application.CloudBox{{Name: "cloud1", Up: made}}, "behind", application.DoctorFaulty},
	} {
		t.Run(c.name, func(t *testing.T) {
			check := hlCheck(t, "2.1.322", map[string]string{"desktop": "2.1.322", "cloud1": "2.1.300"})
			notes := check.Notes.(*apptest.FakeDoctorNotes)
			if !c.synced.IsZero() {
				_ = notes.SetNote(ctx, application.LastSyncKey("cloud1"), c.synced.Format(application.LastSyncFormat))
			}
			check.Book = &apptest.FakeCloudBook{State: application.CloudState{Boxes: c.boxes}}
			check.Plan = plan
			verdict, reason := check.Probe(ctx)
			if verdict != c.verdict {
				t.Fatalf("expected %s, got %s (%s)", c.verdict, verdict, reason)
			}
			if !strings.Contains(reason, "cloud1") || !strings.Contains(reason, c.wantWord) {
				t.Errorf("expected the reason to name cloud1 and %q, got %q", c.wantWord, reason)
			}
		})
	}
}
