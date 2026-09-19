package vault_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// aVault lays out a vault holding one seat, with the pieces of that seat a
// session must never be booted with alongside the ones it must.
func aVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for path, contents := range map[string]string{
		"seats/builder/charter.md":             "the builder's charter",
		"seats/builder/rigs/millwright.md":     "what the builder knows about millwright",
		"seats/builder/rigs/fellowship.md":     "what the builder knows about fellowship",
		"seats/builder/ledger.md":              "every story the builder ever worked",
		"seats/builder/postmortems/2026-09.md": "the story that went wrong",
		"seats/mayor/charter.md":               "the mayor's charter",
		"runs/mw-old.1/result.json":            "{}",
	} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}
	return dir
}

func TestSeatIsTheCharterAndTheMemoryOfOneRig(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "millwright")
	if err != nil {
		t.Fatalf("reading the seat: %v", err)
	}
	if seat.Name != "builder" || seat.Rig != "millwright" {
		t.Errorf("expected the builder seat on millwright, got %q on %q", seat.Name, seat.Rig)
	}
	if seat.Charter != "the builder's charter" {
		t.Errorf("expected the charter, got %q", seat.Charter)
	}
	if seat.Memory != "what the builder knows about millwright" {
		t.Errorf("expected the memory of millwright, got %q", seat.Memory)
	}
}

func TestSeatWithNoMemoryOfTheRigIsNotAnError(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "a-rig-never-worked")
	if err != nil {
		t.Fatalf("a seat with no memory of a rig should still boot: %v", err)
	}
	if seat.Memory != "" {
		t.Errorf("expected no memory, got %q", seat.Memory)
	}
	if seat.Charter == "" {
		t.Error("expected the charter to be read anyway")
	}
}

func TestSeatWithoutACharterIsAnError(t *testing.T) {
	v := vault.New(aVault(t))

	if _, err := v.Seat(context.Background(), "clerk", "millwright"); err == nil {
		t.Error("expected a seat with no charter to be refused: there is nothing to boot into")
	}
}

func TestSeatRefusesANameThatWouldReachOutsideTheVault(t *testing.T) {
	v := vault.New(aVault(t))

	for _, name := range []string{"", "..", "../mayor", "builder/rigs"} {
		if _, err := v.Seat(context.Background(), name, "millwright"); err == nil {
			t.Errorf("expected the seat %q to be refused", name)
		}
		if _, err := v.Seat(context.Background(), "builder", "../../etc/passwd"); err == nil {
			t.Error("expected a rig that climbs out of the vault to be refused")
		}
	}
}

func TestRunFilesLandTogetherUnderTheStory(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)

	path, err := v.PutRunFile(context.Background(), "mw-gq6.6", application.BootFileName, "boot me")
	if err != nil {
		t.Fatalf("writing the boot file: %v", err)
	}
	want := filepath.Join(dir, "runs", "mw-gq6.6", application.BootFileName)
	if path != want {
		t.Errorf("expected the boot file at %q, got %q", want, path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the boot file back: %v", err)
	}
	if string(contents) != "boot me" {
		t.Errorf("expected what was written, got %q", contents)
	}

	// The result belongs beside it, in a directory writing the boot file has
	// already made: the session only redirects into it.
	result := v.RunFile("mw-gq6.6", application.ResultFileName)
	if filepath.Dir(result) != filepath.Dir(path) {
		t.Errorf("expected the result beside the boot file, got %q", result)
	}
	if _, err := os.Stat(filepath.Dir(result)); err != nil {
		t.Errorf("expected the run directory to be there already: %v", err)
	}
}

func TestPutRunFileRefusesAStoryThatWouldReachOutsideTheVault(t *testing.T) {
	v := vault.New(aVault(t))

	if _, err := v.PutRunFile(context.Background(), "../seats/builder", "charter.md", "mine now"); err == nil {
		t.Error("expected a story id that climbs out of the runs directory to be refused")
	}
}

func TestVaultReadsNothingElseInTheSeat(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "millwright")
	if err != nil {
		t.Fatalf("reading the seat: %v", err)
	}
	read := seat.Charter + seat.Memory
	for _, unwanted := range []string{"fellowship", "every story the builder ever worked", "went wrong", "mayor"} {
		if strings.Contains(read, unwanted) {
			t.Errorf("a seat read at boot must not hold %q", unwanted)
		}
	}
}

func TestReadRunFileTellsAResultFromNoResultAtAll(t *testing.T) {
	v := vault.New(aVault(t))

	read, err := v.ReadRunFile(context.Background(), "mw-old.1", application.ResultFileName)
	if err != nil || read != "{}" {
		t.Errorf("expected the result that was written, got %q (%v)", read, err)
	}

	_, err = v.ReadRunFile(context.Background(), "mw-gq6.8", application.ResultFileName)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected a session that wrote nothing to be told from one that wrote a failure, got %v", err)
	}
}

func TestAppendToLedgerOnlyEverAppends(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	ctx := context.Background()

	// The seat's ledger already holds a line with no newline of its own, which
	// is how a file a person edited by hand often ends.
	for _, line := range []string{"| a | first | line |", "| a | second | line |"} {
		if err := v.AppendToLedger(ctx, "builder", line); err != nil {
			t.Fatalf("appending %q: %v", line, err)
		}
	}

	held, err := os.ReadFile(filepath.Join(dir, "seats", "builder", application.LedgerFileName))
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(held), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected the line that was there and the two appended, got %q", lines)
	}
	switch {
	case lines[0] != "every story the builder ever worked":
		t.Errorf("expected what the ledger already held to be untouched, got %q", lines[0])
	case lines[1] != "| a | first | line |" || lines[2] != "| a | second | line |":
		t.Errorf("expected both lines appended in order, got %q", lines)
	}
}

func TestAppendToLedgerMakesTheLedgerOfASeatThatHasNone(t *testing.T) {
	dir := aVault(t)
	if err := vault.New(dir).AppendToLedger(context.Background(), "mayor", "| the mayor's first line |"); err != nil {
		t.Fatalf("appending to a ledger that is not there yet: %v", err)
	}
	held, err := os.ReadFile(filepath.Join(dir, "seats", "mayor", application.LedgerFileName))
	if err != nil || string(held) != "| the mayor's first line |\n" {
		t.Errorf("expected the ledger to have been made with the line in it, got %q (%v)", held, err)
	}
}

func TestAppendToLedgerRefusesWhatWouldNotBeOneLine(t *testing.T) {
	v := vault.New(aVault(t))
	if err := v.AppendToLedger(context.Background(), "builder", "| one |\n| two |"); err == nil {
		t.Error("expected a line with a newline in it to be refused")
	}
	if err := v.AppendToLedger(context.Background(), "../etc", "| mine now |"); err == nil {
		t.Error("expected a seat name that reaches outside the vault to be refused")
	}
}

func TestReadLedgerReadsBackWhatWasAppended(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	ctx := context.Background()

	for _, line := range []string{"| a | first | line |", "| a | second | line |"} {
		if err := v.AppendToLedger(ctx, "mayor", line); err != nil {
			t.Fatalf("appending %q: %v", line, err)
		}
	}

	lines, err := v.ReadLedger(ctx, "mayor")
	if err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(lines) != 2 || lines[0] != "| a | first | line |" || lines[1] != "| a | second | line |" {
		t.Fatalf("expected both lines appended, in order, got %q", lines)
	}

	// The ledger the builder seat already holds, from aVault's own fixture.
	held, err := v.ReadLedger(ctx, "builder")
	if err != nil {
		t.Fatalf("reading the builder's ledger: %v", err)
	}
	if len(held) != 1 || held[0] != "every story the builder ever worked" {
		t.Fatalf("expected the builder's one fixture line, got %q", held)
	}
}

func TestReadLedgerOfASeatWithNoneYetIsEmptyNotAnError(t *testing.T) {
	v := vault.New(aVault(t))
	lines, err := v.ReadLedger(context.Background(), "clerk")
	if err != nil {
		t.Fatalf("reading a ledger nobody has written: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected no lines, got %q", lines)
	}
}

func TestReadLedgerRefusesANameThatWouldReachOutsideTheVault(t *testing.T) {
	v := vault.New(aVault(t))
	if _, err := v.ReadLedger(context.Background(), "../etc"); err == nil {
		t.Error("expected a seat name that reaches outside the vault to be refused")
	}
}

func TestRigMemorySizesAreTheSizesOfTheMemoriesAndNotTheirArchives(t *testing.T) {
	dir := aVault(t)
	archive := filepath.Join(dir, "seats", "builder", "rigs", "millwright-archive.md")
	if err := os.WriteFile(archive, []byte(strings.Repeat("x", 9000)), 0o644); err != nil {
		t.Fatalf("writing the archive: %v", err)
	}

	sizes, err := vault.New(dir).RigMemorySizes(context.Background(), "builder")
	if err != nil {
		t.Fatalf("sizing the memories: %v", err)
	}
	want := []application.RigMemorySize{
		{Rig: "fellowship", Bytes: len("what the builder knows about fellowship")},
		{Rig: "millwright", Bytes: len("what the builder knows about millwright")},
	}
	if len(sizes) != len(want) || sizes[0] != want[0] || sizes[1] != want[1] {
		t.Errorf("expected %v, got %v", want, sizes)
	}
}

func TestRigMemorySizesOfASeatWithNoRigsAreNoneNotAnError(t *testing.T) {
	sizes, err := vault.New(aVault(t)).RigMemorySizes(context.Background(), "mayor")
	if err != nil {
		t.Fatalf("sizing the memories of a seat that has none: %v", err)
	}
	if len(sizes) != 0 {
		t.Errorf("expected no sizes, got %v", sizes)
	}
}

func TestRigMemorySizesRefuseANameThatWouldReachOutsideTheVault(t *testing.T) {
	if _, err := vault.New(aVault(t)).RigMemorySizes(context.Background(), "../etc"); err == nil {
		t.Error("expected a seat name that reaches outside the vault to be refused")
	}
}

func TestAHostFileIsWhatTheSeatKeepsForThatHostAndEmptyWhenItKeepsNone(t *testing.T) {
	dir := aVault(t)
	full := filepath.Join(dir, "seats", "millhand", "hosts", "laptop", "review-since")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("2026-09-18T20:15:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := vault.New(dir)

	got, err := v.HostFile(context.Background(), "millhand", "laptop", "review-since")
	if err != nil || got != "2026-09-18T20:15:00Z\n" {
		t.Fatalf("expected what the file holds, got %q, %v", got, err)
	}
	for _, other := range [][3]string{{"millhand", "vps", "review-since"}, {"millhand", "laptop", "nothing"}, {"mayor", "laptop", "review-since"}} {
		if got, err := v.HostFile(context.Background(), other[0], other[1], other[2]); err != nil || got != "" {
			t.Errorf("expected %v to read as empty and not as a failure, got %q, %v", other, got, err)
		}
	}
	if _, err := v.HostFile(context.Background(), "millhand", "../laptop", "review-since"); err == nil {
		t.Error("expected a host that reaches outside the vault to be refused")
	}
}
