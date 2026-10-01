package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*MayorStale)(nil)

// MayorStaleName is what the check is called: in the log, and on the command
// line as `mw doctor mayor-stale`.
const MayorStaleName = "mayor-stale"

// DefaultMayorStaleLimit is how long a held Mayor's pane may go unchanged
// before the check calls it stale, when nothing says otherwise. The 2026-10-01
// Mayor sat on a prompt about ten minutes; config.DefaultDoctorMayorStaleMinutes
// is the same limit in minutes.
const DefaultMayorStaleLimit = 15 * time.Minute

// The mayor-stale check's damper: one respawn and one alarm to an episode —
// an episode ends when the pane next changes — and a long wait, so a new
// Mayor that is itself slow to show a change is not killed in its turn.
const (
	MayorStaleDamperWait = 30 * time.Minute
	MayorStaleDamperCap  = 1
)

// mayorStaleSeenStateName is where the check remembers, between runs, the
// pane it last saw and since when, apart from the episode key Doctor keeps
// this check's cure state under.
const mayorStaleSeenStateName = "mayor-stale-seen"

// mayorStalePrompt is the line a harness puts above a question it will not go
// on without. A stale pane showing it is a Mayor waiting on a person, not a
// dead one: the check tells the Governor and leaves the window alone.
const mayorStalePrompt = "Do you want to proceed?"

// mayorStalePromptLines is how many of the pane's last non-blank lines an
// alarm about a prompt carries: the question and its numbered options.
const mayorStalePromptLines = 12

// MayorStale is the check that notices a Mayor which still holds the seat but
// has stopped. The heartbeat is the seat's own pane: a working or waiting
// harness redraws it (a spinner, an elapsed clock), so a held Mayor whose
// pane text has not changed for Limit is stuck or dead, whatever its window
// still says. It finds the window the way MayorGone does and leaves the
// seats MayorGone judges — no .mayor-acting, a host that is not home, a window
// gone or holding only a bare shell — to it: those are not held, and this
// check says ok.
//
// Its cure closes the stale window and runs the vault's own bin/mayor-up
// (the one way MayorGone brings a Mayor up too), then sends one alarm. A pane
// showing a harness prompt is not respawned: the alarm carries the prompt and
// its options for the Governor to answer.
type MayorStale struct {
	// Vault is the vault directory: .mayor-acting and bin/mayor-up are read
	// from under it.
	Vault string
	// Tmux and PS are the programs run for tmux and ps. Empty reads the names.
	Tmux string
	PS   string
	// Home and Host are the vault's home file and this host's name, read as
	// MayorGone reads them: a host that is not home holds no Mayor.
	Home application.HomeFile
	Host string
	// State is where the check keeps the pane it last saw.
	State application.DoctorState
	// Limit is how long a held pane may stay as it is. Zero reads
	// DefaultMayorStaleLimit.
	Limit time.Duration
	// Now is the clock. The zero value reads the real one.
	Now func() time.Time
	// Alarm sends the Governor's phone the one alarm a cure ends with. A nil
	// Alarm sends none.
	Alarm func(ctx context.Context, text string) error
	// Timeout bounds each run of bin/mayor-up. Empty reads
	// MayorGoneCureTimeout.
	Timeout time.Duration
	// Settle is how long Cure waits for the closed window's process to be
	// gone when bin/mayor-up still finds it alive (exit 3), and Cure tries
	// three times. Zero reads two seconds.
	Settle time.Duration

	// started is the window id bin/mayor-up printed on its last output line,
	// read by WayBack once Cure has run.
	started string
}

// NewMayorStale is the check over the vault, run through the real tmux and the
// vault's own bin/mayor-up, remembering what it has seen in state.
func NewMayorStale(vault string, state application.DoctorState) *MayorStale {
	return &MayorStale{Vault: vault, State: state}
}

// Name implements application.DoctorCheck.
func (m *MayorStale) Name() string { return MayorStaleName }

// held is MayorGone's reading of the same vault, whose window search and
// bare-shell test this check shares.
func (m *MayorStale) held() *MayorGone {
	return &MayorGone{Vault: m.Vault, Tmux: m.Tmux, PS: m.PS, Home: m.Home, Host: m.Host}
}

// Probe implements application.DoctorCheck: ok when the seat is not held (see
// MayorStale), and when it is held, ok while the pane differs from the last
// look or has stood still less than Limit; faulty, naming the minutes, once
// it has stood still that long. It writes one thing: the pane it saw, in its
// own state key, as vault-dirty remembers its sightings.
func (m *MayorStale) Probe(ctx context.Context) (application.Verdict, string) {
	window, name, held, verdict, reason := m.window(ctx)
	if !held {
		return verdict, reason
	}
	pane, err := m.capture(ctx, window)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading the pane of window %s: %v", window, err)
	}

	since, err := m.since(ctx, window, pane)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if still := m.now().Sub(since); still >= m.limit() {
		return application.DoctorFaulty, fmt.Sprintf("the pane of window %s (%s) has not changed for %d minutes", name, window, int(still/time.Minute))
	}
	return application.DoctorOK, ""
}

// window is the id and name of the Mayor's window when the seat is held. When
// it is not, held is false and the verdict and reason say why: ok for a seat
// nobody holds, cannot-tell for one that could not be read.
func (m *MayorStale) window(ctx context.Context) (id, name string, held bool, verdict application.Verdict, reason string) {
	gone := m.held()
	if gone.Home != nil {
		if home, err := application.IsHome(ctx, gone.Home, gone.Host); err == nil && !home {
			return "", "", false, application.DoctorOK, ""
		}
	}
	actingPath := gone.actingPath()
	acting, err := os.ReadFile(actingPath)
	if err != nil {
		return "", "", false, application.DoctorOK, ""
	}
	name, byName, found, err := gone.locate(ctx, string(acting), actingPath)
	if err != nil {
		return "", "", false, application.DoctorCannotTell, err.Error()
	}
	if !found {
		return "", "", false, application.DoctorOK, ""
	}
	alive, err := gone.paneAlive(ctx, byName[name])
	if err != nil {
		return "", "", false, application.DoctorCannotTell, fmt.Sprintf("asking tmux about window %s: %v", byName[name], err)
	}
	if !alive {
		return "", "", false, application.DoctorOK, ""
	}
	return byName[name], name, true, application.DoctorOK, ""
}

// since is when the pane of window last changed, as far as this check has
// looked: now, for a pane or window it has not seen before, which it saves.
func (m *MayorStale) since(ctx context.Context, window, pane string) (time.Time, error) {
	if m.State == nil {
		return m.now(), nil
	}
	sum := sha256.Sum256([]byte(pane))
	seen := hex.EncodeToString(sum[:])
	episode, err := m.State.Load(ctx, mayorStaleSeenStateName)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading this check's own state: %w", err)
	}
	if len(episode.SeenPaths) == 3 && episode.SeenPaths[0] == window && episode.SeenPaths[1] == seen {
		if at, err := time.Parse(time.RFC3339, episode.SeenPaths[2]); err == nil {
			return at, nil
		}
	}
	at := m.now()
	err = m.State.Save(ctx, mayorStaleSeenStateName, application.DoctorEpisode{SeenPaths: []string{window, seen, at.UTC().Format(time.RFC3339)}})
	if err != nil {
		return time.Time{}, fmt.Errorf("saving this check's own state: %w", err)
	}
	return at, nil
}

// Cure implements application.DoctorCheck. The pane is read again: showing a
// harness prompt, it is left alone and the alarm carries the prompt;
// otherwise the window is closed and bin/mayor-up run, and the alarm says
// which Mayor took the seat, or that none could be started. The alarm is sent
// whichever way the respawn went, once.
func (m *MayorStale) Cure(ctx context.Context) error {
	window, name, held, _, reason := m.window(ctx)
	if !held {
		return fmt.Errorf("the Mayor's window is no longer found (%s)", reason)
	}
	pane, err := m.capture(ctx, window)
	if err != nil {
		return fmt.Errorf("reading the pane of window %s: %w", window, err)
	}
	minutes := 0
	if since, err := m.since(ctx, window, pane); err == nil {
		minutes = int(m.now().Sub(since) / time.Minute)
	}

	if strings.Contains(pane, mayorStalePrompt) {
		text := fmt.Sprintf("The Mayor (window %s) has waited %d minutes on a prompt. The doctor left it alone; answer it in the window:\n%s",
			name, minutes, lastLines(pane, mayorStalePromptLines))
		return m.alarm(ctx, text, nil)
	}

	if _, err := m.held().run(ctx, "kill-window", "-t", window); err != nil {
		return m.alarm(ctx, fmt.Sprintf("The Mayor (window %s) has not changed for %d minutes, and the doctor could not close it: %v", name, minutes, err), err)
	}
	started, upErr := m.mayorUp(ctx)
	if upErr != nil {
		text := fmt.Sprintf("The Mayor (window %s) has not changed for %d minutes. The doctor closed it but could not start a new one: %v", name, minutes, upErr)
		return m.alarm(ctx, text, upErr)
	}
	m.started = started
	return m.alarm(ctx, fmt.Sprintf("The Mayor (window %s) had not changed for %d minutes. The doctor closed it and started a new one in window %s.", name, minutes, started), nil)
}

// alarm sends text and returns cause, or, when cause is nil and the alarm
// itself failed, that failure.
func (m *MayorStale) alarm(ctx context.Context, text string, cause error) error {
	if m.Alarm == nil {
		return cause
	}
	if err := m.Alarm(ctx, text); err != nil {
		if cause != nil {
			return fmt.Errorf("%w (and the alarm could not be sent: %v)", cause, err)
		}
		return fmt.Errorf("the alarm could not be sent: %w", err)
	}
	return cause
}

// mayorUp runs the vault's bin/mayor-up, as MayorGone's cure does, trying
// again after Settle while it still finds the closed window's Mayor alive
// (exit 3), and returns the window id it printed last.
func (m *MayorStale) mayorUp(ctx context.Context) (string, error) {
	gone := m.held()
	gone.Timeout = m.Timeout
	settle := m.Settle
	if settle == 0 {
		settle = 2 * time.Second
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(settle):
			}
		}
		gone.window = ""
		if err = gone.Cure(ctx); err == nil {
			return gone.window, nil
		}
		// MayorGone.Cure folds the exit status into its error text.
		if !strings.Contains(err.Error(), "exit status 3") {
			break
		}
	}
	return "", err
}

// Damper implements application.DoctorCheck.
func (m *MayorStale) Damper() (time.Duration, int) { return MayorStaleDamperWait, MayorStaleDamperCap }

// WayBack implements application.DoctorCheck.
func (m *MayorStale) WayBack() string {
	if m.started == "" {
		return fmt.Sprintf("would close the Mayor's window and run: MW_DOCTOR=1 %s; its way back, once it starts a Mayor, is: tmux kill-window -t '<window id it prints>'", m.held().mayorUpPath())
	}
	return fmt.Sprintf("tmux kill-window -t '%s'", m.started)
}

func (m *MayorStale) limit() time.Duration {
	if m.Limit > 0 {
		return m.Limit
	}
	return DefaultMayorStaleLimit
}

func (m *MayorStale) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// capture is the text of window's pane.
func (m *MayorStale) capture(ctx context.Context, window string) (string, error) {
	return m.held().run(ctx, "capture-pane", "-p", "-t", window)
}

// lastLines is the last n non-blank lines of text, trimmed.
func lastLines(text string, n int) string {
	lines := nonEmptyLines(text)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
