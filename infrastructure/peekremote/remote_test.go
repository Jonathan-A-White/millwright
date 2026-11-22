package peekremote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// standIn puts an ssh on PATH that records its arguments and prints one line.
func standIn(t *testing.T, exit int) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\necho host: desktop\necho oops >&2\nexit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestPeekRunsMwPeekHereOnTheHost(t *testing.T) {
	log := standIn(t, 0)
	got, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.Peek(context.Background(), "desktop", "mw-3evcnk.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "host: desktop\n" {
		t.Fatalf("printed %q", got)
	}
	args, _ := os.ReadFile(log)
	want := "-o\nBatchMode=yes\ndesktop\nbash -lc 'mw peek --here '\\''mw-3evcnk.1'\\'''\n"
	if string(args) != want {
		t.Fatalf("ssh got\n%s\nwant\n%s", args, want)
	}
}

func TestPeekSaysWhenAHostCannotBeReached(t *testing.T) {
	if _, err := (Remote{}).Peek(context.Background(), "desktop", "mw-1"); err == nil || !strings.Contains(err.Error(), "[hands_hosts]") {
		t.Fatalf("expected a refusal naming [hands_hosts], got %v", err)
	}
	standIn(t, 1)
	_, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.Peek(context.Background(), "desktop", "mw-1")
	if err == nil || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("expected ssh's stderr in the error, got %v", err)
	}
}

func TestPeekNamesWhyABoxHasNoWayToBeReached(t *testing.T) {
	_, err := Remote{Gaps: map[string]string{"cloud1": "cloud1 is not a peer of the hub: it has no address"}}.Peek(context.Background(), "cloud1", "mw-1")
	if err == nil || !strings.Contains(err.Error(), "no way to reach cloud1: cloud1 is not a peer of the hub") {
		t.Fatalf("expected the reason, got %v", err)
	}
}

func TestWorktreeStateRunsMwRetryWorktreeStateOnTheHostAndReadsWhatItPrints(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\nprintf 'worktree: /w/mw-1\\nuncommitted: a.go\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	state, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.WorktreeState(context.Background(), "desktop", "mw-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Path != "/w/mw-1" || len(state.Left) != 1 || state.Left[0] != "a.go" {
		t.Fatalf("read %+v", state)
	}
	args, _ := os.ReadFile(log)
	if want := "bash -lc 'mw retry --worktree-state '\\''mw-1'\\'''\n"; !strings.HasSuffix(string(args), want) {
		t.Fatalf("ssh got\n%s\nwant it to end with\n%s", args, want)
	}
}

func TestWorktreeStateIsAnErrorWhenTheHostSaysNothingOfItsWorktree(t *testing.T) {
	standIn(t, 0) // prints "host: desktop", which says nothing of a worktree
	if _, err := (Remote{Reach: map[string]string{"desktop": "ssh desktop"}}).WorktreeState(context.Background(), "desktop", "mw-1"); err == nil {
		t.Fatal("expected a host that did not say what its worktree holds to be an error, not a clean worktree")
	}
	if _, err := (Remote{}).WorktreeState(context.Background(), "desktop", "mw-1"); err == nil || !strings.Contains(err.Error(), "[hands_hosts]") {
		t.Fatalf("expected a refusal naming [hands_hosts], got %v", err)
	}
}

func TestRetryThereRunsMwRetryWithItsFlagsOnTheHostAndReturnsWhatItPrints(t *testing.T) {
	log := standIn(t, 0)
	got, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.RetryThere(context.Background(), "desktop", "mw-1", []string{"--hold"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "host: desktop\n" {
		t.Fatalf("printed %q", got)
	}
	args, _ := os.ReadFile(log)
	want := "bash -lc 'mw retry '\\''--hold'\\'' '\\''mw-1'\\'''\n"
	if !strings.HasSuffix(string(args), want) {
		t.Fatalf("ssh got\n%s\nwant it to end with\n%s", args, want)
	}
	standIn(t, 1)
	if _, err := (Remote{Reach: map[string]string{"desktop": "ssh desktop"}}).RetryThere(context.Background(), "desktop", "mw-1", nil); err == nil || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("expected ssh's stderr in the error, got %v", err)
	}
}

func TestLandRoomRunsMwLandRoomOnTheHostAndReadsWhatItPrints(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\nprintf 'Welcome to cloud14\\nrig: yes\\ngate: free\\nload: 1.5 8 0\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	room, err := Remote{Reach: map[string]string{"cloud14": "ssh mw@10.0.0.14"}}.LandRoom(context.Background(), "cloud14", "lampas")
	if err != nil {
		t.Fatal(err)
	}
	if room.Host != "cloud14" || !room.HasRig || room.GateHeldBy != "" || room.Load.Load != 1.5 || room.Load.Cores != 8 || !room.Free() {
		t.Fatalf("read %+v", room)
	}
	args, _ := os.ReadFile(log)
	if want := "-o\nBatchMode=yes\nmw@10.0.0.14\nbash -lc 'mw land '\\''lampas'\\'' --room'\n"; string(args) != want {
		t.Fatalf("ssh got\n%s\nwant\n%s", args, want)
	}
}

func TestLandRoomIsAnErrorWhenTheHostSaysNothingOfItsRoom(t *testing.T) {
	standIn(t, 0) // prints "host: desktop"
	if _, err := (Remote{Reach: map[string]string{"desktop": "ssh desktop"}}).LandRoom(context.Background(), "desktop", "lampas"); err == nil {
		t.Fatal("expected a host that says nothing of a room to be an error")
	}
}

func TestLandArgvIsTheSshCommandThatRunsMwLandOnTheHost(t *testing.T) {
	argv, err := Remote{Reach: map[string]string{"cloud14": "ssh mw@10.0.0.14"}}.LandArgv("cloud14", "lampas", application.LandScope{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-o", "BatchMode=yes", "mw@10.0.0.14", "bash -lc 'mw land '\\''lampas'\\'''"}
	if strings.Join(argv, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", argv, want)
	}
	if _, err := (Remote{}).LandArgv("cloud14", "lampas", application.LandScope{}); err == nil {
		t.Fatal("expected a host with no way to be reached to be an error")
	}
}

// A drain's landing (--one) and a held batch's parts (--only) run on the host
// with the scope they have here (mw-gq6.1618).
func TestLandArgvCarriesTheScopeOfTheLanding(t *testing.T) {
	r := Remote{Reach: map[string]string{"cloud14": "ssh mw@10.0.0.14"}}
	for _, c := range []struct {
		scope application.LandScope
		want  string
	}{
		{application.LandScope{One: true}, "mw land '\\''lampas'\\'' --one"},
		{application.LandScope{Only: []string{"a-1", "a-2"}}, "mw land '\\''lampas'\\'' --only '\\''a-1,a-2'\\''"},
		{application.LandScope{Without: []string{"a-3"}}, "mw land '\\''lampas'\\'' --without '\\''a-3'\\''"},
	} {
		argv, err := r.LandArgv("cloud14", "lampas", c.scope)
		if err != nil {
			t.Fatal(err)
		}
		if got := argv[len(argv)-1]; got != "bash -lc '"+c.want+"'" {
			t.Errorf("scope %+v: got %q", c.scope, got)
		}
	}
}

// A nightly run handed to a host with room runs mw suite <rig> --file or mw
// timing <rig> there (mw-gq6.1617).
func TestNightlyArgvIsTheSshCommandThatRunsTheNightlyRunOnTheHost(t *testing.T) {
	r := Remote{Reach: map[string]string{"cloud9": "ssh mw@10.0.0.9"}}
	for _, c := range []struct{ kind, want string }{
		{application.NightlySuiteKind, "bash -lc 'mw suite '\\''lampas'\\'' --file'"},
		{application.NightlyTimingKind, "bash -lc 'mw timing '\\''lampas'\\'''"},
	} {
		argv, err := r.NightlyArgv("cloud9", c.kind, "lampas")
		if err != nil {
			t.Fatal(err)
		}
		if got := argv[len(argv)-1]; got != c.want {
			t.Errorf("%s: got %q, want %q", c.kind, got, c.want)
		}
	}
	if _, err := r.NightlyArgv("cloud9", "todo", "lampas"); err == nil {
		t.Fatal("expected a kind of run it does not know to be an error")
	}
	if _, err := (Remote{}).NightlyArgv("cloud9", application.NightlySuiteKind, "lampas"); err == nil {
		t.Fatal("expected a host with no way to be reached to be an error")
	}
}

func TestNextThereRunsMwNextWithTheMayorsWordOnTheHostAndKeepsWhatItPrints(t *testing.T) {
	log := standIn(t, 0)
	flags := []string{"--no-dispatch", "--met-by-mayor", "ran AC 1: it's 1.68 s"}
	got, err := Remote{Reach: map[string]string{"cloud9": "ssh mw@10.0.0.9"}}.NextThere(context.Background(), "cloud9", "mw-gq6.1619", flags)
	if err != nil || got != "host: desktop\n" {
		t.Fatalf("printed %q, %v", got, err)
	}
	args, _ := os.ReadFile(log)
	want := "-o\nBatchMode=yes\nmw@10.0.0.9\nbash -lc 'mw next '\\''mw-gq6.1619'\\'' '\\''--no-dispatch'\\'' '\\''--met-by-mayor'\\'' '\\''ran AC 1: it'\\''\\'\\'''\\''s 1.68 s'\\'''\n"
	if string(args) != want {
		t.Fatalf("ssh got\n%s\nwant\n%s", args, want)
	}

	// A mw next that refuses has still printed its report: it is kept with the error.
	standIn(t, 1)
	got, err = Remote{Reach: map[string]string{"cloud9": "ssh cloud9"}}.NextThere(context.Background(), "cloud9", "mw-gq6.1619", flags)
	if err == nil || !strings.Contains(err.Error(), "oops") || got != "host: desktop\n" {
		t.Fatalf("expected the refusal and the report, got %q, %v", got, err)
	}
}

func TestCheckThereRunsMwCheckWithItsFlagsOnTheHostAndReturnsWhatItPrints(t *testing.T) {
	log := standIn(t, 0)
	got, err := Remote{Reach: map[string]string{"cloud9": "ssh cloud9"}}.CheckThere(context.Background(), "cloud9", "mw-1", []string{"--part", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "host: desktop\n" {
		t.Fatalf("printed %q", got)
	}
	args, _ := os.ReadFile(log)
	want := "bash -lc 'mw check '\\''--part'\\'' '\\''2'\\'' '\\''mw-1'\\'''\n"
	if !strings.HasSuffix(string(args), want) {
		t.Fatalf("ssh got\n%s\nwant it to end with\n%s", args, want)
	}
	standIn(t, 1)
	out, err := Remote{Reach: map[string]string{"cloud9": "ssh cloud9"}}.CheckThere(context.Background(), "cloud9", "mw-1", nil)
	if err == nil || !strings.Contains(err.Error(), "oops") || out != "host: desktop\n" {
		t.Fatalf("expected ssh's stderr in the error and what it printed, got %q, %v", out, err)
	}
}
