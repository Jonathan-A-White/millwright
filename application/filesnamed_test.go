package application_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// filesKeyOf is n distinct file names, as a files key writes them.
func filesKeyOf(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("src/f%d.ts", i)
	}
	return strings.Join(names, ",")
}

func pathSetFiles(t *testing.T, rigFiles *apptest.FakeRigFiles, set ...string) (out, warn string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("E", domain.Path{Rig: "lampas", Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
	tracker.AddStory("E", domain.Story{ID: "B", Title: "b"})
	var o, w bytes.Buffer
	rigs := map[string]string{"lampas": "/rigs/lampas"}
	if _, err := (application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: rigFiles, Out: &o, Err: &w}).Run(t.Context(), "B", "", set); err != nil {
		t.Fatal(err)
	}
	return o.String(), w.String()
}

func TestPathSetWarnsOfMoreFilesThanTheSizeAllows(t *testing.T) {
	_, warn := pathSetFiles(t, apptest.NewFakeRigFiles(), "files="+filesKeyOf(13), "size=M")
	if !strings.Contains(warn, "files named: 13 for size M (allows 12)") {
		t.Errorf("stderr lacks the warning:\n%s", warn)
	}
}

func TestPathSetSaysNothingOfFilesWithinTheAllowance(t *testing.T) {
	out, warn := pathSetFiles(t, apptest.NewFakeRigFiles(), "files="+filesKeyOf(12), "size=M")
	if strings.Contains(out+warn, "files named") {
		t.Errorf("warned of a story within its allowance:\n%s%s", out, warn)
	}
}

func TestAGlobCountsAsTheFilesItMatches(t *testing.T) {
	rigFiles := apptest.NewFakeRigFiles()
	for i := 0; i < 11; i++ {
		rigFiles.Add("/rigs/lampas", fmt.Sprintf("grinds/examples/bible-talk/e%d.json", i))
	}
	_, warn := pathSetFiles(t, rigFiles, "files=src/a.ts,src/b.ts,grinds/examples/bible-talk/*.json", "size=S")
	if !strings.Contains(warn, "files named: 13 for size S (allows 8)") {
		t.Errorf("a glob of 11 files and two files should be 13:\n%s", warn)
	}
}
