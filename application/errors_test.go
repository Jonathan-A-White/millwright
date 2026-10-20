package application_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func errorsOn(files *apptest.FakeErrorFiles, out *bytes.Buffer) application.Errors {
	return application.Errors{
		Files: files, Seat: "builder", Out: out,
		Now: func() time.Time { return time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC) },
	}
}

func TestAnErrorClassReadsBackAsItIsWritten(t *testing.T) {
	class := application.ErrorClass{
		Slug: "gloss-from-a-neighbour", Symptom: "A word's gloss holds words that belong to the next word",
		Cause: "not yet known", Sweep: "every per-word gloss of every text the rig holds",
		Kinds: []string{"word-gloss", "pwa"}, Shared: "exists bsv-kit/gloss", Since: "2026-10-14",
		Checks: []application.ErrorCheck{
			{Date: "2026-10-14", Rig: "lampas", Found: "mw-5r3p30.360"},
			{Date: "2026-10-15", Rig: "legend"},
		},
	}
	back, err := application.ParseErrorClass(class.Slug, class.String())
	if err != nil {
		t.Fatalf("the class does not read back: %v\n%s", err, class.String())
	}
	if back.String() != class.String() {
		t.Errorf("read back differs:\n%s\nwant:\n%s", back.String(), class.String())
	}
	if !strings.Contains(class.String(), "- 2026-10-14 lampas found:mw-5r3p30.360\n- 2026-10-15 legend ok\n") {
		t.Errorf("each check is one dated line, got:\n%s", class.String())
	}
}

func TestAClassFileThatIsNotWholeIsRefusedWithWhy(t *testing.T) {
	for name, text := range map[string]string{
		"no kinds":       "---\nsymptom: s\nsince: 2026-10-14\n---\n",
		"no symptom":     "---\nkinds: pwa\nsince: 2026-10-14\n---\n",
		"a bad verdict":  "---\nsymptom: s\nkinds: pwa\nshared: maybe\nsince: 2026-10-14\n---\n",
		"a bad check":    "---\nsymptom: s\nkinds: pwa\nsince: 2026-10-14\n---\n\n## Checked\n\n- 2026-10-14 lampas broken\n",
		"an unknown key": "---\nsymptom: s\nkinds: pwa\nsince: 2026-10-14\ncolour: red\n---\n",
	} {
		if _, err := application.ParseErrorClass("x", text); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}

func TestErrorsAddWritesTheClassAndRefusesADuplicateSlug(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	add := application.ErrorAdd{
		Slug: "gloss-from-a-neighbour", Symptom: "A gloss belongs to the next word",
		Kinds: []string{"word-gloss"}, Found: "lampas:mw-5r3p30.360",
	}
	if err := register.Add(context.Background(), add); err != nil {
		t.Fatal(err)
	}
	written, ok := files.Classes["gloss-from-a-neighbour"]
	if !ok {
		t.Fatalf("no class file written, got %v", files.Classes)
	}
	class, err := application.ParseErrorClass("gloss-from-a-neighbour", written)
	if err != nil {
		t.Fatal(err)
	}
	if class.Since != "2026-10-14" || len(class.Checks) != 1 || class.Checks[0] != (application.ErrorCheck{Date: "2026-10-14", Rig: "lampas", Found: "mw-5r3p30.360"}) {
		t.Errorf("the class should be dated today and hold the first find, got %+v", class)
	}
	if !strings.Contains(out.String(), "gloss-from-a-neighbour.md") {
		t.Errorf("add should print the file it wrote, got %q", out.String())
	}

	err = register.Add(context.Background(), add)
	if err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a duplicate slug should be refused, got %v", err)
	}
}

func TestErrorsAddRefusesWhatWouldNotMakeAWholeClass(t *testing.T) {
	register := errorsOn(&apptest.FakeErrorFiles{}, &bytes.Buffer{})
	for name, add := range map[string]application.ErrorAdd{
		"a bad slug":         {Slug: "Bad Slug", Symptom: "s", Kinds: []string{"pwa"}},
		"no kinds":           {Slug: "a-class", Symptom: "s"},
		"a placeholder kind": {Slug: "a-class", Symptom: "s", Kinds: []string{"<kinds>"}},
		"no symptom":         {Slug: "a-class", Kinds: []string{"pwa"}},
		"a two-line cause":   {Slug: "a-class", Symptom: "s", Cause: "one\ntwo", Kinds: []string{"pwa"}},
		"a bad find":         {Slug: "a-class", Symptom: "s", Kinds: []string{"pwa"}, Found: "lampas"},
	} {
		if err := register.Add(context.Background(), add); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}

func TestErrorsCheckedAppendsADatedLineAndAtTwoFindsAsksTheSharedComponentQuestion(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	ctx := context.Background()
	if err := register.Add(ctx, application.ErrorAdd{Slug: "distractors-miss-the-point", Symptom: "s",
		Kinds: []string{"quiz"}, Found: "lampas:mw-i7xe0n.7"}); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := register.Checked(ctx, "distractors-miss-the-point", "legend", "ok"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), application.SharedComponentQuestion) {
		t.Errorf("one find asks no question, got %q", out.String())
	}

	out.Reset()
	if err := register.Checked(ctx, "distractors-miss-the-point", "spell-forge", "found:mw-abc.1"); err != nil {
		t.Fatal(err)
	}
	said := out.String()
	if !strings.Contains(said, "seen 2 times") || !strings.Contains(said, application.SharedComponentQuestion) ||
		!strings.Contains(said, "mw-i7xe0n.7") || !strings.Contains(said, "mw-abc.1") {
		t.Errorf("the second find should say so and ask the question, got %q", said)
	}
	class, err := application.ParseErrorClass("distractors-miss-the-point", files.Classes["distractors-miss-the-point"])
	if err != nil {
		t.Fatal(err)
	}
	if len(class.Checks) != 3 || class.Checks[1].Rig != "legend" || class.Checks[1].Found != "" {
		t.Errorf("each check is kept, in order, got %+v", class.Checks)
	}
	if len(files.Writes) != 3 {
		t.Errorf("the register writes its files and nothing else is filed, got %d writes", len(files.Writes))
	}
}

func classKinds(t *testing.T, files *apptest.FakeErrorFiles, slug string) string {
	t.Helper()
	class, err := application.ParseErrorClass(slug, files.Classes[slug])
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(class.Kinds, ",")
}

func TestErrorsCheckedFoundInARigSharingAKindWithTheClassChangesNoKinds(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	ctx := context.Background()
	_ = register.Kinds(ctx, "lampas", []string{"pwa", "grind", "phonics"})
	_ = register.Add(ctx, application.ErrorAdd{Slug: "a-pwa-class", Symptom: "s", Kinds: []string{"pwa"}})
	out.Reset()
	if err := register.Checked(ctx, "a-pwa-class", "lampas", "found:mw-b.2"); err != nil {
		t.Fatal(err)
	}
	if got := classKinds(t, files, "a-pwa-class"); got != "pwa" {
		t.Errorf("a find in a rig sharing a kind adds nothing, got kinds %q", got)
	}
	if strings.Contains(out.String(), "added kinds") {
		t.Errorf("nothing added, nothing said, got %q", out.String())
	}
}

func TestErrorsCheckedFoundInARigSharingNoKindAddsOnlyTheRigsFirstKind(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	ctx := context.Background()
	_ = register.Kinds(ctx, "legend", []string{"pwa", "grind"})
	_ = register.Kinds(ctx, "millwright", []string{"go-cli"})
	_ = register.Add(ctx, application.ErrorAdd{Slug: "a-class", Symptom: "s", Kinds: []string{"go-cli"}, Found: "millwright:mw-a.1"})

	out.Reset()
	if err := register.Checked(ctx, "a-class", "legend", "found:mw-b.2"); err != nil {
		t.Fatal(err)
	}
	if got := classKinds(t, files, "a-class"); got != "go-cli,pwa" {
		t.Errorf("a find in legend should add its first kind, got kinds %q", got)
	}
	if !strings.Contains(out.String(), "added kinds to a-class: pwa (") {
		t.Errorf("the kinds added should be printed, got %q", out.String())
	}

	out.Reset()
	if err := register.Checked(ctx, "a-class", "legend", "found:mw-c.3"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "added kinds") || classKinds(t, files, "a-class") != "go-cli,pwa" {
		t.Errorf("kinds already listed add nothing, got %q, kinds %q", out.String(), classKinds(t, files, "a-class"))
	}
}

func TestErrorsCheckedOkChangesNoKinds(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	ctx := context.Background()
	_ = register.Kinds(ctx, "legend", []string{"pwa"})
	_ = register.Add(ctx, application.ErrorAdd{Slug: "a-class", Symptom: "s", Kinds: []string{"go-cli"}})
	out.Reset()
	if err := register.Checked(ctx, "a-class", "legend", "ok"); err != nil {
		t.Fatal(err)
	}
	if got := classKinds(t, files, "a-class"); got != "go-cli" {
		t.Errorf("an ok check changes no kinds, got %q", got)
	}
	if strings.Contains(out.String(), "added kinds") {
		t.Errorf("an ok check says nothing of kinds, got %q", out.String())
	}
}

func TestErrorsCheckedFoundInARigWithNoKindsChangesNoKinds(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	register := errorsOn(files, &bytes.Buffer{})
	ctx := context.Background()
	_ = register.Add(ctx, application.ErrorAdd{Slug: "a-class", Symptom: "s", Kinds: []string{"go-cli"}})
	if err := register.Checked(ctx, "a-class", "ghost-rig", "found:mw-b.2"); err != nil {
		t.Fatal(err)
	}
	if got := classKinds(t, files, "a-class"); got != "go-cli" {
		t.Errorf("a rig the kinds file does not name adds nothing, got %q", got)
	}
}

func TestErrorsCheckedRefusesAnUnknownClassOrResult(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	register := errorsOn(files, &bytes.Buffer{})
	ctx := context.Background()
	if err := register.Checked(ctx, "ghost", "lampas", "ok"); err == nil {
		t.Error("an unknown class should be refused")
	}
	_ = register.Add(ctx, application.ErrorAdd{Slug: "a-class", Symptom: "s", Kinds: []string{"pwa"}})
	for _, result := range []string{"fine", "found:", "found:not a bead"} {
		if err := register.Checked(ctx, "a-class", "lampas", result); err == nil {
			t.Errorf("the result %q should be refused", result)
		}
	}
}

func TestErrorsListAndShowNameTheQuestionUntilAVerdictIsGiven(t *testing.T) {
	files := &apptest.FakeErrorFiles{}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	ctx := context.Background()
	_ = register.Add(ctx, application.ErrorAdd{Slug: "twice", Symptom: "Seen twice", Kinds: []string{"pwa"}, Found: "lampas:mw-a.1"})
	_ = register.Checked(ctx, "twice", "spell-forge", "found:mw-b.2")
	_ = register.Add(ctx, application.ErrorAdd{Slug: "once", Symptom: "Seen once", Kinds: []string{"pwa"}, Found: "lampas:mw-a.3"})

	out.Reset()
	if err := register.List(ctx); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "once") || !strings.HasPrefix(lines[1], "twice") {
		t.Fatalf("one line a class, by slug, got:\n%s", out.String())
	}
	if strings.Contains(lines[0], application.SharedComponentQuestion) || !strings.Contains(lines[1], application.SharedComponentQuestion) {
		t.Errorf("only the class seen twice asks the question, got:\n%s", out.String())
	}

	out.Reset()
	if err := register.Show(ctx, "twice"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "symptom: Seen twice") || !strings.Contains(out.String(), application.SharedComponentQuestion) {
		t.Errorf("show prints the class and the question, got:\n%s", out.String())
	}

	if err := register.Shared(ctx, "twice", "candidate"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = register.List(ctx)
	if strings.Contains(out.String(), application.SharedComponentQuestion) || !strings.Contains(out.String(), "shared: candidate") {
		t.Errorf("a verdict answers the question, got:\n%s", out.String())
	}
	if err := register.Shared(ctx, "twice", "exists"); err == nil {
		t.Error("exists without where should be refused")
	}
	if err := register.Show(ctx, "ghost"); err == nil {
		t.Error("show of an unknown class should be refused")
	}
}

func TestUncheckedClassesNameTheRigsOfTheirKindsNotYetChecked(t *testing.T) {
	kinds := application.RigKinds{
		"lampas": {"pwa", "grind"}, "spell-forge": {"pwa"}, "millwright": {"go-cli"}, "legend": {"pwa"},
	}
	classes := []application.ErrorClass{
		{Slug: "a-pwa-class", Kinds: []string{"pwa"}, Checks: []application.ErrorCheck{{Rig: "lampas", Found: "mw-x.1"}, {Rig: "legend"}}},
		{Slug: "a-cli-class", Kinds: []string{"go-cli"}, Checks: []application.ErrorCheck{{Rig: "millwright"}}},
		{Slug: "a-grind-class", Kinds: []string{"grind"}},
	}
	got := application.UncheckedClasses(classes, kinds)
	if len(got) != 2 || got[0].Class.Slug != "a-grind-class" || strings.Join(got[0].Rigs, ",") != "lampas" ||
		got[1].Class.Slug != "a-pwa-class" || strings.Join(got[1].Rigs, ",") != "spell-forge" {
		t.Errorf("unchecked: %+v", got)
	}
}

func TestRigKindsReadBackAsWritten(t *testing.T) {
	kinds := application.RigKinds{"lampas": {"pwa", "grind"}, "millwright": {"go-cli"}}
	back, err := application.ParseRigKinds(kinds.String())
	if err != nil {
		t.Fatal(err)
	}
	if back.String() != kinds.String() {
		t.Errorf("read back differs:\n%s\nwant:\n%s", back.String(), kinds.String())
	}
	if _, err := application.ParseRigKinds("lampas = pwa\n"); err == nil {
		t.Error("a line that is not a list should be refused")
	}
}

func TestErrorsKindsSetsARigsKindsAndKeepsTheOthers(t *testing.T) {
	files := &apptest.FakeErrorFiles{Kinds: "millwright = [\"go-cli\"]\n"}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	if err := register.Kinds(context.Background(), "lampas", []string{"pwa", "grind"}); err != nil {
		t.Fatal(err)
	}
	kinds, err := application.ParseRigKinds(files.Kinds)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(kinds["lampas"], ",") != "pwa,grind" || strings.Join(kinds["millwright"], ",") != "go-cli" {
		t.Errorf("kinds: %v", kinds)
	}
	out.Reset()
	if err := register.Kinds(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "lampas: pwa, grind") || !strings.Contains(out.String(), "millwright: go-cli") {
		t.Errorf("no rig lists them all, got %q", out.String())
	}
}

func TestTheCommonErrorsSectionReadsSlugLinesNothingAndMalformedAndEndsAtTheRigMemory(t *testing.T) {
	comment := "Done.\n\nFor common errors:\n- gloss-from-a-neighbour: per-word glosses aligned by position\nnot a slug line\n\n" +
		"For the rig memory:\ngotcha [bd]: Point bd at the vault.\n"
	got := application.ParseCommonErrors(comment)
	if !got.Found || len(got.Lines) != 2 {
		t.Fatalf("two lines, got %+v", got)
	}
	if got.Lines[0].Slug != "gloss-from-a-neighbour" || got.Lines[0].Line != "per-word glosses aligned by position" {
		t.Errorf("first: %+v", got.Lines[0])
	}
	if got.Lines[1].Malformed != "not a slug line" {
		t.Errorf("second should be malformed, got %+v", got.Lines[1])
	}
	if nothing := application.ParseCommonErrors("For common errors: nothing\nFor the rig memory: nothing"); !nothing.Found ||
		len(nothing.Lines) != 1 || !nothing.Lines[0].Nothing {
		t.Errorf("nothing: %+v", nothing)
	}
	if application.ParseCommonErrors("For the rig memory: nothing").Found {
		t.Error("a comment without the section has none")
	}
}

func TestTheRigMemorySectionEndsWhereTheCommonErrorsSectionBegins(t *testing.T) {
	got := application.ParseFactProposals("For the rig memory:\ngotcha [bd]: Point bd at the vault.\n\nFor common errors:\nsome-class: a line\n")
	if len(got.Lines) != 1 || got.Lines[0].Malformed != "" {
		t.Errorf("the common errors lines are not rig memory proposals, got %+v", got.Lines)
	}
}

func TestTheCommonErrorsBlockChecksAKnownClassAndAddsANewOne(t *testing.T) {
	proposals := application.ParseCommonErrors("For common errors:\nknown-class: seen again\nnew-class: it's new\nbad line")
	block := proposals.Block("lampas", "mw-5r3p30.360", true, map[string]bool{"known-class": true}, []string{"pwa", "grind"})
	for _, want := range []string{
		"For common errors (lampas):",
		"mw errors checked known-class lampas found:mw-5r3p30.360",
		`mw errors add new-class --found lampas:mw-5r3p30.360 --kinds pwa,grind --symptom 'it'\''s new'`,
		"MALFORMED: bad line",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the block should hold %q, got:\n%s", want, block)
		}
	}
	unknownKinds := proposals.Block("lampas", "mw-5r3p30.360", true, nil, nil)
	if !strings.Contains(unknownKinds, "--kinds '<the kinds of rig it can occur in>'") {
		t.Errorf("a rig with no kinds leaves the kinds to the Mayor, got:\n%s", unknownKinds)
	}

	missing := application.ParseCommonErrors("For the rig memory: nothing")
	if got := missing.Block("lampas", "mw-x.1", true, nil, nil); !strings.Contains(got, "For common errors (lampas): not given") {
		t.Errorf("a bug's comment without the section is named, got %q", got)
	}
	if got := missing.Block("lampas", "mw-x.1", false, nil, nil); got != "" {
		t.Errorf("a story that is not a bug owes no section, got %q", got)
	}
}

func TestErrorsSeedWritesTheClassesNotThereAndKeepsTheRest(t *testing.T) {
	seed, err := fs.Sub(millwright.Template(), application.ErrorRegisterTemplateDir)
	if err != nil {
		t.Fatal(err)
	}
	files := &apptest.FakeErrorFiles{Classes: map[string]string{"example-word-wrong-sound": "kept as it was"}}
	var out bytes.Buffer
	register := errorsOn(files, &out)
	register.Seed = seed
	if err := register.SeedRegister(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files.Classes["example-word-wrong-sound"] != "kept as it was" {
		t.Error("a class already in the register is never overwritten")
	}
	for _, slug := range []string{"gloss-from-a-neighbour", "grind-ignores-a-readers-no"} {
		if _, ok := files.Classes[slug]; !ok {
			t.Errorf("the seed %s should be written, got %v", slug, files.Classes)
		}
	}
}

// TestTheTemplateSeedsTheFirstThreeLampasClasses: the first three classes, from
// the Lampas bugs of 2026-10-14, each whole and each with its kinds and its find.
func TestTheTemplateSeedsTheFirstThreeLampasClasses(t *testing.T) {
	seed, err := fs.Sub(millwright.Template(), application.ErrorRegisterTemplateDir)
	if err != nil {
		t.Fatal(err)
	}
	for slug, bead := range map[string]string{
		"example-word-wrong-sound":   "mw-5r3p30.351",
		"gloss-from-a-neighbour":     "mw-5r3p30.360",
		"grind-ignores-a-readers-no": "mw-5r3p30.365",
	} {
		text, err := fs.ReadFile(seed, slug+".md")
		if err != nil {
			t.Fatalf("no seed for %s: %v", slug, err)
		}
		class, err := application.ParseErrorClass(slug, string(text))
		if err != nil {
			t.Fatalf("the seed %s is not whole: %v", slug, err)
		}
		if len(class.Kinds) == 0 || class.Sweep == "" || len(class.Occurrences()) != 1 || class.Occurrences()[0].Found != bead {
			t.Errorf("the seed %s should have its kinds, its sweep and its find %s, got %+v", slug, bead, class)
		}
	}
}
