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
