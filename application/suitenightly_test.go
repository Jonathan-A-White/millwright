package application_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestTheNightlySuiteRunsARigAddedToTheRigsAfterItWasMade(t *testing.T) {
	rules := apptest.NewFakeEpicRules()
	rules.Require("leaderboard", domain.EpicRequirements{NightlySuite: true})
	rigs := map[string]string{}
	var ran []string
	suite := &application.NightlySuite{
		Hour: 3, Rigs: map[string]string{"lampas": "/rigs/lampas"}, // as the follower started
		ReadRigs: func() (map[string]string, error) {
			now := map[string]string{"lampas": "/rigs/lampas"}
			for name, dir := range rigs {
				now[name] = dir
			}
			return now, nil
		},
		Rules: rules, History: &apptest.FakeSuiteRecords{},
		Home: func(context.Context) (bool, error) { return true, nil },
		Run:  func(_ context.Context, rig string) error { ran = append(ran, rig); return nil },
		Now:  func() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) },
	}
	job := suite.Job()
	if reason, ok := job.Probe(context.Background()); ok {
		t.Fatalf("leaderboard is not among the rigs yet, but the job was sprung: %q", reason)
	}
	rigs["leaderboard"] = "/rigs/leaderboard"
	if _, ok := job.Probe(context.Background()); !ok {
		t.Fatal("a rig added while the follower runs was not due on the next pass")
	}
	mustDo(t, job.Run(context.Background()))
	if !reflect.DeepEqual(ran, []string{"leaderboard"}) {
		t.Fatalf("expected the added rig's suite run, got %v", ran)
	}
}
