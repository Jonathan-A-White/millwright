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

const (
	wslProcVersion   = "Linux version 6.18.33.2-microsoft-standard-WSL2 (root@build) (gcc 13.2.0) #1 SMP"
	plainProcVersion = "Linux version 6.8.0-45-generic (buildd@lcy02) (gcc 13.2.0) #45-Ubuntu SMP"
)

func procVersion(text string) func() (string, error) {
	return func() (string, error) { return text, nil }
}

func TestRootDiskBudgetIsInertWithNoBudget(t *testing.T) {
	check := &doctor.RootDiskBudget{UsedBytes: rootDiskUsed(500 * gb), ProcVersion: procVersion(plainProcVersion)}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || reason != "n/a: no root_disk_budget_bytes set" {
		t.Fatalf("expected an ok saying no root_disk_budget_bytes is set; got %v %q", verdict, reason)
	}
}

func TestRootDiskBudgetWarnsOnAWSLHostWithNoBudgetNamingTheConfigKey(t *testing.T) {
	check := &doctor.RootDiskBudget{UsedBytes: rootDiskUsed(300 * gb), ProcVersion: procVersion(wslProcVersion)}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected a warning (cannot-tell) on a WSL host with no budget, got %v %q", verdict, reason)
	}
	for _, want := range []string{"root_disk_budget_bytes", "[doctor]", "this host", "lower bound"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("expected the warning to name %q, got %q", want, reason)
		}
	}
}

func TestRootDiskBudgetWithABudgetOnWSLIsJudgedNotWarned(t *testing.T) {
	check := &doctor.RootDiskBudget{Budget: 100 * gb, UsedBytes: rootDiskUsed(20 * gb), ProcVersion: procVersion(wslProcVersion)}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || strings.Contains(reason, "n/a") {
		t.Fatalf("expected an ok carrying the figures, got %v %q", verdict, reason)
	}
}

func TestRootDiskBudgetIsInertWhenProcVersionCannotBeRead(t *testing.T) {
	check := &doctor.RootDiskBudget{ProcVersion: func() (string, error) { return "", errors.New("no /proc") }}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || !strings.HasPrefix(reason, "n/a") {
		t.Fatalf("expected n/a when the host cannot be told to be WSL, got %v %q", verdict, reason)
	}
}

func TestIsWSLVersion(t *testing.T) {
	if !doctor.IsWSLVersion(wslProcVersion) || !doctor.IsWSLVersion("Linux version 4.4.0-19041-Microsoft") {
		t.Fatal("expected WSL2 and WSL1 /proc/version lines to read as WSL")
	}
	if doctor.IsWSLVersion(plainProcVersion) {
		t.Fatal("expected a plain Linux /proc/version not to read as WSL")
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
