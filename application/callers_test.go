package application_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestPathSetCountsTestCallersOfTheFilesNamed(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("E", domain.Path{Rig: "lampas", Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
	tracker.AddStory("E", domain.Story{ID: "B", Title: "b"})
	files := apptest.NewFakeRigFiles()
	files.SetTestCallers("/rigs/lampas", 11)
	var out, warn bytes.Buffer

	_, err := application.PathSet{Tracker: tracker, Rigs: map[string]string{"lampas": "/rigs/lampas"}, RigFiles: files, Out: &out, Err: &warn}.
		Run(t.Context(), "B", "", []string{"files=src/Settings.tsx,src/Settings.test.tsx", "size=S"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(files.AskedFiles(), ","); got != "src/Settings.tsx,src/Settings.test.tsx" {
		t.Errorf("asked for the files %q", got)
	}
	if !strings.Contains(out.String(), "B: test callers: 11 for size S (allows 10)\n") {
		t.Errorf("stdout lacks the count:\n%s", out.String())
	}
	if !strings.HasPrefix(warn.String(), "WARN: 11 test callers for size S; split") {
		t.Errorf("stderr = %q", warn.String())
	}
}

func TestAStoryWithNoSizeOrNoRigHereCountsNothing(t *testing.T) {
	for name, set := range map[string][]string{
		"no size":       {"files=src/Settings.tsx"},
		"no plain file": {"files=src/screens/", "size=S"},
	} {
		tracker := apptest.NewFakeTracker()
		tracker.AddEpic("E", domain.Path{Rig: "lampas", Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
		tracker.AddStory("E", domain.Story{ID: "B", Title: "b"})
		files := apptest.NewFakeRigFiles()
		files.SetTestCallers("/rigs/lampas", 99)
		var out, warn bytes.Buffer
		rigs := map[string]string{"lampas": "/rigs/lampas"}
		if _, err := (application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: &out, Err: &warn}).Run(t.Context(), "B", "", set); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "test callers") || warn.Len() > 0 {
			t.Errorf("%s: counted anyway:\n%s%s", name, out.String(), warn.String())
		}
	}
}
