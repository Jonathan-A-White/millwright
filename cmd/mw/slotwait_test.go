package main

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

type aBenchmarkBook []application.Benchmark

func (b aBenchmarkBook) Recent(context.Context) ([]application.Benchmark, error) { return b, nil }

func TestTheSlotWaitIsScaledByTheGateOfTheRigCheckedOutInTheSlotsDirectory(t *testing.T) {
	book := aBenchmarkBook{{Rig: "lampas", Host: "laptop", GateSeconds: 29 * 60}}
	rigs := map[string]string{"lampas": "/home/x/lampas", "millwright": "/home/x/millwright"}

	wait := func(rigDir string) time.Duration {
		return gateScaledWait(book, rigs, "laptop", application.BenchmarkSettings{})(context.Background(), rigDir)
	}
	if got := wait("/home/x/lampas/"); got != 58*time.Minute {
		t.Errorf("lampas: want 58m0s, got %s", got)
	}
	if got := wait("/home/x/millwright"); got != rig.SlotWait {
		t.Errorf("a rig with no gate history: want %s, got %s", rig.SlotWait, got)
	}
	if got := wait("/home/x/elsewhere"); got != rig.SlotWait {
		t.Errorf("a directory no rig is in: want %s, got %s", rig.SlotWait, got)
	}
}
