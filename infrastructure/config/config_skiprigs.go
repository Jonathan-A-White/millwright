package config

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultSkipRigsWaitMinutes is how long a host that declines a rig passes over
// its auto stories before it takes one itself: skip_rigs_wait_min.
const DefaultSkipRigsWaitMinutes = 20

// SkipRigsSettings is the rigs a host declines at dispatch: the `[dispatch]`
// table's `skip_rigs = ["millwright"]` (default none) and `skip_rigs_wait_min`
// (a whole number of minutes, 1 or more, default DefaultSkipRigsWaitMinutes) the
// way out: a story no other host took in that time is taken here (mw-6ww.119.11.1.3).
type SkipRigsSettings struct {
	Rigs        []string
	WaitMinutes int
}

// skipRigsIn reads the keys of the [dispatch] table the file at path holds.
func skipRigsIn(path string, table map[string]string) (SkipRigsSettings, error) {
	settings := SkipRigsSettings{Rigs: list(table["skip_rigs"]), WaitMinutes: DefaultSkipRigsWaitMinutes}
	if said := strings.TrimSpace(table["skip_rigs_wait_min"]); said != "" {
		n, err := strconv.Atoi(said)
		if err != nil || n < 1 {
			return SkipRigsSettings{}, fmt.Errorf("the [%s] table of %s says skip_rigs_wait_min = %q: it must be a whole number of minutes, 1 or more", DispatchTable, path, said)
		}
		settings.WaitMinutes = n
	}
	return settings, nil
}
