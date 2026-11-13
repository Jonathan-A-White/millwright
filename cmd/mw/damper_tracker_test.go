package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// listsAll is how many times the bd recorded in asked was run with list --all.
func listsAll(t *testing.T, asked string) int {
	t.Helper()
	var lists int
	for _, line := range strings.Split(strings.TrimSpace(readAsked(t, asked)), "\n") {
		if strings.Contains(line, " list --all") {
			lists++
		}
	}
	return lists
}

// mw-6ww.175.30: mw stuck --dampers read the damper counts through a gateway
// that kept nothing, so each read of the beads in them ran bd list --all afresh
// (the list is 120 MB on the live vault). It reads from the status tracker, as
// mw report does (TestReportTrackerReadsEveryBeadOnce), so the list is read once.
func TestStuckDampersTrackerReadsEveryBeadOnce(t *testing.T) {
	asked := aBdThatRecords(t)
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "laptop")

	cmd := newStuckCmd()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs([]string{"--dampers"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("mw stuck --dampers: %v\n%s", err, errs.String())
	}
	if lists := listsAll(t, asked); lists != 1 {
		t.Errorf("mw stuck --dampers ran bd list --all %d times, want once:\n%s", lists, readAsked(t, asked))
	}
}

// mw-6ww.175.30: the review hold of mw postern send counts his review queue
// through the survey; it read the beads through the uncached posternGateway, so
// the queue's reads of the work in hand each ran bd list --all. The real reader
// reads from the status tracker, so the list is read once however many reads the
// count makes, and the hold may count again for the cloud make lines and the
// queue's size.
func TestPosternReviewQueueTrackerReadsEveryBeadOnce(t *testing.T) {
	asked := aBdThatRecords(t)
	dir := t.TempDir()
	t.Setenv("MW_VAULT", dir)
	t.Setenv("MW_HOST", "laptop")

	queue := reviewQueueReader(mwGateway(dir, "laptop"))
	if _, _, err := queue.ReviewQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lists := listsAll(t, asked); lists != 1 {
		t.Errorf("the review hold ran bd list --all %d times for one count, want once:\n%s", lists, readAsked(t, asked))
	}
}
