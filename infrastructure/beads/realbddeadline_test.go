package beads_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mw-gq6.863, mw-gq6.900: the cases that run a real bd share the machine with
// every other case of the package, each starting a bd per call, so under load
// one of them can take more than a minute of wall clock and still be right. A
// case that sets its own shorter deadline (TestReleaseClaimByAnotherActor-
// LeavesTheClaimUntouched had one minute, its siblings five) fails at no fault
// of the code, and so does a case whose forty bd calls add up to more than any
// one deadline set over the whole of it (TestGatewayWorksAStoryThroughBeads, at
// load 42 of 16). A hung bd is ended per call, by the bd on the test's PATH
// (see bdWithCallDeadline), never by the clock over the case, so no case sets a
// deadline over its context: realBdContext is a context without one. Plain
// `go test` runs this scan, so a new case cannot slip past it by being behind
// the build tag.
func TestNoRealBdCaseSetsAWholeCaseDeadline(t *testing.T) {
	files, err := filepath.Glob("*_integration_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("expected to find the real-bd test files, got %v: %v", files, err)
	}
	own := regexp.MustCompile(`context\.With(Timeout|Deadline)\(`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for number, line := range strings.Split(string(body), "\n") {
			if own.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d sets a deadline over a whole case (%s); use realBdContext(t), which has none: a real bd under load is ended per call, not by the clock over the case",
					file, number+1, strings.TrimSpace(line))
			}
		}
	}
}

// mw-gq6.900: bd's claim lease is five minutes, and a case that asks "is this
// claim stale?" with time.Now() after a few more bd calls asks it of the host's
// clock: on a host whose calls take a minute each the answer is yes for a claim
// made a moment ago. A case judges a claim by its own lease, as read back
// (LeaseExpires), never by the host clock.
func TestNoRealBdCaseJudgesAClaimByTheHostClock(t *testing.T) {
	files, err := filepath.Glob("*_integration_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("expected to find the real-bd test files, got %v: %v", files, err)
	}
	hostClock := regexp.MustCompile(`StaleClaims\([^,]+,\s*time\.Now\(\)`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for number, line := range strings.Split(string(body), "\n") {
			if hostClock.MatchString(line) {
				t.Errorf("%s:%d judges a claim by the host clock (%s); use the lease read back from the claim",
					file, number+1, strings.TrimSpace(line))
			}
		}
	}
}
