package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestTheLandRushPageNamesTheSevenKeysTheStopRuleAndTheRollback(t *testing.T) {
	raw, err := os.ReadFile("land-rush.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	for _, want := range []string{
		"landing", "batch_every", "batch_min", "batch_max",
		"batch_fixer_model", "batch_fixer_effort", "batch_fixer_turns",
		`landing = "each"`, "## Rollback", "## The stop rule",
		"runs/batch/", "metrics/factory.jsonl", "mw land",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the page does not name %q", want)
		}
	}
}

func TestTheReadmeLinksTheLandRushPage(t *testing.T) {
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "docs/land-rush.md") {
		t.Error("README.md does not link docs/land-rush.md")
	}
}

// The page answers, for every way a batch is held, who is held and what frees
// them (mw-qqxlwm3.3): one row a case, three cells a row.
func TestTheLandRushPageTabulatesWhenABatchIsHeldWithWhoIsHeldAndWhatFreesThem(t *testing.T) {
	raw, err := os.ReadFile("land-rush.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	if !strings.Contains(doc, "- [When a batch is held](#when-a-batch-is-held)") {
		t.Error("the table of contents does not list 'When a batch is held'")
	}
	_, section, found := strings.Cut(doc, "\n### When a batch is held\n")
	if !found {
		t.Fatal("the page has no section '### When a batch is held'")
	}
	if next := strings.Index(section, "\n#"); next >= 0 {
		section = section[:next]
	}
	var rows [][]string
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	if len(rows) < 3 {
		t.Fatalf("the table has %d lines, want its header, its rule and a row a case", len(rows))
	}
	head := rows[0]
	if len(head) != 3 || head[0] != "Case" || head[1] != "Who is held" || head[2] != "What frees it" {
		t.Errorf("the table's header is %q, want Case | Who is held | What frees it", head)
	}
	cases := rows[2:]
	for _, row := range cases {
		if len(row) != 3 {
			t.Errorf("the row %q has %d cells, want 3", row, len(row))
			continue
		}
		for i, cell := range row {
			if cell == "" {
				t.Errorf("the row %q has an empty cell %d", row, i+1)
			}
		}
	}
	// The cases the sweep found, each named in a row.
	for _, want := range []string{
		"set aside", "red", "fixer", "main is red", "could not be run",
		"changelog", "push", "--without", "held red alone",
	} {
		found := false
		for _, row := range cases {
			if strings.Contains(strings.ToLower(row[0]), strings.ToLower(want)) {
				found = true
			}
		}
		if !found {
			t.Errorf("no row of the table has a case naming %q", want)
		}
	}
}
