package codex

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// AgentsFile is the file at the vault's root that Codex reads for the standing
// instructions of the session it starts there, and so how a seat's charter
// reaches it: Codex has no system-prompt flag.
const AgentsFile = "AGENTS.md"

// Harness is the adapter behind application.SeatHarness too: a seat's own
// session, which is interactive.
var _ application.SeatHarness = (*Harness)(nil)

// SeatSession implements application.SeatHarness: the window that runs a
// seat's own session.
//
// It is an interactive `codex`, with no `exec`, no event stream and no result
// file, and nothing follows it. The sandbox is the Builder's (workspace-write,
// the network on, since bd and git need it). Nobody is assumed to be watching,
// so nothing asks (`-a never`: a command the sandbox blocks fails); an
// Attended launch is asked, `-a on-request`.
//
// The charter does not travel on the command line. Codex reads AGENTS.md at
// the root of the directory it starts in, and `mw member init --harness codex`
// lays one in the vault from the kit's CLAUDE.md, which sends the session to
// seats/mayor/charter.md. That is prompt context, not a system prompt, so the
// kit's AGENTS.md carries the standing rules. The launch's Charter is therefore
// not used here.
//
// Like the Claude adapter's, the command is the program and its arguments with
// no shell reading the kickoff, so a kickoff holding quotes, newlines or a
// dollar sign reaches the session as it was written.
func (h *Harness) SeatSession(l application.SeatLaunch) (application.WindowSpec, error) {
	if err := l.Validate(); err != nil {
		return application.WindowSpec{}, err
	}
	if l.Model != "" && !domain.KnownModel(domain.HarnessCodex, l.Model) {
		return application.WindowSpec{}, fmt.Errorf("starting the %s seat: %q is not a model Codex runs on here (a Codex model is named %s...)", l.Seat, l.Model, domain.CodexModelPrefix)
	}
	if l.Effort != "" && !domain.KnownEffort(l.Effort) {
		return application.WindowSpec{}, fmt.Errorf("starting the %s seat: %q is not an effort a session can be asked for", l.Seat, l.Effort)
	}

	argv := []string{h.program}
	if l.Model != "" {
		argv = append(argv, "-m", string(l.Model))
	}
	if l.Effort != "" {
		argv = append(argv, "-c", "model_reasoning_effort="+string(l.Effort))
	}
	approval := "never"
	if l.Attended {
		approval = "on-request"
	}
	argv = append(argv,
		"--sandbox", "workspace-write",
		"-c", "sandbox_workspace_write.network_access=true",
		"-a", approval,
		l.Kickoff,
	)

	return application.WindowSpec{
		Name: l.Name,
		Dir:  l.Dir,
		Env: map[string]string{
			// Who the session is for mail: the seat itself, never defaulted and
			// never this host's copy of it.
			application.SeatEnv: l.Seat,
		},
		Command: h.withEnvFile(argv),
	}, nil
}

// withEnvFile puts the host's beads environment in front of a seat's argv
// without a shell ever reading the argv: a fixed one-line script reads the env
// file, if readable, exports the server host when one was given, then replaces
// itself with "$@". The file's path and the host are arguments of it, so the
// kickoff still arrives untouched. Neither configured leaves the argv as it was.
func (h *Harness) withEnvFile(argv []string) []string {
	if h.envFile == "" && h.serverHost == "" {
		return argv
	}
	const script = `f=$1; h=$2; shift 2; if [ -r "$f" ]; then set -a; . "$f"; set +a; fi; ` +
		`if [ -n "$h" ]; then export BEADS_DOLT_SERVER_HOST="$h"; fi; exec "$@"`
	return append([]string{h.shell, "-c", script, "sh", h.envFile, h.serverHost}, argv...)
}
