package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// Two grists within a minute fetch their rig once; a grist a minute on
// fetches again, and another rig is fetched on its own account.
func TestGristsWithinAMinuteFetchTheirRigOnce(t *testing.T) {
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	grinds := apptest.NewFakeGrinds()
	refreshes := &application.GrindRefreshes{Every: application.GrindRefreshEvery, Now: func() time.Time { return clock }}
	ctx := context.Background()

	for range 2 {
		if err := refreshes.Refresh(ctx, grinds, "/rigs/cairn"); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(20 * time.Second)
	}
	if got := grinds.Refreshes("/rigs/cairn"); got != 1 {
		t.Fatalf("expected two grists 20 seconds apart to fetch once, got %d", got)
	}
	if err := refreshes.Refresh(ctx, grinds, "/rigs/spellforge"); err != nil || grinds.Refreshes("/rigs/spellforge") != 1 {
		t.Fatalf("expected another rig to be fetched on its own, got %d, %v", grinds.Refreshes("/rigs/spellforge"), err)
	}
	clock = clock.Add(time.Minute)
	if err := refreshes.Refresh(ctx, grinds, "/rigs/cairn"); err != nil || grinds.Refreshes("/rigs/cairn") != 2 {
		t.Fatalf("expected a grist a minute later to fetch again, got %d, %v", grinds.Refreshes("/rigs/cairn"), err)
	}
}

// A fetch that failed is not tried again within the minute, and every grist
// in it is told so.
func TestAFailedFetchIsReportedAndNotRetriedWithinAMinute(t *testing.T) {
	clock := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	grinds := apptest.NewFakeGrinds()
	grinds.RefreshErr = errors.New("no route to host")
	refreshes := &application.GrindRefreshes{Every: application.GrindRefreshEvery, Now: func() time.Time { return clock }}

	for range 2 {
		if err := refreshes.Refresh(context.Background(), grinds, "/rigs/cairn"); err == nil {
			t.Fatal("expected the failed fetch to be reported")
		}
	}
	if got := grinds.Refreshes("/rigs/cairn"); got != 1 {
		t.Fatalf("expected one try, got %d", got)
	}
}
