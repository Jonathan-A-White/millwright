package config

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultRoomMinRootFreeGB is the free space the host's own root filesystem must
// have to start a story, on every host (mw-6ww.185.6); `min_root_free_gb = 0` in
// the [dispatch] table turns the guard off, and a [[dispatch.windows]] entry may
// carry the key. It is application's own default, said again so that this
// package needs not import it.
const DefaultRoomMinRootFreeGB int64 = 15

// roomFloorsIn reads the three disk keys of the [dispatch] table into settings:
// `min_drive_free_gb` (the Windows drive of a WSL host, dispatch starts no
// Builder under it), `min_root_free_gb` (the host's own root filesystem, the
// same) and `drive_warn_gb` (mw doctor warns under it); each a whole number of
// gigabytes, 0 turning it off, and each left as it is when the table says
// nothing.
func roomFloorsIn(table map[string]string, path string, settings *RoomSettings) error {
	for _, floor := range []struct {
		key  string
		into *int64
		off  string
	}{
		{"min_drive_free_gb", &settings.MinDriveFreeGB, "the guard off"},
		{"min_root_free_gb", &settings.MinRootFreeGB, "the guard off"},
		{"drive_warn_gb", &settings.DriveWarnGB, "the warning off"},
	} {
		said := strings.TrimSpace(table[floor.key])
		if said == "" {
			continue
		}
		n, err := strconv.ParseInt(said, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("the [%s] table of %s says %s = %q: it must be a whole number of gigabytes, 0 (%s) or more", DispatchTable, path, floor.key, said, floor.off)
		}
		*floor.into = n
	}
	return nil
}
