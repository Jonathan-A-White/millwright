package application_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestNoRoomSaysWhyAHostIsHeldBack(t *testing.T) {
	tests := []struct {
		name    string
		reading application.LoadReading
		limits  application.RoomLimits
		want    string
	}{
		{"idle with memory to spare", application.LoadReading{Load: 2, Cores: 8, MemAvailableMB: 8000, MemKnown: true}, application.RoomLimits{}, ""},
		{"load at the cores", application.LoadReading{Load: 8, Cores: 8, MemAvailableMB: 8000, MemKnown: true}, application.RoomLimits{}, "load 8.0 of 8 cores"},
		{"memory under the floor", application.LoadReading{Load: 2, Cores: 8, MemAvailableMB: 2047, MemKnown: true}, application.RoomLimits{}, "2047 MB free, under 2048 MB"},
		{"memory at the floor", application.LoadReading{Load: 2, Cores: 8, MemAvailableMB: 2048, MemKnown: true}, application.RoomLimits{}, ""},
		{"both", application.LoadReading{Load: 35, Cores: 20, MemAvailableMB: 100, MemKnown: true}, application.RoomLimits{}, "load 35.0 of 20 cores; 100 MB free, under 2048 MB"},
		{"memory not read", application.LoadReading{Load: 2, Cores: 8}, application.RoomLimits{}, ""},
		{"no core count", application.LoadReading{Load: 50}, application.RoomLimits{}, ""},
		{"a share of a core", application.LoadReading{Load: 5, Cores: 8, MemAvailableMB: 8000, MemKnown: true}, application.RoomLimits{LoadPerCore: 0.5}, "load 5.0 of 8 cores at 0.5 a core"},
		{"a higher floor", application.LoadReading{Load: 1, Cores: 8, MemAvailableMB: 3000, MemKnown: true}, application.RoomLimits{MinFreeMB: 4096}, "3000 MB free, under 4096 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.reading.NoRoom(tt.limits); got != tt.want {
				t.Errorf("NoRoom = %q, want %q", got, tt.want)
			}
		})
	}
}
