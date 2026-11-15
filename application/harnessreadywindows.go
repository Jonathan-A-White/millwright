package application

import (
	"context"
	"io"
	"strings"
	"time"
)

// HarnessReadyWindows is a Windows that opens a seat's window of claude only
// once claude answers on this host (mw-gq6.1328): the seat's session starts in
// a window, not through the Runner, so HarnessReadyRunner's wait (mw-gq6.1281)
// did not reach it, and a seat started while the home's cloud check replaced
// Claude Code died as a story's did. The asks are HarnessReadyRunner's: Ready,
// Tries, Poll, Sleep, Host and Out mean the same here. A window whose command
// does not run claude, or a Windows with no Ready, opens at once. Every other
// method is the wrapped Windows'.
type HarnessReadyWindows struct {
	Windows
	Ready func(ctx context.Context) error
	Tries int
	Poll  time.Duration
	Sleep func(ctx context.Context, d time.Duration) error
	Host  string
	Out   io.Writer
}

// Open implements Windows: it waits for claude to answer, then opens the window.
func (w HarnessReadyWindows) Open(ctx context.Context, spec WindowSpec) error {
	if w.Ready != nil && windowRunsClaude(spec.Command) {
		wait := HarnessReadyRunner{Ready: w.Ready, Tries: w.Tries, Poll: w.Poll, Sleep: w.Sleep, Host: w.Host, Out: w.Out}
		if err := wait.waitForClaude(ctx); err != nil {
			return err
		}
	}
	return w.Windows.Open(ctx, spec)
}

// envScriptArgs is how many words of a window's argv come before the program
// when the host's env file is read first: the shell, -c, the script, "sh" and
// the script's three arguments (the claude harness's withEnvFile).
const envScriptArgs = 7

// windowRunsClaude says whether a window's argv runs claude, bare or behind the
// one-line script that reads the host's env file and then runs `exec "$@"`.
func windowRunsClaude(argv []string) bool {
	if len(argv) > envScriptArgs && isShell(argv[0]) && argv[1] == "-c" && strings.HasSuffix(argv[2], `exec "$@"`) {
		return programIsClaude([][]string{argv[envScriptArgs:]})
	}
	return runsClaude(SessionSpec{Command: argv})
}
