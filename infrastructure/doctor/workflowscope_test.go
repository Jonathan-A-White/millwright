package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// aGh is a gh that prints out, says errOut on stderr and leaves with status.
func aGh(t *testing.T, out, errOut string, status int) doctor.GhScopes {
	t.Helper()
	program := filepath.Join(t.TempDir(), "gh")
	script := "#!/bin/sh\ncat <<'MW_OUT'\n" + out + "\nMW_OUT\ncat >&2 <<'MW_ERR'\n" + errOut + "\nMW_ERR\nexit " + string(rune('0'+status)) + "\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for gh: %v", err)
	}
	return doctor.GhScopes{Program: program}
}

func TestGhScopesAreReadFromTheHeaderGhShows(t *testing.T) {
	for _, tc := range []struct {
		name, out, errOut string
		status            int
		want              application.WorkflowScope
		said              string
	}{
		{"classic token with the scope", "HTTP/2.0 200 OK\nX-Oauth-Scopes: gist, read:org, repo, workflow\n\n{}", "", 0, application.WorkflowScopeHas, "workflow"},
		{"classic token without it", "HTTP/2.0 200 OK\nX-Oauth-Scopes: gist, read:org, repo\n\n{}", "", 0, application.WorkflowScopeLacks, "not workflow"},
		{"a token listing no scope", "HTTP/2.0 200 OK\nX-Oauth-Scopes: \n\n{}", "", 0, application.WorkflowScopeLacks, "no scopes"},
		{"a fine-grained token", "HTTP/2.0 200 OK\nContent-Type: application/json\n\n{}", "", 0, application.WorkflowScopeUnknown, "fine-grained"},
		{"gh not logged in", "", "To get started with GitHub CLI, please run:  gh auth login", 4, application.WorkflowScopeLacks, "not logged in"},
		{"the network down", "", "error connecting to api.github.com", 1, application.WorkflowScopeUnknown, "could not be asked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := aGh(t, tc.out, tc.errOut, tc.status).ReadWorkflowScope(context.Background())
			if err != nil || got.Scope != tc.want || !strings.Contains(got.Said, tc.said) {
				t.Fatalf("expected %s saying %q, got %+v (%v)", tc.want, tc.said, got, err)
			}
		})
	}
}

func TestAHostWithNoGhCannotBeToldAndIsNotRefused(t *testing.T) {
	got, err := doctor.GhScopes{Program: filepath.Join(t.TempDir(), "no-gh")}.ReadWorkflowScope(context.Background())
	if err != nil || got.Scope != application.WorkflowScopeUnknown {
		t.Fatalf("expected unknown, got %+v (%v)", got, err)
	}
}

type aReading application.WorkflowScopeReading

func (a aReading) ReadWorkflowScope(context.Context) (application.WorkflowScopeReading, error) {
	return application.WorkflowScopeReading(a), nil
}

func TestTheWorkflowScopeCheckSaysPerHostWhetherItsCredentialHasTheScope(t *testing.T) {
	for _, tc := range []struct {
		scope   application.WorkflowScope
		verdict application.Verdict
		words   string
	}{
		{application.WorkflowScopeHas, application.DoctorOK, "cloud1's push credential has the workflow scope"},
		{application.WorkflowScopeLacks, application.DoctorOK, "cloud1's push credential lacks the workflow scope"},
		{application.WorkflowScopeUnknown, application.DoctorCannotTell, "cloud1's push credential cannot be read"},
	} {
		verdict, reason := doctor.NewWorkflowScope(aReading{Scope: tc.scope, Said: "gh shows scopes"}, "cloud1").Probe(context.Background())
		if verdict != tc.verdict || !strings.Contains(reason, tc.words) {
			t.Errorf("%s: expected %v saying %q, got %v: %q", tc.scope, tc.verdict, tc.words, verdict, reason)
		}
	}
}
