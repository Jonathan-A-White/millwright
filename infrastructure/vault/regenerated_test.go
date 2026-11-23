package vault_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// mw-gq6.1666: a rig's regenerated_files is a list of tables, each the paths of
// generated files and the command that writes them again.
func TestARigsFileListsItsGeneratedFilesAndTheirGenerators(t *testing.T) {
	want := []domain.RegeneratedFiles{
		{Paths: []string{"docs/lxx-align/gen.txt"}, Run: "npm run lxx:align-expect -- --book gen"},
		{Paths: []string{"docs/a.md", "docs/b/*.md"}, Run: "make docs # not a comment"},
	}
	for name, file := range map[string]string{
		"tables": `union_merge_files = ["ATTRIBUTION.md"]

[[regenerated_files]]
paths = ["docs/lxx-align/gen.txt"]  # one row a chapter
run   = "npm run lxx:align-expect -- --book gen"

[[regenerated_files]]
paths = [
  "docs/a.md",
  "docs/b/*.md",
]
run = "make docs # not a comment"
`,
		"inline": `regenerated_files = [{ paths = ["docs/lxx-align/gen.txt"], run = "npm run lxx:align-expect -- --book gen" },
  { paths = ["docs/a.md", "docs/b/*.md"], run = "make docs # not a comment" }]
`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := aVault(t)
			v := vault.New(dir)
			writeRigFile(t, dir, "lampas", file)

			got, err := v.EpicRequirements(context.Background(), "lampas")
			if err != nil {
				t.Fatalf("reading the rig's file: %v", err)
			}
			if !reflect.DeepEqual(got.RegeneratedFiles, want) {
				t.Errorf("regenerated files = %+v, want %+v", got.RegeneratedFiles, want)
			}
			if got.Any() {
				t.Errorf("regenerated_files is no requirement of an epic, but Any() = true")
			}
			if rules := got.MergeRules(); !reflect.DeepEqual(rules.Regenerated, want) {
				t.Errorf("merge rules = %+v", rules)
			}
			landing, unknown, err := v.LandingRequirements(context.Background(), "lampas")
			if err != nil || len(unknown) != 0 || !reflect.DeepEqual(landing.RegeneratedFiles, want) {
				t.Errorf("a landing read %+v, unknown %v, %v", landing.RegeneratedFiles, unknown, err)
			}
		})
	}
}

func TestARigsFileRefusesAGeneratedFilesEntryThatIsNoEntry(t *testing.T) {
	for name, file := range map[string]string{
		"no run":              "[[regenerated_files]]\npaths = [\"a.txt\"]\n",
		"no paths":            "[[regenerated_files]]\nrun = \"make\"\n",
		"empty paths":         "[[regenerated_files]]\npaths = []\nrun = \"make\"\n",
		"a path outside":      "[[regenerated_files]]\npaths = [\"../a.txt\"]\nrun = \"make\"\n",
		"an absolute path":    "[[regenerated_files]]\npaths = [\"/etc/passwd\"]\nrun = \"make\"\n",
		"a bad glob":          "[[regenerated_files]]\npaths = [\"docs/[.md\"]\nrun = \"make\"\n",
		"an unquoted run":     "[[regenerated_files]]\npaths = [\"a.txt\"]\nrun = make\n",
		"another key in it":   "[[regenerated_files]]\npaths = [\"a.txt\"]\nrun = \"make\"\nguest = \"Luke\"\n",
		"inline, no run":      "regenerated_files = [{ paths = [\"a.txt\"] }]\n",
		"inline, not a table": "regenerated_files = [\"a.txt\"]\n",
		"inline, never ends":  "regenerated_files = [{ paths = [\"a.txt\"], run = \"make\" }\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := aVault(t)
			v := vault.New(dir)
			writeRigFile(t, dir, "lampas", file)
			if _, err := v.EpicRequirements(context.Background(), "lampas"); err == nil {
				t.Errorf("read %q without complaint", file)
			}
			if _, _, err := v.LandingRequirements(context.Background(), "lampas"); err == nil {
				t.Errorf("a landing read %q without complaint", file)
			}
		})
	}
	// The refusal names the key.
	dir := aVault(t)
	writeRigFile(t, dir, "lampas", "[[regenerated_files]]\npaths = [\"a.txt\"]\n")
	if _, err := vault.New(dir).EpicRequirements(context.Background(), "lampas"); err == nil || !strings.Contains(err.Error(), "regenerated_files") {
		t.Errorf("err = %v, want it to name regenerated_files", err)
	}
}
