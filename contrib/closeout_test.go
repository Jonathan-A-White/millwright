package contrib_test

// These tests drive contrib/mw-heavy and contrib/mw-closeout as a detached
// close-out runs them: in a user unit that systemd starts with none of the
// caller's environment (mw-gq6.446). On 2026-10-15 12:23Z a unit made by hand
// (systemd-run --user, ExecStart contrib/mw-heavy mw next mw-ev7oyy.14
// --no-dispatch, PATH only) never read ~/.config/mw/beads.env, so bd opened a
// fresh embedded database in the vault and mw next said 'no issues found
// matching mw-ev7oyy.14'.
//
// The world is a temporary directory: a home holding beads.env, a vault, and
// stand-ins for systemd-run, mw and bd on a PATH of curated real programs, so
// a real systemd-run is never reached. The stand-in systemd-run is honest
// about the one thing that matters here: a service unit (not a --scope) starts
// with only the user manager's own environment, what --setenv names and what
// an EnvironmentFile holds. The stand-in bd is honest the way the real one
// was: without BEADS_DOLT_SERVER_HOST it makes .beads/embeddeddolt where it
// runs and finds nothing.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// closeoutStory is the fixture story the stand-in bd holds.
const closeoutStory = "mw-fix.14"

// The made-up values beads.env holds here.
const (
	closeoutServerHost = "10.99.0.2"
	closeoutPassword   = "made-up-password"
)

// systemdRunStandIn logs its call to calls beside its own directory, then starts the command
// after "--": a --scope in the caller's own environment, as systemd does; a
// service, at once and to its end, with only the manager's PATH (or what
// --setenv says), HOME, the --setenv names and its EnvironmentFile, in the
// home directory unless --same-dir or --working-directory says otherwise, its
// output where StandardOutput says.
const systemdRunStandIn = `#!/bin/sh
calls=$(dirname "$0")/../calls
echo "systemd-run $*" >>"$calls"
scope=""; dir=$HOME; envfile=""; out=""; sets=""
while [ "$#" -gt 0 ]; do
	case $1 in
	--) shift; break ;;
	--scope) scope=1 ;;
	--same-dir) dir=$PWD ;;
	--working-directory=*) dir=${1#--working-directory=} ;;
	--setenv=*) sets="$sets
${1#--setenv=}" ;;
	-p)
		shift
		case $1 in
		EnvironmentFile=*) envfile=${1#EnvironmentFile=}; envfile=${envfile#-} ;;
		StandardOutput=append:* | StandardOutput=file:*) out=${1#StandardOutput=*:} ;;
		esac
		;;
	esac
	shift
done
[ -n "$scope" ] && exec "$@"
[ -n "$out" ] || out=/dev/null
# The unit's own environment, one NAME=VALUE a line, read back below.
unitenv="PATH=$FAKE_MANAGER_PATH
HOME=$HOME$sets"
(
	for v in $(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p'); do
		case $v in PATH|HOME) ;; *) unset "$v" 2>/dev/null ;; esac
	done
	while IFS= read -r line; do
		[ -n "$line" ] && export "$line"
	done <<EOF
$unitenv
EOF
	if [ -n "$envfile" ] && [ -r "$envfile" ]; then set -a; . "$envfile"; set +a; fi
	cd "$dir" || exit 1
	exec "$@"
) >>"$out" 2>&1
echo "unit ended $?" >>"$calls"
`

// mwStandIn is mw next as far as this story goes: it says what beads
// environment it was started with, then reads its story from bd, as the real
// close-out does first.
const mwStandIn = `#!/bin/sh
env | grep '^BEADS_DOLT_' | sort
[ "$1" = next ] || { echo "mw stand-in: only next" >&2; exit 2; }
bd show "$2" --json || exit 1
echo "closed out $2"
`

// bdStandIn holds one story and answers bd show for it, but only with the
// server's host in its environment: without it, like the real bd, it makes a
// second, empty database in .beads/embeddeddolt and finds nothing.
const bdStandIn = `#!/bin/sh
[ "$1" = show ] || { echo "bd stand-in: only show" >&2; exit 2; }
if [ -z "${BEADS_DOLT_SERVER_HOST:-}" ]; then
	mkdir -p .beads/embeddeddolt/sf
	echo "Error: no issues found matching $2" >&2
	exit 1
fi
[ "$2" = "` + closeoutStory + `" ] || { echo "Error: no issues found matching $2" >&2; exit 1; }
echo '[{"id":"` + closeoutStory + `","status":"in_progress"}]'
`

type closeoutWorld struct {
	t     *testing.T
	dir   string
	home  string
	vault string
	path  string // the stand-ins, then the curated real programs
	real  string // the curated real programs alone: the user manager's PATH
}

func newCloseoutWorld(t *testing.T) *closeoutWorld {
	t.Helper()
	w := &closeoutWorld{t: t, dir: t.TempDir()}
	w.home = filepath.Join(w.dir, "home")
	w.vault = filepath.Join(w.dir, "vault")
	w.real = filepath.Join(w.dir, "real")
	bin := filepath.Join(w.dir, "bin")
	for _, d := range []string{w.home, filepath.Join(w.vault, ".beads"), w.real, bin, filepath.Join(w.home, ".config", "mw")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"sh", "flock", "id", "dirname", "mkdir", "cat", "env", "sed", "grep", "sort", "tr"} {
		found, err := exec.LookPath(c)
		if err != nil {
			t.Fatalf("no %s to build the PATH from: %v", c, err)
		}
		_ = os.Symlink(found, filepath.Join(w.real, c))
	}
	for name, body := range map[string]string{"systemd-run": systemdRunStandIn, "mw": mwStandIn, "bd": bdStandIn} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.path = bin + ":" + w.real
	return w
}

// beadsEnv writes the host's beads.env, as ~/.config/mw/beads.env is.
func (w *closeoutWorld) beadsEnv() {
	w.t.Helper()
	body := "# made up for the test\n" +
		"BEADS_DOLT_SERVER_MODE=1\n" +
		"BEADS_DOLT_SERVER_HOST=" + closeoutServerHost + "\n" +
		"BEADS_DOLT_SERVER_PORT=3307\n" +
		"BEADS_DOLT_PASSWORD=" + closeoutPassword + "\n"
	if err := os.WriteFile(filepath.Join(w.home, ".config", "mw", "beads.env"), []byte(body), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// run runs a contrib script in the vault with only PATH and HOME (and env),
// as a shell that never read beads.env would, and returns its output and
// exit status.
func (w *closeoutWorld) run(script string, env []string, args ...string) (string, int) {
	w.t.Helper()
	abs, err := filepath.Abs(script)
	if err != nil {
		w.t.Fatal(err)
	}
	cmd := command(w.t, abs, args...)
	cmd.Dir = w.vault
	cmd.Env = append([]string{
		"PATH=" + w.path,
		"HOME=" + w.home,
		"FAKE_MANAGER_PATH=" + w.real,
	}, env...)
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode()
	} else if err != nil {
		w.t.Fatal(err)
	}
	return string(out), 0
}

func (w *closeoutWorld) read(path string) string {
	w.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatalf("%v (calls: %s)", err, w.calls())
	}
	return string(b)
}

func (w *closeoutWorld) calls() string {
	b, _ := os.ReadFile(filepath.Join(w.dir, "calls"))
	return string(b)
}

// noStrayStore fails when a bd run made .beads/embeddeddolt in the vault.
func (w *closeoutWorld) noStrayStore() {
	w.t.Helper()
	if _, err := os.Stat(filepath.Join(w.vault, ".beads", "embeddeddolt")); err == nil {
		w.t.Fatal("a bd run without BEADS_DOLT_* made .beads/embeddeddolt in the vault")
	}
}

// The incident, replayed: the hand-made unit of 2026-10-15, PATH only and no
// EnvironmentFile, running contrib/mw-heavy mw next. mw-heavy reads beads.env
// itself, so the close-out finds its story and no stray store is made.
func TestAHandMadeCloseOutUnitThroughMwHeavyFindsItsStory(t *testing.T) {
	w := newCloseoutWorld(t)
	w.beadsEnv()
	heavy, err := filepath.Abs("mw-heavy")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(w.dir, "next.out")
	_, code := w.run(filepath.Join(w.dir, "bin", "systemd-run"), nil,
		"--user", "--unit=mw-closeout-fix14", "--working-directory="+w.vault,
		"--setenv=PATH="+w.path, "-p", "StandardOutput=append:"+out,
		"--", heavy, "mw", "next", closeoutStory, "--no-dispatch")
	if code != 0 {
		t.Fatalf("systemd-run exited %d", code)
	}
	got := w.read(out)
	if strings.Contains(got, "no issues found matching") || !strings.Contains(got, "closed out "+closeoutStory) {
		t.Fatalf("the close-out did not find its story:\n%s\ncalls:\n%s", got, w.calls())
	}
	if !strings.Contains(got, "BEADS_DOLT_SERVER_HOST="+closeoutServerHost) {
		t.Fatalf("the command did not carry the server's host from beads.env:\n%s", got)
	}
	w.noStrayStore()
}

// A caller that already has the server's host keeps it: beads.env's own host
// may be stale once the home has moved (mw-j3iis.2), so mw-heavy reads the
// file only when BEADS_DOLT_SERVER_HOST is not set.
func TestMwHeavyKeepsTheServerHostItWasGiven(t *testing.T) {
	w := newCloseoutWorld(t)
	w.beadsEnv()
	got, code := w.run("mw-heavy", []string{"BEADS_DOLT_SERVER_HOST=10.99.0.7"}, "mw", "next", closeoutStory)
	if code != 0 {
		t.Fatalf("mw-heavy exited %d:\n%s", code, got)
	}
	if !strings.Contains(got, "BEADS_DOLT_SERVER_HOST=10.99.0.7") || strings.Contains(got, closeoutPassword) {
		t.Fatalf("mw-heavy read beads.env over a server host it was given:\n%s", got)
	}
}

// A host with no beads.env (an embedded vault) runs the command as before.
func TestMwHeavyWithNoBeadsEnvRunsTheCommandAsBefore(t *testing.T) {
	w := newCloseoutWorld(t)
	got, code := w.run("mw-heavy", nil, "sh", "-c", "echo ran; exit 4")
	if code != 4 || !strings.Contains(got, "ran") {
		t.Fatalf("mw-heavy with no beads.env: exit %d, want 4:\n%s", code, got)
	}
}

// mw-closeout starts the close-out as a user unit carrying beads.env, says
// the unit's name and where its output goes, and the close-out finds its
// story there.
func TestADetachedCloseOutCarriesTheBeadsEnvAndFindsItsStory(t *testing.T) {
	w := newCloseoutWorld(t)
	w.beadsEnv()
	// A seat session's PATH starts with a bd that runs mw bd, which finds the
	// real bd by MW_BD_REAL: without it in the unit, that bd calls itself.
	realBd := filepath.Join(w.dir, "real-bd")
	said, code := w.run("mw-closeout", []string{"MW_BD_REAL=" + realBd, "MW_VAULT=" + w.vault}, closeoutStory)
	if code != 0 {
		t.Fatalf("mw-closeout exited %d:\n%s\ncalls:\n%s", code, said, w.calls())
	}
	unit := "mw-closeout-mw-fix-14"
	out := filepath.Join(w.home, ".local", "state", "mw-closeout", unit+".out")
	if !strings.Contains(said, "unit: "+unit) || !strings.Contains(said, "output: "+out) {
		t.Fatalf("mw-closeout did not name its unit %s and output %s:\n%s", unit, out, said)
	}
	calls := w.calls()
	for _, want := range []string{
		"--unit=" + unit,
		"-p EnvironmentFile=-" + filepath.Join(w.home, ".config", "mw", "beads.env"),
		"--same-dir",
		"--setenv=PATH=" + w.path,
		"--setenv=MW_BD_REAL=" + realBd,
		"--setenv=MW_VAULT=" + w.vault,
		"/mw-heavy mw next " + closeoutStory + " --no-dispatch",
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("the unit was started without %q:\n%s", want, calls)
		}
	}
	got := w.read(out)
	for _, want := range []string{
		"BEADS_DOLT_SERVER_HOST=" + closeoutServerHost,
		"BEADS_DOLT_SERVER_PORT=3307",
		"BEADS_DOLT_PASSWORD=" + closeoutPassword,
		"closed out " + closeoutStory,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the unit's close-out lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(said, closeoutPassword) || strings.Contains(calls, closeoutPassword) {
		t.Fatal("the password from beads.env reached mw-closeout's own output or the command line")
	}
	w.noStrayStore()
}

// Without systemd-run there is no unit to start: mw-closeout says so and
// names the foreground command, and runs nothing.
func TestADetachedCloseOutWithoutSystemdRunRefuses(t *testing.T) {
	w := newCloseoutWorld(t)
	w.beadsEnv()
	if err := os.Remove(filepath.Join(w.dir, "bin", "systemd-run")); err != nil {
		t.Fatal(err)
	}
	said, code := w.run("mw-closeout", nil, closeoutStory)
	if code != 1 || !strings.Contains(said, "mw-heavy mw next "+closeoutStory+" --no-dispatch") {
		t.Fatalf("mw-closeout without systemd-run: exit %d, want 1 naming the foreground command:\n%s", code, said)
	}
}

func TestADetachedCloseOutWantsOneStory(t *testing.T) {
	w := newCloseoutWorld(t)
	for _, args := range [][]string{nil, {"a", "b"}, {"--detached"}} {
		said, code := w.run("mw-closeout", nil, args...)
		if code != 1 || !strings.Contains(said, "usage: mw-closeout <story>") {
			t.Fatalf("mw-closeout %q: exit %d, want 1 and the usage:\n%s", args, code, said)
		}
	}
	if w.calls() != "" {
		t.Fatalf("a refused mw-closeout started something:\n%s", w.calls())
	}
}

func TestTheCloseOutScriptIsExecutableAndParses(t *testing.T) {
	info, err := os.Stat("mw-closeout")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("contrib/mw-closeout is not executable")
	}
	if out, err := command(t, "sh", "-n", "mw-closeout").CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}
