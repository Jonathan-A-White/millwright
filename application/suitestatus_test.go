package application_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestSuiteStatusLineSaysWhereEachFailingFilesBugStands(t *testing.T) {
	landed := time.Date(2026, 10, 18, 11, 20, 0, 0, time.UTC)
	status := func(bugs ...application.SuiteBug) application.SuiteStatus {
		return application.SuiteStatus{Rig: "lampas", Kept: true, Day: "2026-10-18", Sha: "3387eb6f8aaaa", Failing: len(bugs), Bugs: bugs}
	}
	cases := []struct {
		name string
		got  application.SuiteStatus
		want string
	}{
		{"open", status(application.SuiteBug{ID: "mw-a"}), "lampas 2026-10-18 3387eb6f8: 1 failing: mw-a"},
		{"fixed", status(application.SuiteBug{ID: "mw-5r3p30.613", Closed: landed}),
			"lampas 2026-10-18 3387eb6f8: 1 failing: fixed by mw-5r3p30.613 (landed 11:20Z), until the next run"},
		{"fixed on a later day", status(application.SuiteBug{ID: "mw-c", Closed: landed.Add(24 * time.Hour)}),
			"lampas 2026-10-18 3387eb6f8: 1 failing: fixed by mw-c (landed 2026-10-19 11:20Z), until the next run"},
		{"unfiled", status(application.SuiteBug{}), "lampas 2026-10-18 3387eb6f8: 1 failing: unfiled"},
		{"one of each", status(application.SuiteBug{ID: "mw-a"}, application.SuiteBug{ID: "mw-c", Closed: landed}, application.SuiteBug{}),
			"lampas 2026-10-18 3387eb6f8: 3 failing: mw-a, fixed by mw-c (landed 11:20Z), unfiled, until the next run"},
		{"epic not read", application.SuiteStatus{Rig: "lampas", Kept: true, Day: "2026-10-18", Sha: "3387eb6f8", Failing: 1},
			"lampas 2026-10-18 3387eb6f8: 1 failing"},
	}
	for _, c := range cases {
		if got := c.got.Line(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
