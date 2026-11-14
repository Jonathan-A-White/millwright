package doctor_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1296: a drained cloud has max_boxes 0 in the home's config and in the
// vault's copy while the boost's config says what it did; boost-config reads
// only the tables of the boost, never [cloud], so it does not alarm.
func TestBoostConfigDoesNotAlarmOnADrainedCloud(t *testing.T) {
	config := "[cloud]\nmax_boxes = 0\ndrained_max_boxes = 4\nbox_cap = 2\n\n[tests]\nargus = \"npm test\"\n"
	target := "[cloud]\nmax_boxes = 4\nbox_cap = 2\n\n[tests]\nargus = \"npm test\"\n"
	check := boostConfigOnTheHome(t, config, target)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected healthy on a drained cloud, got %s (%s)", verdict, reason)
	}
}
