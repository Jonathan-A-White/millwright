package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

	off := withHostRoom(application.Dispatch{Host: "laptop"}, config.RoomSettings{LoadPerCore: 1, MinFreeMB: 500})
	if !off.Room.NoDriveGuard {
		t.Errorf("min_drive_free_gb = 0 must turn the drive guard off, got %+v", off.Room)
	}
}
