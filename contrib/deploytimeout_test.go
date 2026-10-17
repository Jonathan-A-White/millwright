package contrib_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// mw-gq6.564: the home's tick deploys a rig inside the unit that runs it, under
// that rig's [after_landing_limit] (postern's is 20m). The unit's own stop has to
// be above that limit plus the self-update build before it, else systemd kills
// the deploy part way.
const largestAfterLandingLimitMinutes = 20
const selfUpdateBuildMinutes = 5

func TestTheTicksStopIsAboveTheLargestAfterLandingLimit(t *testing.T) {
	for _, unit := range []string{"mw-millhand-tick.service", "mw-dispatch.service"} {
		text, err := os.ReadFile("systemd/" + unit)
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`(?m)^TimeoutStartSec=(\d+)min$`).FindSubmatch(text)
		if m == nil {
			t.Fatalf("%s has no TimeoutStartSec=<n>min line", unit)
		}
		minutes, _ := strconv.Atoi(string(m[1]))
		if minutes <= largestAfterLandingLimitMinutes+selfUpdateBuildMinutes {
			t.Errorf("%s stops at %d min, which cuts a %d min deploy after a %d min build", unit, minutes, largestAfterLandingLimitMinutes, selfUpdateBuildMinutes)
		}
	}
}
