package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// The two files a close-out may commit in the vault, and the message it commits
// them under. Everything a story writes in the vault is written by one of two
// hands — mw's ledger line and the session's memory of the rig — and these are
// the paths those hands are allowed to reach.

func TestSeatWorkIsTheLedgerAndTheRigMemoryAndNothingElse(t *testing.T) {
	work := application.SeatWork("builder", "millwright")
	want := []string{"seats/builder/ledger.md", "seats/builder/rigs/millwright.md"}
	if strings.Join(work, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected %q, got %q", want, work)
	}
}

func TestSeatWorkIsTheLedgerAloneWhenNoRigWasWorked(t *testing.T) {
	work := application.SeatWork("mayor", "")
	if len(work) != 1 || work[0] != "seats/mayor/ledger.md" {
		t.Fatalf("expected the ledger alone, got %q", work)
	}
}

func TestSeatWorkIsNothingWithoutASeat(t *testing.T) {
	if work := application.SeatWork("", "millwright"); len(work) != 0 {
		t.Fatalf("expected no paths without a seat, got %q", work)
	}
}

func TestSeatWorkRefusesANameThatWouldReachOutsideTheVault(t *testing.T) {
	if work := application.SeatWork("../../etc", "millwright"); len(work) != 0 {
		t.Fatalf("expected a seat name reaching outside the vault to give no paths, got %q", work)
	}
	work := application.SeatWork("builder", "../../etc/passwd")
	if len(work) != 1 || work[0] != "seats/builder/ledger.md" {
		t.Fatalf("expected a rig name reaching outside the vault to leave the ledger alone in the list, got %q", work)
	}
}

func TestVaultCommitMessageNamesTheStoryAndSignsNothing(t *testing.T) {
	message := application.VaultCommitMessage("mw-gq6.40", "mw next cannot sync\nafter its own landing")
	for _, want := range []string{"mw-gq6.40", "mw next cannot sync after its own landing"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected the message to hold %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "\n") {
		t.Fatalf("expected one plain line, got %q", message)
	}
	if signature := application.AIAttribution(message); signature != "" {
		t.Fatalf("expected a message signed by nobody, got %q", signature)
	}
}

func TestVaultCommitMessageNamesTheStoryWhenItHasNoTitle(t *testing.T) {
	message := application.VaultCommitMessage("mw-gq6.40", "   ")
	if !strings.Contains(message, "mw-gq6.40") {
		t.Fatalf("expected the message to name the story, got %q", message)
	}
}
