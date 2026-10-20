package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// creditsContext is what features/grist_credits.feature adds to a mill: a
// stand-in for the claude program that records the environment each session
// started with, the real adapter that runs it, the real key file and spent
// mark, and a log of the events the mill leaves.
type creditsContext struct {
	c *gristContext

	program string
	dir     string // the stand-in's own directory: its log, its answers
	state   string // the directory the spent mark is kept in
	keyFile string
	on      bool
	pooled  bool
	events  *apptest.FakeEventLog

	// shellKey and shellKeySet are ANTHROPIC_API_KEY as the scenario found
	// it, put back when it ends.
	shellKey    string
	shellKeySet bool
	touchedEnv  bool
}

// standIn is the claude program of this feature. It logs, one line a start,
// its pid, whether it was started cold or warm and the ANTHROPIC_API_KEY it
// saw ("-" for none); keeps its arguments; and prints the answer the key-result
// file names when it has a key, else the good answer.
const standIn = `#!/bin/sh
dir=$(dirname "$0")
if [ -n "${ANTHROPIC_API_KEY+x}" ]; then k="$ANTHROPIC_API_KEY"; else k=-; fi
mode=cold
case "$*" in *--input-format*) mode=warm;; esac
printf '%s\0' "$@" > "$dir/args.$$"
printf '%s %s %s\n' "$$" "$mode" "$k" >> "$dir/runs.log"
if [ "$mode" = warm ]; then IFS= read -r line || exit 0; cat > /dev/null; fi
out=ok
if [ "$k" != - ] && [ -f "$dir/key-result" ]; then out=$(cat "$dir/key-result"); fi
cat "$dir/$out.json"
[ "$out" = ok ] || exit 1
`

// creditsRun is one line of the stand-in's log.
type creditsRun struct {
	pid  int
	mode string
	key  string
}

func (cc *creditsContext) wire(m *application.GristGrind) {
	if cc.program == "" {
		return
	}
	real := claude.NewGrinder(claude.WithGrindProgram(cc.program))
	m.Grinder = real
	var credits *application.GristCredits
	if cc.on {
		credits = &application.GristCredits{
			On:     true,
			Key:    grist.CreditKeyFile{Path: cc.keyFile},
			State:  &grist.CreditState{Dir: cc.state},
			Events: cc.events,
			Host:   "laptop",
			Now:    func() time.Time { return gristNow },
		}
	}
	m.Credits = credits
	if cc.pooled {
		pool := &cc.c.pool
		if pool.pool == nil {
			pool.warmer = &apptest.FakeWarmGrinder{}
			pool.pool = &application.GristPool{
				Warmer: real, Slots: 2, TempDir: cc.c.home, Now: pool.now,
				Quiet: application.DefaultGristPoolQuiet, MaxAge: application.DefaultGristPoolMaxAge,
			}
		}
		pool.pool.UseCredits(credits)
		m.Pool = pool.pool
	}
}

func registerGristCredits(ctx *godog.ScenarioContext, c *gristContext) {
	cc := &c.credits
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		cc.c = c
		cc.shellKey, cc.shellKeySet = os.LookupEnv(claude.APIKeyEnv)
		// A key in the environment of whoever runs the suite must not reach
		// the scenario's sessions, nor be mistaken for the scenario's own.
		os.Unsetenv(claude.APIKeyEnv)
		cc.touchedEnv = true
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if cc.touchedEnv {
			if cc.shellKeySet {
				os.Setenv(claude.APIKeyEnv, cc.shellKey)
			} else {
				os.Unsetenv(claude.APIKeyEnv)
			}
		}
		return ctx, nil
	})

	ctx.Given(`^the mill runs its grinds through a stand-in claude$`, cc.standInClaude)
	ctx.Given(`^api_credits is (on|off), and the key file holds the key "([^"]*)" at mode (\d+)$`, cc.keyFileHolds)
	ctx.Given(`^the mill's own environment holds the ANTHROPIC_API_KEY "([^"]*)"$`, cc.environmentHolds)
	ctx.Given(`^the mill keeps a warm pool of real sessions$`, cc.keepsAPool)
	ctx.Given(`^the API answers a grind billed to the key with "([^"]*)"$`, cc.apiAnswers)
	ctx.Given(`^the API refuses the key$`, cc.apiRefuses)
	ctx.Given(`^the key was found spent (\d+) hours ago$`, cc.foundSpent)

	ctx.Then(`^(\d+) sessions? (?:was|were) given the key "([^"]*)" in (?:its|their) environment$`, cc.sessionsGivenKey)
	ctx.Then(`^(\d+) sessions? (?:was|were) given no ANTHROPIC_API_KEY$`, cc.sessionsGivenNone)
	ctx.Then(`^no session was without it$`, cc.noSessionWithout)
	ctx.Then(`^no session was given a key$`, cc.noSessionGivenAKey)
	ctx.Then(`^no argument any session was given holds the key$`, cc.noArgumentHoldsTheKey)
	ctx.Then(`^the mill's record holds one line, billed to "([^"]*)"$`, cc.recordHoldsOneLine)
	ctx.Then(`^the key is in no line of the record, no event and no report$`, cc.keyIsNowhere)
	ctx.Then(`^the mill's report says the key file is not used because its mode is (\d+)$`, cc.reportSaysModeRefused)
	ctx.Then(`^a warm session waits, started with the key "([^"]*)" in its environment$`, cc.warmWaitsWithKey)
	ctx.Then(`^a warm session waits, started with no ANTHROPIC_API_KEY$`, cc.warmWaitsWithout)
	ctx.Then(`^no warm session started with the key is still running$`, cc.noKeyedWarmRuns)
	ctx.Then(`^(\d+) sessions ran: the key was tried, then the same grist without it$`, cc.keyTriedThenNot)
	ctx.Then(`^the grist counts once against the day's limit$`, cc.countsOnce)
	ctx.Then(`^the spent mark was written$`, cc.markWritten)
	ctx.Then(`^the spent mark was kept$`, cc.markKept)
	ctx.Then(`^the spent mark was cleared$`, cc.markCleared)
	ctx.Then(`^one grist-credits-spent event was left for the Mayor saying "([^"]*)", in the normal lane$`, cc.eventLeft)
}

func (cc *creditsContext) standInClaude() error {
	cc.dir = filepath.Join(cc.c.home, "claude-standin")
	cc.state = filepath.Join(cc.c.home, "credits-state")
	cc.keyFile = filepath.Join(cc.c.home, "anthropic-api.key")
	cc.events = &apptest.FakeEventLog{}
	if err := os.MkdirAll(cc.dir, 0o755); err != nil {
		return err
	}
	cc.program = filepath.Join(cc.dir, "claude")
	if err := os.WriteFile(cc.program, []byte(standIn), 0o755); err != nil {
		return err
	}
	good := cc.c.vectors.Answers["answered"].Answer
	var compact bytes.Buffer
	if err := json.Compact(&compact, good); err != nil {
		return err
	}
	ok := `{"type":"result","subtype":"success","is_error":false,"result":"","stop_reason":"end_turn","num_turns":1,"duration_ms":100,"structured_output":` + compact.String() + `}`
	return os.WriteFile(filepath.Join(cc.dir, "ok.json"), []byte(ok+"\n"), 0o644)
}

func (cc *creditsContext) keyFileHolds(switchState, key, mode string) error {
	cc.on = switchState == "on"
	perm, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cc.keyFile, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(cc.keyFile, os.FileMode(perm))
}

func (cc *creditsContext) environmentHolds(key string) error {
	return os.Setenv(claude.APIKeyEnv, key)
}

func (cc *creditsContext) keepsAPool() error {
	cc.pooled = true
	return nil
}

func (cc *creditsContext) apiAnswers(said string) error {
	failed, err := json.Marshal(said)
	if err != nil {
		return err
	}
	text := `{"type":"result","subtype":"success","is_error":true,"result":` + string(failed) + `,"num_turns":1,"duration_ms":10}`
	if err := os.WriteFile(filepath.Join(cc.dir, "refused.json"), []byte(text+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cc.dir, "key-result"), []byte("refused"), 0o644)
}

func (cc *creditsContext) apiRefuses() error {
	return cc.apiAnswers(`API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
}

func (cc *creditsContext) foundSpent(hours int) error {
	return (&grist.CreditState{Dir: cc.state}).Spend(context.Background(),
		application.GristCreditMark{SpentAt: gristNow.Add(-time.Duration(hours) * time.Hour)})
}

// runs reads the stand-in's log, oldest first.
func (cc *creditsContext) runs() ([]creditsRun, error) {
	raw, err := os.ReadFile(filepath.Join(cc.dir, "runs.log"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []creditsRun
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var run creditsRun
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("the stand-in's log has a line it cannot read: %q", line)
		}
		run.pid, _ = strconv.Atoi(fields[0])
		run.mode, run.key = fields[1], fields[2]
		runs = append(runs, run)
	}
	return runs, nil
}

// coldRuns are the sessions started cold, the ones that answered a grist
// without a warm session.
func (cc *creditsContext) coldRuns() ([]creditsRun, error) {
	runs, err := cc.runs()
	var cold []creditsRun
	for _, run := range runs {
		if run.mode == "cold" {
			cold = append(cold, run)
		}
	}
	return cold, err
}

func (cc *creditsContext) sessionsGivenKey(n int, key string) error {
	runs, err := cc.coldRuns()
	if err != nil {
		return err
	}
	got := 0
	for _, run := range runs {
		if run.key == key {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected %d sessions given the key, got %d of %+v", n, got, runs)
	}
	return nil
}

func (cc *creditsContext) sessionsGivenNone(n int) error {
	runs, err := cc.coldRuns()
	if err != nil {
		return err
	}
	got := 0
	for _, run := range runs {
		if run.key == "-" {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected %d sessions given no ANTHROPIC_API_KEY, got %d of %+v", n, got, runs)
	}
	return nil
}

func (cc *creditsContext) noSessionWithout() error {
	runs, err := cc.coldRuns()
	for _, run := range runs {
		if run.key == "-" {
			return fmt.Errorf("a session was given no key: %+v", runs)
		}
	}
	return err
}

func (cc *creditsContext) noSessionGivenAKey() error {
	runs, err := cc.runs()
	for _, run := range runs {
		if run.key != "-" {
			return fmt.Errorf("a session was given the key %q: %+v", run.key, runs)
		}
	}
	return err
}

func (cc *creditsContext) noArgumentHoldsTheKey() error {
	key, err := os.ReadFile(cc.keyFile)
	if err != nil {
		return err
	}
	secret := strings.TrimSpace(string(key))
	files, _ := filepath.Glob(filepath.Join(cc.dir, "args.*"))
	if len(files) == 0 {
		return fmt.Errorf("the stand-in kept no arguments, so nothing was proved")
	}
	for _, file := range files {
		args, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if strings.Contains(string(args), secret) {
			return fmt.Errorf("the key is in the arguments in %s", file)
		}
	}
	return nil
}

func (cc *creditsContext) recordHoldsOneLine(billing string) error {
	lines, err := cc.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	if len(lines) != 1 || lines[0].Billing != billing {
		return fmt.Errorf("expected one line billed to %q, got %+v", billing, lines)
	}
	return nil
}

func (cc *creditsContext) keyIsNowhere() error {
	raw, err := os.ReadFile(cc.keyFile)
	if err != nil {
		return err
	}
	secret := strings.TrimSpace(string(raw))
	lines, err := cc.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	evs, err := cc.events.Since(context.Background(), 0)
	if err != nil {
		return err
	}
	shown := map[string]any{"record": lines, "events": evs, "report": cc.c.out.String(), "notes": cc.c.report.Notes, "delivered": cc.c.backend.Delivered()}
	for where, v := range shown {
		text, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if strings.Contains(string(text), secret) {
			return fmt.Errorf("the key is in the %s: %s", where, text)
		}
	}
	return nil
}

func (cc *creditsContext) reportSaysModeRefused(mode string) error {
	report := cc.c.out.String()
	if !strings.Contains(report, cc.keyFile) || !strings.Contains(report, mode) || !strings.Contains(report, "not used") {
		return fmt.Errorf("the report does not say the key file %s is not used for its mode %s:\n%s", cc.keyFile, mode, report)
	}
	return nil
}

// waitForRun waits, at most ten seconds, for a warm session of the stand-in
// matching want to be in its log: it is started with the grist's pass and logs
// when its shell begins.
func (cc *creditsContext) waitForRun(want func(creditsRun) bool) (creditsRun, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		runs, err := cc.runs()
		if err != nil {
			return creditsRun{}, err
		}
		for _, run := range runs {
			if run.mode == "warm" && want(run) {
				return run, nil
			}
		}
		if time.Now().After(deadline) {
			return creditsRun{}, fmt.Errorf("no such warm session started: %+v", runs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func (cc *creditsContext) warmWaitsWithKey(key string) error {
	_, err := cc.waitForRun(func(r creditsRun) bool { return r.key == key && alive(r.pid) })
	return err
}

func (cc *creditsContext) warmWaitsWithout() error {
	_, err := cc.waitForRun(func(r creditsRun) bool { return r.key == "-" && alive(r.pid) })
	return err
}

func (cc *creditsContext) noKeyedWarmRuns() error {
	runs, err := cc.runs()
	for _, run := range runs {
		if run.mode == "warm" && run.key != "-" && alive(run.pid) {
			return fmt.Errorf("the warm session %d started with the key is still running", run.pid)
		}
	}
	return err
}

func (cc *creditsContext) keyTriedThenNot(n int) error {
	runs, err := cc.coldRuns()
	if err != nil {
		return err
	}
	if len(runs) != n || runs[0].key == "-" || runs[len(runs)-1].key != "-" {
		return fmt.Errorf("expected %d sessions, the key tried first and the last without it, got %+v", n, runs)
	}
	first, err := os.ReadFile(filepath.Join(cc.dir, fmt.Sprintf("args.%d", runs[0].pid)))
	if err != nil {
		return err
	}
	last, err := os.ReadFile(filepath.Join(cc.dir, fmt.Sprintf("args.%d", runs[len(runs)-1].pid)))
	if err != nil {
		return err
	}
	if !bytes.Equal(first, last) {
		return fmt.Errorf("the second session was not given the same grist as the first:\n%q\n%q", first, last)
	}
	return nil
}

func (cc *creditsContext) countsOnce() error {
	lines, err := cc.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	sender := application.KeyFingerprint(cc.c.phoneKey)
	sent := 0
	for _, line := range lines {
		if line.Sender == sender && line.Time.UTC().Format(time.DateOnly) == gristNow.Format(time.DateOnly) {
			sent++
		}
	}
	if sent != 1 {
		return fmt.Errorf("expected the grist counted once against the day's limit, got %d of %+v", sent, lines)
	}
	return nil
}

func (cc *creditsContext) mark() (application.GristCreditMark, bool, error) {
	return (&grist.CreditState{Dir: cc.state}).Mark(context.Background())
}

func (cc *creditsContext) markWritten() error {
	mark, found, err := cc.mark()
	if err != nil || !found || !mark.SpentAt.Equal(gristNow) {
		return fmt.Errorf("expected the spent mark written at %s, got %v %v %v", gristNow, mark, found, err)
	}
	info, err := os.Stat(filepath.Join(cc.state, grist.CreditsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("expected %s kept 0600, got %v %v", grist.CreditsFile, info, err)
	}
	return nil
}

func (cc *creditsContext) markKept() error {
	mark, found, err := cc.mark()
	if err != nil || !found || !mark.SpentAt.Before(gristNow) {
		return fmt.Errorf("expected the earlier spent mark kept, got %v %v %v", mark, found, err)
	}
	return nil
}

func (cc *creditsContext) markCleared() error {
	if mark, found, err := cc.mark(); err != nil || found {
		return fmt.Errorf("expected no spent mark, got %v %v %v", mark, found, err)
	}
	return nil
}

func (cc *creditsContext) eventLeft(said string) error {
	evs, err := cc.events.Since(context.Background(), 0)
	if err != nil {
		return err
	}
	if len(evs) != 1 {
		return fmt.Errorf("expected one event, got %+v", evs)
	}
	ev := evs[0]
	if ev.Kind != events.KindJob || ev.Actor != application.JobGristCreditsSpent+"@laptop" || ev.Lane != events.LaneNormal || !strings.Contains(ev.Detail, said) {
		return fmt.Errorf("expected a normal-lane grist-credits-spent event saying %q, got %+v", said, ev)
	}
	other := map[string]string{"credits spent": "key refused", "key refused": "credits spent"}[said]
	if strings.Contains(ev.Detail, other) {
		return fmt.Errorf("the event says %q as well: %q", other, ev.Detail)
	}
	return nil
}
