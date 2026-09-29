package doctor

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsServer)(nil)

// BeadsServerName is what the check is called: in the log, and on the command
// line as `mw doctor beads-server`.
const BeadsServerName = "beads-server"

// beadsServerShared is the beads_sync mode this check has anything to look
// at in: a host whose bd reaches another host's database over the network.
const beadsServerShared = "shared"

// beadsServerDialTimeout is how long Probe gives the database one TCP dial.
const beadsServerDialTimeout = 3 * time.Second

// The beads-server check's damper: there is no cure to retry, so an episode
// spends one failed attempt, and the second faulty run finds it capped and
// writes the note that wakes the Millhand — the same shape as beads-size.
const (
	BeadsServerDamperWait = 0 * time.Second
	BeadsServerDamperCap  = 1
)

// BeadsServer is the check that a host whose beads live in another host's
// database (beads_sync = shared) can still reach it: one TCP dial of the
// address bd itself is told, BEADS_DOLT_SERVER_HOST on BEADS_DOLT_SERVER_PORT,
// with a short timeout. Unreachable, bd here can read and write nothing — no
// claim, no note, no close-out — so it is faulty; but the database runs on
// another host, so there is no cure from here, only the note that wakes the
// Millhand. On any other host it is ok and inert: there is nothing to reach.
type BeadsServer struct {
	// Mode is this host's beads_sync, and ModeErr why it could not be read.
	Mode    string
	ModeErr error
	// Address is the database's host:port, "" when no host is set, and
	// AddressErr why it could not be read.
	Address    string
	AddressErr error
	// Why says how Mode was chosen when beads_sync is auto ("auto: boost of
	// laptop"), and is named beside it wherever the check speaks of the mode.
	Why string

	// Dial reports whether address answered a TCP dial. The zero value dials
	// for real, with a short timeout.
	Dial func(ctx context.Context, address string) bool
}

// NewBeadsServer is the check for a host whose beads_sync is mode, reaching
// address; modeErr and addressErr are why either could not be read, so that
// a setting at fault is this check's to say rather than every check's.
func NewBeadsServer(mode, address string, modeErr, addressErr error) *BeadsServer {
	return &BeadsServer{Mode: mode, ModeErr: modeErr, Address: address, AddressErr: addressErr}
}

// Name implements application.DoctorCheck.
func (b *BeadsServer) Name() string { return BeadsServerName }

// Probe implements application.DoctorCheck: ok, saying why, on a host whose
// beads_sync is not shared; cannot-tell when beads_sync or the address cannot
// be read, or no host is set; otherwise one dial, ok when it answers and
// faulty when it does not.
func (b *BeadsServer) Probe(ctx context.Context) (application.Verdict, string) {
	switch {
	case b.ModeErr != nil:
		return application.DoctorCannotTell, b.ModeErr.Error()
	case b.Mode != beadsServerShared:
		return application.DoctorOK, fmt.Sprintf("beads_sync is %s: this host keeps its beads itself, so there is no database elsewhere to reach", b.said())
	case b.AddressErr != nil:
		return application.DoctorCannotTell, b.AddressErr.Error()
	case b.Address == "":
		return application.DoctorCannotTell, fmt.Sprintf("beads_sync is %s but no BEADS_DOLT_SERVER_HOST is set in mw doctor's environment (dispatch.env): nothing to dial", b.said())
	}
	if b.dial(ctx, b.Address) {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf("the beads database at %s does not answer: bd here can read and write nothing until it does", b.Address)
}

// Cure implements application.DoctorCheck: there is none from this host.
func (b *BeadsServer) Cure(context.Context) error {
	return fmt.Errorf("no cure here: the beads database at %s runs on another host; a person checks its dolt sql-server there, and `mw doctor wg` this host's link to it", b.Address)
}

// Damper implements application.DoctorCheck.
func (b *BeadsServer) Damper() (time.Duration, int) {
	return BeadsServerDamperWait, BeadsServerDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (b *BeadsServer) WayBack() string { return "none: no cure runs" }

// said is the mode as the check names it: with the reason auto chose it, when
// it did.
func (b *BeadsServer) said() string {
	if b.Why == "" {
		return b.Mode
	}
	return fmt.Sprintf("%s (%s)", b.Mode, b.Why)
}

// dial reports whether address answers a TCP dial, over Dial when it is set,
// otherwise for real with a short timeout.
func (b *BeadsServer) dial(ctx context.Context, address string) bool {
	if b.Dial != nil {
		return b.Dial(ctx, address)
	}
	dialer := net.Dialer{Timeout: beadsServerDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
