package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func hostsFile(t *testing.T, text string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

const hubHosts = "127.0.0.1 localhost\n10.88.0.4 laptop.mw # mw-wg\n"

func TestTheHostsProbeIsFaultyOnTheHubWhenTheFileIsNotReadableByOthers(t *testing.T) {
	check := doctor.NewHostsReadable(hostsFile(t, hubHosts, 0o600))

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"/etc/hosts", "600", "chmod 644 /etc/hosts", "DynamicUser", "alerts-mail", "laptop.mw"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %s", want, reason)
		}
	}
}

func TestTheHostsProbeIsOkOnTheHubWhenTheFileIsMode644(t *testing.T) {
	check := doctor.NewHostsReadable(hostsFile(t, hubHosts, 0o644))

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected ok and nothing said, got %s (%s)", verdict, reason)
	}
}

func TestTheHostsProbeIsOkWhenTheFileIsNotTheHubsEvenIfPrivate(t *testing.T) {
	check := doctor.NewHostsReadable(hostsFile(t, "127.0.0.1 localhost\n", 0o600))

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheHostsProbeIsOkWhenThereIsNoHostsFile(t *testing.T) {
	check := doctor.NewHostsReadable(filepath.Join(t.TempDir(), "hosts"))

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheHostsCureNamesTheFixAndChangesNothing(t *testing.T) {
	path := hostsFile(t, hubHosts, 0o600)
	check := doctor.NewHostsReadable(path)
	check.Probe(context.Background())

	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "chmod 644 /etc/hosts") {
		t.Fatalf("expected the cure to fail naming the fix, got %v", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("the cure changed the file's mode to %o", info.Mode().Perm())
	}
}
