package application

import (
	"context"
	"sort"
	"time"
)

// CommandRun is one run of mw as the host keeps it: the command's path (mw
// version, never its arguments, which can hold private text), the exit status
// and how long it took.
type CommandRun struct {
	At         time.Time `json:"at"`
	Command    string    `json:"command"`
	Status     int       `json:"status"`
	DurationMs int64     `json:"duration_ms"`
}

// CommandLog is this host's record of mw's own runs. The real adapter is
// infrastructure/commandlog, a file under ~/.local/state/mw; it is host state
// and never the vault.
type CommandLog interface {
	// AppendCommandRun writes one line.
	AppendCommandRun(ctx context.Context, run CommandRun) error
	// CommandRunsSince reads every run made at or after the time.
	CommandRunsSince(ctx context.Context, since time.Time) ([]CommandRun, error)
}

// CommandUseDays is how far back a command counts as run.
const CommandUseDays = 30

// CommandUse is which of mw's commands this host has not run lately.
type CommandUse struct {
	Days int `json:"days"`
	// Total is how many commands mw has; Runs how many runs the log holds for
	// the days.
	Total int `json:"total"`
	Runs  int `json:"runs"`
	// NotRun are the commands with no run in the days, in order.
	NotRun []string `json:"not_run"`
	// Unread, when set, is why the log could not be read.
	Unread string `json:"unread,omitempty"`
}

// ReadCommandUse works out which of the commands have no run in the last
// CommandUseDays days before now. A log that cannot be read is Unread, not an
// error: the rest of the report stands.
func ReadCommandUse(ctx context.Context, log CommandLog, commands []string, now time.Time) CommandUse {
	use := CommandUse{Days: CommandUseDays, Total: len(commands)}
	runs, err := log.CommandRunsSince(ctx, now.AddDate(0, 0, -CommandUseDays))
	if err != nil {
		use.Unread = err.Error()
		return use
	}
	seen := map[string]bool{}
	for _, run := range runs {
		seen[run.Command] = true
	}
	use.Runs = len(runs)
	use.NotRun = []string{}
	for _, name := range commands {
		if !seen[name] {
			use.NotRun = append(use.NotRun, name)
		}
	}
	sort.Strings(use.NotRun)
	return use
}
