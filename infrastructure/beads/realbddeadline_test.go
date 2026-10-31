package beads_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mw-gq6.863: the cases that run a real bd share the machine with every other
// case of the package, each starting a bd per call, so under load one of them
// can take more than a minute of wall clock and still be right. A case that
// sets its own shorter deadline (TestReleaseClaimByAnotherActorLeavesTheClaim-
// Untouched had one minute, its siblings five) fails at no fault of the code.
// Every real-bd case takes its deadline from realBdContext, which is the one
// place that says how long a hung bd is waited for. Plain `go test` runs this
// scan, so a new case cannot slip past it by being behind the build tag.
func TestNoRealBdCaseSetsItsOwnDeadline(t *testing.T) {
	files, err := filepath.Glob("*_integration_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("expected to find the real-bd test files, got %v: %v", files, err)
	}
	own := regexp.MustCompile(`context\.WithTimeout\(`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for number, line := range strings.Split(string(body), "\n") {
			if own.MatchString(line) && !strings.Contains(line, "realBdDeadline") {
				t.Errorf("%s:%d sets its own deadline (%s); use realBdContext(t) so a real bd under load gets the same time in every case",
					file, number+1, strings.TrimSpace(line))
			}
		}
	}
}
