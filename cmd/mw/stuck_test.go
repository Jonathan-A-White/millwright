package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestStuckIsACommandThatSaysWhatCountsAsAHeldRoot(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"stuck"})
	if err != nil || cmd.Name() != "stuck" {
		t.Fatalf("mw stuck is not a command: %v", err)
	}
	for _, want := range []string{"icebox", "grilling", "hitl", "demo", application.HeldCommentPrefix, "6 hours", "always exits 0"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("mw stuck --help does not say %q:\n%s", want, cmd.Long)
		}
	}
}

func TestIceHelpSaysItMovesTheBeadBehindItsDependents(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"ice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"never blocks a live one", "closed bead", "cycle"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("mw ice --help does not say %q:\n%s", want, cmd.Long)
		}
	}
}

func TestSleepPassHelpSaysItReadsMwStuck(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"sleep", "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd.Long, "mw stuck") {
		t.Errorf("mw sleep pass --help does not say it reads mw stuck:\n%s", cmd.Long)
	}
}

func TestStuckHasAnInversionsFlagThatIsPlainWhenOff(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"stuck"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("inversions")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("mw stuck has no --inversions flag defaulting to off: %v", flag)
	}
	if !strings.Contains(cmd.Long, "--inversions") {
		t.Errorf("mw stuck --help does not say what --inversions does:\n%s", cmd.Long)
	}
}

func TestStuckHasADampersFlagThatPrintsEveryAlarmWhole(t *testing.T) {
	cmd, _, err := newRootCmd().Find([]string{"stuck"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("dampers")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("mw stuck has no --dampers flag defaulting to off: %v", flag)
	}
	for _, want := range []string{"--dampers", "every DAMPERS alarm", "one a line", "review queue"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("mw stuck --help does not say %q:\n%s", want, cmd.Long)
		}
	}
}

// mw stuck --dampers prints his review queue and, beside it, the number
// Postern's Needs you badge shows, split by kind (mw-gq6.1643).
func TestStuckDampersNeedsYouPrintsTheBadgeBesideTheReviewQueue(t *testing.T) {
	aBdThatRecords(t)
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "laptop")
	state := t.TempDir()
	t.Setenv("MW_POSTERN_VIEW_PATH", filepath.Join(state, "view.b64"))
	doc := application.PosternViewDoc{}
	for kind, n := range map[string]int{application.PosternNeedVerify: 3, application.PosternNeedDemo: 2, application.PosternNeedHands: 1, application.PosternNeedQuestion: 4} {
		for i := 0; i < n; i++ {
			doc.Needs = append(doc.Needs, application.PosternViewNeed{Kind: kind, WaitsFor: application.PosternWaitsYou})
		}
	}
	doc.Beads = []application.PosternViewBead{{Status: "open", Type: "task", Labels: []string{application.LabelHitlTodo}}}
	raw, err := application.PosternNeedsCountsJSON(doc, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(application.PosternNeedsCountsPath(filepath.Join(state, "view.b64")), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newStuckCmd()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs([]string{"--dampers"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("mw stuck --dampers: %v\n%s", err, errs.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[len(lines)-2], "his review queue ") ||
		!strings.HasPrefix(lines[len(lines)-1], "his badge 10: 4 cards, 2 demos, 1 hands steps, 3 landings waiting for his look, 0 other; To do items 1") {
		t.Errorf("mw stuck --dampers does not end with the review queue and the badge beside it:\n%s", out.String())
	}
}
