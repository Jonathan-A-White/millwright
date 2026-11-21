package main

import (
	"bytes"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw land runs its own application.Next (batchLandFor), so the rig's batch claim
// across hosts must be wired there too, naming this host (mw-gq6.1616).
func TestTheBatchLandersClaimIsKeptInTheTrackerNamingThisHost(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	batch := batchLandFor(application.Next{Tracker: tracker, Host: "cloud14"}, mwVault(t.TempDir(), "cloud14"), false, application.LandScope{}, &bytes.Buffer{})
	if batch.Claim.Notes == nil || batch.Claim.Host != "cloud14" {
		t.Fatalf("expected the batch lander to claim the rig in the tracker as cloud14, got %+v", batch.Claim)
	}
}

// A tracker with no key-value store makes no claim: the landing goes ahead.
func TestTheBatchLandersClaimIsNoneWhenTheTrackerKeepsNoNotes(t *testing.T) {
	batch := batchLandFor(application.Next{}, mwVault(t.TempDir(), "cloud14"), false, application.LandScope{}, &bytes.Buffer{})
	if batch.Claim.Notes != nil {
		t.Fatalf("expected no claim, got %+v", batch.Claim)
	}
}
