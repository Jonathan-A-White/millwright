package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A gate command whose port reads its line afresh says so in the batch's log, for
// each of the tests, the related-tests line and the changed-specs line the gate runs.
type readingChecks struct {
	changed bool
	related bool
	at      time.Time
}

func (r readingChecks) read(what string) application.GateRead {
	return application.GateRead{What: what, At: r.at, Changed: r.changed}
}

func (r readingChecks) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{Command: "make test", Passed: true, Read: r.read("tests")}, nil
}

func (r readingChecks) RunRelated(context.Context, string, string, application.RelatedSpan) (application.RelatedRun, error) {
	if !r.related {
		return application.RelatedRun{}, nil
	}
	return application.RelatedRun{Checked: application.Checked{Command: "vitest related a.ts", Passed: true, Read: r.read("related_tests")}, Files: []string{"a.ts"}, Ran: true}, nil
}

func (r readingChecks) RunSpecs(context.Context, string, string, string) (application.SpecsChecked, error) {
	return application.SpecsChecked{Checked: application.Checked{Command: "playwright test a.spec.ts", Passed: true, Read: r.read("changed_specs")}, Specs: []string{"a.spec.ts"}}, nil
}

func theBatchLogOfAGate(t *testing.T, checks readingChecks) (string, string) {
	t.Helper()
	var out strings.Builder
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Checks: checks, RelatedChecks: checks, SpecChecks: checks,
		Host: "laptop", Seat: "builder", Out: &out, Err: io.Discard,
	}}
	report := &application.BatchReport{Rig: "millwright", Target: "main", Base: "origin/main"}
	land.GateTheBatch(context.Background(), t.TempDir(), report)
	return out.String(), report.GateLog
}

func TestTheBatchLogSaysWhenTheGateCommandsWereReadAndThatTheyChanged(t *testing.T) {
	at := time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)

	for _, c := range []struct {
		name    string
		checks  readingChecks
		command string
		what    string
	}{
		{"the tests", readingChecks{at: at}, "make test", "tests"},
		{"the related-tests line", readingChecks{at: at, related: true}, "vitest related a.ts", "related_tests"},
		{"the changed-specs line", readingChecks{at: at}, "playwright test a.spec.ts", "changed_specs"},
	} {
		t.Run(c.name+" unchanged", func(t *testing.T) {
			out, log := theBatchLogOfAGate(t, c.checks)
			read := "mw land: gate " + c.what + ": " + c.command + ": read at 2026-11-06T08:30:00Z\n"
			if !strings.Contains(out, read) {
				t.Errorf("expected the log to say %q, got %q", read, out)
			}
			if strings.Contains(out, "changed during the batch") || strings.Contains(log, "changed during the batch") {
				t.Errorf("expected no change said of a line that did not change, got %q and %q", out, log)
			}
			if !strings.Contains(log, "# gate "+c.what+": "+c.command+": read at 2026-11-06T08:30:00Z\n") {
				t.Errorf("expected the gate log to hold the reading too, got %q", log)
			}
		})
		t.Run(c.name+" changed", func(t *testing.T) {
			changed := c.checks
			changed.changed = true
			out, _ := theBatchLogOfAGate(t, changed)
			want := "mw land: " + c.what + " changed during the batch: running the new line\n"
			if !strings.Contains(out, want) {
				t.Errorf("expected the log to say %q, got %q", want, out)
			}
		})
	}
}
