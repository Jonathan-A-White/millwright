package config_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// mw-6ww.185.6: min_root_free_gb is the floor on the host's own root filesystem:
// 15 until the table says another, 0 turns it off, and a [[dispatch.windows]]
// entry may carry it.
func TestMinRootFreeGBIsReadFromTheTable(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	if got, err := config.Room(); err != nil || got.MinRootFreeGB != 15 {
		t.Fatalf("expected 15 GB by default, got %+v, %v", got, err)
	}
	for said, want := range map[string]int64{"20": 20, "0": 0} {
		writeConfig(t, "[dispatch]\nmin_root_free_gb = "+said+"\n")
		if got, err := config.Room(); err != nil || got.MinRootFreeGB != want {
			t.Errorf("min_root_free_gb = %s: got %+v, %v, want %d", said, got, err, want)
		}
	}
	for _, bad := range []string{"min_root_free_gb = -1\n", "min_root_free_gb = lots\n", "min_root_free_gb = 1.5\n"} {
		writeConfig(t, "[dispatch]\n"+bad)
		if _, err := config.Room(); err == nil || !strings.Contains(err.Error(), "min_root_free_gb") {
			t.Errorf("expected %q to be refused naming the key, got %v", bad, err)
		}
	}
}

func TestADispatchWindowMayCarryMinRootFreeGB(t *testing.T) {
	writeConfig(t, "[dispatch]\nmin_root_free_gb = 10\n\n[[dispatch.windows]]\nfrom = \"00:00\"\nto = \"23:59\"\ntz = \"UTC\"\nmin_root_free_gb = 0\n")
	got, err := config.Room()
	if err != nil || got.MinRootFreeGB != 0 {
		t.Errorf("a window saying min_root_free_gb = 0 while it holds: got %+v, %v, want 0", got, err)
	}
}

// mw-6ww.185.7: stop_free_gb is the stop floor of the disk guard: 5 until the table
// says another, 0 turns it off.
func TestStopFreeGBIsReadFromTheTable(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	if got, err := config.Room(); err != nil || got.StopFreeGB != 5 {
		t.Fatalf("expected 5 GB by default, got %+v, %v", got, err)
	}
	for said, want := range map[string]int64{"8": 8, "0": 0} {
		writeConfig(t, "[dispatch]\nstop_free_gb = "+said+"\n")
		if got, err := config.Room(); err != nil || got.StopFreeGB != want {
			t.Errorf("stop_free_gb = %s: got %+v, %v, want %d", said, got, err, want)
		}
	}
	for _, bad := range []string{"stop_free_gb = -1\n", "stop_free_gb = lots\n"} {
		writeConfig(t, "[dispatch]\n"+bad)
		if _, err := config.Room(); err == nil || !strings.Contains(err.Error(), "stop_free_gb") {
			t.Errorf("expected %q to be refused naming the key, got %v", bad, err)
		}
	}
}
