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
	"os"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/sessionshell"
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
	envFile        string
	claudeEnvFile  string
	serverHost     string
	nice           int
	jobs           int
	timing         map[string][]string
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

// WithEnvFile names a file of shell assignments, such as ~/.config/mw/beads.env
// on a host whose beads database is served, for a session to read before
// anything else it runs (mw-43v9x.23). The command line holds the file's path
// and never anything in it; a file that is missing or unreadable changes
// nothing. An empty path does nothing.
func WithEnvFile(path string) Option {
	return func(h *Harness) { h.envFile = path }
}

// WithClaudeEnvFile names a file of shell assignments, such as
// ~/.config/mw/claude.env holding CLAUDE_CODE_OAUTH_TOKEN, that every Claude
// Code session mw starts, a story's and a seat's, reads before the program runs,
// so a host with a long-lived token keeps working after its login expires. A
// session under the shared tmux server gets none of dispatch's environment and
// mw passes only a few names with -e, so the file is read by the session's own
// shell; the command line holds the file's path and never anything in it. It is
// read when the launch is assembled only if it is there then: a file that is
// missing, or an empty path, leaves the launch as it was.
func WithClaudeEnvFile(path string) Option {
	return func(h *Harness) { h.claudeEnvFile = path }
}

// claudeEnv is the claude env file's path when the file exists, else "".
func (h *Harness) claudeEnv() string {
	if h.claudeEnvFile == "" {
		return ""
	}
	if info, err := os.Stat(h.claudeEnvFile); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return h.claudeEnvFile
}

// WithBeadsServerHost names the host a session's bd reaches the beads database
// server on: the session exports BEADS_DOLT_SERVER_HOST as this after reading
// the env file, so the file's own host — right when it was written, wrong once
// the home has moved — does not decide it (mw-j3iis.2). The host is a value,
// not a secret, so it may be on the command line; nothing else from the file
// ever is. An empty host does nothing, and the file's value stands.
func WithBeadsServerHost(host string) Option {
	return func(h *Harness) { h.serverHost = host }
}

// WithNice starts the harness under `nice -n <n>`, so a Builder's session and
// the tests it runs yield the CPU to the mill's own work on the same host
// (mw-gq6.312). 0, the default, starts it as it was. Only the harness is
// niced: the heartbeat and the close-out are light.
func WithNice(n int) Option {
	return func(h *Harness) { h.nice = n }
}

// WithJobs gives the session JOBS, GOMAXPROCS and VITEST_MAX_WORKERS as n
// (application.WorkerLimitEnv), so the tests a Builder runs start no more
// workers than a host that is also someone's PC can spare (mw-gq6.399). 0, the
// default, adds nothing.
func WithJobs(n int) Option {
	return func(h *Harness) { h.jobs = n }
}

// WithTimingTests gives a session working a rig MW_TIMING=skip and the rig's
// timing_tests globs as MW_TIMING_TESTS (application.TimingEnv), by rig name,
// so the tests a Builder runs by hand leave out what the gate leaves out
// (mw-eo6dj9.2). A rig this does not name gets nothing.
func WithTimingTests(timing map[string][]string) Option {
	return func(h *Harness) { h.timing = timing }
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
	// A close-out alone runs After in the environment it runs in after a
	// harness, and nothing else (mw-gq6.467).
	if l.CloseOutOnly {
		return h.spec(l, sessionshell.AfterLine(l)), nil
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
		"--model", sessionModel(l.Path.Model),
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
	}
	if l.MaxTurns > 0 {
		argv = append(argv, "--max-turns", strconv.Itoa(l.MaxTurns))
	}
	argv = append(argv, l.Kickoff)

	if h.nice > 0 {
		argv = append([]string{"nice", "-n", strconv.Itoa(h.nice)}, argv...)
	}

	// Only the result goes to the file, written beside and moved into place once
	// the session has exited; the bd shim, the heartbeat and the close-out are
	// the same for every harness (sessionshell).
	line := sessionshell.ResultRedirect(argv, l.ResultFile)
	line = sessionshell.WithBdShim(line, l.BdRetry)
	line = sessionshell.WithHeartbeat(line, l)
	line = sessionshell.WithAfter(line, l)
	return h.spec(l, line), nil
}

// spec is the session that runs line for the launch: the host's beads
// environment read ahead of it, and the seat's identity and the story in its
// environment.
func (h *Harness) spec(l application.Launch, line string) application.SessionSpec {
	// The host's beads environment is read first of all, ahead of the
	// heartbeat and the harness (sessionshell.Prelude).
	line = sessionshell.Prelude(line, sessionshell.Env{File: h.envFile, HarnessFile: h.claudeEnv(), ServerHost: h.serverHost})

	identity := application.SeatIdentity(l.Seat, l.Host)
	env := map[string]string{
		// Who beads records as the actor, and who the seat is for mail.
		"BEADS_ACTOR": identity,
		"MW_SEAT":     identity,
		"MW_STORY":    l.StoryID,
		// A story's session is headless: it ends the moment its turn does,
		// so a command backgrounded with it (the test suite, most often)
		// and never waited on is lost the instant the turn ends, taking
		// whatever was never committed with it (mw-gq6.90; it happened
		// three times: mw-gq6.39, twice on mw-gq6.86). This is the
		// harness's own way to take the option away rather than trust it
		// is never used. A seat's own session (SeatSession) is not given
		// this: its mail watcher and waiters run in the background by
		// design.
		"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS": "1",
	}
	// The host's cap on test workers, when it says one (mw-gq6.399).
	for name, value := range application.WorkerLimitEnv(h.jobs) {
		env[name] = value
	}
	// The rig's timing tests, left out as the gate leaves them out.
	for name, value := range application.TimingEnv(application.TimingSkip, h.timing[l.Path.Rig]) {
		env[name] = value
	}
	return application.SessionSpec{
		Name:    application.SessionName(l.StoryID),
		Dir:     l.Dir,
		Env:     env,
		Command: []string{h.shell, "-c", line},
	}
}

// seatSettings is the --settings a seat's session is given.
func seatSettings(attended bool) string {
	if attended {
		return SessionSettings
	}
	return UnattendedSeatSettings
}

// sessionModel is what a story's session is started on: the model's full id
// when the factory knows it, else the name. The alias is resolved by the Claude
// Code on the host, and the desktop's resolved haiku to claude-haiku-4-5, which
// does not run in auto mode: a Haiku Tester there ran in permission mode default
// and every command outside its allow rules was denied (mw-gq6.545). The id
// leaves nothing to the host's version.
func sessionModel(m domain.Model) string {
	if id := domain.FullModelID(m); id != "" {
		return id
	}
	return string(m)
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

// shellLine and shellQuote are sessionshell's, under the names this package
// has always used.
func shellLine(argv []string) string { return sessionshell.ShellLine(argv) }

func shellQuote(word string) string { return sessionshell.ShellQuote(word) }

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
	if l.Model != "" && !domain.KnownModel(domain.HarnessClaude, l.Model) {
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
		// No shell reads the kickoff: the window runs the program itself, so
		// that a kickoff holding quotes, newlines or a dollar sign reaches the
		// session as it was written.
		Command: h.withEnvFile(argv),
	}, nil
}

// withEnvFile puts the host's beads environment in front of a seat's argv
// without a shell ever reading the argv: a fixed one-line script reads the
// file, and the claude env file, if readable, exports the server host when one was given, then
// replaces itself with "$@". The file's path, the host and the program are
// arguments of it, so the kickoff still arrives untouched. Neither a file nor
// a host configured leaves the argv as it was.
func (h *Harness) withEnvFile(argv []string) []string {
	claudeEnv := h.claudeEnv()
	if h.envFile == "" && h.serverHost == "" && claudeEnv == "" {
		return argv
	}
	const script = `f=$1; h=$2; c=$3; shift 3; if [ -r "$f" ]; then set -a; . "$f"; set +a; fi; ` +
		`if [ -n "$c" ] && [ -r "$c" ]; then set -a; . "$c"; set +a; fi; ` +
		`if [ -n "$h" ]; then export BEADS_DOLT_SERVER_HOST="$h"; fi; exec "$@"`
	return append([]string{h.shell, "-c", script, "sh", h.envFile, h.serverHost, claudeEnv}, argv...)
}
