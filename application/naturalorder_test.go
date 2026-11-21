package application_test

import (
	"sort"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestNaturalLessReadsRunsOfDigitsAsNumbers(t *testing.T) {
	want := []string{
		"2026-11-21-9", "2026-11-21-98", "2026-11-21-999", "2026-11-21-1000", "2026-11-21-1001", "2026-11-22-1", "2026-12-01-2",
	}
	got := []string{want[4], want[6], want[0], want[3], want[5], want[2], want[1]}
	sort.Slice(got, func(i, j int) bool { return application.NaturalLess(got[i], got[j]) })
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted %v, want %v", got, want)
		}
	}
	for _, pair := range [][2]string{{"a", "a1"}, {"a2", "a02"}, {"a02", "a3"}, {"", "a"}} {
		if !application.NaturalLess(pair[0], pair[1]) || application.NaturalLess(pair[1], pair[0]) {
			t.Errorf("%q should come before %q, and only that way", pair[0], pair[1])
		}
	}
	if application.NaturalLess("x1", "x1") {
		t.Error("a name is not less than itself")
	}
}
