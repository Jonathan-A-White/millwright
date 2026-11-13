package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// aKitWithCommits makes a kit checkout with n commits and returns its
// directory and the sha of its first commit.
func aKitWithCommits(t *testing.T, parent, name string, n int) (dir, first string) {
	t.Helper()
	dir = filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	for i := 1; i <= n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte(fmt.Sprint(i)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "f")
		gitIn(t, dir, "commit", "-q", "-m", fmt.Sprintf("commit %d", i))
		if i == 1 {
			first = gitIn(t, dir, "rev-parse", "HEAD")
		}
	}
	return dir, first
}

// aRigPinning makes a rig checkout whose package.json pins each kit at a sha.
func aRigPinning(t *testing.T, parent, name string, pins map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var deps []string
	for kit, sha := range pins {
		deps = append(deps, fmt.Sprintf("    %q: \"github:Jonathan-A-White/%s#%s\"", kit, kit, sha))
	}
	text := "{\n  \"dependencies\": {\n" + strings.Join(deps, ",\n") + "\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestKitLagNamesEachRigAndKitThatIsBehind(t *testing.T) {
	parent := t.TempDir()
	_, voiceFirst := aKitWithCommits(t, parent, "voice-kit", 4)
	_, pwaFirst := aKitWithCommits(t, parent, "pwa-kit", 3)
	rigs := map[string]string{
		// three behind voice-kit, none behind pwa-kit
		"postern": aRigPinning(t, parent, "postern", map[string]string{"voice-kit": voiceFirst, "pwa-kit": gitIn(t, filepath.Join(parent, "pwa-kit"), "rev-parse", "HEAD")}),
		// two behind pwa-kit
		"lampas": aRigPinning(t, parent, "lampas", map[string]string{"pwa-kit": pwaFirst}),
		// pins nothing
		"argus": filepath.Join(parent, "argus"),
	}
	check := KitLag{Rigs: rigs, KitDir: func(rig, kit string) string { return filepath.Join(parent, kit) }}

	lines, err := check.Lag(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lampas: pwa-kit 2 behind", "postern: voice-kit 3 behind"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("want %v, got %v", want, lines)
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "postern: voice-kit 3 behind") || !strings.Contains(reason, "lampas: pwa-kit 2 behind") {
		t.Fatalf("want a warning naming both, got %s (%s)", verdict, reason)
	}
}

func TestKitLagIsOkWhenEveryPinIsAtTheHead(t *testing.T) {
	parent := t.TempDir()
	aKitWithCommits(t, parent, "voice-kit", 2)
	head := gitIn(t, filepath.Join(parent, "voice-kit"), "rev-parse", "HEAD")
	rigs := map[string]string{"postern": aRigPinning(t, parent, "postern", map[string]string{"voice-kit": head})}
	check := KitLag{Rigs: rigs, KitDir: func(rig, kit string) string { return filepath.Join(parent, kit) }}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("want ok, got %s (%s)", verdict, reason)
	}
}

// A kit with no checkout on this host, or a pin the checkout does not hold, is
// left out: there is no lag to count.
func TestKitLagLeavesOutAKitItCannotCount(t *testing.T) {
	parent := t.TempDir()
	aKitWithCommits(t, parent, "voice-kit", 2)
	rigs := map[string]string{"postern": aRigPinning(t, parent, "postern", map[string]string{
		"voice-kit": "0123456789012345678901234567890123456789",
		"bsv-kit":   "0123456789012345678901234567890123456789",
	})}
	check := KitLag{Rigs: rigs, KitDir: func(rig, kit string) string { return filepath.Join(parent, kit) }}
	lines, err := check.Lag(context.Background())
	if err != nil || len(lines) != 0 {
		t.Fatalf("want no lines, got %v (%v)", lines, err)
	}
}
