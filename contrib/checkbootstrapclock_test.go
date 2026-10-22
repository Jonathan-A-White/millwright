package contrib_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// mw-gq6.695: scripts/check-bootstrap.sh timed its build-and-run from two
// `date +%s` reads, so a wall clock stepping back (WSL2 does) printed a negative
// time. Here `date` on PATH runs backwards by a minute at every read and docker
// is a stand-in that succeeds, so the printed wall time can only be right if the
// script does not subtract two reads of `date`.
func TestCheckBootstrapWallTimeIsNotNegativeWhenTheClockStepsBack(t *testing.T) {
	if _, err := os.Stat("/proc/uptime"); err != nil {
		t.Skip("no /proc/uptime on this host")
	}
	bin := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(t.TempDir(), "now")
	if err := os.WriteFile(state, []byte("1800000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("date", `read -r n <"`+state+`"
n=$((n - 60))
echo "$n" >"`+state+`"
echo "$n"
`)
	write("docker", `case "$1" in
image) echo 52428800 ;;
esac
exit 0
`)

	script, err := filepath.Abs("../scripts/check-bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := command(t, "sh", script)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check-bootstrap.sh failed: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`wall time (-?\d+)s`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("no 'wall time <n>s' line in:\n%s", out)
	}
	if secs, _ := strconv.Atoi(string(m[1])); secs < 0 {
		t.Errorf("wall time printed %ds with the clock stepping back:\n%s", secs, out)
	}
}
