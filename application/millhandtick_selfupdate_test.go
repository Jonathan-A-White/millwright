package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// fakeCheckout is a factory rig checkout that is behind its origin by one
// commit, until it is advanced.
type fakeCheckout struct {
	head, tip string
	left      string
	dirty     []string
	fetchErr  error
	fetched   int
}

func (f *fakeCheckout) Fetch(context.Context, string) error { f.fetched++; return f.fetchErr }
func (f *fakeCheckout) Tip(context.Context, string, string, string) (string, error) {
	return f.tip, nil
}
func (f *fakeCheckout) Head(context.Context, string) (string, error)          { return f.head, nil }
func (f *fakeCheckout) Uncommitted(context.Context, string) ([]string, error) { return f.dirty, nil }
func (f *fakeCheckout) Advance(_ context.Context, _, _, commit string) (application.Advanced, error) {
	if f.left != "" {
		return application.Advanced{Left: f.left}, nil
	}
	if f.head == commit {
		return application.Advanced{}, nil
	}
	f.head = commit
	return application.Advanced{Moved: true}, nil
}

type fakeAfter struct {
	command string
	ran     application.Ran
	runs    int
}

func (f *fakeAfter) Command(string) string { return f.command }
func (f *fakeAfter) Run(context.Context, string, string) (application.Ran, error) {
	f.runs++
	return f.ran, nil
}

type fakeBuilt map[string]string

func (f fakeBuilt) Built(_ context.Context, rig string) (string, error) { return f[rig], nil }
func (f fakeBuilt) MarkBuilt(_ context.Context, rig, commit string) error {
	f[rig] = commit
	return nil
}

func anUpdatingTick(t *testing.T) (application.MillhandTick, *fakeCheckout, *fakeAfter, fakeBuilt) {
	t.Helper()
	tick, _, _, _ := aTick(t)
	checkout := &fakeCheckout{head: "027f977aaaaaaaa", tip: "bfd385cbbbbbbbb"}
	after := &fakeAfter{command: "make build"}
	built := fakeBuilt{}
	tick.SelfUpdate = application.SelfUpdate{
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Checkout: checkout, After: after, Built: built,
	}
	return tick, checkout, after, built
}

func TestATickNamesTheVersionItBuiltAndSaysItOnce(t *testing.T) {
	tick, _, after, built := anUpdatingTick(t)

	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("running the tick: %v", err)
	}
	if want := "self-update: millwright 027f977 → bfd385c, built"; !strings.Contains(report.Line, want) {
		t.Fatalf("expected %q in %q", want, report.Line)
	}
	if built["millwright"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the build remembered, got %v", built)
	}

	again, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("running the second tick: %v", err)
	}
	if strings.Contains(again.Line, "self-update") || after.runs != 1 {
		t.Errorf("expected the second tick to say nothing and build nothing, got %q after %d builds", again.Line, after.runs)
	}
}

func TestATickWithAMillhandUpStillUpdates(t *testing.T) {
	tick, _, after, _ := anUpdatingTick(t)
	windows := apptest.NewFakeWindows()
	windows.Holds("millhand-2026-09-19-05", level)
	tick.Millhand.Windows = windows

	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("running the tick: %v", err)
	}
	if !strings.Contains(report.Line, "already up") || !strings.Contains(report.Line, "self-update: millwright 027f977 → bfd385c, built") || after.runs != 1 {
		t.Errorf("expected the update before the early return, got %q", report.Line)
	}
}

func TestATickLeavesAFailedBuildToBeTriedAgain(t *testing.T) {
	tick, _, after, built := anUpdatingTick(t)
	after.ran = application.Ran{Command: "make build", Status: 2, Output: "boom\n"}

	report, _ := tick.Run(context.Background())
	if !strings.Contains(report.Line, "the old mw is kept") || !strings.Contains(report.Line, "boom") {
		t.Fatalf("expected the failure said, got %q", report.Line)
	}
	if len(built) != 0 {
		t.Fatalf("expected a failed build not remembered, got %v", built)
	}

	// The checkout is level now, and the next tick builds what is not built.
	after.ran = application.Ran{Command: "make build"}
	report, _ = tick.Run(context.Background())
	if !strings.Contains(report.Line, "self-update: millwright built at bfd385c") || after.runs != 2 {
		t.Errorf("expected the retry to build, got %q after %d builds", report.Line, after.runs)
	}
}

func TestATickDoesNotUpdateInADryRunOrWithoutACommand(t *testing.T) {
	tick, checkout, after, _ := anUpdatingTick(t)
	tick.DryRun = true
	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if checkout.fetched != 0 || after.runs != 0 {
		t.Errorf("expected a dry run to touch nothing, fetched %d, built %d", checkout.fetched, after.runs)
	}

	tick.DryRun = false
	after.command = ""
	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if checkout.fetched != 0 || after.runs != 0 || checkout.head != "027f977aaaaaaaa" {
		t.Errorf("expected a host with no command to be left alone, fetched %d, built %d", checkout.fetched, after.runs)
	}
}

func TestATickSaysWhyAnUpdateCouldNotBeMade(t *testing.T) {
	tick, checkout, after, _ := anUpdatingTick(t)
	checkout.fetchErr = errors.New("no route to host")
	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed fetch not to fail the tick, got %v", err)
	}
	if !strings.Contains(report.Line, "self-update: millwright could not be brought level: fetching: no route to host") || after.runs != 0 {
		t.Errorf("got %q", report.Line)
	}
}
