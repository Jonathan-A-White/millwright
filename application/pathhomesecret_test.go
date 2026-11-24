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
			// A named host carries its host_why, so the warning of a pin with none
			// (mw-gq6.1702) is not counted here.
			pairs := []string{"host=" + host}
			if host != "auto" {
				pairs = append(pairs, "host_why=a test pins it")
			}
			if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", pairs); err != nil {
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

// mw-gq6.1690: the warning is for a secret, not for the product's name: a
// criterion whose YNAB is a fake in a unit test runs on any host.
func TestPathSetWarnsOfAHomeSecretByTheSecretNotTheProductName(t *testing.T) {
	for name, c := range map[string]struct {
		acceptance string
		warns      bool
	}{
		"a fake YNAB transaction":       {"1. a test approves a card ... the fake YNAB transaction's memo is as before", false},
		"a fake YNAB holding its parts": {"1. the fake YNAB still holds its parts", false},
		"a stub YNAB":                   {"1. a test with a stub YNAB lists the categories", false},
		"his real YNAB":                 {"1. mw budget sync against his real YNAB shows the diff", true},
		"a live YNAB":                   {"1. a live YNAB read shows the categories", true},
		"the YNAB token":                {"1. it needs the YNAB token", true},
		"YNAB_TOKEN":                    {"1. with YNAB_TOKEN set it reads the budget", true},
		"the age key":                   {"1. it decrypts with the age key", true},
		"a gdrive sign-in":              {"1. a gdrive sign-in reads the sheet", true},
	} {
		tracker, rules := aStoryWithAcceptance(t, c.acceptance)
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", []string{"host=auto"}); err != nil {
			t.Fatalf("%s: nothing is refused for it, got %v", name, err)
		}
		if got := strings.Contains(errOut.String(), "WARN"); got != c.warns {
			t.Errorf("%s: warns = %v, want %v; got %q", name, got, c.warns, errOut.String())
		}
	}
}
