package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1578: a criterion that needs the age key, gdrive, YNAB or mw budget
// needs a home host; mw path set and mw release warn when the host is not one.
func TestPathSetWarnsOfACriterionNeedingAHomeSecretOnAHostThatIsNotHome(t *testing.T) {
	for name, acceptance := range map[string]string{
		"mw budget": "1. mw budget sync --dry-run prints the diff",
		"age key":   "1. it decrypts with the age key",
		"gdrive":    "1. a gdrive sign-in reads the sheet",
		"YNAB":      "1. the YNAB token reads the budget",
	} {
		for _, host := range []string{"auto", "cloud4"} {
			tracker, rules := aStoryWithAcceptance(t, acceptance)
			var errOut strings.Builder
			if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=" + host}); err != nil {
				t.Fatalf("%s on %s: nothing is refused for it, got %v", name, host, err)
			}
			got := errOut.String()
			if strings.Count(got, "WARN") != 1 || !strings.Contains(got, "mw-gq6.2") || !strings.Contains(got, "home host") || !strings.Contains(got, "criterion 1") {
				t.Errorf("%s on %s: expected one warning naming the story, the criterion and a home host; got %q", name, host, got)
			}
		}
	}
}

func TestPathSetSaysNothingOfAHomeSecretToAStoryPinnedToAHomeHost(t *testing.T) {
	for _, host := range []string{"laptop", "desktop"} {
		tracker, rules := aStoryWithAcceptance(t, "1. mw budget sync --dry-run prints the diff")
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=" + host}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(errOut.String(), "home host") {
			t.Errorf("%s is a home host; got %q", host, errOut.String())
		}
	}
}

func TestReleaseWarnsOfACriterionNeedingAHomeSecretOnAHostThatIsNotHome(t *testing.T) {
	tracker, rules := aStoryWithAcceptance(t, "1. mw budget sync --dry-run prints the diff")
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=auto"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.HoldStory(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := (application.Release{Tracker: tracker, Rules: rules, Out: &out}).Run(context.Background(), "mw-gq6.2"); err != nil {
		t.Fatalf("a warning only, got %v", err)
	}
	if !strings.Contains(out.String(), "WARN") || !strings.Contains(out.String(), "home host") {
		t.Errorf("expected the release to warn of a home host, got %q", out.String())
	}
}
