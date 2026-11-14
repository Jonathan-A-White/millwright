package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*HostsReadable)(nil)

// HostsReadableName is what the check is called: in the log, and on the command
// line as `mw doctor hosts-readable`.
const HostsReadableName = "hosts-readable"

// HostsFile is the hosts file the check watches on a real host.
const HostsFile = "/etc/hosts"

// hostsHubMarker is the comment contrib/wg-enrol ends each line it writes with;
// a hosts file carrying it is the wg hub's.
var hostsHubMarker = []byte("# mw-wg")

// The hosts-readable check's damper: there is no cure to retry, so one failed
// cure attempt is all an episode ever spends, as with age-key.
const (
	HostsReadableDamperWait = 0 * time.Second
	HostsReadableDamperCap  = 1
)

// hostsNoCure begins what Cure always returns: the doctor never changes the
// hosts file's mode.
const hostsNoCure = "no cure: a person runs chmod 644 " + HostsFile

// HostsReadable is the check that the hosts file of the wg hub, the one holding
// '# mw-wg' lines, can be read by others (mode has o+r). A service run as a
// DynamicUser, alerts-mail among them, resolves laptop.mw through it, and with
// the file at mode 600 every charge alert deferred for hours with nothing
// alarming (mw-90p6avd). On any other host's file it says nothing. It warns
// and never cures.
type HostsReadable struct {
	// Path is the hosts file to read. Empty reads HostsFile.
	Path string

	faulty string
}

// NewHostsReadable is the check of the hosts file at path ("" is /etc/hosts).
func NewHostsReadable(path string) *HostsReadable { return &HostsReadable{Path: path} }

// Name implements application.DoctorCheck.
func (h *HostsReadable) Name() string { return HostsReadableName }

// Probe implements application.DoctorCheck: ok when the file is absent or is
// readable by others; when it is not, faulty if it carries a '# mw-wg' line
// (the file is the hub's), ok if not, cannot-tell when it cannot be read to
// find out.
func (h *HostsReadable) Probe(context.Context) (application.Verdict, string) {
	verdict, reason := h.probe()
	h.faulty = ""
	if verdict == application.DoctorFaulty {
		h.faulty = reason
	}
	return verdict, reason
}

func (h *HostsReadable) probe() (application.Verdict, string) {
	path := h.Path
	if path == "" {
		path = HostsFile
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return application.DoctorOK, ""
	}
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot read the mode of %s: %v", path, err)
	}
	mode := info.Mode().Perm()
	if mode&0o004 != 0 {
		return application.DoctorOK, ""
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("%s is mode %o and cannot be read to tell whether it is the wg hub's: %v", HostsFile, mode, err)
	}
	if !bytes.Contains(text, hostsHubMarker) {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf(
		"%s is mode %o, not readable by others, on the wg hub: DynamicUser services such as alerts-mail cannot resolve laptop.mw meanwhile, so every charge alert is deferred; chmod 644 %s",
		HostsFile, mode, HostsFile)
}

// Cure implements application.DoctorCheck: there is none. What it returns
// names what the probe found, so the report a person reads says it.
func (h *HostsReadable) Cure(context.Context) error {
	if h.faulty == "" {
		return errors.New(hostsNoCure)
	}
	return fmt.Errorf("%s: %s", hostsNoCure, h.faulty)
}

// Damper implements application.DoctorCheck.
func (h *HostsReadable) Damper() (time.Duration, int) {
	return HostsReadableDamperWait, HostsReadableDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (h *HostsReadable) WayBack() string {
	return "none: no cure runs; a person sets the mode by hand (chmod 644 " + HostsFile + ")"
}
