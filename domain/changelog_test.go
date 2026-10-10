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

const postern0511 = "# Changelog\n\nWhat changed in each release of Postern.\n\n## 0.5.11\n_2026-10-08_\n- Fixed: Messages sort newest first\n"

func TestAnEntryGoesAboveTheFirstVersionKeepingARigsOwnTitleAndIntro(t *testing.T) {
	entry := ChangelogEntry{Version: "0.5.12", Date: "2026-10-10", Story: "pn-1", Kind: ChangelogNew, Text: "Replies thread"}
	got := string(AddToChangelogMarkdown([]byte(postern0511), entry))
	want := "# Changelog\n\nWhat changed in each release of Postern.\n\n" +
		"## 0.5.12\n_2026-10-10_\n- New: Replies thread\n\n" +
		"## 0.5.11\n_2026-10-08_\n- Fixed: Messages sort newest first\n"
	if got != want {
		t.Errorf("changelog = %q, want %q", got, want)
	}
	if strings.Contains(got, ChangelogTitle) {
		t.Errorf("a rig's own title is not joined by ours: %q", got)
	}
}

func TestAnEntryGoesAfterTheIntroOfAChangelogWithNoVersionYet(t *testing.T) {
	entry := ChangelogEntry{Version: "0.1.0", Date: "2026-10-10", Story: "pn-1", Kind: ChangelogNew, Text: "First"}
	got := string(AddToChangelogMarkdown([]byte("# Changelog\n\nAll notable changes.\n"), entry))
	want := "# Changelog\n\nAll notable changes.\n\n## 0.1.0\n_2026-10-10_\n- New: First\n"
	if got != want {
		t.Errorf("changelog = %q, want %q", got, want)
	}
	got = string(AddToChangelogMarkdown([]byte("# Changelog"), entry))
	want = "# Changelog\n\n## 0.1.0\n_2026-10-10_\n- New: First\n"
	if got != want {
		t.Errorf("changelog = %q, want %q", got, want)
	}
}

func TestAnEntryTheNewestOneAlreadySaysReplacesItInBothChangelogs(t *testing.T) {
	entry := ChangelogEntry{Version: "0.1.1", Date: "2026-09-18", Story: "mw-a.1", Kind: ChangelogNew, Text: "Verses repeat"}

	seeded := []byte(`[{"version":"0.1.0","date":"2026-09-17","story":"mw-a.1","kind":"new","text":"Verses repeat"},{"version":"0.0.9","date":"2026-09-01","story":"mw-z.1","kind":"new","text":"Older"}]`)
	got, err := AddToChangelogJSON(seeded, entry)
	if err != nil || strings.Count(string(got), `"version"`) != 2 || strings.Contains(string(got), "0.1.0") {
		t.Errorf("AddToChangelogJSON over a seeded entry = %s, %v; want it renamed to 0.1.1 with Older kept", got, err)
	}
	other := []byte(`[{"version":"0.1.0","date":"2026-09-17","story":"mw-z.1","kind":"new","text":"Something else"}]`)
	if got, _ := AddToChangelogJSON(other, entry); strings.Count(string(got), `"version"`) != 2 {
		t.Errorf("AddToChangelogJSON over another story's entry = %s; want both kept", got)
	}

	for name, existing := range map[string]string{
		"title then block": "# What's new\n\n## 0.1.0\n_2026-09-17_\n- New: Verses repeat\n",
		"no title":         "## 0.1.0\n_2026-09-17_\n- New: Verses repeat\n",
	} {
		md := string(AddToChangelogMarkdown([]byte(existing), entry))
		if strings.Count(md, "## ") != 1 || !strings.Contains(md, "## 0.1.1\n") || strings.Contains(md, "0.1.0") {
			t.Errorf("%s: AddToChangelogMarkdown over a seeded block = %q; want one block at 0.1.1", name, md)
		}
	}
	older := "# What's new\n\n## 0.1.0\n_2026-09-17_\n- New: Verses repeat\n\n## 0.0.9\n_2026-09-01_\n- New: Older\n"
	if md := string(AddToChangelogMarkdown([]byte(older), entry)); strings.Count(md, "## ") != 2 || !strings.Contains(md, "- New: Older") {
		t.Errorf("AddToChangelogMarkdown kept or lost the wrong blocks: %q", md)
	}
}
