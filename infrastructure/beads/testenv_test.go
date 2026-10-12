package beads_test

import (
	"os"
	"strings"
)

// clearHostBeadsEnv takes the shell's own bd settings out of this test binary,
// so the package's result does not depend on whose shell runs it.
//
// BEADS_DIR and BEADS_DB are how bd finds its database before it looks at the
// directory it runs in. BEADS_DOLT_* are the Dolt server settings of a host
// whose bd runs in server mode (every Laptop shell sources them from
// ~/.config/mw/beads.env). With BEADS_DOLT_SERVER_HOST set, the Gateway takes
// the server's read path: it runs reads side by side and reads many beads with
// one `bd list --id`, which the stand-in bd of blocked_test.go does not answer
// and the exit status 1 it gives fails TestBlockedForHostReadsEachEpicOnce. The
// cases that need a server host set it themselves with t.Setenv.
//
// It is called from TestMain, which is in beads_integration_test.go under the
// beads_integration tag and in beads_integration_skipped_test.go without it.
func clearHostBeadsEnv() {
	os.Unsetenv("BEADS_DIR")
	os.Unsetenv("BEADS_DB")
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "BEADS_DOLT_") {
			os.Unsetenv(name)
		}
	}
}
