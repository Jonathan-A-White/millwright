package application

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// loginGrinder answers every grind with one hammer and keeps the
// API key each call was billed to.
type loginGrinder struct {
	mu   sync.Mutex
	keys []string
}

func (g *loginGrinder) Grind(_ context.Context, call GrindCall) (SessionResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.keys = append(g.keys, call.APIKey)
	return SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[{"name":"Hammer"}]}`), StopReason: "end_turn"}, nil
}

// TestGristEvalNamesItsLogin: an eval's result was read as the live mill's
// when it ground on another login (mw-gq6.1711), so the report, the tables
// file and each row say which login ground it, API key or subscription, and
// the key given is the one each grind is billed to.
func TestGristEvalNamesItsLogin(t *testing.T) {
	for _, tc := range []struct {
		key, login, says string
	}{
		{"", BillingSubscription, "the subscription login"},
		{"sk-ant-test", BillingAPI, "the API key"},
	} {
		t.Run(tc.login, func(t *testing.T) {
			grind := aGrindTree(t, "grinds/sweep.md", map[string]string{"grinds/sweep.md": "List what you see.\n"})
			photos := t.TempDir()
			for name, text := range map[string]string{"shelf.jpg": "jpeg", "shelf.txt": "Hammer\n"} {
				if err := os.WriteFile(filepath.Join(photos, name), []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			grinder := &loginGrinder{}
			var out bytes.Buffer
			report, err := GristEval{
				Grinder:  grinder,
				Ceilings: GristCeilings{Models: []string{"sonnet"}},
				APIKey:   tc.key,
				TempDir:  t.TempDir(),
				Out:      &out,
			}.Run(context.Background(), GristEvalRequest{Grind: grind, Photos: photos, Out: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if len(grinder.keys) != 1 || grinder.keys[0] != tc.key {
				t.Fatalf("expected one grind billed to %q, got %q", tc.key, grinder.keys)
			}
			if len(report.Rows) != 1 || report.Rows[0].Login != tc.login {
				t.Fatalf("expected one row ground on %q, got %+v", tc.login, report.Rows)
			}
			want := "login: ground on " + tc.says
			if !strings.Contains(out.String(), want) {
				t.Errorf("expected the printed report to say %q, got:\n%s", want, out.String())
			}
			md, err := os.ReadFile(report.Markdown)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(md), want) {
				t.Errorf("expected the tables file to say %q, got:\n%s", want, md)
			}
			rows, err := os.ReadFile(report.JSONL)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rows), `"login":"`+tc.login+`"`) {
				t.Errorf("expected each row to carry its login %q, got:\n%s", tc.login, rows)
			}
		})
	}
}
