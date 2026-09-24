package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Wifi)(nil)

// WifiName is what the check is called: in the log, and on the command line
// as `mw doctor wifi`.
const WifiName = "wifi"

// wifiReachStateName is where Wifi keeps its own record of when the internet
// first looked unreachable, under the same State it is given — a key of its
// own, distinct from "wifi", the key Doctor keeps that check's cure episode
// under. The two clocks start at different moments: this one the moment the
// internet first looked down, Doctor's own only once a cure is first
// attempted, which is too late for the five-minute grace this check gives a
// blip before it ever touches the network.
const wifiReachStateName = "wifi-reach"

// The wifi check's timings: how long the internet must look unreachable
// before Probe calls it faulty rather than merely waiting, how long Cure
// gives the disconnect to settle before it rejoins, and how long it then
// polls the probe for the rejoin to have worked.
const (
	WifiWaitBeforeFaulty = 5 * time.Minute
	WifiDisconnectSettle = 3 * time.Second
	WifiReconnectWait    = 20 * time.Second
	WifiReconnectPoll    = 1 * time.Second
)

// The wifi check's damper: idle between two bounces, and how many it spends
// on one fault episode before it gives up and waits for the escalation note
// (a later story) to wake the Millhand.
const (
	WifiDamperWait = 30 * time.Minute
	WifiDamperCap  = 3
)

// Wifi is the check that bounces this host's Wi-Fi, from WSL, when the
// internet has looked unreachable for five minutes straight: it reads the
// network's own name (its SSID) with `netsh wlan show interfaces`, then
// disconnects and reconnects to that same network. It never joins any
// network but the one already associated, and never runs anything elevated.
// On a host that is not Windows-backed — no powershell.exe where Windows
// puts it — the check is inert: cannot-tell, every run.
type Wifi struct {
	// Reach are the host:port pairs the probe tries to resolve and
	// TCP-connect to. Empty reads DefaultDoctorReach.
	Reach []string
	// Powershell is the path checked for powershell.exe, this check's sign
	// that it is running on a Windows-backed host at all. Empty reads
	// DefaultDoctorPowershell.
	Powershell string
	// Netsh is the program run for netsh. Empty reads
	// "/mnt/c/Windows/System32/netsh.exe".
	Netsh string
	// State is where this check keeps its own record of when the internet
	// first looked unreachable, separate from Doctor's own episode for this
	// check's cures.
	State application.DoctorState

	// Now is the clock Probe and Cure read time by. The zero value reads the
	// real one.
	Now func() time.Time
	// Sleep is how Cure waits between netsh commands and while polling the
	// probe after reconnecting. The zero value sleeps for real.
	Sleep func(time.Duration)
}

// NewWifi is the check over the given reach hosts and powershell path, kept
// through state, run through the real netsh.
func NewWifi(reach []string, powershell string, state application.DoctorState) *Wifi {
	return &Wifi{Reach: reach, Powershell: powershell, State: state}
}

// Name implements application.DoctorCheck.
func (w *Wifi) Name() string { return WifiName }

// Probe implements application.DoctorCheck: cannot-tell when this host is not
// Windows-backed; otherwise ok if any reach host connects. Unreachable is
// only faulty once this check's own state says the internet has looked
// unreachable for five minutes straight — before that it is cannot-tell,
// "faulty (waiting 5m)", and no cure runs.
func (w *Wifi) Probe(ctx context.Context) (application.Verdict, string) {
	if _, err := os.Stat(w.powershell()); err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("powershell not found at %s: this host is not Windows-backed", w.powershell())
	}

	if w.reachable(ctx) {
		if err := w.State.Reset(ctx, wifiReachStateName); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("forgetting this check's own state: %v", err)
		}
		return application.DoctorOK, ""
	}

	episode, err := w.State.Load(ctx, wifiReachStateName)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading this check's own state: %v", err)
	}
	first := episode.FirstFaulty
	if first.IsZero() {
		first = w.now()
		if err := w.State.Save(ctx, wifiReachStateName, application.DoctorEpisode{FirstFaulty: first}); err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("saving this check's own state: %v", err)
		}
	}

	if w.now().Sub(first) < WifiWaitBeforeFaulty {
		return application.DoctorCannotTell, "faulty (waiting 5m)"
	}
	return application.DoctorFaulty, "internet unreachable since " + first.UTC().Format(time.RFC3339)
}

// Cure implements application.DoctorCheck: read the current network's SSID,
// disconnect, wait, reconnect to that same SSID, then give the rejoin up to
// WifiReconnectWait, polling the probe, before returning — a wait, not a
// gate: Cure's job is the bounce, not the outcome, so it returns nil once
// the netsh commands themselves succeeded, whether or not the internet was
// back by the time it stopped waiting. It refuses, joining nothing, when the
// interface is not associated with any network to begin with.
func (w *Wifi) Cure(ctx context.Context) error {
	out, err := w.runNetsh(ctx, "wlan", "show", "interfaces")
	if err != nil {
		return fmt.Errorf("reading the current Wi-Fi network (netsh wlan show interfaces): %v: %s", err, out)
	}
	ssid := parseSSID(out)
	if ssid == "" {
		return fmt.Errorf("no SSID in netsh wlan show interfaces output, so there is no network to rejoin: %s", out)
	}

	if out, err := w.runNetsh(ctx, "wlan", "disconnect"); err != nil {
		return fmt.Errorf("netsh wlan disconnect: %v: %s", err, out)
	}
	w.sleep(WifiDisconnectSettle)

	if out, err := w.runNetsh(ctx, "wlan", "connect", "name="+ssid); err != nil {
		return fmt.Errorf("netsh wlan connect name=%s: %v: %s", ssid, err, out)
	}

	deadline := w.now().Add(WifiReconnectWait)
	for !w.reachable(ctx) && w.now().Before(deadline) {
		w.sleep(WifiReconnectPoll)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (w *Wifi) Damper() (time.Duration, int) { return WifiDamperWait, WifiDamperCap }

// WayBack implements application.DoctorCheck: the same command Cure itself
// runs to rejoin, read fresh so it names the network actually associated —
// printed by --dry-run and written to the log beside every cure.
func (w *Wifi) WayBack() string {
	out, err := w.runNetsh(context.Background(), "wlan", "show", "interfaces")
	if err != nil {
		return "netsh wlan connect name=<the current network>"
	}
	ssid := parseSSID(out)
	if ssid == "" {
		return "netsh wlan connect name=<the current network>"
	}
	return "netsh wlan connect name=" + ssid
}

// reachable is ok if any of Reach's hosts resolves and TCP-connects.
func (w *Wifi) reachable(ctx context.Context) bool {
	return Reach(ctx, w.reach())
}

func (w *Wifi) reach() []string {
	if len(w.Reach) > 0 {
		return w.Reach
	}
	return []string{"api.anthropic.com:443", "github.com:443"}
}

func (w *Wifi) powershell() string {
	if w.Powershell != "" {
		return w.Powershell
	}
	return "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
}

func (w *Wifi) runNetsh(ctx context.Context, args ...string) (string, error) {
	program := w.Netsh
	if program == "" {
		program = "/mnt/c/Windows/System32/netsh.exe"
	}
	out, err := exec.CommandContext(ctx, program, args...).CombinedOutput()
	return string(out), err
}

func (w *Wifi) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

func (w *Wifi) sleep(d time.Duration) {
	if w.Sleep == nil {
		time.Sleep(d)
		return
	}
	w.Sleep(d)
}

// parseSSID reads the SSID line of `netsh wlan show interfaces` output — not
// BSSID, which is a separate line with a similar name — and returns "" when
// there is none: the interface is not associated with any network.
func parseSSID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if strings.TrimSpace(key) == "SSID" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
