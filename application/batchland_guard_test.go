package application

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// batchRules is a rig's file that says it lands in batches.
type batchRules struct{}

func (batchRules) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingBatch}}, nil
}

// Only mw land pushes the target branch of a rig that lands in batches: a
// close-out's merge that reaches one (its first read of the rig's file having
// failed) stops before it opens a landing, merges or pushes anything. The
// Landing is nil, so any of those would panic.
func TestACloseOutNeverMergesOntoABatchRigsTarget(t *testing.T) {
	n := Next{Rules: batchRules{}, Host: "vps"}
	c := &closeOut{id: "mw-gq6.1", path: domain.Path{Rig: "lampas", Branch: "main"}, branch: "mw/mw-gq6.1", target: "main"}
	_, err := n.merge(context.Background(), c, &NextReport{})
	if err == nil || reasonOf(err) != ReasonLandingFailed || !strings.Contains(err.Error(), "only mw land pushes main") {
		t.Fatalf("expected the merge refused as landing-failed, naming mw land, got %v", err)
	}
}
