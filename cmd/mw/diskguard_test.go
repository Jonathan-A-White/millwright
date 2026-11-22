package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// mw-6ww.185.7: stop_free_gb of the [dispatch] table is the floor of the guard
// the dispatch tick runs, 5 by default and 0 for off.
func TestHostDiskGuardTakesTheStopFloorKey(t *testing.T) {
	for _, tc := range []struct {
		table  string
		wantGB int64
	}{{"", 5}, {"stop_free_gb = 9\n", 9}, {"stop_free_gb = 0\n", 0}} {
		mwConfig(t, "[dispatch]\nroom_load_per_core = 0.6\n"+tc.table)
		room, err := config.Room()
		if err != nil {
			t.Fatal(err)
		}
		guard, ok := hostDiskGuard("desktop", room, application.Mailbox(nil), io.Discard)
		if !ok {
			t.Fatal("a host with a home directory has a disk guard")
		}
		if guard.StopFreeGB != tc.wantGB {
			t.Errorf("%q: StopFreeGB = %d, want %d", tc.table, guard.StopFreeGB, tc.wantGB)
		}
		if guard.Free == nil || guard.Holders == nil || guard.Lock == nil {
			t.Errorf("%q: the guard has no way to free space, name its holders or keep one tick at a time: %+v", tc.table, guard)
		}
	}
}

// The dispatch tick runs the guard before dispatch.Run, which is what starts anything.
func TestTheDispatchTickRunsTheDiskGuardBeforeItStartsAnything(t *testing.T) {
	raw, err := os.ReadFile("dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	guard, run := strings.Index(src, "runDiskGuard(cmd, host, room"), strings.Index(src, "dispatch.Run(cmd.Context())")
	if guard < 0 || run < 0 || guard > run {
		t.Fatalf("runDiskGuard is at %d and dispatch.Run at %d: the guard must come first", guard, run)
	}
}
