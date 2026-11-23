package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// folderRecords is a batch's records that know which numbers the vault's origin
// already holds, and keep a taken batch's folder apart as the vault does.
type folderRecords struct {
	batchFilesKept
	taken map[int]bool
	apart []int
}

func (r *folderRecords) BatchTakenInOrigin(_ context.Context, _ string, number int) (bool, error) {
	return r.taken[number], nil
}

func (r *folderRecords) KeepBatchApart(_ context.Context, rig string, number int, why string) (string, error) {
	r.apart = append(r.apart, number)
	return application.BatchDir + "/" + rig + "/apart-of-" + "x", nil
}

// mw-gq6.1665: a landing whose folder another host already put in the origin
// commits none of its records under that number: they would overwrite the
// other host's, which survived only in history. It says so.
func TestALandingWhoseBatchFolderIsAlreadyInTheOriginCommitsNothingUnderItsNumber(t *testing.T) {
	files := &apptest.FakeVaultFiles{}
	records := &folderRecords{batchFilesKept: batchFilesKept{put: map[string]string{}}, taken: map[int]bool{637: true}}
	land := application.BatchLand{Next: application.Next{Files: files, Seat: "builder"}, Records: records}
	report := &application.BatchReport{
		Rig: "lampas", Number: 637, Landed: true,
		Records: []string{"runs/batch/lampas/637/outcome.txt", "runs/batch/lampas/637/gate.log"},
	}
	land.CommitTheRecords(context.Background(), report)

	for _, commit := range files.Commits() {
		for _, p := range commit.Paths {
			if strings.HasPrefix(p, "runs/batch/lampas/637/") {
				t.Fatalf("expected nothing committed under the taken number 637, got %v", commit.Paths)
			}
		}
	}
	if len(records.apart) != 1 || records.apart[0] != 637 {
		t.Fatalf("expected the folder of batch 637 kept apart, got %v", records.apart)
	}
	said := strings.Join(report.Notes, "\n")
	if !strings.Contains(said, "637") || !strings.Contains(said, "already") {
		t.Fatalf("expected the notes to say batch 637 is already in the vault's origin, got:\n%s", said)
	}
}

// A folder the origin does not hold is committed as ever.
func TestALandingWhoseBatchFolderIsFreeInTheOriginCommitsIt(t *testing.T) {
	files := &apptest.FakeVaultFiles{}
	records := &folderRecords{batchFilesKept: batchFilesKept{put: map[string]string{}}, taken: map[int]bool{636: true}}
	land := application.BatchLand{Next: application.Next{Files: files, Seat: "builder"}, Records: records}
	report := &application.BatchReport{Rig: "lampas", Number: 637, Landed: true, Records: []string{"runs/batch/lampas/637/outcome.txt"}}
	land.CommitTheRecords(context.Background(), report)
	if len(files.Commits()) != 1 || len(records.apart) != 0 {
		t.Fatalf("expected the records committed, got %v, apart %v", files.Commits(), records.apart)
	}
}
