package application

import "fmt"

// rootUnderFloor is whether the host's own root filesystem, as read, has less
// free than the floor, with the guard on. A reading that could not learn it
// holds the host to nothing (mw-6ww.185.6).
func (r LoadReading) rootUnderFloor(limits RoomLimits) bool {
	return !limits.NoRootGuard && r.RootKnown && r.RootFreeBytes < limits.minRootFreeGB()<<30
}

// rootSaid is what NoRoom says of a root filesystem under its floor.
func (r LoadReading) rootSaid(limits RoomLimits) string {
	return fmt.Sprintf("root has %.1f GB free, under %d GB", float64(r.RootFreeBytes)/(1<<30), limits.minRootFreeGB())
}

// RootFloorAlarm is the DAMPERS line for a host whose dispatch starts no new
// Builder because its root filesystem's free space is under the floor: the host,
// what it has free and the floor, as DriveFloorAlarm says it of the Windows
// drive. It is the same test NoRoom makes, so the line is there exactly while
// dispatch is refusing for it (mw-6ww.185.6).
func (r LoadReading) RootFloorAlarm(host string, limits RoomLimits) (DamperAlarm, bool) {
	if !r.rootUnderFloor(limits) {
		return DamperAlarm{}, false
	}
	return DamperAlarm{
		Principle: DamperRootFloor,
		Text: fmt.Sprintf("%s: root has %.1f GB free, under the %d GB floor: dispatch starts no new Builder on %s until space is freed (`mw doctor root-disk-budget`)",
			host, float64(r.RootFreeBytes)/(1<<30), limits.minRootFreeGB(), host),
		Identity: "root-floor:" + host,
	}, true
}
