package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// WorkflowsDir is where GitHub reads a repository's workflows. A push that
// creates or updates a file under it is refused unless the credential it is
// made with carries the workflow scope (mw-gq6.999).
const WorkflowsDir = ".github/workflows/"

// ErrPushWorkflowScope is a push the remote refused because it creates or
// updates a workflow file and the credential it was made with lacks the
// workflow scope. Trying again, here, never changes it: another host, whose
// credential has the scope, can make it.
var ErrPushWorkflowScope = errors.New("the push was refused: this host's credential lacks the workflow scope")

// WorkflowScopeRefused reports whether err is a push refused for the workflow
// scope.
func WorkflowScopeRefused(err error) bool { return errors.Is(err, ErrPushWorkflowScope) }

// ReasonWorkflowScope is a close-out that did not land a branch changing
// workflow files because this host's push credential may not write them
// (mw-gq6.999): either read before the landing (WorkflowScopeReader) or from
// the refusal of the push itself. The attempt is named tooling and the story
// is given back for the home host, whose credential has the scope.
const ReasonWorkflowScope Reason = "workflow-scope"

// WorkflowScope is what a host's push credential is known to hold of the
// workflow scope.
type WorkflowScope string

const (
	// WorkflowScopeHas is a credential that lists the workflow scope.
	WorkflowScopeHas WorkflowScope = "has"
	// WorkflowScopeLacks is a credential that is known not to carry it: its
	// scopes are listed and the workflow scope is not among them, or there is
	// no login to carry any.
	WorkflowScopeLacks WorkflowScope = "lacks"
	// WorkflowScopeUnknown is a credential whose scopes cannot be read here (no
	// gh, a fine-grained token that lists none, the network down). It is never a
	// reason to refuse a landing: the push itself says.
	WorkflowScopeUnknown WorkflowScope = "unknown"
)

// WorkflowScopeReading is one look at this host's push credential.
type WorkflowScopeReading struct {
	Scope WorkflowScope
	// Said is the reading in a few plain words: the scopes listed, or why they
	// could not be read.
	Said string
}

// WorkflowScopeReader reads this host's push credential for the workflow scope.
// The adapter is the one place that runs gh and reads its words.
type WorkflowScopeReader interface {
	ReadWorkflowScope(ctx context.Context) (WorkflowScopeReading, error)
}

// WorkflowFiles are the files of changed that are workflow files, in order.
func WorkflowFiles(changed []string) []string {
	var found []string
	for _, file := range changed {
		if strings.HasPrefix(strings.TrimPrefix(file, "./"), WorkflowsDir) {
			found = append(found, file)
		}
	}
	return found
}

// workflowScopeStop is the stop of a close-out whose host's credential may not
// write the workflow files its branch changes, in words: err is what git said
// when it was the push that was refused, and is kept for whoever unwraps it.
type workflowScopeStop struct {
	why string
	err error
}

func (s workflowScopeStop) Error() string { return s.why }
func (s workflowScopeStop) Unwrap() error { return s.err }

// workflowScopeWhy says in words why branch cannot be landed from host, with up
// to three of the workflow files it changes.
func workflowScopeWhy(host, branch string, files []string, how string) string {
	shown := files
	if len(shown) > 3 {
		shown = append(append([]string(nil), files[:3]...), fmt.Sprintf("and %d more", len(files)-3))
	}
	return fmt.Sprintf("%s changes workflow files (%s) and the GitHub credential %s pushes with lacks the workflow scope, so %s cannot land it",
		branch, strings.Join(shown, ", "), host, host) + how
}

// workflowScopeGuard is the look before a landing at a branch that changes a
// workflow file: this host's push credential is read, and when it is known to
// lack the workflow scope the story is stopped at once, before anything is
// merged or pushed, rather than after the gate and a refusal (mw-gq6.999). A
// branch that changes no workflow file asks nothing; a credential that cannot
// be read does not stop a landing, the push is the net.
func (n Next) workflowScopeGuard(ctx context.Context, c *closeOut, report *NextReport) (NextReport, bool, error) {
	if n.PushScope == nil || n.Changes == nil {
		return NextReport{}, false, nil
	}
	changed, err := n.Changes.ChangedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to look for workflow files: %v", c.id, err))
		return NextReport{}, false, nil
	}
	files := WorkflowFiles(changed)
	if len(files) == 0 {
		return NextReport{}, false, nil
	}
	reading, err := n.PushScope.ReadWorkflowScope(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the push credential on %s could not be read for the workflow scope: %v", n.Host, err))
		return NextReport{}, false, nil
	}
	if reading.Scope != WorkflowScopeLacks {
		return NextReport{}, false, nil
	}
	how := ""
	if reading.Said != "" {
		how = " (" + reading.Said + ")"
	}
	rep, err := n.stop(ctx, c, report, ReasonWorkflowScope, workflowScopeWhy(n.Host, c.branch, files, how), "")
	return rep, true, err
}

// workflowScopeHandOver is what a close-out that stopped for the workflow scope
// does about the story before it is commented on and given back (mw-gq6.999):
// the attempt is named tooling (mw cause), as a close-out that died is, so that
// it is not counted against the story's attempts, and the story is re-pathed to
// the home host, whose credential has the scope, so that the next dispatch tick
// does not hand it to this host again. It returns the sentence that says so. A
// part that cannot be written is said in it; the stop stands. repathed is
// whether the story now names a host that can take it: only then is its claim
// given back, else the next tick would hand it to a host that lacks the scope
// again.
func (n Next) workflowScopeHandOver(ctx context.Context, c *closeOut, report *NextReport, why string) (said string, repathed bool) {
	named := n.nameToolingCause(ctx, c, report, why)

	home := ""
	if n.Home != nil {
		if record, err := WhereIsHome(ctx, n.Home); err == nil {
			home = record.Host
		}
	}
	switch home {
	case "":
		return fmt.Sprintf("%s The home could not be told, so no host is named for it: run mw path set %s host=<a host whose credential has the workflow scope>.", named, c.id), false
	case n.Host:
		return fmt.Sprintf("%s No other host can take it: %s is the home and the credential it pushes with lacks the workflow scope. "+
			"Give it the scope (the Governor's: keys), then run mw retry %s.", named, home, c.id), false
	}
	if err := writeRetrying(ctx, n, report, "the host of "+c.id, func() error {
		// host_why keeps the re-path of a full or gone host from undoing it
		// (mw-gq6.1165): only the home's credential has the workflow scope.
		return n.Tracker.SetStoryMetadata(ctx, c.id, map[string]string{"host": home, "host_why": "the credential that has the workflow scope is the home's, " + home})
	}); err != nil {
		return fmt.Sprintf("%s It could not be re-pathed to host=%s: %v; run mw path set %s host=%s.", named, home, err, c.id, home), false
	}
	return fmt.Sprintf("%s The story is re-pathed to host=%s, the home, whose credential has the workflow scope, and is given back for it to take.", named, home), true
}

// nameToolingCause names the close-out's attempt tooling (mw cause), so that it
// is not the story's own and is not counted against its attempts, and returns
// the sentence that says so, or says it could not be done. The stop that follows
// stands either way.
func (n Next) nameToolingCause(ctx context.Context, c *closeOut, report *NextReport, why string) string {
	causes := map[int]string{}
	for at, class := range c.detail.AttemptCauses {
		causes[at] = class
	}
	causes[c.attempt()] = CauseTooling
	fields := map[string]string{
		CauseField:         CauseTooling,
		CauseWhyField:      firstLine(why),
		AttemptCausesField: attemptCausesText(causes),
	}
	if err := writeRetrying(ctx, n, report, "the cause of "+c.id, func() error { return n.Tracker.SetStoryMetadata(ctx, c.id, fields) }); err != nil {
		return fmt.Sprintf("Attempt %d could not be named %s (mw cause): %v.", c.attempt(), CauseTooling, err)
	}
	return fmt.Sprintf("Attempt %d is named %s (mw cause), not the story's own: it is not counted against its attempts.", c.attempt(), CauseTooling)
}
