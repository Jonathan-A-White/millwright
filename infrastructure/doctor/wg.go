package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Wg)(nil)

// WgName is what the check is called: in the log, and on the command line as
// `mw doctor wg`.
const WgName = "wg"

// wgHubStateName is where Wg keeps its own record of when the hub first
// looked unreachable over wg0, under the same State it is given — a key of
// its own, distinct from "wg", the key Doctor keeps that check's cure
// episode under, the same way wifi keeps wifiReachStateName apart from its
// own "wifi" episode key.
const wgHubStateName = "wg-hub"

// WgFaultAfter is how long the hub must have looked unreachable over wg0,
// straight, before Probe calls it faulty rather than merely waiting: one blip
// is not a fault.
const WgFaultAfter = 3 * time.Minute

// WgRestartSettle is how long Cure waits after restarting the unit before it
// re-probes, giving the link a chance to come back up before the next run
// reads on it.
const WgRestartSettle = 15 * time.Second

// The wg check's damper: idle between two restarts, and how many it spends on
// one fault episode before it gives up and waits for the escalation note to
// wake the Millhand.
const (
	WgDamperWait = 15 * time.Minute
	WgDamperCap  = 3
)

// wgDialTimeout is how long Probe gives the hub to answer one TCP dial.
const wgDialTimeout = 5 * time.Second

// Wg is the check that restarts this host's wg-quick@wg0 unit when the
// internet is up but the hub is not answering over the wireguard link: a
// TCP dial of the hub's ssh port over wg0, no root, and, faulty, `sudo -n
// systemctl restart` of the unit that holds the link up. It never touches
// anything but that one restart: no edit of the unit or the wireguard
// config, nothing elevated beyond the one command. On a host with no wg0
// interface at all, or that is itself the hub (its own wg0 address is the
// hub's), or while the internet itself looks unreachable, it is cannot-tell,
// not faulty: none of those is this check's fault to cure.
type Wg struct {
	// Reach are the host:port pairs tried to tell whether the internet
	// itself is reachable before this check ever touches wg0. Empty reads
	// DefaultDoctorReach.
	Reach []string
	// Hub is the hub's host:port this check dials over wg0. Empty reads
	// DefaultDoctorWgHub.
	Hub string
	// Unit is the unit this check restarts when the hub is unreachable.
	// Empty reads DefaultDoctorWgUnit.
	Unit string
	// State is where this check keeps its own record of when the hub first
	// looked unreachable, separate from Doctor's own episode for this
	// check's cures.
	State application.DoctorState

	// IP is the program run to read wg0's own address. Empty reads "ip".
	IP string
	// Sudo is the program run to restart the unit. Empty reads "sudo".
	Sudo string

	// Dial reports whether address answered a TCP dial. The zero value
	// dials for real, with a short timeout.
	Dial func(ctx context.Context, address string) bool

	// Now is the clock Probe reads time by. The zero value reads the real
	// one.
	Now func() time.Time
	// Sleep is how Cure waits between restarting the unit and re-probing.
	// The zero value sleeps for real.
	Sleep func(time.Duration)
}

// NewWg is the check over the given hub, reach hosts and unit, kept through
// state, run through the real ip and sudo.
func NewWg(hub string, reach []string, unit string, state application.DoctorState) *Wg {
	return &Wg{Hub: hub, Reach: reach, Unit: unit, State: state}
}

// Name implements application.DoctorCheck.
func (w *Wg) Name() string { return WgName }

// Probe implements application.DoctorCheck: cannot-tell "internet
// unreachable" when none of Reach's hosts connect — a dead internet is the
// wifi check's fault, not this one's; cannot-tell when this host has no wg0
// interface at all, or when wg0's own address is the hub's (this host is the
// hub, with nothing to restart); otherwise a TCP dial of Hub, ok when it
// answers. Unreachable is only faulty once this check's own state says the
// hub has looked unreachable for WgFaultAfter straight — before that it is
// cannot-tell, "faulty (waiting 3m)", and no cure runs.
func (w *Wg) Probe(ctx context.Context) (application.Verdict, string) {
	if !Reach(ctx, w.reach()) {
		return application.DoctorCannotTell, "internet unreachable"
	}

	addr, err := w.wg0Address(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("no wg0 interface: %v", err)
	}
	if host, _, splitErr := net.SplitHostPort(w.hub()); splitErr == nil && addr != "" && addr == host {
		return application.DoctorCannotTell, "this host is the hub: wg0's own address is " + addr
	}

	if w.dial(ctx, w.hub()) {
		if err := w.State.Reset(ctx, wgHubStateName); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("forgetting this check's own state: %v", err)
		}
		return application.DoctorOK, ""
	}

	episode, err := w.State.Load(ctx, wgHubStateName)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
	}
	first := episode.FirstFaulty
	if first.IsZero() {
		first = w.now()
		if err := w.State.Save(ctx, wgHubStateName, application.DoctorEpisode{FirstFaulty: first}); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("saving this check's own state: %v", err)
		}
	}

	if w.now().Sub(first) < WgFaultAfter {
		return application.DoctorCannotTell, "faulty (waiting 3m)"
	}
	return application.DoctorFaulty, "hub unreachable since " + first.UTC().Format(time.RFC3339)
}

// Cure implements application.DoctorCheck: `sudo -n systemctl restart
// <unit>`, a settle, then a re-probe so the next thing to touch the link's
// state reads it fresh rather than stale. A sudo that needs a password —
// sudo -n refusing rather than running — is reported naming the sudoers
// line this host lacks, rather than a bare exec error.
func (w *Wg) Cure(ctx context.Context) error {
	if _, err := w.runSudo(ctx, "systemctl", "restart", w.unit()); err != nil {
		if sudoNeedsPassword(err) {
			return fmt.Errorf("sudo -n systemctl restart %s needs a password: add a sudoers line letting %s run it without one, e.g. via visudo: %q: %w",
				w.unit(), w.sudoUser(), w.sudoUser()+" ALL=(root) NOPASSWD: /usr/bin/systemctl restart "+w.unit(), err)
		}
		return fmt.Errorf("sudo -n systemctl restart %s: %w", w.unit(), err)
	}
	w.sleep(WgRestartSettle)
	w.Probe(ctx)
	return nil
}

// Damper implements application.DoctorCheck.
func (w *Wg) Damper() (time.Duration, int) { return WgDamperWait, WgDamperCap }

// WayBack implements application.DoctorCheck: the command that stops the
// unit, named as text — never run by this check.
func (w *Wg) WayBack() string { return "sudo systemctl stop " + w.unit() }

func (w *Wg) reach() []string {
	if len(w.Reach) > 0 {
		return w.Reach
	}
	return []string{"api.anthropic.com:443", "github.com:443"}
}

func (w *Wg) hub() string {
	if w.Hub != "" {
		return w.Hub
	}
	return "10.88.0.1:22"
}

func (w *Wg) unit() string {
	if w.Unit != "" {
		return w.Unit
	}
	return "wg-quick@wg0"
}

func (w *Wg) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

func (w *Wg) sleep(d time.Duration) {
	if w.Sleep == nil {
		time.Sleep(d)
		return
	}
	w.Sleep(d)
}

// dial reports whether address answers a TCP dial, over Dial when it is set,
// otherwise for real with a short timeout.
func (w *Wg) dial(ctx context.Context, address string) bool {
	if w.Dial != nil {
		return w.Dial(ctx, address)
	}
	dialer := net.Dialer{Timeout: wgDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// wg0Address reads wg0's own address, "" if it has one interface but no
// address assigned, and the error `ip` itself left with when the interface
// does not exist on this host at all.
func (w *Wg) wg0Address(ctx context.Context) (string, error) {
	program := w.IP
	if program == "" {
		program = "ip"
	}
	out, err := exec.CommandContext(ctx, program, "-4", "-o", "addr", "show", "dev", "wg0").Output()
	if err != nil {
		return "", err
	}
	return parseWg0Address(string(out)), nil
}

// runSudo runs one `sudo -n` call.
func (w *Wg) runSudo(ctx context.Context, args ...string) (string, error) {
	program := w.Sudo
	if program == "" {
		program = "sudo"
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"-n"}, args...)...).Output()
	return string(out), err
}

// sudoUser is the name a sudoers line for this cure would be written for:
// this process's own user, or "<user>" when it cannot be read.
func (w *Wg) sudoUser() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "<user>"
	}
	return u.Username
}

// sudoNeedsPassword reports whether err is sudo -n itself refusing because it
// has no cached credential and cannot prompt: "sudo: a password is required"
// on stderr, rather than the command it was asked to run failing.
func sudoNeedsPassword(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return strings.Contains(strings.ToLower(string(exitErr.Stderr)), "password is required")
}

// parseWg0Address reads `ip -4 -o addr show dev wg0`'s own output for the
// address after its "inet" field, "" if there is none.
func parseWg0Address(out string) string {
	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "inet" && i+1 < len(fields) {
			addr, _, _ := strings.Cut(fields[i+1], "/")
			return addr
		}
	}
	return ""
}
