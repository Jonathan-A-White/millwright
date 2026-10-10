package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type smokeNoteStore map[string]string

func (s smokeNoteStore) Note(_ context.Context, k string) (string, error) { return s[k], nil }
func (s smokeNoteStore) SetNote(_ context.Context, k, v string) error     { s[k] = v; return nil }
func (s smokeNoteStore) ClearNote(_ context.Context, k string) error      { delete(s, k); return nil }
func (s smokeNoteStore) NotesWithPrefix(_ context.Context, p string) (map[string]string, error) {
	found := map[string]string{}
	for k, v := range s {
		if strings.HasPrefix(k, p) {
			found[k] = v
		}
	}
	return found, nil
}

type smokeComments struct {
	WorkTracker
	said []string
}

func (c *smokeComments) CommentOnStory(_ context.Context, id, text string) error {
	c.said = append(c.said, id+": "+text)
	return nil
}

type smokeStub struct {
	fail bool
	ran  []string
}

func (s *smokeStub) Run(_ context.Context, app, _ string) (GristSmokeReport, error) {
	s.ran = append(s.ran, app)
	report := GristSmokeReport{App: app, Examples: 1}
	if s.fail {
		report.Failures = []string{app + "/sweep/one: the mill refused it: no licence"}
	}
	return report, nil
}

// mw-gq6.319: a landing that changed grist smokes the apps it changed it for
// once the rig has moved, writes a failure on the story, and holds the rig; a
// rig left unmoved is not smoked, and says so.
func TestALandingSmokesTheAppsItChangedAndWritesAFailureOnTheStory(t *testing.T) {
	ctx := context.Background()
	notes := smokeNoteStore{}
	stub := &smokeStub{fail: true}
	tracker := &smokeComments{}
	n := Next{Tracker: tracker, Host: "laptop", Smoke: GristSmokeAfter{Smoke: stub, Book: GristSmokeBook{Notes: notes, Host: "laptop"}}}
	c := &closeOut{id: "mw-1.1", target: "main", smokeApps: []string{"cairn"}}
	c.path.Rig = "cairn"

	report := &NextReport{}
	n.smoke(ctx, c, report)
	if len(stub.ran) != 0 || len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "not run for cairn") {
		t.Fatalf("expected an unmoved rig not smoked and said so, got ran %v notes %v", stub.ran, report.Notes)
	}

	report = &NextReport{}
	report.Rig.Moved = true
	n.smoke(ctx, c, report)
	if fmt.Sprint(stub.ran) != "[cairn]" {
		t.Fatalf("expected cairn smoked, got %v", stub.ran)
	}
	if len(tracker.said) != 1 || !strings.Contains(tracker.said[0], "FAILED 1 of 1") {
		t.Fatalf("expected the failure written on the story, got %v", tracker.said)
	}
	if why, _ := (GristSmokeBook{Notes: notes}).HeldBy(ctx, "cairn"); why == "" {
		t.Fatal("expected the rig's open stories held")
	}

	stub.fail, tracker.said = false, nil
	report = &NextReport{}
	report.Rig.Moved = true
	n.smoke(ctx, c, report)
	if len(tracker.said) != 0 {
		t.Fatalf("expected no comment for a smoke that passed, got %v", tracker.said)
	}
	if why, _ := (GristSmokeBook{Notes: notes}).HeldBy(ctx, "cairn"); why != "" {
		t.Fatalf("expected the hold lifted by a smoke that passed, got %q", why)
	}
}
