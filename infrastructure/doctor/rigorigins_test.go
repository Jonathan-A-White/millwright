package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestRigOriginsReadsTheOriginOfEachCheckoutThatHasOne(t *testing.T) {
	isolateGitConfig(t, "")
	parent := t.TempDir()
	rigs := map[string]string{}
	for rig, url := range map[string]string{"argus": "https://github.com/o/argus.git", "leaderboard": "git@github.com:o/leaderboard.git", "bare": ""} {
		dir := filepath.Join(parent, rig)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q", "-b", "main")
		if url != "" {
			gitIn(t, dir, "remote", "add", "origin", url)
		}
		rigs[rig] = dir
	}
	rigs["missing"] = filepath.Join(parent, "nowhere")
	got, err := RigOrigins{Rigs: rigs}.Origins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["argus"] != "https://github.com/o/argus.git" || got["leaderboard"] != "git@github.com:o/leaderboard.git" {
		t.Fatalf("want the two origins, got %v", got)
	}
}

// isolateGitConfig gives the test a git config of its own: a global file with
// the given text and no system file, so the host's own insteadOf rewrites
// cannot change what the test reads (mw-gq6.1017).
func isolateGitConfig(t *testing.T, text string) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func aRigStoring(t *testing.T, name, url string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", url)
	return dir
}

const sshToHTTPS = "[url \"https://github.com/\"]\n\tinsteadOf = git@github.com:\n"

// A host whose git config rewrites a stored ssh url to https (url.<base>.insteadOf)
// reaches that origin over https, so the origin is the https one git fetches
// from, and the stored url is reported beside it (mw-gq6.1017).
func TestRigOriginsReadsTheEffectiveOriginWhenInsteadOfRewritesTheStoredOne(t *testing.T) {
	isolateGitConfig(t, sshToHTTPS)
	rigs := map[string]string{
		"lampas": aRigStoring(t, "lampas", "git@github.com:o/lampas.git"),
		"argus":  aRigStoring(t, "argus", "https://github.com/o/argus.git"),
	}
	check := RigOrigins{Rigs: rigs}
	got, err := check.Origins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["lampas"] != "https://github.com/o/lampas.git" || got["argus"] != "https://github.com/o/argus.git" {
		t.Fatalf("want the effective https origins, got %v", got)
	}
	verdict, reason := (&application.RigOriginsCheck{Origins: check.Origins, Rewritten: check.Rewritten}).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("a stored ssh url rewritten to https is ok, got %v: %s", verdict, reason)
	}
	rewritten, err := check.Rewritten(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rewritten) != 1 || rewritten["lampas"] != "git@github.com:o/lampas.git" {
		t.Fatalf("want only lampas's stored url, got %v", rewritten)
	}
}

// With no rewrite, a stored ssh url among https ones is the real fault.
func TestRigOriginsFlagsAStoredSSHOriginWithNoRewrite(t *testing.T) {
	isolateGitConfig(t, "")
	rigs := map[string]string{
		"lampas": aRigStoring(t, "lampas", "git@github.com:o/lampas.git"),
		"argus":  aRigStoring(t, "argus", "https://github.com/o/argus.git"),
		"legend": aRigStoring(t, "legend", "https://github.com/o/legend.git"),
	}
	check := RigOrigins{Rigs: rigs}
	verdict, reason := (&application.RigOriginsCheck{Origins: check.Origins, Rewritten: check.Rewritten}).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "lampas") || strings.Contains(reason, "argus") {
		t.Fatalf("a stored ssh url with no rewrite is flagged, got %v: %s", verdict, reason)
	}
	if strings.Contains(reason, "stored") {
		t.Fatalf("the stored url is named only when it differs from the effective one: %s", reason)
	}
}
