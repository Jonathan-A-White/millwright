package application_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// mw-gq6.1749: lampas batch 694 was held on main's red (sermons.spec.ts:396),
// carried by mw-fnvjof.17.290, and the batch held the carrier among five other
// stories. The follower never asked again for an hour: a hold whose batch held
// the carrier was read as its fix having failed (mw-gq6.1708), though the carrier
// had never been landed alone, and the carrier and nine stories waited for the
// Mayor, whose hand run of it alone landed it green.

// aRigHeldWithItsCarrierAmongOthers is lampas held on main's red at head abc123
// by a batch of six that held the carrier lp-9 with five others, and more queued.
func aRigHeldWithItsCarrierAmongOthers(t *testing.T) *aMainRedRig {
	t.Helper()
	r := &aMainRedRig{j: newLandingJob(t), head: "abc123"}
	r.j.rule("lampas", domain.LandingBatch)
	held := []string{"lp-1", "lp-2", "lp-3", "lp-4", "lp-5", lampasCarrier}
	for i, id := range append(held, "lp-6", "lp-7", "lp-8") {
		r.j.queued("lampas", id, 300-i*10)
	}
	line := mainRedBatch(r.j.now.Add(-10*time.Minute), 694, "abc123", held...)
	line.MainBug, line.MainTests = lampasCarrier, []string{"sermons.spec.ts:396"}
	history := &apptest.FakeBatchMetrics{}
	mustDo(t, history.AppendBatch(context.Background(), line))
	r.landings = r.j.landingsOn(r.j.tracker)
	r.landings.History = history
	r.landings.MainHead = func(_ context.Context, rig, branch string) (string, error) {
		r.asked = append(r.asked, [2]string{rig, branch})
		return r.head, r.headErr
	}
	return r
}

func TestTheFollowerLandsTheRedCarrierAloneOnceTheBackoffAfterAHeldBatchEnds(t *testing.T) {
	r := aRigHeldWithItsCarrierAmongOthers(t)
	var tries int
	r.landings.LandScoped = func(ctx context.Context, rig string, scope application.LandScope) error {
		tries++
		if tries == 1 {
			r.j.asked = append(r.j.asked, rig+" only="+strings.Join(scope.Only, ","))
			return errors.New("held: the rig's tests failed")
		}
		return r.j.landScoped(ctx, rig, scope) // records the ask
	}
	job, ctx := r.landings.Job(), context.Background()

	// At the hold: the carrier was in the batch with others, so it has not been
	// tried alone, and it is landed alone at once.
	_ = job.Run(ctx)
	if want := []string{"lampas only=" + lampasCarrier}; !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected the carrier landed alone at the hold, got %v", r.j.asked)
	}
	// A landing that failed waits, as any does.
	r.j.now = r.j.now.Add(domain.DefaultBatchEvery - time.Minute)
	if reason, due := job.Probe(ctx); due {
		t.Fatalf("expected the rig to wait inside the backoff, got %q", reason)
	}
	// After batch_every, with no bead event between, it is asked again.
	r.j.now = r.j.now.Add(2 * time.Minute)
	if reason, due := job.Probe(ctx); !due || !strings.Contains(reason, lampasCarrier) {
		t.Fatalf("expected the carrier due once the backoff ended, got %q, %v", reason, due)
	}
	mustDo(t, job.Run(ctx))
	want := []string{"lampas only=" + lampasCarrier, "lampas only=" + lampasCarrier}
	if !reflect.DeepEqual(r.j.asked, want) {
		t.Fatalf("expected the carrier landed alone on the second ask, got %v", r.j.asked)
	}
}

// A follower that restarted inside the backoff has no memory of it, and still
// asks for the rig once it ends: the held batch's line is in the history, not in
// the follower.
func TestAFollowerRestartedInsideTheBackoffStillSchedulesTheCarrierOnceItEnds(t *testing.T) {
	r := aRigHeldWithItsCarrierAmongOthers(t)
	r.landings.LandScoped = func(_ context.Context, rig string, scope application.LandScope) error {
		r.j.asked = append(r.j.asked, rig+" only="+strings.Join(scope.Only, ","))
		return errors.New("held: the rig's tests failed")
	}
	_ = r.landings.Job().Run(context.Background())
	r.j.now = r.j.now.Add(10 * time.Minute) // inside the backoff

	log := &apptest.FakeEventLog{}
	restarted := func() *application.EventSpring {
		landings := r.j.landingsOn(r.j.tracker)
		landings.History, landings.MainHead = r.landings.History, r.landings.MainHead
		landings.LandScoped = func(ctx context.Context, rig string, scope application.LandScope) error {
			return r.j.landScoped(ctx, rig, scope)
		}
		return &application.EventSpring{Log: log, Host: "laptop", Jobs: []application.SpringJob{landings.Job()},
			Now: func() time.Time { return r.j.now }}
	}
	follower := restarted()
	ctx := context.Background()
	mustDo(t, follower.Spring(ctx)) // its bearings
	r.j.now = r.j.now.Add(domain.DefaultBatchEvery)
	mustDo(t, follower.Spring(ctx)) // the backoff has ended
	follower.Wait()

	var scheduled []string
	for _, e := range log.All() {
		if e.Kind == events.KindJob && e.Actor == "batch-land@laptop" && e.To == events.JobScheduled {
			scheduled = append(scheduled, e.Detail)
		}
	}
	if len(scheduled) != 1 || !strings.Contains(scheduled[0], lampasCarrier) {
		t.Fatalf("expected batch-land scheduled once for the carrier, got %q", scheduled)
	}
	if got := r.j.asked[len(r.j.asked)-1]; got != "lampas only="+lampasCarrier {
		t.Fatalf("expected the carrier landed alone, got %v", r.j.asked)
	}
}
