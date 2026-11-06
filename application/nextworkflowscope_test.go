package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// workflowRefusal is what git said to mw-hmnm2xi.3's landing push on cloud1,
// the way infrastructure/rig reads it (mw-gq6.999).
func workflowRefusal() error {
	return fmt.Errorf("%w: git push origin HEAD:refs/heads/main: exit status 1: ! [remote rejected] main -> main (refusing to allow a Personal Access Token to create or update workflow `.github/workflows/deploy.yml` without `workflow` scope)",
		application.ErrPushWorkflowScope)
}

// someFilesChanged says every branch has changed the files it holds.
type someFilesChanged []string

func (s someFilesChanged) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return s, nil
}

func (s someFilesChanged) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}

func (s someFilesChanged) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return nil, nil
}

// aCredential is a push credential that reads as scope says.
type aCredential struct {
	reading application.WorkflowScopeReading
	reads   int
}

func (c *aCredential) ReadWorkflowScope(context.Context) (application.WorkflowScopeReading, error) {
	c.reads++
	return c.reading, nil
}

type workflowScopeRun struct {
	report  application.NextReport
	err     error
	lands   *flakyLanding
	tracker *apptest.FakeTracker
	mail    *apptest.FakeMailbox
}

// aCloseOutOnHost closes out mw-x.1 on host, its branch changing files, its
// landing push failing as pushErr says (nil: it works), the push credential
// read through cred (nil: not asked), with the vault's home on the laptop.
func aCloseOutOnHost(t *testing.T, host string, files []string, pushErr error, cred application.WorkflowScopeReader) *workflowScopeRun {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: host})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &flakyLanding{pushFails: 100, err: pushErr}
	if pushErr == nil {
		lands.pushFails = 0
	}
	lands.AheadCount = 1
	run := &workflowScopeRun{lands: lands, tracker: tracker, mail: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Mailbox: run.mail,
		Changes: someFilesChanged(files), PushScope: cred,
		Home:   &apptest.FakeHomeFile{Text: "laptop 2026-11-05T20:00:00Z mayor\n"},
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Seat: "builder", Host: host,
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return run
}

func (r *workflowScopeRun) mailText(t *testing.T) string {
	t.Helper()
	inbox, err := r.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil || len(inbox) == 0 {
		t.Fatalf("expected a mail to the Mayor, got %v and %v", inbox, err)
	}
	return inbox[len(inbox)-1].Subject + "\n" + inbox[len(inbox)-1].Body
}

// expectGivenBack is what both ways of meeting the missing scope end in: the
// close-out stopped for the workflow scope and landed nothing, the attempt is
// named tooling so it is not the story's own, the story is re-pathed to the
// home host, whose credential has the scope, and the Mayor's mail and the
// story's comment name the host that lacks it.
func (r *workflowScopeRun) expectGivenBack(t *testing.T, host string) {
	t.Helper()
	if r.err == nil || r.report.Landed || r.report.Reason != application.ReasonWorkflowScope {
		t.Fatalf("expected a workflow-scope stop that landed nothing, got err %v and %+v", r.err, r.report)
	}
	if !slices.Contains(r.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim given back, the story now naming a host that can take it, asked %v", r.tracker.Asked())
	}
	meta := r.tracker.Metadata("mw-x.1")
	if meta[application.CauseField] != application.CauseTooling || meta[application.AttemptCausesField] != "1:tooling" {
		t.Errorf("expected attempt 1 named tooling, got %v", meta)
	}
	if meta["host"] != "laptop" {
		t.Errorf("expected the story re-pathed to host=laptop, got host %q", meta["host"])
	}
	for what, text := range map[string]string{"the mail": r.mailText(t), "the comment": strings.Join(r.tracker.Comments("mw-x.1"), "\n")} {
		if !strings.Contains(text, host) || !strings.Contains(text, "workflow scope") {
			t.Errorf("expected %s to say that %s lacks the workflow scope, got:\n%s", what, host, text)
		}
		if !strings.Contains(text, "laptop") {
			t.Errorf("expected %s to say the story goes back for laptop, got:\n%s", what, text)
		}
	}
}

func TestALandingPushRefusedForTheWorkflowScopeIsToolingNotTheStorysAndNamesTheHost(t *testing.T) {
	run := aCloseOutOnHost(t, "cloud1", []string{"src/app.go", ".github/workflows/deploy.yml"}, workflowRefusal(), nil)

	run.expectGivenBack(t, "cloud1")
	if run.lands.pushes != 1 {
		t.Errorf("expected the refused push not tried again, got %d pushes", run.lands.pushes)
	}
	if !errors.Is(run.err, application.ErrPushWorkflowScope) && !strings.Contains(run.err.Error(), "workflow scope") {
		t.Errorf("expected the stop to name the cause in words, got %v", run.err)
	}
}

func TestABranchChangingWorkflowsIsNotLandedFromAHostWhoseCredentialLacksTheScope(t *testing.T) {
	cred := &aCredential{reading: application.WorkflowScopeReading{Scope: application.WorkflowScopeLacks, Said: "gh is not logged in"}}
	run := aCloseOutOnHost(t, "cloud1", []string{"src/app.go", ".github/workflows/deploy.yml"}, nil, cred)

	run.expectGivenBack(t, "cloud1")
	if run.lands.pushes != 0 || run.lands.fetches != 1 {
		t.Errorf("expected nothing pushed or merged (one fetch, the close-out's own), got %d pushes and %d fetches", run.lands.pushes, run.lands.fetches)
	}
	if text := run.mailText(t); strings.Contains(text, "exit status") {
		t.Errorf("expected words, not git's stderr, got:\n%s", text)
	}
}

func TestABranchNotChangingWorkflowsLandsWhateverTheCredentialReads(t *testing.T) {
	cred := &aCredential{reading: application.WorkflowScopeReading{Scope: application.WorkflowScopeLacks}}
	run := aCloseOutOnHost(t, "cloud1", []string{"src/app.go", "docs/.github/workflows-notes.md"}, nil, cred)

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed, got %v and %+v", run.err, run.report)
	}
	if cred.reads != 0 {
		t.Errorf("expected the credential not read for a branch that touches no workflow, got %d reads", cred.reads)
	}
}

func TestABranchChangingWorkflowsLandsFromAHostWhoseCredentialHasTheScopeOrIsNotKnown(t *testing.T) {
	for _, scope := range []application.WorkflowScope{application.WorkflowScopeHas, application.WorkflowScopeUnknown} {
		cred := &aCredential{reading: application.WorkflowScopeReading{Scope: scope}}
		run := aCloseOutOnHost(t, "laptop", []string{".github/workflows/deploy.yml"}, nil, cred)

		if run.err != nil || !run.report.Landed {
			t.Errorf("%s: expected the story landed, got %v and %+v", scope, run.err, run.report)
		}
	}
}

func TestAHomeHostThatLacksTheScopeKeepsTheStoryAndSaysSo(t *testing.T) {
	cred := &aCredential{reading: application.WorkflowScopeReading{Scope: application.WorkflowScopeLacks}}
	run := aCloseOutOnHost(t, "laptop", []string{".github/workflows/deploy.yml"}, nil, cred)

	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonWorkflowScope {
		t.Fatalf("expected a workflow-scope stop, got err %v and %+v", run.err, run.report)
	}
	if slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim kept: a host that lacks the scope would take the story again, asked %v", run.tracker.Asked())
	}
	if text := run.mailText(t); !strings.Contains(text, "laptop") || !strings.Contains(text, "No other host") {
		t.Errorf("expected the mail to say laptop, the home, lacks the scope and no other host can take it, got:\n%s", text)
	}
}

// A story of a rig that lands in batches is queued by pushing its branch, which
// GitHub refuses for a workflow file the same way: the stop is the workflow
// scope's, not a failed landing (mw-gq6.999).
func TestAQueuedStoryWhosePushIsRefusedForTheWorkflowScopeIsGivenBackForTheHome(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "cloud1"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &flakyLanding{pushFails: 100, err: workflowRefusal()}
	lands.AheadCount = 1
	run := &workflowScopeRun{lands: lands, tracker: tracker, mail: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Rules: aBatchRigsRules{}, Mailbox: run.mail,
		Home:   &apptest.FakeHomeFile{Text: "laptop 2026-11-05T20:00:00Z mayor\n"},
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Seat: "builder", Host: "cloud1", Remote: "origin",
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")

	run.expectGivenBack(t, "cloud1")
	if run.report.Queued {
		t.Errorf("expected nothing recorded as queued, got %+v", run.report)
	}
}
