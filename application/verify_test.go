package application_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var verifyNow = time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)

func verifyDemo(id, epic, text string) application.StoryDetail {
	return application.StoryDetail{
		Story: domain.Story{ID: id, Title: "Demo " + id}, Status: application.StatusOpen,
		Labels: []string{application.LabelDemo, "hitl"}, Description: text, EpicID: epic,
	}
}

func verifyClosed(id, epic, reason string, ago time.Duration) application.ClosedStory {
	return application.ClosedStory{ID: id, Title: "Story " + id, Type: "task", EpicID: epic, CloseReason: reason, ClosedAt: verifyNow.Add(-ago)}
}

func verifyComment(text string) []application.Comment {
	return []application.Comment{{Author: "mw", Created: verifyNow, Text: text}}
}

const closingWithCheck = "Built it.\n\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n2. Tap Save and see the green tick.\n\nWhat's new: New: a save button\n\nFor the rig memory:\nnothing"

func TestVerifyCandidatesAreOpenDemosAndUnverifiedLandings(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Work: application.WorkInHand{Ready: []application.StoryDetail{
			verifyDemo("d-open", "e-1", "Open the app and look for the green tick."),
			verifyDemo("d-done", "e-1", "A demo he has already verified."),
			{Story: domain.Story{ID: "plain", Title: "no demo label"}, Status: application.StatusOpen},
		}},
		Closed: []application.ClosedStory{
			verifyClosed("s-new", "e-1", "landed on main", 2*24*time.Hour),
			verifyClosed("s-verified", "e-1", "landed on main", 2*24*time.Hour),
			verifyClosed("s-superseded", "e-1", "superseded by s-new", 2*24*time.Hour),
			verifyClosed("s-never", "e-1", "never released: dropped", 2*24*time.Hour),
			verifyClosed("s-old", "e-2", "landed on main", 20*24*time.Hour),
			verifyClosed("s-step", "e-1", "landed", time.Hour),
			{ID: "s-linked", Title: "linked", Type: "task", ClosedAt: verifyNow.Add(-time.Hour), Superseded: true},
			{ID: "s-epicchild", Title: "an epic", Type: "epic", ClosedAt: verifyNow.Add(-time.Hour)},
		},
		Comments: map[string][]application.Comment{
			"s-new":        verifyComment(closingWithCheck),
			"s-verified":   verifyComment("VERIFIED by the Governor's use, from direct:x"),
			"s-superseded": verifyComment(closingWithCheck),
			"s-never":      verifyComment(closingWithCheck),
			"s-old":        verifyComment(closingWithCheck),
			"s-linked":     verifyComment(closingWithCheck),
			"d-done":       verifyComment("VERIFIED by the Governor's use, from direct:y"),
		},
	}
	beads.Closed[5].ID = "mw-mol-step"
	beads.Comments["mw-mol-step"] = verifyComment(closingWithCheck)

	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c.Bead)
	}
	if want := "d-open s-new"; strings.Join(ids, " ") != want {
		t.Fatalf("candidates = %v, want %s", ids, want)
	}
	demo := got[0]
	if demo.Kind != application.VerifyKindDemo || !strings.Contains(demo.Check, "green tick.") || !strings.Contains(demo.Check, "Tap Save and see the green tick") {
		t.Fatalf("the demo carries its description and its epic's closed story's check, got %q", demo.Check)
	}
	if strings.Contains(demo.Check, "For the rig memory") || strings.Contains(demo.Check, "What's new") {
		t.Fatalf("the check text stops at the HOW TO CHECK IT section, got %q", demo.Check)
	}
	if got[1].Kind != application.VerifyKindLanding || !strings.HasPrefix(got[1].Check, "HOW TO CHECK IT") {
		t.Fatalf("the landing's check is its section, got %+v", got[1])
	}
}

func TestVerifyCandidatesLeaveOutALandingWithNoSection(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Closed:   []application.ClosedStory{verifyClosed("s-internal", "", "landed", time.Hour)},
		Comments: map[string][]application.Comment{"s-internal": verifyComment("Internal: nothing for the Governor to look at")},
	}
	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no candidate, got %+v (%v)", got, err)
	}
}

func TestVerifyEvidenceReadsAMessageAndItsPictureByTxid(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		at := verifyNow.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("direct-aaa.txt", time.Hour)
	write("direct-aaa.jpg", time.Hour)
	write("direct-bbb.png", 2*time.Hour)
	write("direct-old.txt", 30*24*time.Hour)
	write("direct-aaa.message.json", time.Hour)
	write("direct-voice.webm", time.Hour)

	got, err := application.ReadVerifyEvidence(dir, verifyNow.Add(-7*24*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Txid != "direct:bbb" || got[1].Txid != "direct:aaa" {
		t.Fatalf("expected direct:bbb then direct:aaa, got %+v", got)
	}
	if len(got[1].Files) != 2 || !strings.HasSuffix(got[1].Files[0], "direct-aaa.jpg") || !strings.HasSuffix(got[1].Files[1], "direct-aaa.txt") {
		t.Fatalf("expected the picture and the text of direct:aaa, got %v", got[1].Files)
	}

	only, err := application.ReadVerifyEvidence(dir, verifyNow, []string{"direct:old"})
	if err != nil || len(only) != 1 || only[0].Txid != "direct:old" {
		t.Fatalf("a txid given is read whatever its age, got %+v (%v)", only, err)
	}
	if _, err := application.ReadVerifyEvidence(dir, verifyNow, []string{"direct:nope"}); err == nil || !strings.Contains(err.Error(), "direct:nope") {
		t.Fatalf("a txid with no file is an error naming it, got %v", err)
	}
}

// The deploy check of mw verify <id> (mw-gq6.760): the deployed marker, the
// healthz URL and the strings a bead names, against a fake marker file, a fake
// HTTP server and a fake bundle.

const deployedCommit = "8fde6ab55a1d5ccbad7d1cfdc8190382679d4874"

// servedBuild is a bundle that carries the build the landing of the fixture made.
const servedBuild = `var v="0.1.20";var c="8fde6ab";`

// voteString holds a $, which a shell would have expanded.
const voteString = "Vote for tonight's best trunk and you could win a $150 Amazon gift card"

type deployMarks map[string]string

func (m deployMarks) Built(_ context.Context, key string) (string, error) { return m[key], nil }
func (m deployMarks) MarkBuilt(_ context.Context, key, commit string) error {
	m[key] = commit
	return nil
}

// httpWeb is a VerifyWeb that asks a real, local, fake server.
type httpWeb struct{}

func (httpWeb) Get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// boardServer serves a health answer, an index page naming one hashed bundle
// and that bundle, which holds bundleText.
func boardServer(t *testing.T, health string, healthStatus int, bundleText string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/leaderboard/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(healthStatus)
		_, _ = io.WriteString(w, health)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `<html><head><script type="module" crossorigin src="/assets/index-Ab12_cD.js"></script></head></html>`)
	})
	mux.HandleFunc("/assets/index-Ab12_cD.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, bundleText)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// deployHistory is a CommitHistory that knows only the answers it was given,
// keyed "<outer> <inner>"; any other pair is a commit the checkout lacks.
type deployHistory map[string]bool

func (h deployHistory) Contains(_ context.Context, _, outer, inner string) (bool, error) {
	if held, ok := h[outer+" "+inner]; ok {
		return held, nil
	}
	return false, errors.New("fatal: Not a valid commit name " + inner)
}

// deployAfter is an AfterLanding that names a command only for the rigs it holds.
type deployAfter map[string]string

func (a deployAfter) Command(rig string) string { return a[rig] }
func (deployAfter) Run(context.Context, string, string) (application.Ran, error) {
	return application.Ran{}, nil
}

type deployFixture struct {
	beads   *apptest.FakeVerifyBeads
	rules   *apptest.FakeEpicRules
	marks   deployMarks
	history deployHistory
	after   deployAfter
	out     *strings.Builder
}

func newDeployFixture(server *httptest.Server, healthz bool, description string, comments ...string) deployFixture {
	f := deployFixture{
		beads:   &apptest.FakeVerifyBeads{Details: map[string]application.StoryDetail{}, Comments: map[string][]application.Comment{}},
		rules:   apptest.NewFakeEpicRules(),
		marks:   deployMarks{"leaderboard.deployed": deployedCommit},
		history: deployHistory{},
		after:   deployAfter{"leaderboard": "make deploy"},
		out:     &strings.Builder{},
	}
	f.beads.Details["lb-1"] = application.StoryDetail{
		Story:       domain.Story{ID: "lb-1", Title: "vote link", Overrides: domain.Path{Rig: "leaderboard"}},
		Status:      "closed",
		Description: description,
		CloseReason: "landed on main in batch 20 of leaderboard (batch commit 8fde6ab55a1d, its fast-forward dac87a77c1cf, version 0.1.20), 1 commits, the rig's tests on the batch: pass",
	}
	for _, text := range comments {
		f.beads.Comments["lb-1"] = append(f.beads.Comments["lb-1"], application.Comment{Author: "builder", Text: text})
	}
	requirements := domain.EpicRequirements{}
	if server != nil {
		requirements.SiteURL = server.URL + "/"
		if healthz {
			requirements.HealthzURL = server.URL + "/api/leaderboard/healthz"
		}
	}
	f.rules.Require("leaderboard", requirements)
	return f
}

func (f deployFixture) run(t *testing.T) (application.VerifyDeployResult, string) {
	t.Helper()
	result, err := application.VerifyDeploy{
		Beads: f.beads, Rules: f.rules, Marks: f.marks, Web: httpWeb{}, Out: f.out,
		History: f.history, After: f.after, Dirs: map[string]string{"leaderboard": "/rigs/leaderboard"},
	}.Run(context.Background(), "lb-1")
	if err != nil {
		t.Fatal(err)
	}
	return result, f.out.String()
}

func TestVerifyDeployPassesWhenMarkerHealthzAndStringsAllAgree(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, `var a="x";var b="`+voteString+`";`)
	f := newDeployFixture(server, true, "The vote card.\n\nVerify strings:\n"+voteString+"\n- `var a=\"x\"`\n\nMore text.", "Done.", "Verify strings: var b=")

	result, out := f.run(t)

	if result.Failed() {
		t.Fatalf("expected a pass, got %+v\n%s", result, out)
	}
	for _, want := range []string{
		"deploy OK", "8fde6ab55a1d", "healthz OK", "ok",
		"found: " + voteString, "found: var a=\"x\"", "found: var b=",
		"verify lb-1: deploy OK, healthz OK, strings 3/3 found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestVerifyDeployCountsAStringNotInTheBundleAndFails(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, `var b="`+voteString+`";`)
	f := newDeployFixture(server, true, "Verify strings:\n"+voteString+"\nnot in the bundle at all")

	result, out := f.run(t)

	if !result.Failed() {
		t.Fatalf("a missing string must fail, got %+v\n%s", result, out)
	}
	for _, want := range []string{"found: " + voteString, "not found: not in the bundle at all", "strings 1/2 found"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestVerifyDeployFindsAStringWhoseApostropheTheBundleEscapes(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, `var b='Vote for tonight\'s best trunk';`)
	f := newDeployFixture(server, true, "Verify strings:\nVote for tonight's best trunk")

	result, out := f.run(t)

	if result.Failed() || !strings.Contains(out, "strings 1/1 found") {
		t.Fatalf("expected the escaped apostrophe to be found, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployFailsOnAMarkerThatIsNotTheLandingCommit(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, "")
	f := newDeployFixture(server, true, "")
	f.marks["leaderboard.deployed"] = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	f.history["1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 8fde6ab55a1d"] = false

	result, out := f.run(t)

	if result.Deploy != application.VerifyMismatch || !result.Failed() || !strings.Contains(out, "deploy MISMATCH") || !strings.Contains(out, "1111111") || !strings.Contains(out, "8fde6ab55a1d") {
		t.Fatalf("expected a MISMATCH naming both commits for a landing the deployed commit lacks, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployPassesWhenTheLandingIsContainedInALaterDeployedCommit(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, servedBuild)
	f := newDeployFixture(server, true, "")
	later := "e67ce02156e0534021d0e9ec601a4b9f666e8721"
	f.marks["leaderboard.deployed"] = later
	f.history[later+" 8fde6ab55a1d"] = true

	result, out := f.run(t)

	if result.Deploy != application.VerifyOK || result.Failed() || !strings.Contains(out, "deploy OK") || !strings.Contains(out, "contained in the deployed commit") || strings.Contains(out, "MISMATCH") {
		t.Fatalf("expected deploy OK saying the landing is contained, got %+v\n%s", result, out)
	}
}

func TestVerifyDeploySaysItCouldNotTellForACommitTheCheckoutDoesNotKnow(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, "")
	f := newDeployFixture(server, true, "")
	f.marks["leaderboard.deployed"] = "e67ce02156e0534021d0e9ec601a4b9f666e8721"

	result, out := f.run(t)

	if result.Deploy == application.VerifyOK || result.Deploy == application.VerifyMismatch || !result.Failed() || !strings.Contains(out, "could not tell") || strings.Contains(out, "deploy OK") {
		t.Fatalf("expected a could-not-tell that is never OK, got %+v\n%s", result, out)
	}
}

func TestVerifyDeploySaysTheDeployIsNotTrackedForARigThatKeepsNoDeployMark(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, servedBuild)
	f := newDeployFixture(server, true, "")
	delete(f.marks, "leaderboard.deployed")
	delete(f.after, "leaderboard")

	result, out := f.run(t)

	if result.Deploy == application.VerifyMismatch || !strings.Contains(out, "not tracked here") || strings.Contains(out, "deploy MISMATCH") {
		t.Fatalf("expected the deploy to be called not tracked, not a MISMATCH, got %+v\n%s", result, out)
	}
	if result.Failed() {
		t.Fatalf("a rig that keeps no deploy tracking must not fail the verify, got %+v\n%s", result, out)
	}
	if line := result.Line("lb-1"); !strings.Contains(line, "deploy not tracked") {
		t.Fatalf("expected the summary line to say not tracked, got %q", line)
	}
}

func TestVerifyDeployStillCallsAMissingMarkOfARigThatDeploysAMismatch(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, "")
	f := newDeployFixture(server, true, "")
	delete(f.marks, "leaderboard.deployed")

	result, out := f.run(t)

	if result.Deploy != application.VerifyMismatch || !strings.Contains(out, "deploy MISMATCH") || !strings.Contains(out, "no deployed mark") {
		t.Fatalf("expected a MISMATCH for a rig with a deploy command and no mark, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployFailsOnADeployFailedMarkAtTheLandingCommit(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, "")
	f := newDeployFixture(server, true, "")
	f.marks["leaderboard.deploy-failed"] = deployedCommit + " 2"

	result, out := f.run(t)

	if !result.Failed() || !strings.Contains(out, "deploy MISMATCH") || !strings.Contains(out, "deploy-failed") {
		t.Fatalf("expected a failed deploy to be named, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployFailsWhenHealthzDoesNotAnswerWell(t *testing.T) {
	server := boardServer(t, "down", http.StatusServiceUnavailable, "")
	f := newDeployFixture(server, true, "")

	result, out := f.run(t)

	if !result.Failed() || !strings.Contains(out, "healthz FAIL") || !strings.Contains(out, "503") {
		t.Fatalf("expected healthz FAIL with the status, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployWithNoStringsSaysSoAndStillRunsTheOtherChecks(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, servedBuild)
	f := newDeployFixture(server, true, "Nothing to grep here.", "A comment.")

	result, out := f.run(t)

	if result.Failed() {
		t.Fatalf("no strings is no failure, got %+v\n%s", result, out)
	}
	for _, want := range []string{"no strings named on the bead", "deploy OK", "healthz OK", "verify lb-1: deploy OK, healthz OK, strings 0/0 found"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestVerifyDeployWithNoHealthzKeySaysSoAndFailsOnlyOnTheOtherChecks(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, `var b="here";`)
	f := newDeployFixture(server, false, "Verify strings:\nhere")

	result, out := f.run(t)

	if result.Failed() || !strings.Contains(out, "no healthz for leaderboard") || !strings.Contains(out, "healthz none") {
		t.Fatalf("expected no healthz to pass quietly, got %+v\n%s", result, out)
	}

	f = newDeployFixture(server, false, "Verify strings:\nnot here")
	if result, out = f.run(t); !result.Failed() || !strings.Contains(out, "no healthz for leaderboard") {
		t.Fatalf("a missing string still fails, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployNamesTheLandingCommitFromAnEachLandingToo(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, servedBuild)
	f := newDeployFixture(server, true, "")
	detail := f.beads.Details["lb-1"]
	detail.CloseReason = "landed on main (fast-forward, 8fde6ab, version 0.1.4)"
	f.beads.Details["lb-1"] = detail

	result, out := f.run(t)

	if result.Failed() || !strings.Contains(out, "deploy OK") {
		t.Fatalf("expected the each-landing commit to be read, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployFailsWhenTheBeadNamesNoLandingCommit(t *testing.T) {
	server := boardServer(t, "ok", http.StatusOK, "")
	f := newDeployFixture(server, true, "")
	detail := f.beads.Details["lb-1"]
	detail.CloseReason = "never released: dropped"
	f.beads.Details["lb-1"] = detail

	result, out := f.run(t)

	if !result.Failed() || !strings.Contains(out, "deploy MISMATCH") || !strings.Contains(out, "no landing commit") {
		t.Fatalf("expected MISMATCH for a bead that names no commit, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployStringsSayNoSiteWhenTheRigHasNone(t *testing.T) {
	f := newDeployFixture(nil, false, "Verify strings:\nhere")

	result, out := f.run(t)

	if !result.Failed() || !strings.Contains(out, "no site_url for leaderboard") || !strings.Contains(out, "strings 0/1 found") {
		t.Fatalf("expected the missing site to fail the strings, got %+v\n%s", result, out)
	}
}

func TestVerifyDeployRefusesABeadTheTrackerDoesNotHold(t *testing.T) {
	f := newDeployFixture(nil, false, "")
	_, err := application.VerifyDeploy{Beads: f.beads, Rules: f.rules, Marks: f.marks, Web: httpWeb{}, Out: f.out}.Run(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected an error naming the bead, got %v", err)
	}
}

func TestVerifyCandidatesTakeTheSectionNotALaterNoteThatMentionsTheWords(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Closed:   []application.ClosedStory{verifyClosed("s-new", "", "landed", time.Hour)},
		Comments: map[string][]application.Comment{"s-new": append(verifyComment(closingWithCheck), application.Comment{Author: "mw", Created: verifyNow, Text: "Sent to his phone; HOW TO CHECK IT posted in his thread, direct:abc"})},
	}
	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected one candidate, got %+v (%v)", got, err)
	}
	if !strings.Contains(got[0].Check, "Tap Save and see the green tick") {
		t.Fatalf("expected the builder's section, got %q", got[0].Check)
	}
}

// servedSite is a fake host that serves a rig's page and one bundle, and counts
// the requests it was asked.
type servedSite struct {
	server *httptest.Server
	asked  *int
}

func newServedSite(t *testing.T, bundleText string) servedSite {
	t.Helper()
	asked := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.URL.Path == "/assets/index-Zz90.js" {
			_, _ = io.WriteString(w, bundleText)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `<html><head><script type="module" src="/assets/index-Zz90.js"></script></head></html>`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return servedSite{server: server, asked: &asked}
}

// movedPage is a Pages-like host that serves only a page that has moved.
func newMovedPage(t *testing.T) servedSite {
	t.Helper()
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "<html><body>This page has moved.</body></html>")
	}))
	t.Cleanup(server.Close)
	return servedSite{server: server, asked: &asked}
}

func TestVerifyReadsTheServedBuildOfTheSiteURL(t *testing.T) {
	pages := newMovedPage(t)

	// The landing is version 0.1.20 at batch commit 8fde6ab55a1d.
	for _, c := range []struct {
		name, bundle string
		fails        bool
		want         string
	}{
		{"another build", `var v="0.1.19";`, true, "deploy MISMATCH"},
		{"the landing's version", `var v="0.1.20";`, false, "build OK"},
		{"the landing's commit", `var c="8fde6ab";`, false, "build OK"},
	} {
		t.Run(c.name, func(t *testing.T) {
			site := newServedSite(t, c.bundle)
			f := newDeployFixture(nil, false, "No strings named here.")
			f.rules.Require("leaderboard", domain.EpicRequirements{SiteURL: site.server.URL + "/"})

			result, out := f.run(t)

			if result.Failed() != c.fails {
				t.Fatalf("Failed() = %v, want %v\n%s", result.Failed(), c.fails, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output lacks %q:\n%s", c.want, out)
			}
			if *site.asked == 0 {
				t.Errorf("the host named by site_url was not read:\n%s", out)
			}
			if *pages.asked != 0 {
				t.Errorf("the Pages-like host was read %d time(s)", *pages.asked)
			}
		})
	}
}

func TestVerifyDeployAPageThatCannotBeReadIsNoPassForTheBuild(t *testing.T) {
	pages := newMovedPage(t)
	f := newDeployFixture(nil, false, "No strings named here.")
	f.rules.Require("leaderboard", domain.EpicRequirements{SiteURL: pages.server.URL + "/missing"})

	result, out := f.run(t)

	if !result.Failed() || !strings.Contains(out, "build FAIL") {
		t.Fatalf("expected the unread page to fail the build, got %+v\n%s", result, out)
	}
}

func TestVerifyOfARigWithNoSiteURLSaysTheLiveSiteWasNotRead(t *testing.T) {
	f := newDeployFixture(nil, false, "No strings named here.")

	result, out := f.run(t)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "no site_url") || !strings.Contains(last, "the live site was not read") {
		t.Fatalf("the last line must say the live site was not read, got %q\n%s", last, out)
	}
	if strings.HasSuffix(last, "strings 0/0 found") {
		t.Fatalf("a bare pass: %q", last)
	}
	if result.Deploy != application.VerifyOK {
		t.Fatalf("the deploy check is unchanged, got %+v", result)
	}
}

func TestVerifyOfARigWithNoAfterLandingCommitAndNoSiteURLNeedsNoSuchWord(t *testing.T) {
	f := newDeployFixture(nil, false, "No strings named here.")
	f.after = deployAfter{}
	f.marks = deployMarks{}

	_, out := f.run(t)

	if strings.Contains(out, "the live site was not read") {
		t.Fatalf("a rig with no deploy and no site has no live site to read:\n%s", out)
	}
}
