package sessionshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestShellQuoteLeavesPlainWordsAloneAndWrapsTheRest(t *testing.T) {
	for _, word := range []string{"claude", "--model", "/a/b/boot.md", "a,b=c+d%e@f:g"} {
		if got := ShellQuote(word); got != word {
			t.Errorf("expected %q to be left alone, got %q", word, got)
		}
	}
	wrapped := map[string]string{
		"":          "''",
		"two words": "'two words'",
		"it's":      `'it'\''s'`,
		"$HOME":     "'$HOME'",
	}
	for word, want := range wrapped {
		if got := ShellQuote(word); got != want {
			t.Errorf("quoting %q: expected %q, got %q", word, want, got)
		}
	}
}

func TestShellLineJoinsTheWordsItQuoted(t *testing.T) {
	got := ShellLine([]string{"agent", "--name", "mw-1", "it's a story"})
	if want := `agent --name mw-1 'it'\''s a story'`; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestResultIsWrittenBesideThenMovedIntoPlace(t *testing.T) {
	got := ResultRedirect([]string{"agent", "go"}, "/v/runs/mw-1/result.json", "")
	want := "agent go > /v/runs/mw-1/result.json.tmp; mv /v/runs/mw-1/result.json.tmp /v/runs/mw-1/result.json"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func launch() application.Launch {
	return application.Launch{
		Heartbeat: []string{"mw", "next", "--heartbeat", "mw-1"},
		After:     []string{"mw", "next", "mw-1"},
	}
}

func TestHeartbeatRunsBesideAndIsWaitedForBeforeWhatFollows(t *testing.T) {
	l := launch()
	got := WithHeartbeat("agent go", l)
	for _, want := range []string{"mw next --heartbeat mw-1 & HB=$!; agent go; kill $HB 2>/dev/null; wait $HB 2>/dev/null"} {
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	}
	l.HeartbeatLog = "/v/hb.log"
	got = WithHeartbeat("agent go", l)
	for _, want := range []string{"mkfifo", "tee -a /v/hb.log", "HBT=$!", "wait $HBT", `rm -rf "$HBD"`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestAfterIsChainedWithASemicolonAndKeptInItsLog(t *testing.T) {
	l := launch()
	if got, want := WithAfter("agent go", l), "agent go; mw next mw-1"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	l.AfterLog = "/v/next.log"
	got := AfterLine(l)
	for _, want := range []string{"{ printf ", application.CloseOutStarted, "; mw next mw-1; s=$?; ", application.CloseOutEnded} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
	if want := "; } 2>&1 | tee -a /v/next.log"; !strings.HasSuffix(got, want) {
		t.Errorf("expected the line to end %q, got %q", want, got)
	}
	l.After = nil
	if got := WithAfter("agent go", l); got != "agent go" {
		t.Errorf("expected no After to add nothing, got %q", got)
	}
}

func TestBdShimIsOnlyAddedWhenALauncherAsksForIt(t *testing.T) {
	if got := WithBdShim("agent go", nil); got != "agent go" {
		t.Errorf("expected no retry to add nothing, got %q", got)
	}
	got := WithBdShim("agent go", []string{"mw", "bd"})
	for _, want := range []string{"SHIM=$(mktemp -d)", `MW_BD_REAL="$REAL"`, "exec mw bd", "( export MW_BD_REAL=", "agent go ); if [ -n \"$SHIM\" ]; then rm -rf \"$SHIM\"; fi"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestPreludeReadsEachEnvFileThenSetsTheServerHost(t *testing.T) {
	got := Prelude("agent go", Env{File: "/h/beads.env", HarnessFile: "/h/claude.env", ServerHost: "10.0.0.2"})
	want := "if [ -r /h/beads.env ]; then set -a; . /h/beads.env; set +a; fi; " +
		"if [ -r /h/claude.env ]; then set -a; . /h/claude.env; set +a; fi; " +
		"export BEADS_DOLT_SERVER_HOST=10.0.0.2; agent go"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if got := Prelude("agent go", Env{}); got != "agent go" {
		t.Errorf("expected an empty Env to add nothing, got %q", got)
	}
}

// runAfter runs the close-out as the session's line would, returning what the
// log kept. The close-out is a script that does what the test says.
func runAfter(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	closeOut := filepath.Join(dir, "closeout")
	if err := os.WriteFile(closeOut, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "next-1.log")
	l := launch()
	l.After = []string{closeOut}
	l.AfterLog = log
	_ = exec.Command("sh", "-c", AfterLine(l)).Run()
	kept, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("expected the log to exist: %v", err)
	}
	return string(kept)
}

// mw-gq6.1038: a close-out that says nothing still leaves its start line and
// how it ended, so an empty log no longer hides that it ran at all.
func TestACloseOutThatSaysNothingStillLeavesItsStartAndItsExitStatus(t *testing.T) {
	kept := runAfter(t, "exit 3")
	lines := strings.Split(strings.TrimRight(kept, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], application.CloseOutStarted+" 20") || !strings.HasSuffix(lines[0], "Z") {
		t.Errorf("expected a start line with a UTC time, then an end line, got %q", kept)
	}
	if len(lines) == 2 && lines[1] != application.CloseOutEnded+": exit 3" {
		t.Errorf("expected the end line to carry exit 3, got %q", lines[1])
	}
}

// A close-out killed by a signal is named by it.
func TestACloseOutKilledBySignalIsEndedWithTheSignalNamed(t *testing.T) {
	kept := runAfter(t, "kill -TERM $$")
	if want := application.CloseOutEnded + ": exit 143 (signal 15)"; !strings.Contains(kept, want) {
		t.Errorf("expected %q in %q", want, kept)
	}
}

// A close-out whose whole pane is killed (the host shut down under it) cannot
// write an end line; the start line it wrote first is what is left.
func TestACloseOutKilledWithItsShellLeavesOnlyItsStartLine(t *testing.T) {
	kept := runAfter(t, "kill -KILL $PPID")
	if !strings.HasPrefix(kept, application.CloseOutStarted+" 20") || strings.Contains(kept, application.CloseOutEnded) {
		t.Errorf("expected only the start line, got %q", kept)
	}
}

// runResult runs the session's line for a stand-in harness, the way a pane's
// shell would, and returns what the line itself printed on stderr.
func runResult(t *testing.T, dir, harness string) string {
	t.Helper()
	script := filepath.Join(dir, "harness")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+harness+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := ResultRedirect([]string{script}, filepath.Join(dir, "result-1.json"), filepath.Join(dir, "stderr-1.log"))
	cmd := exec.Command("sh", "-c", line)
	var shown strings.Builder
	cmd.Stderr = &shown
	_ = cmd.Run()
	return shown.String()
}

// mw-gq6.1677: the harness's stderr is kept beside its result, and still shown
// in the pane; the result holds stdout alone.
func TestTheHarnessStderrIsKeptBesideTheResultAndStillShown(t *testing.T) {
	dir := t.TempDir()
	shown := runResult(t, dir, "echo '{\"ok\":true}'; echo boom >&2; exit 2")
	kept, err := os.ReadFile(filepath.Join(dir, "stderr-1.log"))
	if err != nil {
		t.Fatalf("expected stderr-1.log to exist: %v", err)
	}
	if string(kept) != "boom\n" {
		t.Errorf("expected stderr-1.log to hold boom, got %q", kept)
	}
	result, err := os.ReadFile(filepath.Join(dir, "result-1.json"))
	if err != nil {
		t.Fatalf("expected the result to be moved into place: %v", err)
	}
	if string(result) != "{\"ok\":true}\n" {
		t.Errorf("expected the result to hold stdout only, got %q", result)
	}
	if shown != "boom\n" {
		t.Errorf("expected the line's own stderr to carry boom, got %q", shown)
	}
}

// With no stderr file the line is as it was: stderr goes to the pane alone.
func TestNoStderrFileLeavesTheLineAsItWas(t *testing.T) {
	if got, want := ResultRedirect([]string{"agent", "go"}, "/v/r.json", ""), "agent go > /v/r.json.tmp; mv /v/r.json.tmp /v/r.json"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// mw-gq6.1687: the launch line as dispatch composes it (ResultRedirectFrom, then
// WithBdShim, WithHeartbeat, WithAfter, Prelude) is read by a shell, not only
// compared as a string: bash -n must parse it, and both bash and sh must run the
// stand-in harness under it with the result, the stderr file and the pane right,
// and the shim's bd ahead of the real one on the harness's PATH alone.
func TestTheWholeLaunchLineParsesAndRunsUnderBashAndSh(t *testing.T) {
	for _, shell := range []string{"bash", "sh"} {
		for _, withStdin := range []bool{false, true} {
			name := shell + "/stdin-off"
			if withStdin {
				name = shell + "/stdin-on"
			}
			t.Run(name, func(t *testing.T) {
				path, err := exec.LookPath(shell)
				if err != nil {
					t.Skipf("no %s on this host", shell)
				}
				dir := t.TempDir()
				realBin := filepath.Join(dir, "realbin")
				if err := os.MkdirAll(realBin, 0o755); err != nil {
					t.Fatal(err)
				}
				realBd := filepath.Join(realBin, "bd")
				if err := os.WriteFile(realBd, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				harness := filepath.Join(dir, "harness")
				body := "#!/bin/sh\n" +
					"printf 'real=%s\\n' \"$MW_BD_REAL\"\n" +
					"printf 'bd=%s\\n' \"$(command -v bd)\"\n" +
					"printf 'stdin=%s\\n' \"$(cat 2>/dev/null)\"\n" +
					"printf 'oops\\n' >&2\n"
				if err := os.WriteFile(harness, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
				stdinFile := ""
				if withStdin {
					stdinFile = filepath.Join(dir, "boot.md")
					if err := os.WriteFile(stdinFile, []byte("boot text"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				result := filepath.Join(dir, "result-1.json")
				stderrFile := filepath.Join(dir, "stderr-1.log")
				l := application.Launch{
					Heartbeat:    []string{"true"},
					HeartbeatLog: filepath.Join(dir, "hb.log"),
					After:        []string{"true"},
					AfterLog:     filepath.Join(dir, "next-1.log"),
					BdRetry:      []string{"mw", "bd"},
				}
				line := ResultRedirectFrom([]string{harness}, stdinFile, result, stderrFile)
				line = WithBdShim(line, l.BdRetry)
				line = WithHeartbeat(line, l)
				line = WithAfter(line, l)
				line = Prelude(line, Env{ServerHost: "10.0.0.2"})

				if out, err := exec.Command("bash", "-n", "-c", line).CombinedOutput(); err != nil {
					t.Fatalf("bash -n refused the line: %v\n%s\n%s", err, out, line)
				}
				cmd := exec.Command(path, "-c", line)
				cmd.Env = append(os.Environ(), "PATH="+realBin+string(os.PathListSeparator)+os.Getenv("PATH"))
				var pane strings.Builder
				cmd.Stderr = &pane
				if err := cmd.Run(); err != nil {
					t.Fatalf("%s failed on the line: %v\n%s\n%s", shell, err, pane.String(), line)
				}

				got, err := os.ReadFile(result)
				if err != nil {
					t.Fatalf("expected the result in place: %v\n%s", err, pane.String())
				}
				lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
				if len(lines) != 3 {
					t.Fatalf("expected three result lines, got %q", got)
				}
				if lines[0] != "real="+realBd {
					t.Errorf("expected MW_BD_REAL to name the real bd %s, got %q", realBd, lines[0])
				}
				shimBd := strings.TrimPrefix(lines[1], "bd=")
				if shimBd == realBd || filepath.Base(shimBd) != "bd" || filepath.Dir(shimBd) == realBin {
					t.Errorf("expected the shim's bd ahead of the real one, got %q", lines[1])
				}
				wantStdin := "stdin="
				if withStdin {
					wantStdin += "boot text"
				}
				if lines[2] != wantStdin {
					t.Errorf("expected %q, got %q", wantStdin, lines[2])
				}
				if _, err := os.Stat(filepath.Dir(shimBd)); err == nil {
					t.Errorf("expected the shim's directory removed after the session")
				}
				if _, err := os.Stat(result + ".tmp"); err == nil {
					t.Errorf("expected the temp result moved away")
				}
				kept, err := os.ReadFile(stderrFile)
				if err != nil || string(kept) != "oops\n" {
					t.Errorf("expected stderr-1.log to hold oops, got %q (%v)", kept, err)
				}
				if !strings.Contains(pane.String(), "oops\n") {
					t.Errorf("expected the pane to show oops, got %q", pane.String())
				}
			})
		}
	}
}
