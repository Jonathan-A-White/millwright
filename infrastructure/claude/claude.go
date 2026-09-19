// Package claude turns a launch into the command line and environment that
// start a Claude Code session. It is the adapter behind application.Harness.
//
// It assembles and nothing else: it starts no session, reaches no network and
// spends no fuel. What it returns is an application.SessionSpec, which a Runner
// starts.
package claude

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Program is the command this adapter builds a line for, and Shell is what
// reads that line. The line is read by a shell because the session's result is
// a redirection: Claude Code prints its result JSON and has no flag for writing
// it to a file.
const (
	Program = "claude"
	Shell   = "/bin/sh"
)

// The permission modes Claude Code takes (`claude --permission-mode`).
const (
	PermissionAcceptEdits = "acceptEdits"
	PermissionAuto        = "auto"
	PermissionBypass      = "bypassPermissions"
	PermissionManual      = "manual"
	PermissionDontAsk     = "dontAsk"
	PermissionPlan        = "plan"
)

// BeadsAllowRule lets a session run the bd program without being asked. Its
// work is tracked in beads, and in `auto` mode the permission classifier judges
// each command on its own: it has refused a session's `bd close` as a write to
// an external system, at random and on both hosts, and with
// `--permission-prompts none` a refusal is final — the story's steps stay open
// and nothing lands. An allow rule is resolved before the classifier is asked
// ("narrow Bash and PowerShell allow rules such as `Bash(npm test)` stay in
// effect in auto mode. Claude Code resolves them before the classifier runs" —
// the auto mode configuration reference); only broad rules like `Bash(*)` and
// wildcarded interpreters are suspended there, and this is neither.
//
// It is the whole program rather than a list of subcommands, by the Governor's
// decision (mw-gq6.43): it lives in the rig, the same on both hosts, and
// nothing has to be added to anybody's own Claude settings. The rule is the
// program, a space and a trailing `*`, which is the form the permission
// reference and Claude Code's own dialog write (`Bash(bd:*)` is documented as
// an equivalent way to write the same trailing wildcard). The space is part of
// the rule: `Bash(bd*)` would also match `bdwhatever`.
//
// A rule matches each subcommand of a compound command separately, so a line
// that chains bd with anything else — `bd show x | head; cat CONTEXT.md` — is
// not allowed by this rule alone; the rest still goes to the classifier. That
// is why the Builder's memory of this rig says to run bd on its own.
const BeadsAllowRule = `Bash(bd *)`

// SessionSettings is the Claude Code settings every session this factory starts
// is given. It says two things, in one JSON document because `--settings` takes
// one.
//
// The session signs nothing: no trailer on a commit, no line in a pull request
// description, no session link. Claude Code's default is to sign both —
// `Co-Authored-By: <the model> <noreply@anthropic.com>` on a commit and
// `Generated with ...` in a pull request — and this factory's commits carry
// neither: a seat outlives every session that occupies it, so the seat signs
// the work and the model never does. Setting each part to the empty string is
// how the settings reference says to hide it (`attribution.commit`,
// `attribution.pr`, `attribution.sessionUrl`); the older
// `includeCoAuthoredBy: false` is deprecated and is ignored the moment
// `attribution` is set, so it is not sent.
//
// The session may run bd without being asked: see BeadsAllowRule for why that
// is here and why it is the whole program.
//
// It travels as a JSON string on the command line, which `claude --settings`
// takes as readily as a path, so that no settings file is ever written into a
// rig's worktree — where a session could commit it by accident, and where
// somebody would have to remember to take it away again.
const SessionSettings = `{"attribution":{"commit":"","pr":"","sessionUrl":false},"permissions":{"allow":["` + BeadsAllowRule + `"]}}`

// DefaultPermissionMode is how an unattended session is allowed to act. `auto`
// is the least permissive mode a Builder can actually work in: a Builder must
// edit files, build, test and commit inside its worktree with nobody watching.
// `manual` and `plan` stop at the first prompt, `acceptEdits` takes the edits
// but stops at the first command, and `dontAsk` allows only what the settings
// already list, so anything unforeseen is denied silently. `auto` lets the
// classifier allow the ordinary work and still refuse what is dangerous, which
// `bypassPermissions` would not. Every refusal is reported in the session's
// result JSON, so a session that was stopped says so.
const DefaultPermissionMode = PermissionAuto

// Harness assembles Claude Code sessions.
type Harness struct {
	program        string
	shell          string
	permissionMode string
}

// Harness satisfies the port.
var _ application.Harness = (*Harness)(nil)

// Option is a setting of a Harness, given to New.
type Option func(*Harness)

// WithProgram names the Claude Code command to run, for a host that keeps it
// somewhere unusual — and for tests, which put a harmless command in its place.
func WithProgram(name string) Option {
	return func(h *Harness) { h.program = name }
}

// WithShell names the shell that reads the session's command line.
func WithShell(path string) Option {
	return func(h *Harness) { h.shell = path }
}

// WithPermissionMode sets how much the session may do without being asked.
// See DefaultPermissionMode for why the default is what it is.
func WithPermissionMode(mode string) Option {
	return func(h *Harness) { h.permissionMode = mode }
}

// New returns a Harness that runs Claude Code as this host has it, unless an
// option says otherwise.
func New(opts ...Option) *Harness {
	h := &Harness{program: Program, shell: Shell, permissionMode: DefaultPermissionMode}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Name implements application.Harness.
func (h *Harness) Name() domain.Harness { return domain.HarnessClaude }

// Session implements application.Harness: the session that works one launch.
//
// The session is a headless run — `--print` with `--output-format json` — so
// that what it cost and how it ended can be read back afterwards, and it is
// primed with the boot file rather than with a prompt on the command line, so
// that no part of a story's text has to survive a shell.
func (h *Harness) Session(l application.Launch) (application.SessionSpec, error) {
	if err := l.Validate(); err != nil {
		return application.SessionSpec{}, err
	}
	if l.Path.Harness != domain.HarnessClaude {
		return application.SessionSpec{}, fmt.Errorf("launching %s: this harness is %s, not %s", l.StoryID, domain.HarnessClaude, l.Path.Harness)
	}
	if !knownPermissionMode(h.permissionMode) {
		return application.SessionSpec{}, fmt.Errorf("launching %s: %q is not a permission mode Claude Code takes", l.StoryID, h.permissionMode)
	}

	argv := []string{
		h.program,
		"--print",
		"--output-format", "json",
		"--model", string(l.Path.Model),
		"--effort", string(l.Path.Effort),
		"--permission-mode", h.permissionMode,
		// Nobody is at the keyboard: anything that would still ask is denied
		// rather than left waiting forever in a pane nobody is watching.
		"--permission-prompts", "none",
		"--append-system-prompt-file", l.BootFile,
		// The session signs nothing it commits, and may run bd without being
		// asked. See SessionSettings.
		"--settings", SessionSettings,
		"--name", l.StoryID,
		l.Kickoff,
	}

	// Only the result goes to the file. What Claude Code says on stderr stays
	// in the session, where a person who attaches to it can read it.
	line := shellLine(argv) + " > " + shellQuote(l.ResultFile)

	// Whatever comes after the session is chained with `;`, not `&&`: a session
	// that failed, ran out of fuel or died is exactly the one whose closing out
	// must still happen, and `&&` would skip it.
	if len(l.After) > 0 {
		line += "; " + shellLine(l.After)
	}

	identity := application.SeatIdentity(l.Seat, l.Host)
	return application.SessionSpec{
		Name: application.SessionName(l.StoryID),
		Dir:  l.Dir,
		Env: map[string]string{
			// Who beads records as the actor, and who the seat is for mail.
			"BEADS_ACTOR": identity,
			"MW_SEAT":     identity,
			"MW_STORY":    l.StoryID,
		},
		Command: []string{h.shell, "-c", line},
	}, nil
}

// knownPermissionMode reports whether Claude Code takes this mode.
func knownPermissionMode(mode string) bool {
	switch mode {
	case PermissionAcceptEdits, PermissionAuto, PermissionBypass,
		PermissionManual, PermissionDontAsk, PermissionPlan:
		return true
	}
	return false
}

// shellLine is an argv written as one line for a shell to read back as the
// same argv.
func shellLine(argv []string) string {
	words := make([]string, 0, len(argv))
	for _, word := range argv {
		words = append(words, shellQuote(word))
	}
	return strings.Join(words, " ")
}

// shellQuote is one word of a shell line. A word a shell would read back
// unchanged is left alone, so that a person attached to the session can read
// what is running; anything else is wrapped in single quotes, inside which
// every character is literal — a quote, a colon, a newline, a dollar sign, a
// backslash — with the one exception of a single quote itself, which is closed,
// escaped and reopened.
func shellQuote(word string) string {
	if shellSafe(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// shellSafe reports whether a word means itself to a shell as it is written.
// The list is deliberately short: everything not on it gets quoted.
func shellSafe(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=@,+%", r):
		default:
			return false
		}
	}
	return true
}
