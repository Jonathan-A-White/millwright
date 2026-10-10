package rig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

type runningMw struct{ ran []string }

func (r *runningMw) Run(_ context.Context, app, kind string) (application.GristSmokeReport, error) {
	r.ran = append(r.ran, app+" "+kind)
	return application.GristSmokeReport{App: app, Failures: []string{"old"}}, nil
}

func builtMw(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, BuiltBinary), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// mw-gq6.339: the smoke is the built mw's, asked as JSON, with --kind when given.
func TestBuiltSmokerAsksTheBuiltMwAndReadsItsReport(t *testing.T) {
	dir := builtMw(t, `echo "$@" > "$(dirname "$0")/asked"
echo '{"app":"cairn","examples":3,"failures":["cairn/sweep/one: the mill refused it"],"warnings":["w"]}'`)
	running := &runningMw{}
	report, err := NewBuiltSmoker(dir, running).Run(context.Background(), "cairn", "sweep")
	if err != nil {
		t.Fatal(err)
	}
	if report.App != "cairn" || report.Examples != 3 || len(report.Failures) != 1 || len(report.Warnings) != 1 {
		t.Fatalf("expected the built mw's report, got %+v", report)
	}
	if len(running.ran) != 0 {
		t.Fatalf("expected the running mw left out of it, it smoked %v", running.ran)
	}
	asked, _ := os.ReadFile(filepath.Join(dir, "bin", "asked"))
	if strings.TrimSpace(string(asked)) != "grist smoke cairn --json --kind sweep" {
		t.Fatalf("the built mw was asked %q", asked)
	}
}

// A built mw that cannot make the smoke is a smoke that could not be made, with
// what it said.
func TestBuiltSmokerSaysWhyTheBuiltMwMadeNoReport(t *testing.T) {
	dir := builtMw(t, `echo "mw grist smoke: the app cairn has no rig" >&2
echo "second line" >&2
exit 1`)
	_, err := NewBuiltSmoker(dir, &runningMw{}).Run(context.Background(), "cairn", "")
	if err == nil || !strings.Contains(err.Error(), "the app cairn has no rig") || strings.Contains(err.Error(), "second line") {
		t.Fatalf("expected the first line of what the built mw said, got %v", err)
	}
}

// A build that left no mw is smoked by the one that is running, as it was.
func TestBuiltSmokerFallsBackWhenTheBuildLeftNoMw(t *testing.T) {
	running := &runningMw{}
	report, err := NewBuiltSmoker(t.TempDir(), running).Run(context.Background(), "cairn", "")
	if err != nil || len(report.Failures) != 1 || len(running.ran) != 1 {
		t.Fatalf("expected the running mw's smoke, got %+v, %v, %v", report, err, running.ran)
	}
	if _, err := NewBuiltSmoker(t.TempDir(), nil).Run(context.Background(), "cairn", ""); err == nil {
		t.Fatal("expected an error with no built mw and no fallback")
	}
}
