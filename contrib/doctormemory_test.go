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

// mw-gq6.1732: mw-mail-notify.service logged "384.5M memory peak, 100.4M memory
// swap peak" on 2026-11-23, at its 384M MemoryMax (its usual runs are 16-80M). The
// unit keeps room above that peak and the story id beside the cap.
func TestTheMailNotifyUnitHasRoomAboveItsPeak(t *testing.T) {
	text, err := os.ReadFile("systemd/mw-mail-notify.service")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^MemoryMax=(\d+)(M|G)$`).FindSubmatch(text)
	if m == nil {
		t.Fatal("mw-mail-notify.service has no MemoryMax=<n>M (or <n>G) line")
	}
	n, _ := strconv.Atoi(string(m[1]))
	megabytes := n
	if string(m[2]) == "G" {
		megabytes = n * 1024
	}
	if megabytes < 768 {
		t.Errorf("mw-mail-notify.service stops at %dM, below the 768M that holds its 384.5M peak with room", megabytes)
	}
	if !regexp.MustCompile(`(?m)^OOMScoreAdjust=500$`).Match(text) {
		t.Error("mw-mail-notify.service lost OOMScoreAdjust=500: a squeeze on memory should kill it before a seat")
	}
	if !regexp.MustCompile(`mw-gq6\.1732`).Match(text) {
		t.Error("mw-mail-notify.service does not name mw-gq6.1732 beside its MemoryMax")
	}
}
