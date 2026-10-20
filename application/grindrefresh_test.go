package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// Three grists queued behind a fetch that timed out do not fetch again: the
// one slow fetch costs one limit, not one per grist (mw-gq6.647).
func TestGristsBehindATimedOutFetchDoNotFetchAgain(t *testing.T) {
	clock := time.Date(2026, 10, 20, 16, 20, 0, 0, time.UTC)
	grinds := apptest.NewFakeGrinds()
	grinds.RefreshErr = fmt.Errorf("fetching main: %w", application.ErrGrindRefreshTimedOut)
	refreshes := &application.GrindRefreshes{Every: application.GrindRefreshEvery, Now: func() time.Time { return clock }}

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = refreshes.Refresh(context.Background(), grinds, "/rigs/lampas")
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if !errors.Is(err, application.ErrGrindRefreshTimedOut) {
			t.Errorf("grist %d: expected the timed-out refresh, got %v", i, err)
		}
	}
	if got := grinds.Refreshes("/rigs/lampas"); got != 1 {
		t.Fatalf("expected one fetch for three grists, got %d", got)
	}
}
