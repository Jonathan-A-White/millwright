package doctor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*CloudCapsRetired)(nil)

// CloudCapsRetiredName is what the check is called: in the log, and on the
// command line as `mw doctor cloud-caps-retired`.
const CloudCapsRetiredName = "cloud-caps-retired"

// The cloud-caps-retired check's damper: it never cures, so one episode spends
// one.
const (
	CloudCapsRetiredDamperWait = 0 * time.Second
	CloudCapsRetiredDamperCap  = 1
)

// cloudCapsRetiredNoCure begins what Cure always returns: the config file is a
// person's to edit.
const cloudCapsRetiredNoCure = "no cure: a person removes the line from [cloud.caps] in the config file"

// CloudCapsRetired is the check that [cloud.caps] names no host that
// retired_hosts retires (mw-gq6.1111). The cloud check counts no sessions for
// a retired host, so such a line does no harm now, but it is a stale line that
// once held every box back; the check warns (cannot-tell) naming each host. It
// reads the config and changes nothing.
type CloudCapsRetired struct {
	// Caps are the hosts [cloud.caps] names, none where there is no [cloud]
	// table, and Retired the hosts of retired_hosts.
	Caps    func() ([]string, error)
	Retired func() ([]string, error)
}

// Name implements application.DoctorCheck.
func (c *CloudCapsRetired) Name() string { return CloudCapsRetiredName }

// Probe implements application.DoctorCheck: ok when no host of [cloud.caps] is
// retired, cannot-tell naming each one that is, or when the config could not
// be read.
func (c *CloudCapsRetired) Probe(context.Context) (application.Verdict, string) {
	caps, err := c.Caps()
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot read [cloud.caps]: %v", err)
	}
	retired, err := c.Retired()
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot read retired_hosts: %v", err)
	}
	var named []string
	for _, host := range caps {
		if application.IsRetired(retired, host) {
			named = append(named, host)
		}
	}
	if len(named) == 0 {
		return application.DoctorOK, ""
	}
	sort.Strings(named)
	return application.DoctorCannotTell, fmt.Sprintf("[cloud.caps] names %s, which retired_hosts retires: the cloud check counts no sessions for it; remove the line", strings.Join(named, ", "))
}

// Cure implements application.DoctorCheck: there is none.
func (c *CloudCapsRetired) Cure(context.Context) error { return errors.New(cloudCapsRetiredNoCure) }

// Damper implements application.DoctorCheck.
func (c *CloudCapsRetired) Damper() (time.Duration, int) {
	return CloudCapsRetiredDamperWait, CloudCapsRetiredDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (c *CloudCapsRetired) WayBack() string { return "none: no cure runs" }
