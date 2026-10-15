package doctor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*MayorStuck)(nil)

// MayorStuckName is what the check is called: in the log, and on the command
// line as `mw doctor mayor-stuck`.
const MayorStuckName = "mayor-stuck"

// DefaultMayorStuckWindow is how far back the check reads the Mayor's replies:
// a Mayor whose every reply in this long was an API error is stuck.
const DefaultMayorStuckWindow = 10 * time.Minute

// The mayor-stuck check's damper: one successor and one alarm to an episode,
// and a long wait, so a successor that is itself slow to answer is not closed
// in its turn.
const (
	MayorStuckDamperWait = 30 * time.Minute
	MayorStuckDamperCap  = 1
)

// mayorStuckAlarmStateName is where the check remembers, between runs, the seq
// of the emergency its alarm was written as, until the Mayor answers again and
// the check clears it.
const mayorStuckAlarmStateName = "mayor-stuck-alarm"

// mayorStuckLimitStateName is where the check remembers, between runs, the
// window it saw parked on the usage limit, since when, and whether the
// Governor was paged about it, until the pane leaves the limit.
const mayorStuckLimitStateName = "mayor-stuck-limit"

// mayorStuckLimitChoice is a choice of the menu the harness puts up at the
// usage limit — "1. Stop and wait for limit to reset", "2. Switch to usage
// credits" — marked or not by its selection arrow, drawn in a box or not. The
// words quoted in prose, as a story's title quotes them, are not a choice.
var mayorStuckLimitChoice = regexp.MustCompile(`^[\s│|]*(?:[❯>]\s*)?\d+\.\s+(?:Stop and wait|Switch to usage credits)`)

// mayorStuckLimitNotice is the harness's own line at the limit, which begins
// with its words ("⎿  You've hit your weekly limit · resets …"); a sentence
// that only mentions them does not.
var mayorStuckLimitNotice = regexp.MustCompile(`^[\s│|⎿]*(?i:you(?:'|’)ve|you have) hit your\b`)

// mayorStuckLimitTail is how far up from the bottom of the pane the limit
// notice is read when no limit menu stands under it: the input box and status
// lines are below it, and a Mayor that went on working has scrolled it away.
const mayorStuckLimitTail = 12

// mayorStuckDialTimeout bounds one TCP connect to a proxy.
const mayorStuckDialTimeout = 3 * time.Second

// MayorStuckProxyNames are the environment settings that send a session's
// traffic through a proxy, which the cure drops when the proxy is dead.
var MayorStuckProxyNames = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}

// MayorStuck is the check that notices a Mayor which holds the seat, whose
// pane may even redraw, but whose every reply lately was an API error: the
// network it was started on is gone — on 2026-10-06 the Laptop left the phone
// tether and the proxy the Mayor was started behind, an ssh tunnel to the
// phone, closed — and neither MayorGone nor MayorStale can tell. It reads the
// Mayor's transcript through application.TranscriptReplies: stuck when it
// holds at least one reply from the last Window and every one of them was an
// API error. Where the seat is not held (see MayorStale) it says ok.
//
// It reads the Mayor's pane too: a Mayor parked on the harness's usage-limit
// menu ("Stop and wait" / "Switch to usage credits"), or its "You've hit your
// weekly limit" notice, writes no reply at all, so the transcript cannot see it
// (mw-gq6.433: @138 sat there 2h12m on 2026-10-15). A pane on the limit for
// Window is the factory stopped. A successor would meet the same limit, so that
// cure leaves the window alone and pages the Governor once for the window.
//
// Its cure drops each proxy setting whose port refuses a TCP connect from the
// environment bin/mayor-up runs in and from the tmux server's global
// environment — a proxy that answers is kept — closes the Mayor's window and
// runs the vault's own bin/mayor-up, which starts a successor from the newest
// handoff with no handover. It sends one alarm, on the emergency lane.
type MayorStuck struct {
	// Vault is the vault directory: .mayor-acting and bin/mayor-up are read
	// from under it, and it is the directory the Mayor's session runs in.
	Vault string
	// Tmux and PS are the programs run for tmux and ps. Empty reads the names.
	Tmux string
	PS   string
	// Home and Host are the vault's home file and this host's name, read as
	// MayorGone reads them: a host that is not home holds no Mayor.
	Home application.HomeFile
	Host string
	// State is where the check keeps its alarm's seq.
	State application.DoctorState
	// Replies is the Mayor's transcript.
	Replies application.TranscriptReplies
	// Window is how far back the replies are read. Zero reads
	// DefaultMayorStuckWindow.
	Window time.Duration
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time
	// Dial opens a TCP connection to address ("host:port") and closes it, or
	// says why it could not. Nil dials for real.
	Dial func(ctx context.Context, address string) error
	// Environ is the environment the successor would start in. Nil reads this
	// process's.
	Environ func() []string
	// Alarm, Clear, Timeout and Settle are MayorStale's.
	Alarm   func(ctx context.Context, text string) (uint64, error)
	Clear   func(ctx context.Context, text string, clears uint64) error
	Timeout time.Duration
	Settle  time.Duration

	// started is the window id bin/mayor-up printed on its last output line.
	started string
	// limited is whether the last Probe found the pane parked on the usage
	// limit, and outcome what Cure did about it, both read by WayBack.
	limited bool
	outcome string
}

// NewMayorStuck is the check over the vault, run through the real tmux and the
// vault's own bin/mayor-up, remembering its alarm in state.
func NewMayorStuck(vault string, state application.DoctorState) *MayorStuck {
	return &MayorStuck{Vault: vault, State: state}
}

// Name implements application.DoctorCheck.
func (m *MayorStuck) Name() string { return MayorStuckName }

// stale is MayorStale over the same vault, whose window search, bin/mayor-up
// retries and way back this check shares.
func (m *MayorStuck) stale(env []string) *MayorStale {
	return &MayorStale{
		Vault: m.Vault, Tmux: m.Tmux, PS: m.PS, Home: m.Home, Host: m.Host, State: m.State,
		Timeout: m.Timeout, Settle: m.Settle, Env: env,
	}
}

// Probe implements application.DoctorCheck: ok where the seat is not held;
// faulty, naming the minutes and the harness's notice, when the pane has stood
// on the usage limit for Window, and ok while it has stood there less, whatever
// the transcript says; cannot-tell when the transcript cannot be read; ok when
// the last Window holds no reply, or one that worked; faulty, naming the count
// and the last error, when it holds replies and every one was an API error. It
// writes only its own record of when it first saw the pane on the limit, and,
// once the Mayor has answered again after an alarm, the one event that ends it
// (neither in a dry run).
func (m *MayorStuck) Probe(ctx context.Context) (application.Verdict, string) {
	st := m.stale(nil)
	id, name, held, verdict, reason := st.window(ctx)
	if !held {
		return verdict, reason
	}
	// A pane that cannot be read is no reason to stop reading the transcript:
	// it is judged as one not on the limit.
	pane, _ := st.capture(ctx, id)
	limit, on := limitOf(pane)
	m.limited = on
	since, err := m.limitSince(ctx, id, on)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if on {
		// Parked on the limit, the Mayor writes nothing: its replies say
		// nothing of it, and a respawn would meet the same limit.
		if still := m.now().Sub(since); still >= m.window() {
			return application.DoctorFaulty, fmt.Sprintf("the Mayor (window %s, %s) is stopped at the usage limit: its pane has shown it for %d minutes with no output: %s",
				name, id, int(still/time.Minute), strings.Join(strings.Fields(firstLimitLine(limit)), " "))
		}
		return application.DoctorOK, ""
	}
	if m.Replies == nil {
		return application.DoctorCannotTell, "there is no transcript to read the Mayor's replies from"
	}
	window := m.window()
	replies, err := m.Replies.Replies(ctx, m.Vault, m.now().Add(-window))
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading the Mayor's transcript: %v", err)
	}
	worked := 0
	for _, reply := range replies {
		if !reply.APIError {
			worked++
		}
	}
	if len(replies) > 0 && worked == 0 {
		last := replies[len(replies)-1]
		return application.DoctorFaulty, fmt.Sprintf("every one of the Mayor's %d replies in the last %d minutes (window %s) was an API Error, the last at %s: %s",
			len(replies), int(window/time.Minute), name, last.At.UTC().Format("15:04:05Z"), strings.Join(strings.Fields(last.Text), " "))
	}
	if worked > 0 {
		if err := m.ended(ctx, name); err != nil {
			return application.DoctorCannotTell, err.Error()
		}
	}
	return application.DoctorOK, ""
}

// ended tells the Governor, once, that a Mayor the check alarmed about has
// answered again, naming the alarm's emergency, and forgets the alarm. With no
// alarm recorded, or no way to send, there is nothing to say; the record is
// kept until the event is sent, so one that could not be sent is tried again.
func (m *MayorStuck) ended(ctx context.Context, name string) error {
	if m.State == nil || application.DoctorDryRun(ctx) {
		return nil
	}
	episode, err := m.State.Load(ctx, mayorStuckAlarmStateName)
	if err != nil {
		return fmt.Errorf("reading this check's own state: %w", err)
	}
	clears := seqAt(episode.SeenPaths, 0)
	if clears == 0 {
		return nil
	}
	if m.Clear != nil {
		text := fmt.Sprintf("The Mayor (window %s) is answering again", name)
		if err := m.Clear(ctx, text, clears); err != nil {
			return fmt.Errorf("the Mayor-is-answering event could not be sent: %w", err)
		}
	}
	if err := m.State.Reset(ctx, mayorStuckAlarmStateName); err != nil {
		return fmt.Errorf("forgetting this check's own state: %w", err)
	}
	return nil
}

// Cure implements application.DoctorCheck: drop the dead proxies, close the
// Mayor's window, run bin/mayor-up, and send the one alarm whichever way the
// respawn went. A pane parked on the usage limit is left alone instead, and
// the alarm, sent once for the window, carries the limit and its menu.
func (m *MayorStuck) Cure(ctx context.Context) error {
	st := m.stale(nil)
	m.outcome = ""
	window, name, held, _, reason := st.window(ctx)
	if !held {
		return fmt.Errorf("the Mayor's window is no longer found (%s)", reason)
	}
	if pane, err := st.capture(ctx, window); err == nil {
		if limit, on := limitOf(pane); on {
			return m.pageTheLimit(ctx, window, name, limit)
		}
	}
	env, dropped := m.withoutDeadProxies(ctx, st)
	st.Env = env

	if _, err := st.held().run(ctx, "kill-window", "-t", window); err != nil {
		return m.alarm(ctx, fmt.Sprintf("The Mayor (window %s) is stuck on API Errors, and the doctor could not close it: %v", name, err), err)
	}
	started, upErr := st.mayorUp(ctx)
	if upErr != nil {
		text := fmt.Sprintf("The Mayor (window %s) was stuck on API Errors. The doctor closed it but could not start a new one: %v", name, upErr)
		return m.alarm(ctx, text, upErr)
	}
	m.started = started
	text := fmt.Sprintf("The Mayor (window %s) was stuck on API Errors: every reply in %d minutes failed. The doctor closed it and started a new one in window %s, with no handover.",
		name, int(m.window()/time.Minute), started)
	if len(dropped) > 0 {
		text += " It dropped a dead proxy: " + strings.Join(dropped, ", ") + "."
	}
	return m.alarm(ctx, text, nil)
}

// pageTheLimit sends the Governor the one alarm for a Mayor parked on the
// usage limit, unless this window's was sent already, and leaves the window as
// it is.
func (m *MayorStuck) pageTheLimit(ctx context.Context, window, name, limit string) error {
	m.outcome = "left the Mayor's window alone: it is stopped at the usage limit, which a new Mayor would meet too, so there is nothing to undo"
	seen, err := m.loadLimit(ctx)
	if err != nil {
		return err
	}
	if len(seen) == 3 && seen[0] == window && seen[2] == "paged" {
		return nil
	}
	minutes := int(m.window() / time.Minute)
	if len(seen) >= 2 && seen[0] == window {
		if at, err := time.Parse(time.RFC3339, seen[1]); err == nil {
			minutes = int(m.now().Sub(at) / time.Minute)
		}
	}
	text := fmt.Sprintf("The Mayor (window %s) is stopped at the usage limit, so the factory is stopped: its pane has shown it for %d minutes with no output. "+
		"The doctor left the window alone, as a new Mayor would meet the same limit. Answer it in the window once the limit resets, or switch to usage credits:\n%s",
		name, minutes, limit)
	if err := m.alarm(ctx, text, nil); err != nil {
		return err
	}
	if m.State == nil || len(seen) < 2 || seen[0] != window {
		return nil
	}
	if err := m.State.Save(ctx, mayorStuckLimitStateName, application.DoctorEpisode{SeenPaths: []string{seen[0], seen[1], "paged"}}); err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// limitSince is when this check first saw window's pane on the usage limit,
// in this run of sightings: now, for a window it had not seen there, which it
// saves. A pane off the limit forgets the sighting, so the limit coming back
// later is timed, and paged, again. The pane may redraw while it stands on
// the limit (a countdown to the reset), so the time is the first sighting's,
// not the last change's. A dry run saves and forgets nothing.
func (m *MayorStuck) limitSince(ctx context.Context, window string, on bool) (time.Time, error) {
	if m.State == nil {
		return m.now(), nil
	}
	seen, err := m.loadLimit(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if !on {
		if len(seen) > 0 && !application.DoctorDryRun(ctx) {
			if err := m.State.Reset(ctx, mayorStuckLimitStateName); err != nil {
				return time.Time{}, fmt.Errorf("forgetting this check's own state: %w", err)
			}
		}
		return m.now(), nil
	}
	if len(seen) >= 2 && seen[0] == window {
		if at, err := time.Parse(time.RFC3339, seen[1]); err == nil {
			return at, nil
		}
	}
	at := m.now()
	if application.DoctorDryRun(ctx) {
		return at, nil
	}
	if err := m.State.Save(ctx, mayorStuckLimitStateName, application.DoctorEpisode{SeenPaths: []string{window, at.UTC().Format(time.RFC3339)}}); err != nil {
		return time.Time{}, fmt.Errorf("saving this check's own state: %w", err)
	}
	return at, nil
}

// loadLimit is the usage-limit sighting this check keeps: the window, when it
// was first seen there, and "paged" once the Governor was told.
func (m *MayorStuck) loadLimit(ctx context.Context) ([]string, error) {
	if m.State == nil {
		return nil, nil
	}
	episode, err := m.State.Load(ctx, mayorStuckLimitStateName)
	if err != nil {
		return nil, fmt.Errorf("reading this check's own state: %w", err)
	}
	return episode.SeenPaths, nil
}

// limitOf is the usage limit a pane is parked on, when it shows one: the
// harness's notice and the choices of its limit menu, each line trimmed. With
// the menu up, the notice is read anywhere on the pane; without it, only in the
// pane's last mayorStuckLimitTail lines.
func limitOf(pane string) (string, bool) {
	lines := nonEmptyLines(strings.ReplaceAll(pane, "\u00a0", " "))
	menu := false
	for _, line := range lines {
		if mayorStuckLimitChoice.MatchString(line) {
			menu = true
			break
		}
	}
	var shown []string
	for i, line := range lines {
		switch {
		case mayorStuckLimitChoice.MatchString(line):
			shown = append(shown, line)
		case mayorStuckLimitNotice.MatchString(line) && application.AtUsageLimit(line) && (menu || i >= len(lines)-mayorStuckLimitTail):
			shown = append(shown, line)
		}
	}
	return strings.Join(shown, "\n"), len(shown) > 0
}

// firstLimitLine is the first line of what limitOf read: the notice, where
// the pane shows one.
func firstLimitLine(limit string) string {
	first, _, _ := strings.Cut(limit, "\n")
	return first
}

// withoutDeadProxies is the environment the successor starts in with every
// proxy setting whose port refuses a connection taken out, and what was
// dropped, named as "NAME=value". It also unsets, in the tmux server's global
// environment, each proxy setting the server holds whose port refuses: a
// window tmux opens takes its environment from there. A proxy that answers,
// or whose value names no port to try, is left as it is.
func (m *MayorStuck) withoutDeadProxies(ctx context.Context, st *MayorStale) (env, dropped []string) {
	verdicts := map[string]bool{}
	dead := func(value string) bool {
		if v, seen := verdicts[value]; seen {
			return v
		}
		v := m.refused(ctx, value)
		verdicts[value] = v
		return v
	}

	base := m.environ()
	env = make([]string, 0, len(base))
	for _, pair := range base {
		name, value, _ := strings.Cut(pair, "=")
		if isProxyName(name) && dead(value) {
			dropped = append(dropped, name+"="+value)
			continue
		}
		env = append(env, pair)
	}

	if out, err := st.held().run(ctx, "show-environment", "-g"); err == nil {
		var unset []string
		for _, line := range strings.Split(out, "\n") {
			name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && isProxyName(name) && dead(value) {
				unset = append(unset, name)
				dropped = append(dropped, "tmux "+name+"="+value)
			}
		}
		sort.Strings(unset)
		for _, name := range unset {
			_, _ = st.held().run(ctx, "set-environment", "-g", "-u", name)
		}
	}
	return env, dropped
}

// refused reports whether the proxy value names points at a port that does not
// accept a TCP connection. A value it cannot read a host and port from is not
// refused: the proxy is kept.
func (m *MayorStuck) refused(ctx context.Context, value string) bool {
	address, ok := proxyAddress(value)
	if !ok {
		return false
	}
	dial := m.Dial
	if dial == nil {
		dial = dialTCP
	}
	return dial(ctx, address) != nil
}

// dialTCP connects to address and closes the connection.
func dialTCP(ctx context.Context, address string) error {
	dialer := net.Dialer{Timeout: mayorStuckDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

// isProxyName reports whether name is one of MayorStuckProxyNames.
func isProxyName(name string) bool {
	for _, proxy := range MayorStuckProxyNames {
		if name == proxy {
			return true
		}
	}
	return false
}

// proxyDefaultPorts are the ports a proxy URL with no port of its own means.
var proxyDefaultPorts = map[string]string{
	"http": "80", "https": "443", "socks": "1080", "socks4": "1080", "socks4a": "1080", "socks5": "1080", "socks5h": "1080",
}

// proxyAddress is the "host:port" a proxy setting points at: a URL, or a bare
// host:port, which reads as http. ok is false for a value with no host or a
// scheme whose port is unknown.
func proxyAddress(value string) (address string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = proxyDefaultPorts[strings.ToLower(u.Scheme)]
	} else if _, err := strconv.Atoi(port); err != nil {
		return "", false
	}
	if port == "" {
		return "", false
	}
	return net.JoinHostPort(u.Hostname(), port), true
}

// alarm sends text and returns cause, or, when cause is nil and the alarm
// itself failed, that failure. The seq of the emergency it was written as is
// kept until the Mayor answers again.
func (m *MayorStuck) alarm(ctx context.Context, text string, cause error) error {
	if m.Alarm == nil {
		return cause
	}
	seq, err := m.Alarm(ctx, text)
	if err != nil {
		if cause != nil {
			return fmt.Errorf("%w (and the alarm could not be sent: %v)", cause, err)
		}
		return fmt.Errorf("the alarm could not be sent: %w", err)
	}
	if seq != 0 && m.State != nil {
		if err := m.State.Save(ctx, mayorStuckAlarmStateName, application.DoctorEpisode{SeenPaths: []string{strconv.FormatUint(seq, 10)}}); err != nil {
			return fmt.Errorf("saving this check's own state: %w", err)
		}
	}
	return cause
}

// Damper implements application.DoctorCheck.
func (m *MayorStuck) Damper() (time.Duration, int) { return MayorStuckDamperWait, MayorStuckDamperCap }

// WayBack implements application.DoctorCheck.
func (m *MayorStuck) WayBack() string {
	if m.outcome != "" {
		return m.outcome
	}
	if m.limited && m.started == "" {
		return "would leave the Mayor's window alone, as it is stopped at the usage limit, and page the Governor once with the limit and its menu"
	}
	if m.started == "" {
		return fmt.Sprintf("would drop any dead proxy setting, close the Mayor's window and run: MW_DOCTOR=1 %s; its way back, once it starts a Mayor, is: tmux kill-window -t '<window id it prints>'", m.stale(nil).held().mayorUpPath())
	}
	return fmt.Sprintf("tmux kill-window -t '%s'", m.started)
}

func (m *MayorStuck) window() time.Duration {
	if m.Window > 0 {
		return m.Window
	}
	return DefaultMayorStuckWindow
}

func (m *MayorStuck) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *MayorStuck) environ() []string {
	if m.Environ != nil {
		return m.Environ()
	}
	return os.Environ()
}
