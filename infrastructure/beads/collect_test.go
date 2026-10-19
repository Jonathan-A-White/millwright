package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// refusal is what the home's Dolt server answers a procedure its beads user may
// not call (mw-gq6.629).
const refusal = "command denied to user 'beads'@'%'"

// servedVault makes the vault a host that serves its beads keep: .beads/dolt
// holds the data folder and metadata.json names the database in it.
func servedVault(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".beads", "dolt", "sf", ".dolt"), 0o755); err != nil {
		t.Fatalf("making the data folder: %v", err)
	}
	meta := `{"database": "dolt", "backend": "dolt", "dolt_mode": "embedded", "dolt_database": "sf"}`
	if err := os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatalf("writing metadata.json: %v", err)
	}
	t.Setenv("BEADS_DOLT_SERVER_HOST", "127.0.0.1")
}

// collectStandIns writes a bd and a dolt that each record what they were called
// with, one line a call, and answer as the scripts say. bdBody and doltBody are
// shell run after the call is recorded.
func collectStandIns(t *testing.T, bdBody, doltBody string) (dir, calls string, options []beads.Option) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	dir = t.TempDir()
	calls = filepath.Join(dir, "calls")
	bd := filepath.Join(dir, "bd-stand-in")
	dolt := filepath.Join(dir, "dolt-stand-in")
	bdScript := fmt.Sprintf("#!/bin/sh\necho \"bd $*\" >> %q\n%s\nexit 0\n", calls, bdBody)
	doltScript := fmt.Sprintf("#!/bin/sh\necho \"dolt in $(basename \"$PWD\") $*\" >> %q\n%s\nexit 0\n", calls, doltBody)
	for path, script := range map[string]string{bd: bdScript, dolt: doltScript} {
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("writing the stand-in: %v", err)
		}
	}
	return dir, calls, []beads.Option{beads.WithProgram(bd), beads.WithDoltProgram(dolt)}
}

// On the home the server's beads user may not call DOLT_GC, so the collection
// is the local Dolt user's: the dolt command line in the data folder.
func TestGCOnAServedHostRunsDoltGCAsTheLocalUserInTheDataFolder(t *testing.T) {
	dir, calls, options := collectStandIns(t, "", "")
	servedVault(t, dir)
	gateway := beads.New(dir, options...)

	if err := gateway.GC(context.Background()); err != nil {
		t.Fatalf("collecting: %v", err)
	}
	ran := readCalls(t, calls)
	if collect := indexOf(ran, "dolt in dolt", "--use-db sf", "CALL DOLT_GC()"); collect < 0 {
		t.Fatalf("expected DOLT_GC run by the dolt command line in .beads/dolt, got:\n%s", strings.Join(ran, "\n"))
	}
	if viaBd := indexOf(ran, "bd", "gc"); viaBd >= 0 {
		t.Fatalf("expected no bd gc, which the server refuses and bd takes for done, got %q", ran[viaBd])
	}
}

func TestGCOnAServedHostThatDoltRefusesFails(t *testing.T) {
	dir, _, options := collectStandIns(t, "", `echo "`+refusal+`" >&2; exit 1`)
	servedVault(t, dir)
	gateway := beads.New(dir, options...)

	err := gateway.GC(context.Background())
	if err == nil || !strings.Contains(err.Error(), "command denied to user") {
		t.Fatalf("expected the refusal as the collection's failure, got %v", err)
	}
}

// A refusal that comes with an exit status of 0 is still a refusal.
func TestGCOnAServedHostCountsARefusalDoltExitsZeroOnAsAFailure(t *testing.T) {
	dir, _, options := collectStandIns(t, "", `echo "`+refusal+`"`)
	servedVault(t, dir)
	gateway := beads.New(dir, options...)

	err := gateway.GC(context.Background())
	if err == nil || !strings.Contains(err.Error(), "command denied to user") {
		t.Fatalf("expected the refusal as the collection's failure, got %v", err)
	}
}

// bd gc exits 0 on a refusal by the server (mw-gq6.629): the host's collection
// must not read that as done.
func TestGCThroughBdCountsARefusalBdExitsZeroOnAsAFailure(t *testing.T) {
	dir, _, options := collectStandIns(t, `echo "Dolt GC: `+refusal+`" >&2`, "")
	gateway := beads.New(dir, options...)

	err := gateway.GC(context.Background())
	if err == nil || !strings.Contains(err.Error(), "command denied to user") {
		t.Fatalf("expected the refusal as the collection's failure, got %v", err)
	}
}

func TestGCThroughBdStillRunsBdGCWhereTheHostIsNotServed(t *testing.T) {
	dir, calls, options := collectStandIns(t, "", "")
	gateway := beads.New(dir, options...)

	if err := gateway.GC(context.Background()); err != nil {
		t.Fatalf("collecting: %v", err)
	}
	ran := readCalls(t, calls)
	if indexOf(ran, "bd", "gc", "--skip-decay", "--force") < 0 || indexOf(ran, "dolt") >= 0 {
		t.Fatalf("expected bd gc and no dolt, got:\n%s", strings.Join(ran, "\n"))
	}
}

// squashingBd answers the squash's queries as a served database does.
const squashingBd = `case "$*" in
  *"AS commits"*) echo '[{"commits": 96100}]' ;;
  *"ORDER BY date ASC LIMIT 1"*) echo '[{"commit_hash": "o2lrqserg17hatn16t1act9uiiron8uj"}]' ;;
  *"FROM dolt_remote_branches"*) echo '[]' ;;
esac`

func TestSquashOnAServedHostEndsWithTheFullCollectionAsTheLocalUser(t *testing.T) {
	dir, calls, options := collectStandIns(t, squashingBd, "")
	servedVault(t, dir)
	gateway := beads.New(dir, options...)

	if err := gateway.SquashHistory(context.Background()); err != nil {
		t.Fatalf("squashing: %v", err)
	}
	ran := readCalls(t, calls)
	commit := indexOf(ran, "bd", "DOLT_COMMIT")
	collect := indexOf(ran, "dolt in dolt", "--use-db sf", "CALL DOLT_GC('--full')")
	if commit < 0 || collect < 0 || commit > collect {
		t.Fatalf("expected the commit, then the full DOLT_GC by dolt, got:\n%s", strings.Join(ran, "\n"))
	}
	if viaBd := indexOf(ran, "bd", "gc"); viaBd >= 0 {
		t.Fatalf("expected no bd gc, got %q", ran[viaBd])
	}
}

func TestSquashWhoseFullCollectionIsRefusedSaysSo(t *testing.T) {
	dir, _, options := collectStandIns(t, squashingBd, `echo "`+refusal+`" >&2; exit 1`)
	servedVault(t, dir)
	gateway := beads.New(dir, options...)

	err := gateway.SquashHistory(context.Background())
	if err == nil || !strings.Contains(err.Error(), "command denied to user") {
		t.Fatalf("expected the refusal as the squash's failure, got %v", err)
	}
}

func TestSquashThroughBdCountsARefusalOfItsFullCollectionAsAFailure(t *testing.T) {
	dir, _, options := collectStandIns(t, squashingBd+"\n"+`case "$*" in *"gc"*) echo "`+refusal+`" >&2 ;; esac`, "")
	gateway := beads.New(dir, options...)

	err := gateway.SquashHistory(context.Background())
	if err == nil || !strings.Contains(err.Error(), "command denied to user") {
		t.Fatalf("expected the refusal as the squash's failure, got %v", err)
	}
}
