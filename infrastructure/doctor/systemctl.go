package doctor

import (
	"errors"
	"os/exec"
	"strings"
)

// notInstalledReason is what a check answers ok with when none of the units
// it was given has a unit file on this host at all: a host that plainly
// never runs them, not a fault.
const notInstalledReason = "n/a: none of these units is installed here"

// unitNotFound reports whether err is systemctl itself saying, on stderr,
// that the unit it was asked about has no unit file on this host — not
// installed, rather than systemctl being unable to answer at all (no
// systemctl on PATH, which fails before any process runs and so is never an
// *exec.ExitError).
func unitNotFound(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return strings.Contains(strings.ToLower(string(exitErr.Stderr)), "no such file or directory")
}
