package domain

import (
	"strings"
	"testing"
)

func TestTheWhatsNewLineOfAClosingCommentIsReadByItsForms(t *testing.T) {
	for _, tc := range []struct {
		comment string
		want    WhatsNew
	}{
		{"Done.\nWhat's new: New: Pick a verse to hear it\nFor the rig memory: nothing", WhatsNew{Kind: ChangelogNew, Text: "Pick a verse to hear it"}},
		{"What's new: Fixed: Listen stops at the verse's end", WhatsNew{Kind: ChangelogFixed, Text: "Listen stops at the verse's end"}},
		{"What’s new: fixed: Curly apostrophe", WhatsNew{Kind: ChangelogFixed, Text: "Curly apostrophe"}},
		{"**What's new:** New: Bold marker", WhatsNew{Kind: ChangelogNew, Text: "Bold marker"}},
		{"What's new: none", WhatsNew{None: true}},
		{"What's new: None.", WhatsNew{None: true}},
		{"What's new: Plain words with no kind", WhatsNew{Text: "Plain words with no kind"}},
	} {
		got, found := ParseWhatsNew(tc.comment)
		if !found || got != tc.want {
			t.Errorf("ParseWhatsNew(%q) = %+v, %v; want %+v", tc.comment, got, found, tc.want)
		}
	}
	for _, none := range []string{"", "Done and green.", "What's new:", "What's new:   "} {
		if got, found := ParseWhatsNew(none); found {
			t.Errorf("ParseWhatsNew(%q) = %+v, true; want it not found", none, got)
		}
	}
}

func TestTheNewestCommentWithAWhatsNewLineIsTheOneUsed(t *testing.T) {
	got, found := NewestWhatsNew([]string{"What's new: New: old", "What's new: Fixed: newer", "Just a note"})
	if !found || got.Kind != ChangelogFixed || got.Text != "newer" {
		t.Errorf("NewestWhatsNew = %+v, %v", got, found)
	}
	if _, found := NewestWhatsNew([]string{"nothing", "here"}); found {
		t.Errorf("expected none found")
	}
}

func TestATitleBecomesANoteWithoutItsTags(t *testing.T) {
	for title, want := range map[string]string{
		"[bug] Listen stops early":              "Listen stops early",
		"[millwright] Verses scroll to the top": "Verses scroll to the top",
		"[bug] [millwright]  Two   tags":        "Two tags",
		"millwright: Verses scroll to the top":  "Verses scroll to the top",
		"Plain title":                           "Plain title",
		"[BUG] Upper case tag":                  "Upper case tag",
	} {
		if got := NoteFromTitle(title, "millwright"); got != want {
			t.Errorf("NoteFromTitle(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestAnEntryGoesAtTheTopOfTheJSONChangelogAndCreatesIt(t *testing.T) {
	first := ChangelogEntry{Version: "0.1.2", Date: "2026-09-01", Story: "mw-a.1", Kind: ChangelogNew, Text: "Verses repeat"}
	made, err := AddToChangelogJSON(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n  {\n    \"version\": \"0.1.2\",\n    \"date\": \"2026-09-01\",\n    \"story\": \"mw-a.1\",\n    \"kind\": \"new\",\n    \"text\": \"Verses repeat\"\n  }\n]\n"
	if string(made) != want {
		t.Errorf("created changelog = %q, want %q", made, want)
	}

	second := ChangelogEntry{Version: "0.1.3", Date: "2026-09-02", Story: "mw-a.2", Kind: ChangelogFixed, Text: "It's <fixed> & done"}
	both, err := AddToChangelogJSON(made, second)
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(string(both), "0.1.3"), strings.Index(string(both), "0.1.2"); i < 0 || j < 0 || i > j {
		t.Errorf("expected 0.1.3 above 0.1.2, got %s", both)
	}
	if !strings.Contains(string(both), `"It's <fixed> & done"`) {
		t.Errorf("expected the text unescaped, got %s", both)
	}
	if _, err := AddToChangelogJSON([]byte("{not json"), second); err == nil {
		t.Errorf("expected a refusal of a changelog that is not an array")
	}
	if blank, err := AddToChangelogJSON([]byte("\n"), second); err != nil || !strings.Contains(string(blank), "0.1.3") {
		t.Errorf("an empty file is created over: %s, %v", blank, err)
	}
}

func TestAnEntryGoesUnderTheTitleOfTheMarkdownChangelogAndCreatesIt(t *testing.T) {
	first := ChangelogEntry{Version: "0.1.2", Date: "2026-09-01", Story: "mw-a.1", Kind: ChangelogNew, Text: "Verses repeat"}
	made := AddToChangelogMarkdown(nil, first)
	if want := "# What's new\n\n## 0.1.2\n_2026-09-01_\n- New: Verses repeat\n"; string(made) != want {
		t.Errorf("created changelog = %q, want %q", made, want)
	}
	second := ChangelogEntry{Version: "0.1.3", Date: "2026-09-02", Story: "mw-a.2", Kind: ChangelogFixed, Text: "Listen stops"}
	both := AddToChangelogMarkdown(made, second)
	if want := "# What's new\n\n## 0.1.3\n_2026-09-02_\n- Fixed: Listen stops\n\n## 0.1.2\n_2026-09-01_\n- New: Verses repeat\n"; string(both) != want {
		t.Errorf("changelog = %q, want %q", both, want)
	}
	// A file somebody wrote without the title keeps what it holds, below the entry.
	kept := string(AddToChangelogMarkdown([]byte("older notes\n"), second))
	if want := "# What's new\n\n## 0.1.3\n_2026-09-02_\n- Fixed: Listen stops\n\nolder notes\n"; kept != want {
		t.Errorf("changelog = %q, want %q", kept, want)
	}
}
