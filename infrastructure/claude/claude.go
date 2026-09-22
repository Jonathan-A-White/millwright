// Package claude turns a launch into the command line and environment that
// start a Claude Code session. It is the adapter behind application.Harness.
//
// It assembles and nothing else: it starts no session, reaches no network and
// spends no fuel. What it returns is an application.SessionSpec, which a Runner
// starts.
package claude

import (
	"encoding/json"
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

// testsAllowRule turns a rig's own [tests] command line into the Bash allow
// rules a session working that rig may run without being asked (mw-gq6.83):
// the whole line, verbatim, and — for a command of the form `<a> && <b>` —
// each side again on its own, so a Builder refused the whole may still run
// half of it alone. A rig this host names no command for gets none.
//
// The text is never rewritten by a shell of ours: it is split on the literal
// `" && "` a config author wrote, not reparsed. A command holding a newline or
// a `;` could run more than the one command an allow rule is meant to bound,
// so it is refused here rather than quietly turned into a rule that allows
// more than it names.
func testsAllowRule(command string) ([]string, error) {
	if command == "" {
		return nil, nil
	}
	if strings.ContainsAny(command, "\n;") {
		return nil, fmt.Errorf("the tests command %q cannot become a session allow rule: "+
			"a newline or a `;` could run more than the one command it names", command)
	}
	rules := []string{bashAllowRule(command)}
	if parts := strings.Split(command, " && "); len(parts) > 1 {
		for _, part := range parts {
			rules = append(rules, bashAllowRule(part))
		}
	}
	return rules, nil
}

// bashAllowRule is the allow rule for one exact command line.
func bashAllowRule(command string) string { return "Bash(" + command + ")" }

// DenyHookCommand is what Claude Code runs for a PermissionRequest hook of an
// unattended seat, and what it prints is the answer to the prompt: a deny with
// the reason Claude is told. The hooks reference gives the shape — a
// `hookSpecificOutput` with `hookEventName` `PermissionRequest` and a
// `decision` of `behavior` `deny` and a `message` — and says exit code 2 is not
// honored for this event, so the answer has to be the JSON, printed with exit 0.
// It is printf and nothing else: `jq` is not known to be on both hosts, and printf is in
// every /bin/sh. The message holds no single quote, so that the whole JSON
// document sits inside single quotes.
const DenyHookCommand = `printf '%s' '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"Nobody is watching this seat, so a permission it is not already granted is denied rather than asked for. Use what is allowed, or write down what you needed in your handoff."}}}'`

// UnattendedSeatSettings is SessionSettings with the deny hook added: what a
// seat's session is given when nobody is watching it (mw-gq6.77).
//
// A Builder's `--permission-prompts none` does nothing in an interactive
// session (`claude --help`: it says who answers permission prompts "with
// --print"), and `--permission-mode dontAsk` would drop the auto classifier
// altogether, which a seat keeps. The way the permission-modes reference gives
// for an interactive session is a `PermissionRequest` hook, which "can answer
// the prompt the way it answers any other". No matcher is given, and the hooks
// reference says a group with none "activates on every occurrence of the
// event", so no tool is left to ask about. Nothing the classifier or an allow
// rule already decides reaches the hook: it is only ever run for a prompt.
var UnattendedSeatSettings = withDenyHook(SessionSettings)

// sessionSettings is the --settings a story's session is given: SessionSettings,
// plus a rule for tests's own allow rules when tests is not empty (see
// testsAllowRule). A rig with no [tests] line — tests is "" — gets exactly
// SessionSettings, byte for byte, so a host that names no tests for any rig
// sees no change from before this rule existed.
//
// The document is read and written back rather than spliced, for the same
// reason withDenyHook is: so that the rule's own quotes and parentheses are
// escaped by the JSON encoder and not by hand.
func sessionSettings(tests string) (string, error) {
	if tests == "" {
		return SessionSettings, nil
	}
	rules, err := testsAllowRule(tests)
	if err != nil {
		return "", err
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(SessionSettings), &doc); err != nil {
		panic(fmt.Sprintf("SessionSettings is not JSON: %v", err))
	}
	permissions := doc["permissions"].(map[string]any)
	allow := permissions["allow"].([]any)
	for _, rule := range rules {
		allow = append(allow, rule)
	}
	permissions["allow"] = allow

	out, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("the session settings for %q do not encode: %w", tests, err)
	}
	return string(out), nil
}

// withDenyHook is settings with the PermissionRequest hook that runs
// DenyHookCommand added. The document is read and written back rather than
// spliced, so that the command's quotes are escaped by the JSON encoder and not
// by hand. SessionSettings is a constant of this package, so it cannot fail.
func withDenyHook(settings string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(settings), &doc); err != nil {
		panic(fmt.Sprintf("SessionSettings is not JSON: %v", err))
	}
	doc["hooks"] = map[string]any{
		"PermissionRequest": []any{
			map[string]any{"hooks": []any{
				map[string]any{"type": "command", "command": DenyHookCommand},
			}},
		},
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Sprintf("the unattended seat settings do not encode: %v", err))
	}
	return string(out)
}

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
	tests          map[string]string
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

// WithTests names each rig's own tests command, by rig name, as the [tests]
// table of the config file has it (config.Tests). A session working a rig
// this names is given a Bash allow rule for that rig's own command, on top of
// BeadsAllowRule, so a Builder may run its rig's own tests without being
// asked (mw-gq6.83). A rig this does not name gets no such rule.
func WithTests(commands map[string]string) Option {
	return func(h *Harness) { h.tests = commands }
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
	settings, err := sessionSettings(h.tests[l.Path.Rig])
	if err != nil {
		return application.SessionSpec{}, fmt.Errorf("launching %s: %w", l.StoryID, err)
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
		// The session signs nothing it commits, may run bd without being asked,
		// and may run its own rig's tests when h.tests names one. See
		// SessionSettings and sessionSettings.
		"--settings", settings,
		"--name", l.StoryID,
		l.Kickoff,
	}

	// Only the result goes to the file. What Claude Code says on stderr stays
	// in the session, where a person who attaches to it can read it.
	//
	// The redirect writes to a temp path beside the real one and is renamed
	// into place only once the session has fully exited, rather than opening
	// the real path directly: a plain `> l.ResultFile` opens (and truncates)
	// that exact path the moment this line starts, and keeps the same file
	// descriptor for the session's whole run however long that is — so
	// anything else that replaces the path while the session is still going
	// (a git operation elsewhere in the vault touched it, on the incident this
	// guards against, mw-gq6.89) orphans that descriptor, and the session's
	// own completed write lands nowhere anyone can read it. The rename is
	// atomic on the same filesystem, so whatever the real path held meanwhile
	// is replaced whole, never partially, by this session's own result.
	tmp := l.ResultFile + ".tmp"
	line := shellLine(argv) + " > " + shellQuote(tmp) + "; mv " + shellQuote(tmp) + " " + shellQuote(l.ResultFile)

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

// seatSettings is the --settings a seat's session is given.
func seatSettings(attended bool) string {
	if attended {
		return SessionSettings
	}
	return UnattendedSeatSettings
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

// Harness is the adapter behind application.SeatHarness too: a seat's own
// session, which is interactive.
var _ application.SeatHarness = (*Harness)(nil)

// SeatSession implements application.SeatHarness: the window that runs a
// seat's own session.
//
// It is the opposite of Session in nearly every way that matters. The session
// is interactive — no `--print`, no result file, and so no `--permission-prompts
// none`, which does nothing without `--print`; it is primed with the seat's
// charter rather than with a story; and nothing follows it, because a seat's
// session ends when its occupant hands off, not when a story is done.
//
// Nobody is assumed to be watching it. What it shares with Session is
// SessionSettings, and unless the launch is Attended it also has the deny hook
// of UnattendedSeatSettings, which is how an interactive session is kept from
// hanging on a prompt (mw-gq6.77). An attended one is asked, as it always was.
//
// The charter travels as a path, not as text: a charter is pages long, and a
// command line a person can read is worth more than one that carries a
// document through a terminal.
func (h *Harness) SeatSession(l application.SeatLaunch) (application.WindowSpec, error) {
	if err := l.Validate(); err != nil {
		return application.WindowSpec{}, err
	}
	if l.Model != "" && !domain.KnownModel(l.Model) {
		return application.WindowSpec{}, fmt.Errorf("starting the %s seat: %q is not a model the factory runs on", l.Seat, l.Model)
	}
	if l.Effort != "" && !domain.KnownEffort(l.Effort) {
		return application.WindowSpec{}, fmt.Errorf("starting the %s seat: %q is not an effort a session can be asked for", l.Seat, l.Effort)
	}
	if !knownPermissionMode(h.permissionMode) {
		return application.WindowSpec{}, fmt.Errorf("starting the %s seat: %q is not a permission mode Claude Code takes", l.Seat, h.permissionMode)
	}

	argv := []string{h.program}
	if l.Model != "" {
		argv = append(argv, "--model", string(l.Model))
	}
	if l.Effort != "" {
		argv = append(argv, "--effort", string(l.Effort))
	}
	argv = append(argv,
		// Still `auto`: the classifier keeps deciding what it can. The hook only
		// answers what would have been asked of a person.
		"--permission-mode", h.permissionMode,
		"--append-system-prompt-file", l.Charter,
		// The seat signs the work and the model never does, and bd runs without
		// being asked, as in a Builder's session. See SessionSettings.
		"--settings", seatSettings(l.Attended),
		"--name", l.Name,
		l.Kickoff,
	)

	return application.WindowSpec{
		Name: l.Name,
		Dir:  l.Dir,
		Env: map[string]string{
			// Who the session is for mail: the seat itself, never defaulted and
			// never this host's copy of it — a seat's mail is the seat's,
			// wherever the session reading it happens to run.
			application.SeatEnv: l.Seat,
		},
		// No shell reads this: the window runs the program itself, so that a
		// kickoff holding quotes, newlines or a dollar sign reaches the session
		// as it was written.
		Command: argv,
	}, nil
}
