package contrib_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// boostRigsRig is a home with a config.toml of the given [cloud] lines, a [rigs]
// table of checkouts that each have the origin said, and a [tests] line for each
// rig in gated. It returns the environment to run the wrapper in
// (mw-gq6.1566).
func boostRigsRig(t *testing.T, cloudLines string, origins map[string]string, gated []string) []string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git to make the rigs' checkouts with")
	}
	env, _ := boostAddressRig(t, "wg_endpoint = \"203.0.113.9:51820\"\nregion = \"ewr\"\nbeads_host = \"10.88.0.2\"\n", boostWg0)
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	rigs, tests := "", ""
	for _, name := range []string{"millwright", "lampas", "scratch", "sermon-index", "bare"} {
		dir := filepath.Join(home, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
		if url := origins[name]; url != "" {
			if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", url).CombinedOutput(); err != nil {
				t.Fatalf("git remote add: %v: %s", err, out)
			}
		}
		rigs += name + " = \"" + dir + "\"\n"
	}
	for _, name := range gated {
		tests += name + " = \"npm test\"\n"
	}
	config := "vault = \"x\"\n\n" + cloudLines + "\n[rigs]\n" + rigs + "\n[tests]\n" + tests
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

// rigsLine is the dry run's listing of the rigs the box clones, or "".
func rigsLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "    would: clone on the box (BOOST_RIGS): "); ok {
			return rest
		}
	}
	return ""
}

var boostRigOrigins = map[string]string{
	"millwright":   "https://github.com/Jonathan-A-White/millwright.git",
	"lampas":       "https://github.com/Jonathan-A-White/lampas.git",
	"scratch":      "https://github.com/Jonathan-A-White/scratch.git",
	"sermon-index": "git@github.com:Jonathan-A-White/sermon-index.git",
}

// TestVultrBoostUpClonesEveryRigWithAGateOnTheBox: with no [cloud] rigs, a new
// box is told to clone each rig of the home's [rigs] that has a [tests] gate and
// an origin, millwright (the box's own checkout) and a rig with no gate or no
// origin left out.
func TestVultrBoostUpClonesEveryRigWithAGateOnTheBox(t *testing.T) {
	env := boostRigsRig(t, "[cloud]\nmax_boxes = 2\n", boostRigOrigins, []string{"millwright", "lampas", "sermon-index", "bare"})
	out, errOut, err := runBoost(t, env, "up", "--dry-run", "--name", "cloud3")
	if err != nil {
		t.Fatalf("up --dry-run: %v: %s", err, errOut)
	}
	want := "lampas=https://github.com/Jonathan-A-White/lampas.git sermon-index=git@github.com:Jonathan-A-White/sermon-index.git"
	if got := rigsLine(out); got != want {
		t.Errorf("the rigs the box clones = %q, want %q\n%s", got, want, out)
	}
}

// TestVultrBoostUpClonesTheRigsCloudNames: a [cloud] rigs list is the rigs, each
// a bare name (its origin is read) or a name=url.
func TestVultrBoostUpClonesTheRigsCloudNames(t *testing.T) {
	env := boostRigsRig(t, "[cloud]\nrigs = \"scratch sermon-index=https://github.com/o/si.git\"\n", boostRigOrigins, []string{"lampas"})
	out, errOut, err := runBoost(t, env, "up", "--dry-run", "--name", "cloud3")
	if err != nil {
		t.Fatalf("up --dry-run: %v: %s", err, errOut)
	}
	want := "scratch=https://github.com/Jonathan-A-White/scratch.git sermon-index=https://github.com/o/si.git"
	if got := rigsLine(out); got != want {
		t.Errorf("the rigs the box clones = %q, want %q\n%s", got, want, out)
	}
}

// TestVultrBoostUpPrefersCloudRigsToTheTfvars: a [cloud] rigs list wins over a
// rigs line of the box's settings.
func TestVultrBoostUpPrefersCloudRigsToTheTfvars(t *testing.T) {
	env := boostRigsRig(t, "[cloud]\nrigs = \"scratch\"\n", boostRigOrigins, []string{"lampas"})
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "MW_VAULT="); ok {
			f := filepath.Join(v, "hosts", "vultr", "vultr-boost.tfvars")
			if err := os.WriteFile(f, []byte(mustRead(t, f)+"rigs = \"old=https://github.com/o/old.git\"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	out, _, err := runBoost(t, env, "up", "--dry-run", "--name", "cloud3")
	if err != nil {
		t.Fatalf("up --dry-run: %v", err)
	}
	if got, want := rigsLine(out), "scratch=https://github.com/Jonathan-A-White/scratch.git"; got != want {
		t.Errorf("the rigs the box clones = %q, want %q", got, want)
	}
}
