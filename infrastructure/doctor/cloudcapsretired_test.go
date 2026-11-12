package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func capsAndRetired(caps, retired []string) *doctor.CloudCapsRetired {
	return &doctor.CloudCapsRetired{
		Caps:    func() ([]string, error) { return caps, nil },
		Retired: func() ([]string, error) { return retired, nil },
	}
}

func TestCloudCapsRetiredWarnsNamingARetiredHostInTheCaps(t *testing.T) {
	check := capsAndRetired([]string{"laptop", "desktop"}, []string{"desktop"})
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "desktop") || strings.Contains(reason, "laptop") || !strings.Contains(reason, "[cloud.caps]") {
		t.Fatalf("expected a warning naming desktop alone, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "no cure") {
		t.Fatalf("expected no cure, got %v", err)
	}
}

func TestCloudCapsRetiredIsOkWhenNoCapsHostIsRetired(t *testing.T) {
	for name, check := range map[string]*doctor.CloudCapsRetired{
		"none retired": capsAndRetired([]string{"laptop", "desktop"}, nil),
		"no caps":      capsAndRetired(nil, []string{"desktop"}),
		"other host":   capsAndRetired([]string{"laptop"}, []string{"desktop"}),
	} {
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Errorf("%s: expected ok, got %s (%s)", name, verdict, reason)
		}
	}
}

func TestCloudCapsRetiredCannotTellWhenTheConfigDoesNotRead(t *testing.T) {
	check := &doctor.CloudCapsRetired{
		Caps:    func() ([]string, error) { return nil, errors.New("bad toml") },
		Retired: func() ([]string, error) { return nil, nil },
	}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
	check = &doctor.CloudCapsRetired{
		Caps:    func() ([]string, error) { return nil, nil },
		Retired: func() ([]string, error) { return nil, errors.New("bad toml") },
	}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
}
