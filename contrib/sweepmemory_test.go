package contrib_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// mw-gq6.1730: mw-host-sweep.service was OOM-killed at its 384M MemoryMax on the
// desktop (385M peak, 26 worktrees and the vault's runs/), so that host's stale
// worktrees were never swept. The unit keeps room above that peak.
func TestTheHostSweepUnitHasRoomAboveTheDesktopsPeak(t *testing.T) {
	text, err := os.ReadFile("systemd/mw-host-sweep.service")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^MemoryMax=(\d+)(M|G)$`).FindSubmatch(text)
	if m == nil {
		t.Fatal("mw-host-sweep.service has no MemoryMax=<n>M (or <n>G) line")
	}
	n, _ := strconv.Atoi(string(m[1]))
	megabytes := n
	if string(m[2]) == "G" {
		megabytes = n * 1024
	}
	if megabytes < 1024 {
		t.Errorf("mw-host-sweep.service stops at %dM, below the 1G that holds the desktop's 385M peak with room", megabytes)
	}
	if !regexp.MustCompile(`(?m)^OOMScoreAdjust=500$`).Match(text) {
		t.Error("mw-host-sweep.service lost OOMScoreAdjust=500: a squeeze on memory should kill it before a seat")
	}
}
