package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A rig at 7990/8000 of its boot render is the friction mw memory add --make-room
// answers: any new line is over the budget at once (mw-gq6.738).

const tightSentence = "The clock fixture starts at midnight of day." // 44 bytes: its line costs 60

func tightFact(subject, since, sentence string) string {
	return "---\nsubject: " + subject + "\nkind: gotcha\nstatus: current\nsource: mw-1\nsince: " + since + "\n---\n\n" + sentence + "\n"
}

// aTightRig puts a rig "tight" in the vault whose boot render is exactly 7990
// bytes, with old gotchas that make-room may demote, and sets the budget to 8000.
func aTightRig(t *testing.T) (vaultDir string) {
	t.Helper()
	vaultDir, _ = gitVaultWithRemote(t, true)
	t.Setenv("MW_RIG_MEMORY_BYTES", "8000")
	rig := "seats/builder/rigs/tight"
	for i, word := range []string{"alpha", "bravo", "charly"} {
		sentence := fmt.Sprintf("The %s service keeps its state in a file that a restart must remove by hand first.", word)
		putFile(t, vaultDir, fmt.Sprintf("%s/facts/%s.md", rig, word), tightFact(word, fmt.Sprintf("2026-01-0%d", i+1), sentence))
	}
	// The about text is padded so that the whole render is 7990 bytes.
	putFile(t, vaultDir, rig+"/about.md", "About.\n")
	size := renderOf(t, vaultDir, rig)
	putFile(t, vaultDir, rig+"/about.md", "About "+strings.Repeat("x", 7990-size-1)+".\n")
	if got := renderOf(t, vaultDir, rig); got != 7990 {
		t.Fatalf("the fixture render is %d bytes, want 7990", got)
	}
	return vaultDir
}

func renderOf(t *testing.T, vaultDir, rig string) int {
	t.Helper()
	about, err := os.ReadFile(filepath.Join(vaultDir, rig, "about.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(vaultDir, rig, "facts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		text, err := os.ReadFile(filepath.Join(vaultDir, rig, "facts", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(text)
	}
	facts, _ := application.LoadRigFacts(files)
	return len(application.RenderRigMemory(string(about), facts))
}

func TestMemoryAddMakeRoomAddsA60ByteLineToARigAt7990Of8000(t *testing.T) {
	vaultDir := aTightRig(t)
	stdout, _ := runMW(t, "memory", "add", "tight", "--make-room", "--kind", "gotcha", "--subject", "clk", "--source", "mw-1", tightSentence)
	if !strings.Contains(stdout, "added the-clock-fixture-starts-at") {
		t.Errorf("not added:\n%s", stdout)
	}
	if size := renderOf(t, vaultDir, "seats/builder/rigs/tight"); size > 8000 {
		t.Errorf("the render is %d/8000 after the add", size)
	}
	fact, err := os.ReadFile(filepath.Join(vaultDir, "seats/builder/rigs/tight/facts/the-clock-fixture-starts-at.md"))
	if err != nil || strings.Contains(string(fact), "boot: no") {
		t.Errorf("the new fact is missing or demoted: %v\n%s", err, fact)
	}
	alpha, _ := os.ReadFile(filepath.Join(vaultDir, "seats/builder/rigs/tight/facts/alpha.md"))
	if !strings.Contains(string(alpha), "boot: no") {
		t.Errorf("the oldest gotcha was not demoted:\n%s\n%s", alpha, stdout)
	}
}

func TestMemoryAddWithoutMakeRoomPrintsTheCommandAndTheOverage(t *testing.T) {
	vaultDir := aTightRig(t)
	stdout, _ := runMW(t, "memory", "add", "tight", "--kind", "gotcha", "--subject", "clk", "--source", "mw-1", tightSentence)
	for _, want := range []string{"over the budget by 50 bytes", "mw memory make-room tight --bytes 0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the add does not say %q:\n%s", want, stdout)
		}
	}
	alpha, _ := os.ReadFile(filepath.Join(vaultDir, "seats/builder/rigs/tight/facts/alpha.md"))
	if strings.Contains(string(alpha), "boot: no") {
		t.Errorf("nothing should be demoted without --make-room")
	}
}

func TestMemoryMakeRoomDryRunWritesNothing(t *testing.T) {
	vaultDir := aTightRig(t)
	before := gitIn(t, vaultDir, "status", "--porcelain")
	stdout, _ := runMW(t, "memory", "make-room", "tight", "--bytes", "100", "--dry-run")
	if !strings.Contains(stdout, "would demote alpha") || !strings.Contains(stdout, "render before: 7990/8000") {
		t.Errorf("the plan:\n%s", stdout)
	}
	if after := gitIn(t, vaultDir, "status", "--porcelain"); after != before {
		t.Errorf("a dry run changed the vault:\n%s\nwas\n%s", after, before)
	}
}
