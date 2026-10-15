package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aGrindTree writes a rig with a grind file naming instructions, an answer
// schema, and the given files (relative to the rig), and gives the path of
// the grind file.
func aGrindTree(t *testing.T, instructions string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	grind := `{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],"model":"sonnet","effort":"low",` +
		`"instructions":"` + instructions + `","answerSchema":"schema.json","attachments":{"min":0,"max":4}}`
	files["grinds/sweep.json"] = grind
	files["schema.json"] = `{"type":"object"}`
	for rel, text := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "grinds", "sweep.json")
}

func TestReadGrindTreeJoinsADirectorysPartsInNameOrder(t *testing.T) {
	path := aGrindTree(t, "grinds/sweep.instructions", map[string]string{
		"grinds/sweep.instructions/20-second.md": "Second.\n",
		"grinds/sweep.instructions/10-first.md":  "First.\n",
		"grinds/sweep.instructions/30-third.md":  "Third.\n",
		"grinds/sweep.instructions/notes.txt":    "not a part",
	})
	_, instructions, _, err := readGrindTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "First.\n\nSecond.\n\nThird.\n"; instructions != want {
		t.Fatalf("instructions = %q, want %q", instructions, want)
	}
}

func TestReadGrindTreeReadsOneFileAsItAlwaysDid(t *testing.T) {
	path := aGrindTree(t, "grinds/sweep.md", map[string]string{"grinds/sweep.md": "List what you see.\n"})
	_, instructions, _, err := readGrindTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "List what you see.\n"; instructions != want {
		t.Fatalf("instructions = %q, want %q", instructions, want)
	}
}

func TestReadGrindTreeRefusesADirectoryWithNoParts(t *testing.T) {
	empty := aGrindTree(t, "grinds/sweep.instructions", map[string]string{})
	if err := os.MkdirAll(filepath.Join(filepath.Dir(empty), "sweep.instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	noMarkdown := aGrindTree(t, "grinds/sweep.instructions", map[string]string{"grinds/sweep.instructions/notes.txt": "x"})
	for name, path := range map[string]string{"an empty directory": empty, "a directory with no .md": noMarkdown} {
		_, _, _, err := readGrindTree(path)
		if err == nil || !strings.Contains(err.Error(), "empty instructions") {
			t.Errorf("%s: expected a refusal for empty instructions, got %v", name, err)
		}
	}
}
