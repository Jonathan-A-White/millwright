package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// mw-gq6.1469: the dispatch mw next runs after a close-out was built with no Room, so
// a Builder started straight after a close-out skipped room_load_per_core,
// room_min_free_mb and the WSL drive floor that the timer's dispatch honours. Every
// dispatch cmd/mw builds takes the host's room through withHostRoom.
func TestEveryDispatchCmdMwBuildsIsGivenTheHostRoom(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !strings.Contains(line, "application.Dispatch{") {
				continue
			}
			built++
			if !strings.Contains(line, "withHostRoom(withHostRechecks(application.Dispatch{") {
				t.Errorf("%s:%d builds a dispatch with no room: wrap it in withHostRoom(..., room), as the timer's dispatch does", file, n+1)
			}
		}
	}
	if built == 0 {
		t.Fatal("expected cmd/mw to build at least one application.Dispatch, found none: the scan reads the wrong thing")
	}
}

// withHostRoom sets what a dispatch judges a host's room by from one config's room:
// the same limits the timer's dispatch and mw status read.
func TestWithHostRoomGivesADispatchTheConfigsRoomLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	room := config.RoomSettings{LoadPerCore: 0.7, MinFreeMB: 900, MinDriveFreeGB: 12}
	got := withHostRoom(application.Dispatch{Host: "laptop"}, room)
	want := application.RoomLimits{LoadPerCore: 0.7, MinFreeMB: 900, MinDriveFreeGB: 12, NoDriveGuard: false}
	if got.Room != want {
		t.Errorf("Room = %+v, want %+v", got.Room, want)
	}
	if !reflect.DeepEqual(got.Grist, hostGristRoom(room)) {
		t.Errorf("Grist = %+v, want what the timer reads: %+v", got.Grist, hostGristRoom(room))
	}

	skipping := withHostRoom(application.Dispatch{Host: "laptop"}, config.RoomSettings{Skip: config.SkipRigsSettings{Rigs: []string{"millwright"}, WaitMinutes: 30}})
	if !reflect.DeepEqual(skipping.SkipRigs, application.SkipRigs{Rigs: []string{"millwright"}, Wait: 30 * time.Minute}) {
		t.Errorf("SkipRigs = %+v, want millwright declined with a way out of 30 minutes", skipping.SkipRigs)
	}

	off := withHostRoom(application.Dispatch{Host: "laptop"}, config.RoomSettings{LoadPerCore: 1, MinFreeMB: 500})
	if !off.Room.NoDriveGuard {
		t.Errorf("min_drive_free_gb = 0 must turn the drive guard off, got %+v", off.Room)
	}
}

// mw-gq6.1488: the dispatch mw next runs after a close-out is built through
// withHostDispatchLocks(withHostRoom(withHostRechecks(...), room), vault), with the
// room read by config.Room() from the [dispatch] table. A config saying
// min_drive_free_gb = 15 must reach that dispatch's Room, and a config saying 0 must
// turn its drive guard off. (The literal itself sits in next.go's RunE; the guard
// test above keeps it wrapped in withHostRoom.)
func TestNextsDispatchTakesTheDispatchTablesRoomKeys(t *testing.T) {
	for _, tc := range []struct {
		said    string
		wantGB  int64
		noGuard bool
	}{{"15", 15, false}, {"0", 0, true}} {
		mwConfig(t, "[dispatch]\nroom_load_per_core = 0.6\nroom_min_free_mb = 700\nmin_drive_free_gb = "+tc.said+"\n")
		room, err := config.Room()
		if err != nil {
			t.Fatal(err)
		}
		got := withHostDispatchLocks(withHostRoom(withHostRechecks(application.Dispatch{Host: "laptop"}, application.Suite{}), room), t.TempDir())
		if int64(got.Room.MinDriveFreeGB) != tc.wantGB || got.Room.NoDriveGuard != tc.noGuard {
			t.Errorf("min_drive_free_gb = %s: Room = %+v, want MinDriveFreeGB %d, NoDriveGuard %v", tc.said, got.Room, tc.wantGB, tc.noGuard)
		}
		if got.Room.LoadPerCore != 0.6 || got.Room.MinFreeMB != 700 {
			t.Errorf("Room = %+v, want the [dispatch] table's load and memory keys", got.Room)
		}
	}
}

// mw-gq6.1512: the Next that mw next and mw land build starts a Builder session
// (a send-back, a batch's fixer), so it is given the host's room as the timer's
// dispatch is: wrapped in withHostNextRoom. landsmoke.go's Next starts none.
func TestEveryNextCmdMwRunsAfterALandingIsGivenTheHostRoom(t *testing.T) {
	for file, want := range map[string]string{"next.go": "withHostNextRoom(application.Next{\n\t\t\t\tTracker:    gateway,", "land.go": "withHostNextRoom(application.Next{\n\t\t\t\tTracker:   gateway,"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s builds the Next of a landing with no room: wrap it in withHostNextRoom(..., room)", file)
		}
	}
	got := withHostNextRoom(application.Next{Host: "laptop"}, config.RoomSettings{LoadPerCore: 0.7, MinFreeMB: 900, MinDriveFreeGB: 12})
	if want := (application.RoomLimits{LoadPerCore: 0.7, MinFreeMB: 900, MinDriveFreeGB: 12}); got.Room != want {
		t.Errorf("Room = %+v, want %+v", got.Room, want)
	}
}
