package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// posternSteps is postern's after-landing command as the hosts name it, read
// as its steps: a build, a copy of the site aside, the deploy, the shots and
// their publishing.
var posternSteps = []string{
	"npm ci --no-audit --no-fund",
	"npm run build",
	"ssh -o BatchMode=yes root@allmymind.org 'rsync -a --delete /var/www/postern/ /var/www/postern.prev/'",
	"rsync -az --delete -e 'ssh -o BatchMode=yes' dist/ root@allmymind.org:/var/www/postern/",
	"npm run shots",
	"npm run shots:publish -- latest",
}

// posternRan is a run of that command that ended in its step-th step, the
// output of every step before it marked as the adapter marks it.
func posternRan(step, status int, stepOutput string) application.Ran {
	var out strings.Builder
	for i := 1; i <= step; i++ {
		out.WriteString(application.AfterLandingStepMark(i, len(posternSteps), posternSteps[i-1]) + "\n")
		if i < step {
			out.WriteString("done\n")
		}
	}
	out.WriteString(stepOutput)
	return application.Ran{Command: strings.Join(posternSteps, " && "), Steps: posternSteps, Step: step, Status: status, Output: out.String()}
}

func TestAShotsFailureAfterAGoodDeployReadsAsDeployedWithTheShotsNotPublished(t *testing.T) {
	for _, tc := range []struct {
		name string
		ran  application.Ran
		want string
	}{
		{
			"a failing shot",
			posternRan(5, 1, "== shots at 390x844 ==\n  ✘  12 [shots] › tests/e2e/ideas-only-banner.spec.ts:13:1 › the Ideas only banner sits on the channel (20.1s)\n  1 failed\n\n== shots at 1280x800 ==\n  1 skipped\n  67 passed (2.0m)\n"),
			"deployed; shots not published: exit status 1, failing: ✘ 12 [shots] › tests/e2e/ideas-only-banner.spec.ts:13:1 › the Ideas only banner sits on the channel",
		},
		{
			"the port already used",
			posternRan(5, 1, "== shots at 1280x800 ==\nError: http://localhost:5370 is already used, make sure that nothing is running on the port/url or set reuseExistingServer:true in config.webServer.\n"),
			"deployed; shots not published: exit status 1, the shots' server found its port already used: http://localhost:5370 is already used",
		},
		{
			"the publishing",
			posternRan(6, 1, "rsync: connection unexpectedly closed\nerror: failed to push some refs\n"),
			"deployed; shots not published: exit status 1: rsync: connection unexpectedly closed / error: failed to push some refs",
		},
	} {
		line := tc.ran.Line()
		if !strings.Contains(line, tc.want) {
			t.Errorf("%s: expected the line to say %q, got %q", tc.name, tc.want, line)
		}
		if !tc.ran.Deployed() {
			t.Errorf("%s: expected the run to count as deployed", tc.name)
		}
	}

	stopped := posternRan(5, -1, "== shots at 390x844 ==\n")
	stopped.TimedOut = 5 * time.Minute
	if line := stopped.Line(); !strings.Contains(line, "deployed; shots not published: stopped after 5m0s, still running") {
		t.Errorf("expected a shots run stopped at the limit to say so after the deploy, got %q", line)
	}
}

func TestAFailureBeforeTheDeployIsNotCalledDeployed(t *testing.T) {
	ran := posternRan(2, 2, "vite: build failed\n")
	if ran.Deployed() {
		t.Error("expected a build that failed before the deploy not to count as deployed")
	}
	line := ran.Line()
	if strings.Contains(line, "deployed") || !strings.Contains(line, "step 2 of 6, npm run build: exit status 2: vite: build failed") {
		t.Errorf("expected the line to name the step that failed and its own output, got %q", line)
	}
}

func TestAOneStepCommandReadsAsItAlwaysDid(t *testing.T) {
	ran := application.Ran{Command: "make build", Steps: []string{"make build"}, Step: 1, Status: 2, Output: "one\ntwo\nthree\nfour\n"}
	if got, want := ran.Line(), "after landing: make build: exit status 2: two / three / four"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
