package claude_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// A grind's result names the version its program said it was, and so does the
// result a grind that ran out of time returns with its error, which holds
// nothing else: the record of a grind that went wrong is the one a version is
// most wanted on (mw-gq6.1712).
func TestGrindResultNamesTheHarnessVersion(t *testing.T) {
	program, _ := grindStandIn(t, "cat <<'EOF'\n"+liveResult+"\nEOF\n")
	result, err := claude.NewGrinder(claude.WithGrindProgram(program)).Grind(context.Background(), aGrindCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.HarnessVersion != standInVersion || !result.Finished() {
		t.Fatalf("expected a finished result run by %q, got %+v", standInVersion, result)
	}

	slow, _ := grindStandIn(t, "sleep 30 &\nwait\n")
	call := aGrindCall(t, t.TempDir())
	call.Timeout = 200 * time.Millisecond
	failed, err := claude.NewGrinder(claude.WithGrindProgram(slow)).Grind(context.Background(), call)
	if !errors.Is(err, application.ErrGrindTimedOut) {
		t.Fatalf("expected ErrGrindTimedOut, got %v", err)
	}
	if failed.HarnessVersion != standInVersion || failed.Turns != 0 || failed.Answer != nil {
		t.Fatalf("expected the timed-out result to hold the version alone, got %+v", failed)
	}
}

// A warm session's answer names the version its program said it was before
// the session was started.
func TestAWarmSessionsAnswerNamesTheHarnessVersion(t *testing.T) {
	program, _ := warmStandIn(t, "echo '"+streamResult(t)+"'\n")
	session, err := claude.NewGrinder(claude.WithGrindProgram(program)).Warm(aWarmCall(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Answer(context.Background(), "Grind this grist.", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.HarnessVersion != standInVersion || !result.Finished() {
		t.Fatalf("expected a finished answer run by %q, got %+v", standInVersion, result)
	}
}

// The grinder asks its program for its version once for each install of it:
// a second grind on the same file does not run it again, a program updated in
// place is asked again, and a program that cannot say leaves the version out
// and grinds all the same.
func TestGrindReadsTheVersionOnceForEachInstall(t *testing.T) {
	seen := t.TempDir()
	program := filepath.Join(seen, "claude")
	asked := filepath.Join(seen, "asked")
	standIn := func(version string) {
		t.Helper()
		script := "#!/bin/sh\n" +
			"if [ \"$*\" = --version ]; then echo x >> " + asked + "; " + version + "; fi\n" +
			"cat <<'EOF'\n" + liveResult + "\nEOF\n"
		if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	timesAsked := func() int {
		data, _ := os.ReadFile(asked)
		return strings.Count(string(data), "x")
	}
	grind := func(g *claude.Grinder) string {
		t.Helper()
		result, err := g.Grind(context.Background(), aGrindCall(t, t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		return result.HarnessVersion
	}

	standIn("echo '2.1.7 (Claude Code)'; exit 0")
	grinder := claude.NewGrinder(claude.WithGrindProgram(program))
	if first, second := grind(grinder), grind(grinder); first != "2.1.7 (Claude Code)" || second != first {
		t.Fatalf("expected both grinds run by 2.1.7, got %q and %q", first, second)
	}
	if n := timesAsked(); n != 1 {
		t.Fatalf("expected the version asked once for two grinds, it was asked %d times", n)
	}

	standIn("echo '2.1.8 (Claude Code)'; exit 0")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(program, later, later); err != nil {
		t.Fatal(err)
	}
	if got := grind(grinder); got != "2.1.8 (Claude Code)" {
		t.Fatalf("expected the updated program's version, got %q", got)
	}

	standIn("echo 'unknown option' >&2; exit 1")
	if err := os.Chtimes(program, later.Add(time.Minute), later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := grind(grinder); got != "" {
		t.Fatalf("expected no version from a program that cannot say, got %q", got)
	}
	if n := timesAsked(); n != 3 {
		t.Fatalf("expected the version asked once for each of three installs, it was asked %d times", n)
	}
}
