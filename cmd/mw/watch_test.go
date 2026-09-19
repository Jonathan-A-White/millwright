package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/spf13/cobra"
)

// Nothing here reaches a network or ssh: a config without a [watch] table has
// nothing to reach, URLs of a scheme nobody serves cannot be fetched, and the
// one wake is a FakeWatch.

func watchCmd(t *testing.T, config string) (out, errs string, err error) {
	t.Helper()
	mwConfig(t, config)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs([]string{"watch"})
	err = root.Execute()
	return stdout.String(), stderr.String(), err
}

func watchLog(t *testing.T) []string {
	t.Helper()
	home, _ := os.UserHomeDir()
	log, err := os.ReadFile(filepath.Join(home, WatchStateDir, "log"))
	if err != nil {
		t.Fatalf("reading the watch log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(log)), "\n")
}

func TestWatchWithNoWatchTableHasNothingToWatch(t *testing.T) {
	out, _, err := watchCmd(t, "host = \"laptop\"\n")
	if err != nil || out != "nothing to watch\n" {
		t.Fatalf("expected the one line and no failure, got %q, %v", out, err)
	}
	if got := exitCode(err); got != 0 {
		t.Errorf("expected status 0, got %d", got)
	}
	if log := watchLog(t); len(log) != 1 || !strings.HasSuffix(log[0], "Z nothing to watch") {
		t.Errorf("expected one dated line in the log, got %q", log)
	}
}

func TestWatchWhereNothingOutsideAnswersIsALocalFaultAndWakesNobody(t *testing.T) {
	out, errs, err := watchCmd(t, "vault = \"/nonexistent\"\nhost = \"laptop\"\n\n[watch]\nssh = \"vps-ssh\"\nhost = \"vps\"\n"+
		"outside = [\"nosuch://one.example\", \"nosuch://two.example\"]\nblog = \"nosuch://blog.example\"\n")
	if err != nil || out != "local-fault\n" || errs != "" {
		t.Fatalf("expected the one line and no failure, got %q, %q, %v", out, errs, err)
	}
	if log := watchLog(t); len(log) != 1 || !strings.HasSuffix(log[0], "Z local-fault") {
		t.Errorf("expected one dated line in the log, got %q", log)
	}
}

func TestWatchWithAHalfMadeWatchTableIsRefused(t *testing.T) {
	_, _, err := watchCmd(t, "[watch]\nblog = \"https://blog.example\"\n")
	if err == nil || !strings.Contains(err.Error(), "ssh, host, outside") {
		t.Fatalf("expected the table to be refused, got %v", err)
	}
	if got := exitCode(err); got != 1 {
		t.Errorf("expected a plain failure, got %d", got)
	}
}

func TestWatchCallingForAWakeLeavesWithSixAndPrintsNoErrorBesidesTheLine(t *testing.T) {
	world := apptest.NewFakeWatch()
	world.Answering["https://one.example"] = true
	world.Health = "2026-09-19T12:00:00Z verdict=unwell:mayor_gone\n"

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := &cobra.Command{Use: "mw", SilenceUsage: true}
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd := newWatchCmd()
	root.AddCommand(cmd)
	// The command's own RunE reads the real config, so run the shared part of it.
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runWatch(cmd, application.Watch{
			Probes:   world,
			Settings: application.WatchSettings{SSH: "vps-ssh", Host: "vps", Outside: []string{"https://one.example"}},
			Now:      func() time.Time { return time.Date(2026, 9, 19, 12, 5, 0, 0, time.UTC) },
			Out:      cmd.OutOrStdout(),
		})
	}
	root.SetArgs([]string{"watch"})
	err := root.Execute()

	if got := exitCode(err); got != 6 {
		t.Fatalf("expected status 6, got %d (%v)", got, err)
	}
	if stdout.String() != "unwell mayor_gone\n" || stderr.String() != "" {
		t.Errorf("expected only the one line, on stdout, got %q and %q", stdout.String(), stderr.String())
	}
}
