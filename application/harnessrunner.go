package application

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// The wait a session's start makes for claude to answer when HarnessReadyRunner
// says nothing of it: 24 asks, 5 seconds apart, two minutes in all, longer than
// the install of a version of Claude Code takes to replace the binary.
const (
	DefaultHarnessTries = 24
	DefaultHarnessPoll  = 5 * time.Second
)

// HarnessReadyRunner is a Runner that starts a session of claude only once
// claude answers on this host (mw-gq6.1281). The home's cloud check replaces a
// box's Claude Code while the box's own dispatch ticks, and mw-gq6.1247, started
// in between, died with "nice: 'claude': No such file or directory". Ready is the
// host's `claude --version`; a Start whose command does not run claude, or a
// Runner with no Ready, starts at once. Ready is asked up to Tries times, Poll
// apart (zero are the defaults), waiting with Sleep (nil is a real wait). A claude
// that never answers is a failed Start, which gives the story's claim back with
// no attempt counted, because the session never ran. Every other method is the
// wrapped Runner's.
type HarnessReadyRunner struct {
	Runner
	Ready func(ctx context.Context) error
	Tries int
	Poll  time.Duration
	Sleep func(ctx context.Context, d time.Duration) error
	Host  string
	Out   io.Writer
}

// runsClaude says whether a session's command starts claude: a command of the
// session's own whose program is claude, as the story's shell line has it
// (`nice -n 10 claude --print ...`), or a bare argv. A word
// that merely says claude, in a path, a file's name or the kickoff's text, does
// not count (mw-gq6.1329): a Codex session's line is full of them.
func runsClaude(spec SessionSpec) bool {
	argv := spec.Command
	if len(argv) >= 3 && isShell(argv[0]) && argv[1] == "-c" {
		return programIsClaude(shellCommands(argv[2]))
	}
	return programIsClaude([][]string{argv})
}

func isShell(program string) bool {
	switch path.Base(program) {
	case "sh", "bash", "dash", "zsh":
		return true
	}
	return false
}

// programIsClaude says whether any of the commands, each a list of words, runs
// claude once the words that only wrap a program are set aside.
func programIsClaude(commands [][]string) bool {
	for _, words := range commands {
		for len(words) > 0 {
			word := words[0]
			switch path.Base(word) {
			case "nice", "timeout", "exec", "env", "command":
				words = skipWrapper(path.Base(word), words[1:])
				continue
			}
			if path.Base(word) == "claude" {
				return true
			}
			break
		}
	}
	return false
}

// skipWrapper drops what a wrapping program takes before the program it runs:
// nice's -n and its number, timeout's duration, env's assignments.
func skipWrapper(wrapper string, words []string) []string {
	for len(words) > 0 {
		word := words[0]
		switch {
		case wrapper == "nice" && word == "-n" && len(words) > 1:
			words = words[2:]
		case strings.HasPrefix(word, "-"):
			words = words[1:]
		case wrapper == "timeout":
			// The duration, 90m, is the one word before the program.
			return words[1:]
		case wrapper == "env" && strings.Contains(word, "="):
			words = words[1:]
		default:
			return words
		}
	}
	return words
}

// shellCommands splits a shell line into its simple commands, each a list of
// words: quotes make one word of what they hold, and ; & | ( ) and a newline end
// a command. Words such as if, then, do and else begin nothing of their own, so
// they are dropped from the front of a command; a test's [ ... ] is a command of
// its own whose program is [.
func shellCommands(line string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		for len(words) > 0 && isShellKeyword(words[0]) {
			words = words[1:]
		}
		if len(words) > 0 {
			commands = append(commands, words)
		}
		words = nil
	}
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\'':
			inWord = true
			for i++; i < len(runes) && runes[i] != '\''; i++ {
				word.WriteRune(runes[i])
			}
		case r == '"':
			inWord = true
			for i++; i < len(runes) && runes[i] != '"'; i++ {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
				}
				word.WriteRune(runes[i])
			}
		case r == '\\' && i+1 < len(runes):
			inWord = true
			i++
			word.WriteRune(runes[i])
		case r == ' ' || r == '\t':
			endWord()
		case r == ';' || r == '&' || r == '|' || r == '(' || r == ')' || r == '\n':
			endCommand()
		default:
			inWord = true
			word.WriteRune(r)
		}
	}
	endCommand()
	return commands
}

func isShellKeyword(word string) bool {
	switch word {
	case "if", "then", "else", "elif", "fi", "do", "done", "while", "until", "{", "}", "!":
		return true
	}
	return false
}

// Start implements Runner: it waits for claude to answer, then starts the session.
func (r HarnessReadyRunner) Start(ctx context.Context, spec SessionSpec) error {
	if err := r.awaitClaude(ctx, spec); err != nil {
		return err
	}
	return r.Runner.Start(ctx, spec)
}

func (r HarnessReadyRunner) awaitClaude(ctx context.Context, spec SessionSpec) error {
	if r.Ready == nil || !runsClaude(spec) {
		return nil
	}
	tries, poll, wait := r.Tries, r.Poll, r.Sleep
	if tries < 1 {
		tries = DefaultHarnessTries
	}
	if poll <= 0 {
		poll = DefaultHarnessPoll
	}
	if wait == nil {
		wait = sleepFor
	}
	var err error
	for try := 1; try <= tries; try++ {
		if err = r.Ready(ctx); err == nil {
			return nil
		}
		if try == tries {
			break
		}
		if try == 1 && r.Out != nil {
			fmt.Fprintf(r.Out, "  claude does not answer on %s (%s): waiting for it, up to %d asks %s apart\n", r.Host, oneLine(err.Error()), tries, poll)
		}
		if werr := wait(ctx, poll); werr != nil {
			return werr
		}
	}
	return fmt.Errorf("claude --version did not answer on %s in %d asks %s apart: %w", r.Host, tries, poll, err)
}
