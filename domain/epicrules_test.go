package domain

import (
	"strings"
	"testing"
)

func shortNames(short []Shortfall) string {
	var names []string
	for _, s := range short {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

func TestHasSectionReadsHeadingsAndColonLines(t *testing.T) {
	for _, tc := range []struct {
		description string
		want        bool
	}{
		{"## Demo\nshow it", true},
		{"# demo:\nshow it", true},
		{"Intro\n  Demo: a walkthrough", true},
		{"demo: lower case counts", true},
		{"We will demo it later", false},
		{"Demonstration: not it", false},
		{"## Demonstration\nx", false},
		{"", false},
	} {
		if got := HasSection(tc.description, "Demo"); got != tc.want {
			t.Errorf("HasSection(%q) = %v, want %v", tc.description, got, tc.want)
		}
	}
}

func TestCheckLastStoryNeedsEverySibling(t *testing.T) {
	rules := EpicRequirements{LastStoryLabels: []string{"demo"}}
	chain := []StoryShape{
		{Key: "a"},
		{Key: "b", Needs: []string{"a"}},
		{Key: "d", Labels: []string{"demo"}, Needs: []string{"b"}},
	}
	if short := rules.Check(EpicShape{Stories: chain}); len(short) != 0 {
		t.Errorf("a demo story waiting on b, which waits on a, is last: got %v", short)
	}

	loose := []StoryShape{
		{Key: "a"},
		{Key: "b"},
		{Key: "d", Labels: []string{"demo"}, Needs: []string{"a"}},
	}
	short := rules.Check(EpicShape{Stories: loose})
	if len(short) != 1 || !strings.Contains(short[0].Why, "does not wait on b") {
		t.Errorf("a demo story that skips b is not last: got %v", short)
	}

	none := rules.Check(EpicShape{Stories: []StoryShape{{Key: "a"}}})
	if len(none) != 1 || !strings.Contains(none[0].Why, "no story of the epic carries") {
		t.Errorf("no labelled story: got %v", none)
	}

	// A need outside the epic is no sibling, and the epic's only story is trivially last.
	alone := []StoryShape{{Key: "d", Labels: []string{"Demo"}, Needs: []string{"elsewhere"}}}
	if short := rules.Check(EpicShape{Stories: alone}); len(short) != 0 {
		t.Errorf("got %v", short)
	}
}

func TestCheckListsEveryMissingNameInTheRigsOrder(t *testing.T) {
	rules := EpicRequirements{Sections: []string{"Demo", "Rollback"}, LastStoryLabels: []string{"demo"}}
	short := rules.Check(EpicShape{Description: "Rollback: none", Stories: []StoryShape{{Key: "a"}}})
	if got := shortNames(short); got != "Demo,demo" {
		t.Errorf("missing = %q, want Demo,demo", got)
	}
}

func TestWaiverLabelsTellTheSectionFromTheLabelOfTheSameName(t *testing.T) {
	section, story := WaiverLabel(true, "Release Notes"), WaiverLabel(false, "demo")
	if section != "epic-waiver:section:release-notes" || story != "epic-waiver:story:demo" {
		t.Errorf("labels = %q, %q", section, story)
	}
	labels := []string{"x", WaiverLabel(true, "Demo")}
	if !Waives(labels, Shortfall{Name: "Demo", Section: true}) || Waives(labels, Shortfall{Name: "demo"}) {
		t.Error("waiving the Demo section must not waive the demo story")
	}
}

func TestMatchIsExactSoAWaiverNamesOneRequirement(t *testing.T) {
	rules := EpicRequirements{Sections: []string{"Demo"}, LastStoryLabels: []string{"demo"}}
	if s, l := rules.Match("Demo"); !s || l {
		t.Errorf("Demo = %v, %v", s, l)
	}
	if s, l := rules.Match("demo"); s || !l {
		t.Errorf("demo = %v, %v", s, l)
	}
	if s, l := rules.Match("DEMO"); s || l {
		t.Errorf("DEMO = %v, %v", s, l)
	}
}
