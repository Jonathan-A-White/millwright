package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRigOriginsReadsTheOriginOfEachCheckoutThatHasOne(t *testing.T) {
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

// A host whose git config rewrites urls (url.<base>.insteadOf) must not change
// the scheme mw doctor rig-origins reads: the origin is what the checkout
// stores, not what git would fetch from (mw-gq6.1016).
func TestRigOriginsReadsTheStoredOriginNotOneRewrittenByInsteadOf(t *testing.T) {
	global := filepath.Join(t.TempDir(), "gitconfig")
	rewrite := "[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"
	if err := os.WriteFile(global, []byte(rewrite), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	dir := filepath.Join(t.TempDir(), "argus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", "https://github.com/o/argus.git")
	got, err := RigOrigins{Rigs: map[string]string{"argus": dir}}.Origins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["argus"] != "https://github.com/o/argus.git" {
		t.Fatalf("want the stored https origin, got %v", got)
	}
}
