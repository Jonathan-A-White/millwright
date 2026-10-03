package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

const gb = 1_000_000_000

func rootDiskUsed(n int64) func() (int64, error) {
	return func() (int64, error) { return n, nil }
}

func TestRootDiskBudgetIsInertWithNoBudget(t *testing.T) {
	check := &doctor.RootDiskBudget{UsedBytes: rootDiskUsed(500 * gb)}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || reason != "n/a: no root_disk_budget_bytes set" {
		t.Fatalf("expected an ok saying no root_disk_budget_bytes is set; got %v %q", verdict, reason)
	}
}

func TestRootDiskBudgetIsFaultyPastBudgetMinusMarginNamingTheFigures(t *testing.T) {
	check := &doctor.RootDiskBudget{Budget: 100 * gb, Margin: 10 * gb, UsedBytes: rootDiskUsed(91 * gb)}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v %q", verdict, reason)
	}
	for _, want := range []string{"91.0 GB", "100.0 GB", "10.0 GB"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("expected the reason to name %q, got %q", want, reason)
		}
	}
}

func TestRootDiskBudgetIsOKAtOrUnderBudgetMinusMargin(t *testing.T) {
	for _, used := range []int64{20 * gb, 90 * gb} {
		check := &doctor.RootDiskBudget{Budget: 100 * gb, Margin: 10 * gb, UsedBytes: rootDiskUsed(used)}
		verdict, reason := check.Probe(context.Background())
		if verdict != application.DoctorOK {
			t.Fatalf("used %d: expected ok, got %v %q", used, verdict, reason)
		}
		if !strings.Contains(reason, "100.0 GB") {
			t.Fatalf("expected the ok reason to carry the figures, got %q", reason)
		}
	}
}

func TestRootDiskBudgetMarginDefaultsToTenGigabytes(t *testing.T) {
	check := &doctor.RootDiskBudget{Budget: 100 * gb, UsedBytes: rootDiskUsed(91 * gb)}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected the 10 GB default margin to make 91 of 100 faulty, got %v %q", verdict, reason)
	}
}

func TestRootDiskBudgetCannotTellWhenTheRootCannotBeRead(t *testing.T) {
	check := &doctor.RootDiskBudget{Budget: 100 * gb, UsedBytes: func() (int64, error) { return 0, errors.New("statfs: boom") }}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "boom") {
		t.Fatalf("expected cannot-tell naming the error, got %v %q", verdict, reason)
	}
}

func TestRootDiskBudgetCureWritesOneNoteToTheMayorAndNothingElse(t *testing.T) {
	var notes []string
	check := &doctor.RootDiskBudget{
		Budget: 100 * gb, Margin: 10 * gb, UsedBytes: rootDiskUsed(91 * gb),
		Note: func(_ context.Context, text string) error { notes = append(notes, text); return nil },
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("expected the cure to succeed, got %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected one note, got %v", notes)
	}
	for _, want := range []string{"root-disk-budget:", "91.0 GB of a 100.0 GB budget", "under 10.0 GB headroom", "Optimize-VHD", "root_disk_budget_bytes is raised"} {
		if !strings.Contains(notes[0], want) {
			t.Fatalf("expected the note to hold %q, got %q", want, notes[0])
		}
	}
}

func TestRootDiskBudgetCureFailsWhenTheNoteCannotBeWritten(t *testing.T) {
	check := &doctor.RootDiskBudget{
		Budget: 100 * gb, UsedBytes: rootDiskUsed(95 * gb),
		Note: func(context.Context, string) error { return errors.New("offline") },
	}
	if err := check.Cure(context.Background()); err == nil {
		t.Fatal("expected a failed note to fail the cure, so the damper tries again")
	}
}

func TestRootDiskBudgetDamperAndWayBack(t *testing.T) {
	check := &doctor.RootDiskBudget{}
	wait, cap := check.Damper()
	if wait != doctor.RootDiskBudgetDamperWait || cap != doctor.RootDiskBudgetDamperCap {
		t.Fatalf("damper constants not returned: %v %d", wait, cap)
	}
	if !strings.Contains(check.WayBack(), "root_disk_budget_bytes = 0") {
		t.Fatalf("expected the way back to say how to make the check inert, got %q", check.WayBack())
	}
}

func TestRootDiskBudgetRealStatfsReadsSomeUsedBytes(t *testing.T) {
	used, err := doctor.RootUsedBytes()
	if err != nil || used <= 0 {
		t.Fatalf("expected the real root to have used bytes, got %d: %v", used, err)
	}
}
