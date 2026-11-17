package application

import (
	"context"
	"fmt"
	"strings"
)

// HostLoad is how busy this host is, read when a story is about to be taken
// (and by mw status, to say whether one would be). It is a port so that a
// dispatch can be tested at a load and a free memory a scenario names, without
// waiting for the machine to have them.
type HostLoad interface {
	Load(ctx context.Context) (LoadReading, error)
}

// LoadReading is the 1-minute load average of a host, the number of cores it has
// to spend it on and the memory it has left.
type LoadReading struct {
	Load  float64
	Cores int
	// MemAvailableMB is the memory the host could give a new process without
	// swapping (MemAvailable of /proc/meminfo), in megabytes, when MemKnown. A
	// reading that could not learn it leaves MemKnown false, and no floor
	// applies.
	MemAvailableMB int64
	MemKnown       bool
	// SwapInKBPerS and SwapOutKBPerS are the memory the host was swapping in and
	// out a second, vmstat's si and so, when SwapKnown. A reading that did not
	// sample it leaves SwapKnown false (mw-t0z3fu.2).
	SwapInKBPerS  float64
	SwapOutKBPerS float64
	SwapKnown     bool
	// DriveName and DriveFreeBytes are the Windows drive holding a WSL host's
	// vhdx and the space it has left, from the report the Windows task writes
	// into WSL, when DriveKnown. A host that is not WSL, or has no current
	// report, leaves DriveKnown false, and no floor applies (mw-gq6.1381).
	DriveName      string
	DriveFreeBytes int64
	DriveKnown     bool
}

// Busy reports whether the load has reached the core count: every core has
// something waiting its turn.
func (r LoadReading) Busy() bool { return r.Cores > 0 && r.Load >= float64(r.Cores) }

// GateFarOver is how many times the core count a load must reach to be far over
// it (mw-gq6.903). It is where the desktop's 11 gates run twice (2026-10-16 to
// 11-03) told the host's failures from the story's: the 3 whose first failure
// was read at twice the cores or more (32.3, 40.0 and 49.1 of 16) all passed on
// the run made once the host calmed, and the 8 read under twice (16.6 to 30.0 of
// 16) all failed again, each a second whole gate for nothing. A gate starts with
// the host below its cores (55 of 55 on the desktop in the 7 days to 11-03), so
// the load a failure is read at is the load the run rose to.
const GateFarOver = 2.0

// FarOver reports whether the load has reached GateFarOver times the core count:
// a gate run here fails for the host, and one is not started or run again here.
func (r LoadReading) FarOver() bool { return r.Cores > 0 && r.Load >= GateFarOver*float64(r.Cores) }

// The room a host must have to take on another story (mw-t0z3fu.1): its
// 1-minute load under DefaultRoomLoadPerCore of a core each, and at least
// DefaultRoomMinFreeMB of memory available. Both are the config file's
// [dispatch] room_load_per_core and room_min_free_mb to change.
const (
	DefaultRoomLoadPerCore       = 1.0
	DefaultRoomMinFreeMB   int64 = 2048
	// DefaultRoomMinDriveFreeGB is the free space a WSL host's Windows drive must
	// have to take a story: the desktop's D: went read-only twice at under 1 GB
	// (mw-gq6.1381). The config file's [dispatch] min_drive_free_gb changes it,
	// and 0 turns the guard off.
	DefaultRoomMinDriveFreeGB int64 = 10
)

// RoomLimits is how much room a host must have before it starts a story. The
// cap on sessions is still the ceiling: room can only hold a story back that the
// cap would have let through. Zero values read the defaults above.
type RoomLimits struct {
	// LoadPerCore times the core count is the load at which the host has no room.
	LoadPerCore float64
	// MinFreeMB is the memory available below which the host has no room.
	MinFreeMB int64
	// MinDriveFreeGB is the free space, in gigabytes, a WSL host's Windows drive
	// must have; zero reads DefaultRoomMinDriveFreeGB, and NoDriveGuard turns the
	// guard off.
	MinDriveFreeGB int64
	NoDriveGuard   bool
}

func (l RoomLimits) minDriveFreeGB() int64 {
	if l.MinDriveFreeGB <= 0 {
		return DefaultRoomMinDriveFreeGB
	}
	return l.MinDriveFreeGB
}

func (l RoomLimits) loadPerCore() float64 {
	if l.LoadPerCore <= 0 {
		return DefaultRoomLoadPerCore
	}
	return l.LoadPerCore
}

func (l RoomLimits) minFreeMB() int64 {
	if l.MinFreeMB <= 0 {
		return DefaultRoomMinFreeMB
	}
	return l.MinFreeMB
}

// NoRoom says why a host with this reading is to start no story, or "" when it
// has room. What the reading does not know — a load with no core count, a memory
// that was not read — is not held against the host: the cap still applies.
func (r LoadReading) NoRoom(limits RoomLimits) string {
	var why []string
	if perCore := limits.loadPerCore(); r.Cores > 0 && r.Load >= float64(r.Cores)*perCore {
		if perCore == 1 {
			why = append(why, fmt.Sprintf("load %.1f of %d cores", r.Load, r.Cores))
		} else {
			why = append(why, fmt.Sprintf("load %.1f of %d cores at %.2g a core", r.Load, r.Cores, perCore))
		}
	}
	if floor := limits.minFreeMB(); r.MemKnown && r.MemAvailableMB < floor {
		why = append(why, fmt.Sprintf("%d MB free, under %d MB", r.MemAvailableMB, floor))
	}
	if floor := limits.minDriveFreeGB(); !limits.NoDriveGuard && r.DriveKnown && r.DriveFreeBytes < floor<<30 {
		why = append(why, fmt.Sprintf("%s has %.1f GB free, under %d GB: the WSL disk cannot grow", r.DriveName, float64(r.DriveFreeBytes)/(1<<30), floor))
	}
	return strings.Join(why, "; ")
}

// ReadRoom reads the host once and says why it has no room, or "" when it has
// room. read is false when the host could not be asked at all — there is no
// load to ask, or it failed — and then it is not held back: the cap still
// applies.
func ReadRoom(ctx context.Context, load HostLoad, limits RoomLimits) (why string, read bool) {
	if load == nil {
		return "", false
	}
	reading, err := load.Load(ctx)
	if err != nil {
		return "", false
	}
	return reading.NoRoom(limits), true
}

// JobRoom is the actor of the job events that say a host has lost its room to
// start a story, or has it back: dispatch-room@host in the log.
const JobRoom = "dispatch-room"
