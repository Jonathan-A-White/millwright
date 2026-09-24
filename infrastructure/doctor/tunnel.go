package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Tunnel)(nil)

// TunnelName is what the check is called: in the log, and on the command
// line as `mw doctor tunnel`.
const TunnelName = "tunnel"

// TunnelRestartSettle is how long Cure waits after restarting the tunnel's
// unit before it re-probes, giving the reverse ssh connection a chance to
// come back up before the next run reads on it.
const TunnelRestartSettle = 15 * time.Second

// The tunnel check's damper: idle between two restarts, and how many it
// spends on one fault episode before it gives up and waits for the
// escalation note (a later story) to wake the Millhand.
const (
	TunnelDamperWait = 15 * time.Minute
	TunnelDamperCap  = 3
)

// Tunnel is the check that restarts the Laptop's reverse ssh tunnel to the
// VPS when the internet is up but the tunnel itself is down: one ssh call to
// the VPS asking whether anything is listening on the tunnel's own port,
// and, faulty, `systemctl --user restart` of the unit that holds the tunnel
// open. It never touches the VPS itself beyond that one read-only ssh call,
// never edits the unit, never uses sudo, and never restarts anything but the
// named unit. When the internet itself looks unreachable — checked the same
// way the wifi check does — that is cannot-tell, not faulty: a dead
// internet is wifi's fault to cure, not this one's.
type Tunnel struct {
	// Reach are the host:port pairs tried to tell whether the internet
	// itself is reachable before this check ever touches ssh. Empty reads
	// DefaultDoctorReach.
	Reach []string
	// Host is the VPS's ssh name this check connects to.
	Host string
	// Command is the command run over ssh on the VPS to check the tunnel's
	// listener. Empty reads DefaultDoctorTunnelProbe.
	Command string
	// Unit is the user unit this check restarts when the tunnel is down.
	// Empty reads DefaultDoctorTunnelUnit.
	Unit string
	// SSH is the program run for ssh. Empty reads "ssh".
	SSH string
	// Systemctl is the program run for systemctl. Empty reads "systemctl".
	Systemctl string

	// Sleep is how Cure waits between restarting the unit and re-probing.
	// The zero value sleeps for real.
	Sleep func(time.Duration)
}

// NewTunnel is the check over the given VPS host, reach hosts, unit and
// probe command, run through the real ssh and systemctl.
func NewTunnel(host string, reach []string, unit, command string) *Tunnel {
	return &Tunnel{Host: host, Reach: reach, Unit: unit, Command: command}
}

// Name implements application.DoctorCheck.
func (t *Tunnel) Name() string { return TunnelName }

// Probe implements application.DoctorCheck: cannot-tell "internet
// unreachable" when none of Reach's hosts connect — a dead internet is the
// wifi check's fault, not this one's; otherwise one ssh call to the VPS
// running Command, faulty "tunnel port not listening on <host>" when it
// prints no listening socket, ok when it does, and cannot-tell when ssh
// itself fails to get through.
func (t *Tunnel) Probe(ctx context.Context) (application.Verdict, string) {
	if !Reach(ctx, t.reach()) {
		return application.DoctorCannotTell, "internet unreachable"
	}

	out, err := t.runSSH(ctx, t.Host, t.command())
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("ssh %s: %v", t.Host, err)
	}
	if !tunnelListening(out) {
		return application.DoctorFaulty, "tunnel port not listening on " + t.Host
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: `systemctl --user restart
// <unit>`, a settle, then a re-probe so the next thing to touch the tunnel's
// state reads it fresh rather than stale. It refuses only when the restart
// itself could not be run; whether the tunnel is back up by the time it
// stopped waiting is for the next probe to say — a wait, not a gate, the way
// the wifi check's own Cure is.
func (t *Tunnel) Cure(ctx context.Context) error {
	if _, err := t.runSystemctl(ctx, "restart", t.unit()); err != nil {
		return fmt.Errorf("systemctl --user restart %s: %w", t.unit(), err)
	}
	t.sleep(TunnelRestartSettle)
	t.Probe(ctx)
	return nil
}

// Damper implements application.DoctorCheck.
func (t *Tunnel) Damper() (time.Duration, int) { return TunnelDamperWait, TunnelDamperCap }

// WayBack implements application.DoctorCheck: the unit this check restarts,
// stopped by hand.
func (t *Tunnel) WayBack() string { return "systemctl --user stop " + t.unit() }

func (t *Tunnel) reach() []string {
	if len(t.Reach) > 0 {
		return t.Reach
	}
	return []string{"api.anthropic.com:443", "github.com:443"}
}

func (t *Tunnel) command() string {
	if t.Command != "" {
		return t.Command
	}
	return "ss -ltn sport = :2222"
}

func (t *Tunnel) unit() string {
	if t.Unit != "" {
		return t.Unit
	}
	return "reverse-tunnel.service"
}

func (t *Tunnel) sleep(d time.Duration) {
	if t.Sleep == nil {
		time.Sleep(d)
		return
	}
	t.Sleep(d)
}

// runSSH runs one ssh call against the VPS, in BatchMode so that nothing is
// ever asked of a person, and returns what it printed.
func (t *Tunnel) runSSH(ctx context.Context, host, command string) (string, error) {
	program := t.SSH
	if program == "" {
		program = "ssh"
	}
	out, err := exec.CommandContext(ctx, program,
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, command).Output()
	return string(out), err
}

// runSystemctl runs one systemctl --user call.
func (t *Tunnel) runSystemctl(ctx context.Context, args ...string) (string, error) {
	program := t.Systemctl
	if program == "" {
		program = "systemctl"
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"--user"}, args...)...).Output()
	return string(out), err
}

// tunnelListening reads ss's output for a line naming a listening socket —
// ss prints a header even when nothing matches its filter, so an empty
// answer is not the test; a line whose first field is LISTEN is.
func tunnelListening(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "LISTEN" {
			return true
		}
	}
	return false
}
