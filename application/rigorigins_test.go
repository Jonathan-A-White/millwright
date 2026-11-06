package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func rigOriginsCheck(origins map[string]string) *application.RigOriginsCheck {
	return &application.RigOriginsCheck{
		Origins: func(context.Context) (map[string]string, error) { return origins, nil },
	}
}

func TestDoctorSaysNothingOfRigsWhoseOriginsShareAScheme(t *testing.T) {
	verdict, reason := rigOriginsCheck(map[string]string{
		"argus":       "https://github.com/Jonathan-A-White/argus.git",
		"leaderboard": "https://github.com/Jonathan-A-White/leaderboard.git",
		"millwright":  "https://github.com/Jonathan-A-White/millwright",
	}).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("rigs that all use https are fine, got %v: %s", verdict, reason)
	}
}

func TestDoctorAlarmsOnARigWhoseOriginSchemeDiffersFromTheHostsOtherRigs(t *testing.T) {
	verdict, reason := rigOriginsCheck(map[string]string{
		"argus":       "https://github.com/Jonathan-A-White/argus.git",
		"leaderboard": "git@github.com:Jonathan-A-White/leaderboard.git",
		"millwright":  "https://github.com/Jonathan-A-White/millwright.git",
		"postern":     "https://github.com/Jonathan-A-White/postern.git",
	}).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("a git@ origin among https ones is faulty, got %v: %s", verdict, reason)
	}
	for _, want := range []string{"leaderboard", "ssh", "https", "git@github.com:Jonathan-A-White/leaderboard.git", "git -C <leaderboard's checkout> remote set-url origin https://github.com/Jonathan-A-White/leaderboard.git"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the alarm should say %q, got: %s", want, reason)
		}
	}
	for _, not := range []string{"argus", "postern"} {
		if strings.Contains(reason, not) {
			t.Errorf("the alarm names %s, which uses the host's scheme: %s", not, reason)
		}
	}
}

func TestDoctorNamesTheSchemesWhenNoneHoldsAMajority(t *testing.T) {
	verdict, reason := rigOriginsCheck(map[string]string{
		"argus":      "https://github.com/Jonathan-A-White/argus.git",
		"millwright": "ssh://git@github.com/Jonathan-A-White/millwright.git",
	}).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("two rigs with two schemes are faulty, got %v: %s", verdict, reason)
	}
	for _, want := range []string{"argus", "millwright", "https", "ssh"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the alarm should say %q, got: %s", want, reason)
		}
	}
}

func TestDoctorCannotTellWhenTheOriginsCannotBeRead(t *testing.T) {
	check := &application.RigOriginsCheck{
		Origins: func(context.Context) (map[string]string, error) { return nil, errors.New("boom") },
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "boom") {
		t.Fatalf("an unreadable origin list is cannot-tell naming why, got %v: %s", verdict, reason)
	}
}

func TestOriginSchemeNamesHowAnOriginIsReached(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/o/r.git":     "https",
		"http://example.com/o/r.git":     "http",
		"git@github.com:o/r.git":         "ssh",
		"ssh://git@github.com/o/r.git":   "ssh",
		"file:///srv/origin.git":         "file",
		"/srv/origin.git":                "file",
		"git://github.com/o/r.git":       "git",
		"HTTPS://github.com/o/r.git":     "https",
		"  https://github.com/o/r.git\n": "https",
	} {
		if got := application.OriginScheme(url); got != want {
			t.Errorf("OriginScheme(%q) = %q, want %q", url, got, want)
		}
	}
}
