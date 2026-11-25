package application

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1727: a story under a molecule (a formula run's step) is not work in an
// epic, so its close in the window adds none to the epics moved.
func TestEpicsMovedLeavesOutAFormulaRun(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r := FactoryReportResult{Window: ReportWindow{Since: now.Add(-24 * time.Hour), Until: now}}
	story := func(id, epic string) StoryDetail {
		return StoryDetail{Story: domain.Story{ID: id}, EpicID: epic, ClosedAt: now.Add(-time.Hour)}
	}

	got := r.epicsMoved([]StoryDetail{
		story("mw-mol-abc.1", "mw-mol-abc"),
		story("mw-mol-abc.2", "mw-mol-abc"),
		story("lk-1", "lk-epic"),
	})

	if got != 1 {
		t.Errorf("expected only the real epic counted as moved, got %d", got)
	}
}
