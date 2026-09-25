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

// noUserManagerReason is what a check answers ok with when `systemctl --user`
// itself cannot reach a user manager on this host at all: a system-scope
// install (User=root, no XDG_RUNTIME_DIR or DBUS_SESSION_BUS_ADDRESS) rather
// than a fault to chase every few minutes.
const noUserManagerReason = "n/a: no user manager on this host"

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

// noUserManager reports whether err is systemctl itself saying, on stderr,
// that it cannot connect to a user manager's bus at all: `systemctl --user`
// run where there is no session bus for it to reach, the shape a system-scope
// unit's environment leaves it in.
func noUserManager(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return strings.Contains(strings.ToLower(string(exitErr.Stderr)), "failed to connect to bus")
}
