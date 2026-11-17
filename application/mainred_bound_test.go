package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1421: the run of the failing tests alone on main is given its bound
// from the moment it starts, a wait for a test-runner slot not counted; a run
// that cannot end says on the first line of the refusal that main was not checked.

// verdictLine is the line of a refusal's mail that says it stopped: the first
// line of its body, under the subject.
func verdictLine(mail string) string {
	for _, line := range strings.Split(mail, "\n") {
		if strings.Contains(line, "STOPPED (") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func TestTheRunOnMainAloneIsHandedItsBoundNotADeadlineThatTheWaitForASlotWouldEat(t *testing.T) {
	var bound, deadline time.Duration
	named := &namedTestsThat{run: func(ctx context.Context, _ []string) (application.NamedRun, error) {
		bound = application.RunBoundOf(ctx)
		if at, ok := ctx.Deadline(); ok {
			deadline = time.Until(at)
		}
		return application.NamedRun{Checked: application.Checked{Command: "go test -run", Passed: true}, Ran: true}, nil
	}}
	aTestsFailStopWithBounds(t, named, 7*time.Minute, 0)
	if bound != 7*time.Minute {
		t.Errorf("expected the adapter given the bound of 7m0s to start counting once it has a slot, got %s", bound)
	}
	// The whole call may take the bound and the longest wait for a slot.
	if want := 7*time.Minute + application.DefaultMainAloneSlotWait - time.Minute; deadline < want {
		t.Errorf("expected the context's deadline to leave room for the wait for a slot (over %s), got %s", want, deadline)
	}
}

func TestARunCutAtItsBoundOnceStartedOpensTheRefusalWithMainNotChecked(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{}, &application.RunCutError{Bound: 10 * time.Minute}
	}}
	got := aTestsFailStopWithBounds(t, named, 0, 0)
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail, as nothing is excused, got %q (%v)", got.report.Reason, got.err)
	}
	if !strings.HasPrefix(got.report.Why, application.MainNotCheckedWords) {
		t.Errorf("expected the refusal's first words to be %q, got %q", application.MainNotCheckedWords, got.report.Why)
	}
	first := verdictLine(got.mail)
	for _, want := range []string{"tests-fail", "main not checked", "the run did not end in 10m0s"} {
		if !strings.Contains(first, want) {
			t.Errorf("expected the first line of the mail to say %q, got %q (mail: %s)", want, first, got.mail)
		}
	}
}

func TestARunThatWaitedForASlotForTooLongOpensTheRefusalWithMainNotChecked(t *testing.T) {
	named := &namedTestsThat{run: func(ctx context.Context, _ []string) (application.NamedRun, error) {
		<-ctx.Done()
		return application.NamedRun{}, ctx.Err()
	}}
	got := aTestsFailStopWithBounds(t, named, 20*time.Millisecond, 20*time.Millisecond)
	first := verdictLine(got.mail)
	if !strings.Contains(first, "main not checked") || !strings.Contains(first, "did not end in") {
		t.Errorf("expected the first line of the mail to say main was not checked, got %q", first)
	}
}

func TestARunOfMainThatWasMadeDoesNotSayMainWasNotChecked(t *testing.T) {
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\nFAIL\n")}
	got := aTestsFailStopWithBounds(t, named, 0, 0)
	first := verdictLine(got.mail)
	if strings.Contains(first, "main not checked") {
		t.Errorf("expected a refusal after a run on main to leave out main not checked, got %q", first)
	}
}
