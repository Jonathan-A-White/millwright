package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestACapacityBlockPrintsEachValueAndLeavesTheRigOk(t *testing.T) {
	report := application.ReportCapacity("leaderboard", domain.Capacity{
		ExpectedViewers: 300, SSECap: 500, NginxWorkerConnections: 1024, MemoryFreeMB: 800,
	})

	if report.Faulty() {
		t.Errorf("expected no fault, got %v", report.Missing)
	}
	out := report.String()
	for _, want := range []string{
		"leaderboard: expected concurrent viewers 300",
		"leaderboard: SSE cap 500",
		"leaderboard: nginx worker_connections 1024",
		"leaderboard: memory free 800 MB",
		"ok: leaderboard records every capacity fact",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the report, got:\n%s", want, out)
		}
	}
}

func TestARigWithNoCapacityBlockNamesEachMissingValueAndIsFaulty(t *testing.T) {
	report := application.ReportCapacity("leaderboard", domain.Capacity{})

	if !report.Faulty() || len(report.Missing) != 4 {
		t.Fatalf("expected four missing facts, got %v", report.Missing)
	}
	out := report.String()
	for _, want := range []string{
		"leaderboard: expected concurrent viewers MISSING (capacity_viewers in rigs/leaderboard.toml)",
		"leaderboard: SSE cap MISSING (capacity_sse_cap in rigs/leaderboard.toml)",
		"leaderboard: nginx worker_connections MISSING (capacity_nginx_worker_connections in rigs/leaderboard.toml)",
		"leaderboard: memory free MISSING (capacity_memory_free_mb in rigs/leaderboard.toml)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the report, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ok:") {
		t.Errorf("a rig missing facts is not ok, got:\n%s", out)
	}
}

func TestACapacityBlockMissingOneValueNamesOnlyThatOne(t *testing.T) {
	report := application.ReportCapacity("leaderboard", domain.Capacity{ExpectedViewers: 300, SSECap: 500, MemoryFreeMB: 800})

	if len(report.Missing) != 1 || !strings.Contains(report.Missing[0], "nginx worker_connections") {
		t.Fatalf("expected only nginx worker_connections missing, got %v", report.Missing)
	}
	if !strings.Contains(report.String(), "leaderboard: SSE cap 500") {
		t.Errorf("expected the recorded values still printed, got:\n%s", report.String())
	}
}
