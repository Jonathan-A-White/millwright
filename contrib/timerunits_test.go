package contrib_test

// This test drives scripts/check-timer-units.sh against a stand-in
// systemd-analyze on the front of PATH, so that what the real one would say about
// a host's own user units can be said on any host. It never reads or writes the
// real ~/.config/systemd and never calls the real systemd-analyze or systemctl:
// HOME is a temporary directory too. Nothing here may be changed to run without
// them.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runTimerUnits runs the check with a systemd-analyze that prints output to its
// standard error and exits with status, and returns what the check printed and
// whether it passed.
func runTimerUnits(t *testing.T, output string, status string) (string, bool) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-timer-units.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	standIn := "#!/bin/sh\ncat \"$MW_TEST_DIR/output\" >&2\nexit " + status + "\n"
	if err := os.WriteFile(filepath.Join(bin, "systemd-analyze"), []byte(standIn), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output"), []byte(output), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", script)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+dir,
		"MW_TEST_DIR="+dir,
	)
	out, err := cmd.CombinedOutput()
	if _, isExit := err.(*exec.ExitError); err != nil && !isExit {
		t.Fatalf("could not run %s: %v", script, err)
	}
	return string(out), err == nil
}

func TestCheckTimerUnitsVerify(t *testing.T) {
	const hostUnit = "/home/someone/.config/systemd/user/reverse-tunnel.service"

	cases := []struct {
		name   string
		output string
		status string
		passes bool
		// each of these must appear in what the check printed
		wants []string
	}{
		{
			name:   "a host's own unit draws a warning: noted, not failed",
			output: hostUnit + ":7: Unit uses KillMode=none. This is unsafe.\n",
			status: "0",
			passes: true,
			wants:  []string{"ignored: not the rig's unit", hostUnit + ":7: Unit uses KillMode=none"},
		},
		{
			name:   "a warning naming a rig unit by its path fails",
			output: "contrib/systemd/mw-dispatch.service:9: Unknown key name 'Nope' in section 'Service', ignoring.\n",
			status: "0",
			passes: false,
			wants:  []string{"contrib/systemd/mw-dispatch.service:9", "something to say about the unit files"},
		},
		{
			name:   "a warning naming a rig unit by its name fails",
			output: "mw-millhand-tick.timer: Command /usr/bin/nope is not executable.\n",
			status: "0",
			passes: false,
			wants:  []string{"mw-millhand-tick.timer: Command", "something to say about the unit files"},
		},
		{
			name:   "a host's warning beside a rig unit's fails on the rig's alone",
			output: hostUnit + ":7: Unit uses KillMode=none.\ncontrib/systemd/mw-dispatch.service:9: Unknown key name.\n",
			status: "0",
			passes: false,
			wants:  []string{"ignored: not the rig's unit", "contrib/systemd/mw-dispatch.service:9"},
		},
		{
			name:   "a non-zero exit fails, even with nothing printed",
			output: "",
			status: "1",
			passes: false,
			wants:  []string{"rejected the unit files"},
		},
		{
			name:   "a non-zero exit fails, even when only a host's unit is named",
			output: hostUnit + ":7: Unit uses KillMode=none.\n",
			status: "1",
			passes: false,
			wants:  []string{"rejected the unit files"},
		},
		{
			name:   "no output passes",
			output: "",
			status: "0",
			passes: true,
			wants:  []string{"OK: contrib/systemd: verified by systemd-analyze"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, passed := runTimerUnits(t, c.output, c.status)
			if passed != c.passes {
				t.Errorf("passed = %v, want %v; the check printed:\n%s", passed, c.passes, out)
			}
			for _, want := range c.wants {
				if !strings.Contains(out, want) {
					t.Errorf("the check did not print %q; it printed:\n%s", want, out)
				}
			}
		})
	}
}
