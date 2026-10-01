package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// talkModelHost is the host the talk model scenarios run on.
const talkModelHost = "laptop"

// talkModelTmux is the fake terminal: a tmux that knows only the three things
// mw talk model asks of one. list-windows prints the windows file, capture-pane
// prints the screen file of the window named by -t, and send-keys appends its
// arguments, a call to a line, to the keys file. Anything else fails, so that a
// command the watch should never send is noticed.
const talkModelTmux = `#!/bin/sh
dir=$(dirname "$0")
while [ "$1" = "-L" ]; do shift 2; done
what=$1
shift
case $what in
list-windows) cat "$dir/windows" ;;
capture-pane)
	target=""
	while [ $# -gt 0 ]; do
		[ "$1" = "-t" ] && target=$2
		shift
	done
	cat "$dir/screen-$target"
	;;
send-keys) echo "$what $*" >>"$dir/keys" ;;
*) echo "the fake tmux does not do $what" >&2; exit 1 ;;
esac
`

// talkModelContext holds the vault a scenario's watch reads the Mayor's acting
// file in, the directory of the fake tmux, and the clock the watch keeps. As
// in seat_reap_steps.go, a sleep does not wait: it moves the clock on and lets
// what the scenario scheduled for that look happen.
type talkModelContext struct {
	vault string
	term  string
	now   time.Time

	look   int
	events map[int][]func() error
	// typedOnLook is the look the watch typed on, 0 for none.
	typedOnLook int

	report application.TalkModelReport
	err    error
}

// talkModelTerminal is the real tmux adapter on the fake tmux, noting on which
// look it was typed into.
type talkModelTerminal struct {
	*tmux.Windows
	c *talkModelContext
}

func (t talkModelTerminal) Type(ctx context.Context, window, text string) error {
	if err := t.Windows.Type(ctx, window, text); err != nil {
		return err
	}
	t.c.typedOnLook = t.c.look
	return nil
}

// InitializeTalkModelScenario registers the steps of features/talk_model.feature.
func InitializeTalkModelScenario(ctx *godog.ScenarioContext) {
	c := &talkModelContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = talkModelContext{
			events: map[int][]func() error{},
			now:    time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		for _, dir := range []string{c.vault, c.term} {
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
		}
		return ctx, nil
	})

	ctx.Given(`^a vault where the Mayor acts as "([^"]*)"$`, c.aVaultWhereTheMayorActsAs)
	ctx.Given(`^the Mayor's acting file now says "([^"]*)"$`, c.theActingFileNowSays)
	ctx.Given(`^a fake terminal with the window "([^"]*)" named "([^"]*)"$`, c.aFakeTerminalWithTheWindow)
	ctx.Given(`^the window "([^"]*)" shows an empty input line$`, c.theWindowShowsAnEmptyInputLine)
	ctx.Given(`^the window "([^"]*)" shows the suggestion "([^"]*)"$`, c.theWindowShowsTheSuggestion)
	ctx.Given(`^the window "([^"]*)" shows the draft "([^"]*)"$`, c.theWindowShowsTheDraft)
	ctx.Given(`^the window "([^"]*)" shows a question with no prompt$`, c.theWindowShowsAQuestion)
	ctx.Given(`^the window "([^"]*)" is working$`, c.theWindowIsWorking)
	ctx.Given(`^before look (\d+) the window "([^"]*)" shows an empty input line$`, c.beforeLookAnEmptyInputLine)
	ctx.Given(`^before look (\d+) the window "([^"]*)" shows the draft "([^"]*)"$`, c.beforeLookTheDraft)

	ctx.When(`^mw talk model "([^"]*)" watches, looking every (\d+) seconds for up to (\d+) minutes$`, c.mwTalkModelWatches)

	ctx.Then(`^the keys sent to the terminal are "([^"]*)" and Enter, into "([^"]*)", on look (\d+)$`, c.theKeysSentAre)
	ctx.Then(`^no key was sent to the terminal$`, c.noKeyWasSent)
	ctx.Then(`^mw talk model gave up$`, c.mwTalkModelGaveUp)
	ctx.Then(`^mw talk model is refused saying "([^"]*)"$`, c.mwTalkModelIsRefused)
	ctx.Then(`^the talk log's last line says "([^"]*)"$`, c.theTalkLogsLastLineSays)
	ctx.Then(`^the talk log's line (\d+) says "([^"]*)"$`, c.theTalkLogsLineSays)
}

func (c *talkModelContext) aVaultWhereTheMayorActsAs(acting string) error {
	dir, err := os.MkdirTemp("", "mw-talk-model-vault-")
	if err != nil {
		return fmt.Errorf("making a vault: %w", err)
	}
	c.vault = dir
	return c.theActingFileNowSays(acting)
}

func (c *talkModelContext) theActingFileNowSays(acting string) error {
	return os.WriteFile(filepath.Join(c.vault, application.ActingFileName("mayor")), []byte(acting+"\n"), 0o644)
}

func (c *talkModelContext) aFakeTerminalWithTheWindow(id, name string) error {
	dir, err := os.MkdirTemp("", "mw-talk-model-tmux-")
	if err != nil {
		return fmt.Errorf("making a fake terminal: %w", err)
	}
	c.term = dir
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(talkModelTmux), 0o755); err != nil {
		return err
	}
	// The pane's pid is left empty: nothing dates the window, and no ps runs.
	return os.WriteFile(filepath.Join(dir, "windows"), []byte(id+"||"+name+"\n"), 0o644)
}

// show puts a screen in the window's pane: lines of Claude Code's screen as
// capture-pane -e prints them, between the rules around its input line.
func (c *talkModelContext) show(id string, lines ...string) error {
	const rule = "────────────────────────────────────────"
	screen := "● Done.\n\n" + rule + "\n" + strings.Join(lines, "\n") + "\n" + rule + "\n  ? for shortcuts\n"
	return os.WriteFile(filepath.Join(c.term, "screen-"+id), []byte(screen), 0o644)
}

func (c *talkModelContext) theWindowShowsAnEmptyInputLine(id string) error {
	return c.show(id, "❯ ")
}

func (c *talkModelContext) theWindowShowsTheSuggestion(id, suggestion string) error {
	return c.show(id, "❯ \x1b[2m"+suggestion+"\x1b[0m")
}

func (c *talkModelContext) theWindowShowsTheDraft(id, draft string) error {
	return c.show(id, "❯ "+draft)
}

func (c *talkModelContext) theWindowShowsAQuestion(id string) error {
	return c.show(id, "Do you want to proceed?", " 1. Yes", " 2. No")
}

func (c *talkModelContext) theWindowIsWorking(id string) error {
	return c.show(id, "✻ Thinking… (12s · esc to interrupt)", "", "❯ ")
}

// before schedules what happens just before a look.
func (c *talkModelContext) before(look int, event func() error) {
	c.events[look] = append(c.events[look], event)
}

func (c *talkModelContext) beforeLookAnEmptyInputLine(look int, id string) error {
	c.before(look, func() error { return c.theWindowShowsAnEmptyInputLine(id) })
	return nil
}

func (c *talkModelContext) beforeLookTheDraft(look int, id, draft string) error {
	c.before(look, func() error { return c.theWindowShowsTheDraft(id, draft) })
	return nil
}

// sleep is the watch's sleep: no waiting, the clock moves on by the interval
// and what the scenario scheduled for the look it leads to happens.
func (c *talkModelContext) sleep(_ context.Context, d time.Duration) error {
	c.look++
	c.now = c.now.Add(d)
	for _, event := range c.events[c.look] {
		if err := event(); err != nil {
			return fmt.Errorf("making something happen before look %d: %w", c.look, err)
		}
	}
	return nil
}

func (c *talkModelContext) mwTalkModelWatches(model string, seconds, minutes int) error {
	files := vault.New(c.vault)
	windows := tmux.NewWindows(tmux.WithProgram(filepath.Join(c.term, "tmux")), tmux.WithSocket("mw-talk-model-fake"))
	c.report, c.err = application.TalkModel{
		Seats:    files,
		Terminal: talkModelTerminal{Windows: windows, c: c},
		Log:      files,
		Seat:     "mayor",
		Host:     talkModelHost,
		Model:    domain.Model(model),
		Interval: time.Duration(seconds) * time.Second,
		Limit:    time.Duration(minutes) * time.Minute,
		Now:      func() time.Time { return c.now },
		Sleep:    c.sleep,
	}.Run(context.Background())
	return nil
}

// keys is every send-keys call the fake tmux was given, a line each.
func (c *talkModelContext) keys() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.term, "keys"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), nil
}

func (c *talkModelContext) theKeysSentAre(text, id string, look int) error {
	keys, err := c.keys()
	if err != nil {
		return err
	}
	want := []string{"send-keys -t " + id + " -l -- " + text, "send-keys -t " + id + " Enter"}
	if strings.Join(keys, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("expected the keys %q, the terminal was sent %q (%v, %q)", want, keys, c.err, c.report.Said)
	}
	if c.typedOnLook != look {
		return fmt.Errorf("expected the keys to be sent on look %d, they were sent on look %d", look, c.typedOnLook)
	}
	if c.err != nil || c.report.Outcome != application.TalkModelTyped {
		return fmt.Errorf("expected the watch to end typed, it ended %q with %v", c.report.Outcome, c.err)
	}
	return nil
}

func (c *talkModelContext) noKeyWasSent() error {
	keys, err := c.keys()
	if err != nil {
		return err
	}
	if len(keys) != 0 {
		return fmt.Errorf("expected no key sent to the terminal, it was sent %q", keys)
	}
	return nil
}

func (c *talkModelContext) mwTalkModelGaveUp() error {
	if c.report.Outcome != application.TalkModelGaveUp || c.err == nil {
		return fmt.Errorf("expected mw talk model to give up, it ended with %q and %v", c.report.Outcome, c.err)
	}
	return nil
}

func (c *talkModelContext) mwTalkModelIsRefused(want string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected mw talk model to be refused saying %q, it ended with %v", want, c.err)
	}
	if c.look != 0 {
		return fmt.Errorf("expected nothing watched, the watch made %d looks", c.look)
	}
	return nil
}

// logLines is what the Mayor's talk log holds, a string a line.
func (c *talkModelContext) logLines() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.vault, application.TalkLogFileName("mayor")))
	if err != nil {
		return nil, fmt.Errorf("reading the talk log: %w", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), nil
}

// talkLogLine checks one line of the log: dated, named for the watch, then
// saying what it did.
func talkLogLine(line, want string) error {
	stamp, rest, ok := strings.Cut(line, " talk model ")
	if _, err := time.Parse(time.RFC3339, stamp); !ok || err != nil {
		return fmt.Errorf("expected a dated talk model line, got %q", line)
	}
	if _, said, _ := strings.Cut(rest, ": "); said != want {
		return fmt.Errorf("expected the talk log to say %q, it says %q", want, line)
	}
	return nil
}

func (c *talkModelContext) theTalkLogsLastLineSays(want string) error {
	lines, err := c.logLines()
	if err != nil {
		return err
	}
	return talkLogLine(lines[len(lines)-1], want)
}

func (c *talkModelContext) theTalkLogsLineSays(n int, want string) error {
	lines, err := c.logLines()
	if err != nil {
		return err
	}
	if n < 1 || n > len(lines) {
		return fmt.Errorf("expected the talk log to have a line %d, it holds %q", n, lines)
	}
	return talkLogLine(lines[n-1], want)
}
