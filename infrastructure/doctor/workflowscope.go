package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck         = (*WorkflowScope)(nil)
	_ application.WorkflowScopeReader = GhScopes{}
)

// WorkflowScopeName is what the check is called: in the log, and on the command
// line as `mw doctor workflow-scope`.
const WorkflowScopeName = "workflow-scope"

// ghScopesWait is how long `gh api` is given to answer.
const ghScopesWait = 30 * time.Second

// GhScopes reads the scopes of the GitHub credential a host pushes with, from
// the X-OAuth-Scopes header `gh api -i user` shows, the way the factory's hosts
// are logged in (gh is also their git credential helper). A token that lists
// no scopes (a fine-grained one) and a host with no gh, or no network, are
// WorkflowScopeUnknown, never a reason to refuse a landing; a host where gh is
// not logged in has no credential to carry the scope, and lacks it (mw-gq6.999).
type GhScopes struct {
	// Program is gh; empty is "gh" on the PATH.
	Program string
}

// ReadWorkflowScope implements application.WorkflowScopeReader.
func (g GhScopes) ReadWorkflowScope(ctx context.Context) (application.WorkflowScopeReading, error) {
	program := g.Program
	if program == "" {
		program = "gh"
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return application.WorkflowScopeReading{Scope: application.WorkflowScopeUnknown, Said: program + " is not installed here"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ghScopesWait)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "api", "-i", "user")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	return readGhScopes(out.String(), errOut.String(), runErr), nil
}

// readGhScopes is what `gh api -i user` said, read: its output, what it said on
// stderr, and how it ended.
func readGhScopes(out, errOut string, runErr error) application.WorkflowScopeReading {
	for _, line := range strings.Split(out, "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(name), "x-oauth-scopes") {
			continue
		}
		var scopes []string
		for _, scope := range strings.Split(value, ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
		listed := "no scopes"
		if len(scopes) > 0 {
			listed = "scopes " + strings.Join(scopes, ", ")
		}
		for _, scope := range scopes {
			if scope == "workflow" {
				return application.WorkflowScopeReading{Scope: application.WorkflowScopeHas, Said: "gh shows " + listed}
			}
		}
		return application.WorkflowScopeReading{Scope: application.WorkflowScopeLacks, Said: "gh shows " + listed + ", not workflow"}
	}
	said := strings.ToLower(errOut + "\n" + out)
	if runErr != nil && (strings.Contains(said, "gh auth login") || strings.Contains(said, "not logged in") ||
		strings.Contains(said, "authentication required") || strings.Contains(said, "http 401")) {
		return application.WorkflowScopeReading{Scope: application.WorkflowScopeLacks, Said: "gh is not logged in here"}
	}
	if runErr != nil {
		why := firstNonEmptyLine(errOut)
		if why == "" {
			why = runErr.Error()
		}
		return application.WorkflowScopeReading{Scope: application.WorkflowScopeUnknown, Said: "gh could not be asked: " + why}
	}
	return application.WorkflowScopeReading{Scope: application.WorkflowScopeUnknown, Said: "gh lists no scopes for this token (a fine-grained one?)"}
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// WorkflowScope is the check that says, for this host, whether its push
// credential may write workflow files (mw-gq6.999): a story whose branch
// changes a file under .github/workflows/ is not landed from a host that
// cannot. Every host reports its own, in its own run of mw doctor. It never
// finds a fault, because the scope is a person's to give (the Governor's:
// keys): it says ok with the reading, or cannot-tell when gh cannot be read.
type WorkflowScope struct {
	Reader application.WorkflowScopeReader
	Host   string
}

// NewWorkflowScope is the check over reader, for host.
func NewWorkflowScope(reader application.WorkflowScopeReader, host string) *WorkflowScope {
	return &WorkflowScope{Reader: reader, Host: host}
}

// Name implements application.DoctorCheck.
func (w *WorkflowScope) Name() string { return WorkflowScopeName }

// Probe implements application.DoctorCheck.
func (w *WorkflowScope) Probe(ctx context.Context) (application.Verdict, string) {
	reading, err := w.Reader.ReadWorkflowScope(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	switch reading.Scope {
	case application.WorkflowScopeHas:
		return application.DoctorOK, fmt.Sprintf("%s's push credential has the workflow scope (%s)", w.Host, reading.Said)
	case application.WorkflowScopeLacks:
		return application.DoctorOK, fmt.Sprintf("%s's push credential lacks the workflow scope (%s): a story that changes %s is not landed here, "+
			"and goes to the home", w.Host, reading.Said, application.WorkflowsDir)
	}
	return application.DoctorCannotTell, fmt.Sprintf("%s's push credential cannot be read for the workflow scope (%s)", w.Host, reading.Said)
}

// Cure implements application.DoctorCheck: there is nothing to cure.
func (w *WorkflowScope) Cure(context.Context) error {
	return errors.New("the workflow scope is a person's to give the credential; mw doctor has no cure for it")
}

// Damper implements application.DoctorCheck.
func (w *WorkflowScope) Damper() (time.Duration, int) { return 24 * time.Hour, 1 }

// WayBack implements application.DoctorCheck.
func (w *WorkflowScope) WayBack() string { return "none needed: the check changes nothing" }
