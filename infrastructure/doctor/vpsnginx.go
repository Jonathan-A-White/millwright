package doctor

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*VPSNginx)(nil)

// VPSNginxName is what the check is called: in the log, and on the command
// line as `mw doctor vps-nginx`.
const VPSNginxName = "vps-nginx"

// The vps-nginx check's damper: a failed alarm is tried again a minute on, at
// the next run, and given up on, for a person, after three.
const (
	VPSNginxDamperWait = time.Minute
	VPSNginxDamperCap  = 3
)

// VPSNginxSeenStateName is where the check keeps, between runs, what it has
// told the Governor: a latch (SeenPaths holding the fault told, then the seq
// of the emergency event that told him, where there is one) so that the same
// fault is told once and the next ok is the "put right" one, which clears that
// seq. It is a key of its own, apart from the episode key Doctor keeps this
// check's cure state under.
const VPSNginxSeenStateName = "vps-nginx-seen"

// VPSNginx is the check that the VPS's nginx postern_api upstream sends the
// phone to the home first and to every other backend only as `backup`
// (mw-gq6.274). It runs only on the home host, reads the upstream through the
// application.VPSNginx reader and tells the Governor once per change of
// state: a fault is told when it first appears or its words change, and again
// once it is put right. A VPS it cannot reach is "not checked", never a fault
// and never a reason to forget what was told. Its cure is the alarm itself: it
// changes nothing on the VPS, whose file is edited by a person.
type VPSNginx struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Reader reads the VPS's upstream.
	Reader application.VPSNginxReader
	// State is where the check keeps what it has told the Governor.
	State application.DoctorState
	// Alarm sends the Governor the alarm text and gives the seq of the
	// emergency event it was written as, 0 for none. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) (uint64, error)
	// Clear tells the Governor the upstream is right again, in the normal
	// lane, naming in clears the emergency it ends (0 for none).
	Clear func(ctx context.Context, text string, clears uint64) error

	// due is the alarm Probe found owing, for Cure to send.
	due vpsNginxDue
}

// vpsNginxDue is an alarm Probe found owing: its text, the fault it told (the
// latch), whether it is the "put right" one and the seq it ends.
type vpsNginxDue struct {
	text    string
	fault   string
	back    bool
	clears  uint64
	pending bool
}

// NewVPSNginx is the check over the vault's home file, run on host, reading
// the VPS through reader, remembering what it has told in state.
func NewVPSNginx(home application.HomeFile, host string, reader application.VPSNginxReader, state application.DoctorState) *VPSNginx {
	return &VPSNginx{Home: home, Host: host, Reader: reader, State: state}
}

// Name implements application.DoctorCheck.
func (v *VPSNginx) Name() string { return VPSNginxName }

// Probe implements application.DoctorCheck: ok, n/a, on a host that is not
// home; ok, "not checked", when the VPS or the home cannot be read; faulty
// when the upstream is wrong and the Governor has not been told this fault;
// ok when it is wrong and he has; faulty, for the "put right" alarm, when it
// is right and he was told it was not; otherwise ok.
func (v *VPSNginx) Probe(ctx context.Context) (application.Verdict, string) {
	v.due = vpsNginxDue{}
	if v.Home == nil {
		return application.DoctorOK, "n/a: no home file to say whether this host is home"
	}
	home, err := application.IsHome(ctx, v.Home, v.Host)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, "n/a: this host is not home"
	}

	episode, err := v.State.Load(ctx, VPSNginxSeenStateName)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
	}
	told := ""
	if len(episode.SeenPaths) >= 1 {
		told = episode.SeenPaths[0]
	}

	reading := v.Reader.Read(ctx)
	switch reading.State {
	case application.VPSNginxNotChecked:
		return application.DoctorOK, "not checked: " + reading.Why

	case application.VPSNginxFault:
		if reading.Why == told {
			return application.DoctorOK, "fault already told: " + reading.Why
		}
		v.due = vpsNginxDue{
			text: fmt.Sprintf("The VPS's nginx postern_api upstream is wrong: %s. Postern on the phone will hang or read offline. "+
				"Fix it on the VPS (ssh root@allmymind.org): %s. The way back: %s.", reading.Why, reading.Fix, reading.WayBack),
			fault:   reading.Why,
			pending: true,
		}
		return application.DoctorFaulty, v.due.text
	}

	if told == "" {
		return application.DoctorOK, ""
	}
	v.due = vpsNginxDue{
		text:    "The VPS's nginx postern_api upstream is right again: home first, every other backend backup.",
		back:    true,
		clears:  seqAt(episode.SeenPaths, 1),
		pending: true,
	}
	return application.DoctorFaulty, v.due.text
}

// Cure implements application.DoctorCheck: send the alarm Probe found owing,
// then remember it. The state is changed only once the alarm is sent, so one
// that could not be sent is tried again.
func (v *VPSNginx) Cure(ctx context.Context) error {
	if !v.due.pending {
		return nil
	}
	due := v.due
	if due.back {
		if v.Clear == nil {
			return fmt.Errorf("there is no way to send the vps-nginx \"right again\" event")
		}
		if err := v.Clear(ctx, due.text, due.clears); err != nil {
			return fmt.Errorf("the vps-nginx alarm could not be sent: %w", err)
		}
		v.due = vpsNginxDue{}
		if err := v.State.Reset(ctx, VPSNginxSeenStateName); err != nil {
			return fmt.Errorf("forgetting this check's own state: %w", err)
		}
		return nil
	}
	if v.Alarm == nil {
		return fmt.Errorf("there is no way to send the vps-nginx alarm")
	}
	seq, err := v.Alarm(ctx, due.text)
	if err != nil {
		return fmt.Errorf("the vps-nginx alarm could not be sent: %w", err)
	}
	v.due = vpsNginxDue{}
	err = v.State.Save(ctx, VPSNginxSeenStateName, application.DoctorEpisode{SeenPaths: withSeq([]string{due.fault}, seq)})
	if err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (v *VPSNginx) Damper() (time.Duration, int) { return VPSNginxDamperWait, VPSNginxDamperCap }

// WayBack implements application.DoctorCheck.
func (v *VPSNginx) WayBack() string {
	return "none needed: the alarm only tells the Governor; the fix it names keeps a .bak of the site file to copy back"
}
