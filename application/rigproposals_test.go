package application_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTheFiveProposalFormsAndNothingAreParsedTyped(t *testing.T) {
	comment := "Done.\n\nFor the rig memory:\n" +
		"gotcha [bd]: Point every bd call at the vault with -C.\n" +
		"decision [tmux]: Tests use a private socket.\n" +
		"supersede bd-dash-c: Use -C, as its own call.\n" +
		"retire old-note: The script it described is gone.\n" +
		"recheck some-fact: bd 1.4 may have changed it.\n" +
		"nothing\n"
	got := application.ParseFactProposals(comment)
	want := []application.FactProposal{
		{Verb: application.ProposeGotcha, Subject: "bd", Sentence: "Point every bd call at the vault with -C."},
		{Verb: application.ProposeDecision, Subject: "tmux", Sentence: "Tests use a private socket."},
		{Verb: application.ProposeSupersede, Slug: "bd-dash-c", Sentence: "Use -C, as its own call."},
		{Verb: application.ProposeRetire, Slug: "old-note", Sentence: "The script it described is gone."},
		{Verb: application.ProposeRecheck, Slug: "some-fact", Sentence: "bd 1.4 may have changed it."},
		{Verb: application.ProposeNothing},
	}
	if !got.Found {
		t.Fatalf("the section was not found")
	}
	if len(got.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got.Lines), len(want), got.Lines)
	}
	for i, line := range got.Lines {
		if line.Malformed != "" {
			t.Errorf("line %d was flagged malformed: %q", i, line.Malformed)
		}
		if !reflect.DeepEqual(line.Proposal, want[i]) {
			t.Errorf("line %d: got %+v, want %+v", i, line.Proposal, want[i])
		}
	}
}

func TestAMalformedProposalLineIsNamed(t *testing.T) {
	for name, line := range map[string]string{
		"a missing colon":       "gotcha [bd] Point every bd call at the vault",
		"an unknown verb":       "remember [bd]: Something.",
		"an empty sentence":     "gotcha [bd]:",
		"a missing subject":     "decision: No subject given.",
		"a slug with a space":   "retire two words: Because.",
		"a verb alone":          "supersede",
		"prose that is no verb": "The tests were slow.",
	} {
		got := application.ParseFactProposals("For the rig memory:\n" + line + "\n")
		if len(got.Lines) != 1 || got.Lines[0].Malformed != line {
			t.Errorf("%s: got %+v, want one malformed line %q", name, got.Lines, line)
		}
	}
}

func TestTheSectionRunsFromItsHeadingToTheEndAndTakesListMarkers(t *testing.T) {
	got := application.ParseFactProposals("gotcha [x]: before the heading, not read.\n" +
		"For the rig memory: nothing\n")
	if len(got.Lines) != 1 || got.Lines[0].Proposal.Verb != application.ProposeNothing {
		t.Errorf("a line after the heading's colon: got %+v", got.Lines)
	}

	got = application.ParseFactProposals("For the rig memory:\n\n- gotcha [bd]: A bulleted one.\n2. nothing.\n")
	if len(got.Lines) != 2 || got.Lines[0].Proposal.Sentence != "A bulleted one." || got.Lines[1].Proposal.Verb != application.ProposeNothing {
		t.Errorf("bullets and blanks: got %+v", got.Lines)
	}

	if got := application.ParseFactProposals("Done. Nothing else."); got.Found {
		t.Errorf("a comment with no section was found to have one: %+v", got)
	}
}

func aRigMemorySection() application.FactProposals {
	return application.ParseFactProposals("Done.\nFor the rig memory:\n" +
		"gotcha [bd]: Point every bd call at the vault with -C.\n" +
		"decision [tmux]: Tests use a private socket.\n" +
		"supersede bd-dash-c: Use -C, as its own call.\n" +
		"retire old-note: The script it described is gone.\n" +
		"recheck some-fact: bd 1.4 may have changed it.\n" +
		"nothing\n")
}

func TestRigMemoryBlockOfAFactsRigKeepsTheMwMemoryLines(t *testing.T) {
	want := "For the rig memory (lampas):\n" +
		"mw memory add lampas --kind gotcha --subject 'bd' --source mw-9 'Point every bd call at the vault with -C.'\n" +
		"mw memory add lampas --kind decision --subject 'tmux' --source mw-9 'Tests use a private socket.'\n" +
		"mw memory supersede lampas bd-dash-c --source mw-9 'Use -C, as its own call.'\n" +
		"mw memory retire lampas old-note --reason 'The script it described is gone.'\n" +
		"mw memory recheck lampas some-fact --why 'bd 1.4 may have changed it.'\n" +
		"nothing"
	if got := aRigMemorySection().RigMemoryBlock("lampas", "mw-9", ""); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRigMemoryBlockOfAFlatFileRigNamesTheFlatFileForEachVerb(t *testing.T) {
	file := application.FlatRigFile("builder", "postern")
	if file != "seats/builder/rigs/postern.md" {
		t.Fatalf("the flat file is %q", file)
	}
	want := "For the rig memory (postern):\n" +
		"place by hand in seats/builder/rigs/postern.md: gotcha [bd]: Point every bd call at the vault with -C. (mw-9)\n" +
		"place by hand in seats/builder/rigs/postern.md: decision [tmux]: Tests use a private socket. (mw-9)\n" +
		"edit by hand in seats/builder/rigs/postern.md: supersede bd-dash-c: Use -C, as its own call. (mw-9)\n" +
		"edit by hand in seats/builder/rigs/postern.md: retire old-note: The script it described is gone. (mw-9)\n" +
		"edit by hand in seats/builder/rigs/postern.md: recheck some-fact: bd 1.4 may have changed it. (mw-9)\n" +
		"nothing"
	got := aRigMemorySection().RigMemoryBlock("postern", "mw-9", file)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "mw memory") {
		t.Errorf("a flat-file rig was handed an mw memory line:\n%s", got)
	}
}

func TestRigMemoryBlockOfAFlatFileRigStillFlagsMalformedLines(t *testing.T) {
	got := application.ParseFactProposals("For the rig memory:\nremember the vault\n").
		RigMemoryBlock("postern", "mw-9", application.FlatRigFile("builder", "postern"))
	if got != "For the rig memory (postern):\nMALFORMED: remember the vault" {
		t.Errorf("got %q", got)
	}
}

// foldersOf is the seat's rigs directory in memory: the rigs that have a folder.
type foldersOf map[string]bool

func (f foldersOf) ReadRigFacts(_ context.Context, _, rig string) (string, map[string]string, bool, error) {
	if !f[rig] {
		return "", nil, false, nil
	}
	return "About.", map[string]string{}, true, nil
}
func (foldersOf) WriteRigFact(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (foldersOf) ReadRigEval(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (foldersOf) WriteRigEval(context.Context, string, string, string) (string, error) {
	return "", nil
}

func TestHasFactsFolderAndMemoryAddRefusalAgreeOnWhichRigsHaveOne(t *testing.T) {
	ctx := context.Background()
	files := foldersOf{"lampas": true}
	for rig, want := range map[string]bool{"lampas": true, "postern": false} {
		got, err := application.HasFactsFolder(ctx, files, "builder", rig)
		if err != nil || got != want {
			t.Errorf("%s: HasFactsFolder = %v, %v; want %v", rig, got, err, want)
		}
		err = application.Memory{Files: files, Seat: "builder", Out: io.Discard}.List(ctx, application.MemoryList{Rig: rig})
		refused := err != nil && strings.Contains(err.Error(), "make seats/builder/rigs/"+rig+"/ first")
		if refused == want {
			t.Errorf("%s: has folder %v but the list's error was %v", rig, want, err)
		}
	}
}

func TestAReadyMemoryCommandIsParsedToTheSameProposalAsTheTypedForm(t *testing.T) {
	comment := "Done.\n\nFor the rig memory:\n" +
		"mw memory add millwright --kind gotcha --subject bd --source mw-gq6.771 \"Point every bd call at the vault with -C.\"\n" +
		"- mw memory add millwright --kind decision --subject 'tmux' --slug private-socket 'Tests use a builder'\\''s private socket.'\n" +
		"mw memory supersede millwright bd-dash-c --source mw-gq6.771 \"Use -C, as its own call.\"\n" +
		"mw memory retire millwright old-note --reason \"The script it described is gone.\"\n" +
		"mw memory recheck millwright some-fact --why \"bd 1.4 may have changed it.\"\n"
	got := application.ParseFactProposals(comment)
	want := []application.FactProposal{
		{Verb: application.ProposeGotcha, Subject: "bd", Sentence: "Point every bd call at the vault with -C."},
		{Verb: application.ProposeDecision, Subject: "tmux", NewSlug: "private-socket", Sentence: "Tests use a builder's private socket."},
		{Verb: application.ProposeSupersede, Slug: "bd-dash-c", Sentence: "Use -C, as its own call."},
		{Verb: application.ProposeRetire, Slug: "old-note", Sentence: "The script it described is gone."},
		{Verb: application.ProposeRecheck, Slug: "some-fact", Sentence: "bd 1.4 may have changed it."},
	}
	if len(got.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got.Lines), len(want), got.Lines)
	}
	for i, line := range got.Lines {
		if line.Malformed != "" || !reflect.DeepEqual(line.Proposal, want[i]) {
			t.Errorf("line %d: got %+v, want %+v", i, line, want[i])
		}
	}
	// The mail renders each as the command that places it, with the story as source
	// and a slug the Builder chose kept.
	block := got.RigMemoryBlock("millwright", "mw-gq6.771", "")
	for _, wantLine := range []string{
		"mw memory add millwright --kind gotcha --subject 'bd' --source mw-gq6.771 'Point every bd call at the vault with -C.'",
		"mw memory add millwright --kind decision --subject 'tmux' --slug private-socket --source mw-gq6.771 'Tests use a builder'\\''s private socket.'",
	} {
		if !strings.Contains(block, wantLine) {
			t.Errorf("the block lacks %q:\n%s", wantLine, block)
		}
	}
}

func TestAMemoryCommandThatCannotBeReadIsNamedMalformed(t *testing.T) {
	for name, line := range map[string]string{
		"an add without a subject":   "mw memory add millwright --kind gotcha 'A sentence.'",
		"an add of a wrong kind":     "mw memory add millwright --kind note --subject bd 'A sentence.'",
		"an unclosed quote":          "mw memory add millwright --kind gotcha --subject bd 'A sentence.",
		"a retire without a reason":  "mw memory retire millwright old-note",
		"an unknown flag":            "mw memory add millwright --kind gotcha --subject bd --bogus x 'A sentence.'",
		"an unknown mw memory verb":  "mw memory forget millwright old-note",
		"a command with extra words": "mw memory add millwright --kind gotcha --subject bd 'One.' 'Two.'",
		"a retire with a bad slug":   "mw memory retire millwright 'two words' --reason x",
		"an empty sentence":          "mw memory add millwright --kind gotcha --subject bd ''",
	} {
		got := application.ParseFactProposals("For the rig memory:\n" + line + "\n")
		if len(got.Lines) != 1 || got.Lines[0].Malformed != line {
			t.Errorf("%s: got %+v, want one malformed line %q", name, got.Lines, line)
		}
	}
}

func TestEachFormulaAsksForReadyMemoryCommands(t *testing.T) {
	for _, file := range []string{"tdd-feature.formula.json", "chore.formula.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "formulas", file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var formula struct {
			Steps []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("decoding %s: %v", file, err)
		}
		closing := 0
		for _, s := range formula.Steps {
			if !strings.Contains(s.Description, "For the rig memory:") {
				continue
			}
			closing++
			for _, want := range []string{"mw memory add <rig> --kind", "mw memory supersede <rig> <slug>", "mw memory retire <rig> <slug> --reason", "mw memory recheck <rig> <slug> --why"} {
				if !strings.Contains(s.Description, want) {
					t.Errorf("%s step %s: expected it to ask for %q, got %q", file, s.ID, want, s.Description)
				}
			}
		}
		if closing == 0 {
			t.Errorf("%s: no closing step names the rig memory section", file)
		}
	}
}
