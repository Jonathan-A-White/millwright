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
	if len(spring.Jobs) != 3 || spring.Jobs[0].Name != "dispatch" || spring.Jobs[1].Name != "mail-notify" ||
		spring.Jobs[2].Name != "chain-stamp" || spring.Jobs[2].Every != chainStampEvery {
		t.Fatalf("the jobs are %+v, want dispatch, mail-notify and chain-stamp", spring.Jobs)
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

func TestHomeSpringHasAGristJobOnlyWhereTheConfigHasAGristTable(t *testing.T) {
	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = &fakeUnits{installed: map[string]bool{}}
	for _, c := range []struct {
		name, config string
		want         bool
	}{
		{"no [grist] table", "host = \"laptop\"\n", false},
		{"a [grist] table", "host = \"laptop\"\n\n[grist]\nconcurrency = 2\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			mwConfig(t, c.config)
			spring, err := homeSpring(t.TempDir()+"/log.jsonl", "laptop", &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			has := false
			for _, j := range spring.Jobs {
				if j.Name == application.GristJobName {
					has = true
					if j.Probe == nil || j.Every != 0 {
						t.Fatalf("the grist job is %+v: it is sprung by a probe, on no clock", j)
					}
				}
			}
			if has != c.want {
				t.Fatalf("the grist job is there: %v, want %v", has, c.want)
			}
		})
	}
}
