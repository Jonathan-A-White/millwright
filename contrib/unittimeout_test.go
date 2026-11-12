package contrib_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1110: mw usage read pauses the factory when the reserve is reached, and
// that pause waits up to application.PauseWait on the Builders. The unit that
// runs it was stopped at 3 minutes, which killed the pause half done.

func startTimeout(t *testing.T, unit string) time.Duration {
	t.Helper()
	text, err := os.ReadFile("systemd/" + unit)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^TimeoutStartSec=(\d+)(min|s)$`).FindSubmatch(text)
	if m == nil {
		t.Fatalf("%s has no TimeoutStartSec=<n>min (or <n>s) line", unit)
	}
	n, _ := strconv.Atoi(string(m[1]))
	if string(m[2]) == "s" {
		return time.Duration(n) * time.Second
	}
	return time.Duration(n) * time.Minute
}

// The probe's own work before the pause: claude starting, the screen, a few looks.
const usageProbeMargin = 5 * time.Minute

func TestTheUsageReadUnitOutlastsThePauseItRuns(t *testing.T) {
	got := startTimeout(t, "mw-usage-read.service")
	want := application.PauseWait + usageProbeMargin
	if got < want {
		t.Errorf("mw-usage-read.service stops at %v, below the pause's longest wait %v plus the probe's %v", got, application.PauseWait, usageProbeMargin)
	}
}

// waitsOnBuildersOrTheNetwork are the units whose command can wait: the longest
// wait each command makes, to which the unit's TimeoutStartSec must be no
// shorter.
var longestWait = map[string]time.Duration{
	// mw usage read: the probe, then mw pause, which waits PauseWait on Builders.
	"mw-usage-read.service": application.PauseWait + usageProbeMargin,
	// mw dispatch and mw millhand tick: a deploy after landing, see deploytimeout_test.go.
	"mw-dispatch.service":      (largestAfterLandingLimitMinutes + selfUpdateBuildMinutes) * time.Minute,
	"mw-millhand-tick.service": (largestAfterLandingLimitMinutes + selfUpdateBuildMinutes) * time.Minute,
	// mw postern mirror, mw needs sweep, mw-mail-notify, mw report send, mw millhand review:
	// the network; their units were set with margin (docs/bd-timeouts.md).
	"mw-postern-mirror.service":  5 * time.Minute,
	"mw-needs-nightly.service":   5 * time.Minute,
	"mw-mail-notify.service":     2 * time.Minute,
	"mw-report.service":          2 * time.Minute,
	"mw-parent-note.service":     2 * time.Minute,
	"mw-millhand-review.service": 2 * time.Minute,
}

func TestEveryUnitThatCanWaitOutlastsItsLongestWait(t *testing.T) {
	for unit, wait := range longestWait {
		if got := startTimeout(t, unit); got < wait {
			t.Errorf("%s stops at %v, below its command's longest wait %v", unit, got, wait)
		}
	}
	// A unit not listed is a unit nobody has said the wait of.
	listed := map[string]bool{"mw-doctor.service": true, "mw-health.service": true, "mw-unit-failed@.service": true}
	for unit := range longestWait {
		listed[unit] = true
	}
	files, err := filepath.Glob("systemd/mw-*.service")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		if listed[name] || name == "mw-view-follow.service" {
			continue
		}
		t.Errorf("%s is in neither longestWait nor the list of units that do not wait on Builders or the network", strings.TrimPrefix(f, "systemd/"))
	}
}
