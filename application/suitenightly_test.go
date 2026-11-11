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

func TestTheSuiteHostDoesNotBeginAgainBeforeSuiteEveryWhenARunKeptNoResult(t *testing.T) {
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{NightlySuite: true, SuiteEvery: 4 * time.Hour, SuiteHost: "desktop"})
	clock := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	var ran int
	suite := &application.NightlySuite{
		Hour: 8, Rigs: map[string]string{"lampas": "/rigs/lampas"}, Host: "desktop",
		Rules: rules, History: &apptest.FakeSuiteRecords{}, Heads: apptest.NewFakeRigHeads(),
		Home: func(context.Context) (bool, error) { return false, nil },
		Run:  func(context.Context, string) error { ran++; return nil },
		Now:  func() time.Time { return clock },
	}
	job := suite.Job()
	mustDo(t, job.Run(context.Background()))
	clock = clock.Add(3 * time.Hour)
	if _, ok := job.Probe(context.Background()); ok {
		t.Fatal("a run that kept no result was tried again before suite_every had passed")
	}
	clock = clock.Add(90 * time.Minute)
	if _, ok := job.Probe(context.Background()); !ok {
		t.Fatal("the suite host did not try again once suite_every had passed")
	}
	if ran != 1 {
		t.Fatalf("expected one run, got %d", ran)
	}
}

func TestTheSuiteHostLeavesARigOutWhenMainsHeadCannotBeRead(t *testing.T) {
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{NightlySuite: true, SuiteEvery: 4 * time.Hour, SuiteHost: "desktop"})
	records := &apptest.FakeSuiteRecords{}
	if _, err := records.PutSuiteFile(context.Background(), "lampas", "2026-10-21.json", `{"sha":"abc","started":"2026-10-21T01:00:00Z"}`); err != nil {
		t.Fatal(err)
	}
	suite := &application.NightlySuite{
		Hour: 8, Rigs: map[string]string{"lampas": "/rigs/lampas"}, Host: "desktop",
		Rules: rules, History: records, Heads: apptest.NewFakeRigHeads(), // knows no head
		Home: func(context.Context) (bool, error) { return false, nil },
		Run:  func(context.Context, string) error { t.Fatal("ran without a head"); return nil },
		Now:  func() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) },
	}
	if _, ok := suite.Job().Probe(context.Background()); ok {
		t.Fatal("the job was sprung with no head to compare")
	}
}
