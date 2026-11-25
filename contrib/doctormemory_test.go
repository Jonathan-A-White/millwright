package contrib_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// mw-gq6.1731: mw-doctor.service peaked at 384.0M with 497.7M of swap on cloud4,
// right at its 384M MemoryMax: the run reads the beads of every host and
// worktree through bd processes side by side, whose memory counts in the unit's
// cgroup, so a squeezed run was one step from the kernel's kill. Both the --user
// and the system unit keep room above that peak.
func TestTheDoctorUnitsHaveRoomAboveCloud4sPeak(t *testing.T) {
	for _, unit := range []string{"mw-doctor.service", "system/mw-doctor.service"} {
		text, err := os.ReadFile("systemd/" + unit)
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`(?m)^MemoryMax=(\d+)(M|G)$`).FindSubmatch(text)
		if m == nil {
			t.Fatalf("%s has no MemoryMax=<n>M (or <n>G) line", unit)
		}
		n, _ := strconv.Atoi(string(m[1]))
		megabytes := n
		if string(m[2]) == "G" {
			megabytes = n * 1024
		}
		if megabytes < 1024 {
			t.Errorf("%s stops at %dM, below the 1G that holds cloud4's 384M peak with room", unit, megabytes)
		}
		if !regexp.MustCompile(`(?m)^OOMScoreAdjust=500$`).Match(text) {
			t.Errorf("%s lost OOMScoreAdjust=500: a squeeze on memory should kill it before a seat", unit)
		}
	}
}
