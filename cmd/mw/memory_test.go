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

// mw memory supersede --by <slug> retires a fact as superseded by an existing
// one, writes no new fact, commits the one file and is documented in --help
// (mw-gq6.762).
func TestMemorySupersedeByRetiresTheFactAndAddsNoFact(t *testing.T) {
	vaultDir, _ := gitVaultWithRemote(t, true)
	facts := "seats/builder/rigs/demo/facts/"
	putFile(t, vaultDir, facts+"old-fact.md", tightFact("clock", "2026-10-01", "An old sentence."))
	putFile(t, vaultDir, facts+"new-fact.md", tightFact("clock", "2026-10-02", "A new sentence."))
	gitIn(t, vaultDir, "add", "-A")
	gitIn(t, vaultDir, "commit", "-qm", "facts")
	gitIn(t, vaultDir, "push", "-q")

	help, _ := runMW(t, "memory", "supersede", "--help")
	if !strings.Contains(help, "--by") || !strings.Contains(help, "superseded by <existing-slug>") {
		t.Errorf("supersede --help does not document --by:\n%s", help)
	}

	before := gitIn(t, vaultDir, "rev-parse", "HEAD")
	runMW(t, "memory", "supersede", "demo", "old-fact", "--by", "new-fact")
	theCommitHolds(t, vaultDir, before, facts+"old-fact.md")
	old, err := os.ReadFile(filepath.Join(vaultDir, facts, "old-fact.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status: retired", "superseded by new-fact"} {
		if !strings.Contains(string(old), want) {
			t.Errorf("old-fact.md lacks %q:\n%s", want, old)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(vaultDir, facts))
	if len(entries) != 2 {
		t.Errorf("a fact was added: %v", entries)
	}
}

func TestTheHelpOfQueryAndDemoteSaysOneTermPerArgument(t *testing.T) {
	memory := newMemoryCmd()
	for _, name := range []string{"query", "demote"} {
		sub, _, err := memory.Find([]string{name})
		if err != nil || sub.Name() != name {
			t.Fatalf("no %s command: %v", name, err)
		}
		if !strings.Contains(sub.Long, "One term per argument") {
			t.Errorf("the help of %s lacks 'One term per argument':\n%s", name, sub.Long)
		}
	}
}

func TestDemoteHasAnAutoQuestionAndAddABootAndAQuestion(t *testing.T) {
	memory := newMemoryCmd()
	for name, flags := range map[string][]string{"demote": {"auto-question"}, "add": {"boot", "question"}} {
		sub, _, _ := memory.Find([]string{name})
		for _, flag := range flags {
			if sub.Flags().Lookup(flag) == nil {
				t.Errorf("%s has no --%s", name, flag)
			}
		}
	}
}

// The line a Landed mail prints for an add puts -- before the sentence, so a
// sentence that begins with -- is pasted as a sentence, not a flag (mw-gq6.1370).
func TestMemoryAddTakesASentenceStartingWithDashDashAfterAnEndOfFlags(t *testing.T) {
	vaultDir := aTightRig(t)
	t.Setenv("MW_RIG_MEMORY_BYTES", "100000")
	// The words of the printed line, as mw MemoryCommand prints them.
	p := application.FactProposal{Verb: application.ProposeGotcha, Subject: "flags", NewSlug: "dash-dash", Sentence: "--flag is a word of the sentence."}
	if printed := p.MemoryCommand("tight", "mw-1"); !strings.HasSuffix(printed, "--source mw-1 -- '--flag is a word of the sentence.'") {
		t.Fatalf("the printed line is not the one under test: %s", printed)
	}
	stdout, _ := runMW(t, "memory", "add", "tight", "--kind", "gotcha", "--subject", "flags", "--slug", "dash-dash", "--source", "mw-1", "--", "--flag is a word of the sentence.")
	if !strings.Contains(stdout, "added dash-dash") {
		t.Fatalf("not added:\n%s", stdout)
	}
	fact, err := os.ReadFile(filepath.Join(vaultDir, "seats/builder/rigs/tight/facts/dash-dash.md"))
	if err != nil || !strings.Contains(string(fact), "--flag is a word of the sentence.") {
		t.Errorf("the fact is missing or wrong: %v\n%s", err, fact)
	}
}

// make-room --help says what the Governor rule is now: his confirmation being
// asked is not his word, and a phrase quoted as an example is not either; it no
// longer says 'the Governor' is skipped anywhere (mw-gq6.1501).
func TestMemoryMakeRoomHelpNamesTheConfirmationException(t *testing.T) {
	help, _ := runMW(t, "memory", "make-room", "--help")
	flat := strings.Join(strings.Fields(help), " ")
	if strings.Contains(flat, "'the Governor', \"Governor's\" or 'his words' anywhere") {
		t.Errorf("make-room --help still says the Governor is skipped anywhere:\n%s", help)
	}
	for _, want := range []string{"the Governor to confirm", "quoted", "asserted"} {
		if !strings.Contains(flat, want) {
			t.Errorf("make-room --help does not name %q:\n%s", want, help)
		}
	}
}
