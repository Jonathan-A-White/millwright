package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

type fakeUnits struct {
	installed map[string]bool
	started   []string
}

func (f *fakeUnits) Installed(_ context.Context, unit string) bool { return f.installed[unit] }
func (f *fakeUnits) Start(_ context.Context, unit string) error {
	f.started = append(f.started, unit)
	return nil
}

func TestHomeSpringRunsTheTimersOwnUnitsForTheInstalledJobsAndSaysWhatIsMissing(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	units := &fakeUnits{installed: map[string]bool{dispatchUnit: true, mailNotifyUnit: true}}
	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = units

	var said bytes.Buffer
	log := t.TempDir() + "/log.jsonl"
	spring, err := homeSpring(log, "laptop", &said)
	if err != nil {
		t.Fatal(err)
	}
	if len(spring.Jobs) != 2 || spring.Jobs[0].Name != "dispatch" || spring.Jobs[1].Name != "mail-notify" {
		t.Fatalf("the jobs are %+v, want dispatch and mail-notify", spring.Jobs)
	}
	if !strings.Contains(said.String(), "mw-millhand-tick.service is not installed here") {
		t.Fatalf("the missing unit was not said: %q", said.String())
	}
	if _, ok := spring.Jobs[0].Wants(events.Event{Kind: events.KindBeadChanged, Bead: "mw-a", From: events.Start, To: events.BeadOpen}); !ok {
		t.Fatal("a bead opened does not spring the dispatch")
	}
	if err := spring.Jobs[0].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := spring.Jobs[1].Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(units.started, " ") != dispatchUnit+" "+mailNotifyUnit {
		t.Fatalf("started %v", units.started)
	}
	var _ application.EventSpringer = spring
}
